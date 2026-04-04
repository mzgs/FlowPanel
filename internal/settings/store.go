package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	settingsTableName        = "settings"
	legacyPanelSettingsTable = "panel_settings"
	panelSettingsKeyPrefix   = "panel."
	panelNameKey             = panelSettingsKeyPrefix + "panel_name"
	gitHubTokenKey           = panelSettingsKeyPrefix + "github_token"
)

var panelSettingKeys = []string{
	panelNameKey,
	gitHubTokenKey,
}

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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ensure settings transaction: %w", err)
	}

	if err := ensureKeyValueTable(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := migrateLegacyPanelSettings(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ensure settings transaction: %w", err)
	}

	return nil
}

func (s *Store) Get(ctx context.Context) (Record, error) {
	if s == nil || s.db == nil {
		return defaultRecord(), nil
	}

	query := fmt.Sprintf(`
SELECT key, value
FROM %s
WHERE key IN (?, ?)
`, settingsTableName)

	rows, err := s.db.QueryContext(ctx, query, panelNameKey, gitHubTokenKey)
	if err != nil {
		return Record{}, fmt.Errorf("get settings: %w", err)
	}
	defer rows.Close()

	record := defaultRecord()
	found := false
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return Record{}, fmt.Errorf("scan settings row: %w", err)
		}

		found = true
		switch key {
		case panelNameKey:
			if strings.TrimSpace(value) != "" {
				record.PanelName = strings.TrimSpace(value)
			}
		case gitHubTokenKey:
			record.GitHubToken = strings.TrimSpace(value)
		}
	}

	if err := rows.Err(); err != nil {
		return Record{}, fmt.Errorf("iterate settings rows: %w", err)
	}
	if !found {
		return Record{}, sql.ErrNoRows
	}

	return record, nil
}

func (s *Store) Upsert(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert settings transaction: %w", err)
	}

	if err := ensureKeyValueTable(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	statement := fmt.Sprintf(`
INSERT INTO %s (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, settingsTableName)

	values := map[string]string{
		panelNameKey:   record.PanelName,
		gitHubTokenKey: record.GitHubToken,
	}

	for _, key := range panelSettingKeys {
		if _, err := tx.ExecContext(ctx, statement, key, values[key]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert settings key %q: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert settings transaction: %w", err)
	}

	return nil
}

func ensureKeyValueTable(ctx context.Context, tx *sql.Tx) error {
	statement := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`, settingsTableName)

	if _, err := tx.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure settings table: %w", err)
	}

	return nil
}

func migrateLegacyPanelSettings(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExists(ctx, tx, legacyPanelSettingsTable)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	var record Record

	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
SELECT panel_name
FROM %s
WHERE id = 1
`, legacyPanelSettingsTable)).Scan(
		&record.PanelName,
	)
	switch {
	case err == nil:
		if err := upsertRecordTx(ctx, tx, record); err != nil {
			return err
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return fmt.Errorf("load legacy panel settings: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE %s`, legacyPanelSettingsTable)); err != nil {
		return fmt.Errorf("drop legacy panel settings table: %w", err)
	}

	return nil
}

func upsertRecordTx(ctx context.Context, tx *sql.Tx, record Record) error {
	statement := fmt.Sprintf(`
INSERT INTO %s (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, settingsTableName)

	values := map[string]string{
		panelNameKey:   record.PanelName,
		gitHubTokenKey: record.GitHubToken,
	}

	for _, key := range panelSettingKeys {
		if _, err := tx.ExecContext(ctx, statement, key, values[key]); err != nil {
			return fmt.Errorf("migrate settings key %q: %w", key, err)
		}
	}

	return nil
}

func tableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table' AND name = ?
`, name).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check table %q: %w", name, err)
	}

	return count > 0, nil
}
