package pm2

import (
	"context"
	"database/sql"
	"fmt"
)

type Store struct {
	db *sql.DB
}

type Definition struct {
	Name             string
	ScriptPath       string
	WorkingDirectory string
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
CREATE TABLE IF NOT EXISTS pm2_processes (
    position INTEGER PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    script_path TEXT NOT NULL DEFAULT '',
    working_directory TEXT NOT NULL DEFAULT ''
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure pm2 processes table: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]Definition, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if err := s.Ensure(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT name, script_path, working_directory
FROM pm2_processes
ORDER BY position ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list pm2 processes: %w", err)
	}
	defer rows.Close()

	definitions := make([]Definition, 0)
	for rows.Next() {
		var definition Definition
		if err := rows.Scan(&definition.Name, &definition.ScriptPath, &definition.WorkingDirectory); err != nil {
			return nil, fmt.Errorf("scan pm2 process row: %w", err)
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pm2 process rows: %w", err)
	}

	return definitions, nil
}

func (s *Store) Replace(ctx context.Context, definitions []Definition) error {
	if s == nil || s.db == nil {
		return nil
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pm2 process replace: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pm2_processes`); err != nil {
		return fmt.Errorf("clear pm2 processes: %w", err)
	}

	if len(definitions) == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit empty pm2 process replace: %w", err)
		}
		return nil
	}

	statement, err := tx.PrepareContext(ctx, `
INSERT INTO pm2_processes (position, name, script_path, working_directory)
VALUES (?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("prepare pm2 process insert: %w", err)
	}
	defer statement.Close()

	for index, definition := range definitions {
		if _, err := statement.ExecContext(ctx, index, definition.Name, definition.ScriptPath, definition.WorkingDirectory); err != nil {
			return fmt.Errorf("insert pm2 process at position %d: %w", index, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit pm2 process replace: %w", err)
	}

	return nil
}
