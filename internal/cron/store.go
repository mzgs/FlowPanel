package cron

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
CREATE TABLE IF NOT EXISTS cron_jobs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    schedule_spec TEXT NOT NULL,
    command_text TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS cron_job_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    output_text TEXT NOT NULL,
    error_text TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cron_job_runs_job_started_at
ON cron_job_runs (job_id, started_at DESC, id DESC);
`

	return dbutil.ExecStatements(ctx, s.db, dbutil.Statement{
		SQL:          statement,
		ErrorContext: "ensure cron jobs table",
	})
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

func (s *Store) Update(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE cron_jobs
SET name = ?, schedule_spec = ?, command_text = ?
WHERE id = ?
`, record.Name, record.Schedule, record.Command, record.ID)
	if err != nil {
		return fmt.Errorf("update cron job %q: %w", record.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated cron job rows %q: %w", record.ID, err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete cron job %q transaction: %w", id, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM cron_job_runs WHERE job_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete cron job runs %q: %w", id, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete cron job %q: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete cron job %q: %w", id, err)
	}

	return nil
}

func (s *Store) ListExecutionLogs(ctx context.Context, limitPerJob int) (map[string][]ExecutionLog, error) {
	if s == nil || s.db == nil {
		return map[string][]ExecutionLog{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, status, started_at, finished_at, duration_ms, output_text, error_text
FROM cron_job_runs
ORDER BY started_at DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list cron job runs: %w", err)
	}
	defer rows.Close()

	logsByJob := make(map[string][]ExecutionLog)
	for rows.Next() {
		var (
			logEntry       ExecutionLog
			startedAtUnix  int64
			finishedAtUnix int64
		)

		if err := rows.Scan(
			&logEntry.ID,
			&logEntry.JobID,
			&logEntry.Status,
			&startedAtUnix,
			&finishedAtUnix,
			&logEntry.DurationMS,
			&logEntry.Output,
			&logEntry.Error,
		); err != nil {
			return nil, fmt.Errorf("scan cron job run row: %w", err)
		}

		if limitPerJob > 0 && len(logsByJob[logEntry.JobID]) >= limitPerJob {
			continue
		}

		logEntry.StartedAt = time.Unix(0, startedAtUnix).UTC()
		logEntry.FinishedAt = time.Unix(0, finishedAtUnix).UTC()
		logsByJob[logEntry.JobID] = append(logsByJob[logEntry.JobID], logEntry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cron job run rows: %w", err)
	}

	return logsByJob, nil
}

func (s *Store) InsertExecutionLog(ctx context.Context, logEntry ExecutionLog) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO cron_job_runs (id, job_id, status, started_at, finished_at, duration_ms, output_text, error_text)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`,
		logEntry.ID,
		logEntry.JobID,
		logEntry.Status,
		logEntry.StartedAt.UTC().UnixNano(),
		logEntry.FinishedAt.UTC().UnixNano(),
		logEntry.DurationMS,
		logEntry.Output,
		logEntry.Error,
	)
	if err != nil {
		return fmt.Errorf("insert cron job run %q: %w", logEntry.ID, err)
	}

	return nil
}
