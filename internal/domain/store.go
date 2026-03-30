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
    created_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure domains table: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, hostname, kind, target, created_at
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
			record        Record
			kind          string
			createdAtUnix int64
		)

		if err := rows.Scan(&record.ID, &record.Hostname, &kind, &record.Target, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("scan domain row: %w", err)
		}

		record.Kind = Kind(kind)
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
INSERT INTO domains (id, hostname, kind, target, created_at)
VALUES (?, ?, ?, ?, ?)
`, record.ID, record.Hostname, string(record.Kind), record.Target, record.CreatedAt.UTC().UnixNano())
	if err == nil {
		return nil
	}

	if isDuplicateHostnameError(err) {
		return ErrDuplicateHostname
	}

	return fmt.Errorf("insert domain %q: %w", record.Hostname, err)
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
