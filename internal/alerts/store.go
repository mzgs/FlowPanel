package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	dbutil "flowpanel/internal/db"
)

type Store struct {
	db *sql.DB
}

type State struct {
	Key             string
	Severity        string
	Title           string
	Message         string
	Status          string
	FirstSeenAt     time.Time
	LastSeenAt      time.Time
	LastNotifiedAt  time.Time
	OccurrenceCount int
}

type Delivery struct {
	ID            string
	AlertKey      string
	Channel       string
	Payload       []byte
	Attempts      int
	NextAttemptAt time.Time
}

func NewStore(db *sql.DB) *Store {
	if db == nil {
		return nil
	}
	return &Store{db: db}
}

func (s *Store) Ensure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	return dbutil.ExecStatements(ctx, s.db,
		dbutil.Statement{SQL: `
CREATE TABLE IF NOT EXISTS alert_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    config_json TEXT NOT NULL
);`, ErrorContext: "ensure alert settings table"},
		dbutil.Statement{SQL: `
CREATE TABLE IF NOT EXISTS alert_states (
    alert_key TEXT PRIMARY KEY,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    message TEXT NOT NULL,
    status TEXT NOT NULL,
    first_seen_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    last_notified_at INTEGER NOT NULL DEFAULT 0,
    occurrence_count INTEGER NOT NULL DEFAULT 1
);`, ErrorContext: "ensure alert states table"},
		dbutil.Statement{SQL: `
CREATE TABLE IF NOT EXISTS notification_deliveries (
    id TEXT PRIMARY KEY,
    alert_key TEXT NOT NULL,
    channel TEXT NOT NULL,
    payload BLOB NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    sent_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_notification_deliveries_due
ON notification_deliveries (status, next_attempt_at);`, ErrorContext: "ensure notification deliveries table"},
	)
}

func (s *Store) GetConfig(ctx context.Context) (Config, error) {
	if s == nil || s.db == nil {
		return DefaultConfig(), nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT config_json FROM alert_settings WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("load alert settings: %w", err)
	}
	config := DefaultConfig()
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return Config{}, fmt.Errorf("decode alert settings: %w", err)
	}
	return normalizeConfig(config), nil
}

func (s *Store) SaveConfig(ctx context.Context, config Config) error {
	if s == nil || s.db == nil {
		return nil
	}
	raw, err := json.Marshal(normalizeConfig(config))
	if err != nil {
		return fmt.Errorf("encode alert settings: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO alert_settings (id, config_json) VALUES (1, ?)
ON CONFLICT(id) DO UPDATE SET config_json = excluded.config_json`, string(raw))
	if err != nil {
		return fmt.Errorf("save alert settings: %w", err)
	}
	return nil
}

func (s *Store) GetState(ctx context.Context, key string) (State, bool, error) {
	if s == nil || s.db == nil {
		return State{}, false, nil
	}
	var firstSeen, lastSeen, lastNotified int64
	var state State
	err := s.db.QueryRowContext(ctx, `
SELECT alert_key, severity, title, message, status, first_seen_at, last_seen_at, last_notified_at, occurrence_count
FROM alert_states WHERE alert_key = ?`, key).Scan(
		&state.Key, &state.Severity, &state.Title, &state.Message, &state.Status,
		&firstSeen, &lastSeen, &lastNotified, &state.OccurrenceCount,
	)
	if err == sql.ErrNoRows {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("load alert state %q: %w", key, err)
	}
	state.FirstSeenAt = time.Unix(0, firstSeen).UTC()
	state.LastSeenAt = time.Unix(0, lastSeen).UTC()
	if lastNotified > 0 {
		state.LastNotifiedAt = time.Unix(0, lastNotified).UTC()
	}
	return state, true, nil
}

func (s *Store) SaveState(ctx context.Context, state State) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO alert_states (alert_key, severity, title, message, status, first_seen_at, last_seen_at, last_notified_at, occurrence_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(alert_key) DO UPDATE SET
    severity = excluded.severity,
    title = excluded.title,
    message = excluded.message,
    status = excluded.status,
    first_seen_at = excluded.first_seen_at,
    last_seen_at = excluded.last_seen_at,
    last_notified_at = excluded.last_notified_at,
    occurrence_count = excluded.occurrence_count`,
		state.Key, state.Severity, state.Title, state.Message, state.Status,
		state.FirstSeenAt.UTC().UnixNano(), state.LastSeenAt.UTC().UnixNano(), unixNanoOrZero(state.LastNotifiedAt), state.OccurrenceCount,
	)
	if err != nil {
		return fmt.Errorf("save alert state %q: %w", state.Key, err)
	}
	return nil
}

func (s *Store) Enqueue(ctx context.Context, delivery Delivery) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO notification_deliveries (id, alert_key, channel, payload, status, attempts, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, 'pending', 0, ?, ?)`, delivery.ID, delivery.AlertKey, delivery.Channel, delivery.Payload, now.UnixNano(), now.UnixNano())
	if err != nil {
		return fmt.Errorf("enqueue %s alert delivery: %w", delivery.Channel, err)
	}
	return nil
}

func (s *Store) Due(ctx context.Context, limit int) ([]Delivery, error) {
	if s == nil || s.db == nil {
		return []Delivery{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, alert_key, channel, payload, attempts, next_attempt_at
FROM notification_deliveries
WHERE status = 'pending' AND next_attempt_at <= ?
ORDER BY next_attempt_at ASC LIMIT ?`, time.Now().UTC().UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("list due alert deliveries: %w", err)
	}
	defer rows.Close()
	deliveries := make([]Delivery, 0, limit)
	for rows.Next() {
		var delivery Delivery
		var nextAttempt int64
		if err := rows.Scan(&delivery.ID, &delivery.AlertKey, &delivery.Channel, &delivery.Payload, &delivery.Attempts, &nextAttempt); err != nil {
			return nil, fmt.Errorf("scan alert delivery: %w", err)
		}
		delivery.NextAttemptAt = time.Unix(0, nextAttempt).UTC()
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (s *Store) MarkSent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries SET status = 'sent', sent_at = ?, last_error = '' WHERE id = ?`, time.Now().UTC().UnixNano(), id)
	if err != nil {
		return fmt.Errorf("mark alert delivery sent: %w", err)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, delivery Delivery, next time.Time, message string, final bool) error {
	status := "pending"
	if final {
		status = "failed"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE notification_deliveries SET status = ?, attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		status, delivery.Attempts+1, next.UTC().UnixNano(), message, delivery.ID)
	if err != nil {
		return fmt.Errorf("update failed alert delivery: %w", err)
	}
	return nil
}

func (s *Store) PruneDeliveries(ctx context.Context, cutoff time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_deliveries WHERE status != 'pending' AND created_at < ?`, cutoff.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("prune alert deliveries: %w", err)
	}
	return nil
}

func unixNanoOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}
