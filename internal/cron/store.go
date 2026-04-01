package cron

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
CREATE TABLE IF NOT EXISTS cron_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    schedule_spec TEXT NOT NULL,
    command_text TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure cron jobs table: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if s == nil || s.db == nil {
		return []Record{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, schedule_spec, command_text, created_at
FROM cron_jobs
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list cron jobs: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var (
			record        Record
			createdAtUnix int64
		)

		if err := rows.Scan(&record.ID, &record.Name, &record.Schedule, &record.Command, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("scan cron job row: %w", err)
		}

		record.CreatedAt = time.Unix(0, createdAtUnix).UTC()
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cron job rows: %w", err)
	}

	return records, nil
}

func (s *Store) Insert(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO cron_jobs (id, name, schedule_spec, command_text, created_at)
VALUES (?, ?, ?, ?, ?)
`, record.ID, record.Name, record.Schedule, record.Command, record.CreatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("insert cron job %q: %w", record.ID, err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete cron job %q: %w", id, err)
	}

	return nil
}
