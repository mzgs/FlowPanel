package backup

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
CREATE TABLE IF NOT EXISTS google_drive_backups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    size INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure google drive backups table: %w", err)
	}

	return nil
}

func (s *Store) ListGoogleDrive(ctx context.Context) ([]Record, error) {
	if s == nil || s.db == nil {
		return []Record{}, nil
	}
	if err := s.Ensure(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, size, created_at
FROM google_drive_backups
ORDER BY created_at DESC, name DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list google drive backup metadata: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var (
			record         Record
			createdAtNanos int64
		)
		if err := rows.Scan(&record.ID, &record.Name, &record.Size, &createdAtNanos); err != nil {
			return nil, fmt.Errorf("scan google drive backup metadata row: %w", err)
		}

		record.CreatedAt = time.Unix(0, createdAtNanos).UTC()
		record.Location = LocationGoogleDrive
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate google drive backup metadata rows: %w", err)
	}

	return records, nil
}

func (s *Store) UpsertGoogleDrive(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}

	record.ID = strings.TrimSpace(record.ID)
	record.Name = strings.TrimSpace(record.Name)
	if record.ID == "" {
		return fmt.Errorf("google drive backup id is required")
	}
	if record.Name == "" {
		return fmt.Errorf("google drive backup name is required")
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO google_drive_backups (id, name, size, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    size = excluded.size,
    created_at = excluded.created_at
`, record.ID, record.Name, record.Size, record.CreatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("upsert google drive backup metadata %q: %w", record.ID, err)
	}

	return nil
}

func (s *Store) DeleteGoogleDrive(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM google_drive_backups WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete google drive backup metadata %q: %w", id, err)
	}

	return nil
}
