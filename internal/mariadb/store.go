package mariadb

import (
	"context"
	"database/sql"
	"fmt"
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
CREATE TABLE IF NOT EXISTS databases (
    name TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    host TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure mariadb databases table: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context) (map[string]DatabaseRecord, error) {
	if s == nil || s.db == nil {
		return map[string]DatabaseRecord{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT name, username, password, host
FROM databases
ORDER BY name ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list mariadb databases: %w", err)
	}
	defer rows.Close()

	records := make(map[string]DatabaseRecord)
	for rows.Next() {
		var record DatabaseRecord
		if err := rows.Scan(&record.Name, &record.Username, &record.Password, &record.Host); err != nil {
			return nil, fmt.Errorf("scan mariadb database row: %w", err)
		}
		records[record.Name] = record
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mariadb database rows: %w", err)
	}

	return records, nil
}

func (s *Store) Upsert(ctx context.Context, record DatabaseRecord) error {
	if s == nil || s.db == nil {
		return nil
	}

	now := time.Now().UTC().UnixNano()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO databases (name, username, password, host, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
    username = excluded.username,
    password = excluded.password,
    host = excluded.host,
    updated_at = excluded.updated_at
`, record.Name, record.Username, record.Password, record.Host, now, now)
	if err != nil {
		return fmt.Errorf("upsert mariadb database %q: %w", record.Name, err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, name string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM databases WHERE name = ?`, name); err != nil {
		return fmt.Errorf("delete mariadb database %q: %w", name, err)
	}

	return nil
}
