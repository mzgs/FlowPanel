package events

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbutil "flowpanel/internal/db"
)

type Store struct {
	db *sql.DB
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

	const statement = `
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    actor TEXT NOT NULL,
    category TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    resource_label TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_created_at
ON events (created_at DESC, id DESC);
`

	return dbutil.ExecStatements(ctx, s.db, dbutil.Statement{
		SQL:          statement,
		ErrorContext: "ensure events table",
	})
}

func (s *Store) Insert(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO events (
    id,
    actor,
    category,
    action,
    resource_type,
    resource_id,
    resource_label,
    status,
    message,
    created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		record.ID,
		record.Actor,
		record.Category,
		record.Action,
		record.ResourceType,
		record.ResourceID,
		record.ResourceLabel,
		record.Status,
		record.Message,
		record.CreatedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("insert event %q: %w", record.ID, err)
	}

	return nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Record, error) {
	if s == nil || s.db == nil {
		return []Record{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, actor, category, action, resource_type, resource_id, resource_label, status, message, created_at
FROM events
ORDER BY created_at DESC, id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0, limit)
	for rows.Next() {
		var (
			record        Record
			createdAtUnix int64
		)

		if err := rows.Scan(
			&record.ID,
			&record.Actor,
			&record.Category,
			&record.Action,
			&record.ResourceType,
			&record.ResourceID,
			&record.ResourceLabel,
			&record.Status,
			&record.Message,
			&createdAtUnix,
		); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}

		record.CreatedAt = time.Unix(0, createdAtUnix).UTC()
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}

	return records, nil
}
