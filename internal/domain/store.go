package domain

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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
CREATE TABLE IF NOT EXISTS domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    cache_enabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure domains table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE domains ADD COLUMN cache_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("ensure domains.cache_enabled column: %w", err)
		}
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, hostname, kind, target, cache_enabled, created_at
FROM domains
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var (
			record          Record
			kind            string
			cacheEnabledInt int64
			createdAtUnix   int64
		)

		if err := rows.Scan(&record.ID, &record.Hostname, &kind, &record.Target, &cacheEnabledInt, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("scan domain row: %w", err)
		}

		record.Kind = Kind(kind)
		record.CacheEnabled = cacheEnabledInt != 0
		if message := validateKind(record.Kind); message != "" {
			return nil, fmt.Errorf("invalid persisted domain kind %q for %q", kind, record.Hostname)
		}

		record.CreatedAt = time.Unix(0, createdAtUnix).UTC()
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domain rows: %w", err)
	}

	return records, nil
}

func (s *Store) Insert(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO domains (id, hostname, kind, target, cache_enabled, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`, record.ID, record.Hostname, string(record.Kind), record.Target, boolToInt(record.CacheEnabled), record.CreatedAt.UTC().UnixNano())
	if err == nil {
		return nil
	}

	if isDuplicateHostnameError(err) {
		return ErrDuplicateHostname
	}

	return fmt.Errorf("insert domain %q: %w", record.Hostname, err)
}

func (s *Store) Update(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE domains
SET hostname = ?, kind = ?, target = ?, cache_enabled = ?, created_at = ?
WHERE id = ?
`, record.Hostname, string(record.Kind), record.Target, boolToInt(record.CacheEnabled), record.CreatedAt.UTC().UnixNano(), record.ID)
	if err == nil {
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("update domain %q: %w", record.ID, rowsErr)
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}

	if isDuplicateHostnameError(err) {
		return ErrDuplicateHostname
	}

	return fmt.Errorf("update domain %q: %w", record.ID, err)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete domain %q: %w", id, err)
	}

	return nil
}

func isDuplicateHostnameError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "hostname")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}
