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
	Interpreter      string
	ManuallyStopped  bool
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
    working_directory TEXT NOT NULL DEFAULT '',
    interpreter TEXT NOT NULL DEFAULT ''
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure pm2 processes table: %w", err)
	}
	if err := s.ensureColumn(ctx, "interpreter", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "manually_stopped", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
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
SELECT name, script_path, working_directory, manually_stopped, interpreter
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
		if err := rows.Scan(&definition.Name, &definition.ScriptPath, &definition.WorkingDirectory, &definition.ManuallyStopped, &definition.Interpreter); err != nil {
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
INSERT INTO pm2_processes (position, name, script_path, working_directory, manually_stopped, interpreter)
VALUES (?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("prepare pm2 process insert: %w", err)
	}
	defer statement.Close()

	for index, definition := range definitions {
		if _, err := statement.ExecContext(ctx, index, definition.Name, definition.ScriptPath, definition.WorkingDirectory, definition.ManuallyStopped, definition.Interpreter); err != nil {
			return fmt.Errorf("insert pm2 process at position %d: %w", index, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit pm2 process replace: %w", err)
	}

	return nil
}

func (s *Store) ensureColumn(ctx context.Context, columnName, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(pm2_processes)`)
	if err != nil {
		return fmt.Errorf("inspect pm2 processes columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return fmt.Errorf("scan pm2 processes column: %w", err)
		}
		if name == columnName {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pm2 processes columns: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE pm2_processes ADD COLUMN %s %s", columnName, definition)); err != nil {
		return fmt.Errorf("add pm2 processes column %s: %w", columnName, err)
	}

	return nil
}
