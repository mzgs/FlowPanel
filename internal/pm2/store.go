package pm2

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	dbutil "flowpanel/internal/db"
)

type Store struct {
	db *sql.DB
}

type Definition struct {
	Name             string
	ScriptPath       string
	WorkingDirectory string
	Interpreter      string
	Environment      map[string]string
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
    interpreter TEXT NOT NULL DEFAULT '',
    environment_json TEXT NOT NULL DEFAULT '',
    manually_stopped INTEGER NOT NULL DEFAULT 0
);
`
	if err := dbutil.ExecStatements(ctx, s.db, dbutil.Statement{
		SQL:          statement,
		ErrorContext: "ensure pm2 processes table",
	}); err != nil {
		return err
	}

	return ensurePM2StoreColumn(ctx, s.db, "pm2_processes", "environment_json", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) List(ctx context.Context) ([]Definition, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if err := s.Ensure(ctx); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT name, script_path, working_directory, environment_json, manually_stopped, interpreter
FROM pm2_processes
ORDER BY position ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list pm2 processes: %w", err)
	}
	defer rows.Close()

	definitions := make([]Definition, 0)
	for rows.Next() {
		var (
			definition      Definition
			environmentJSON string
		)
		if err := rows.Scan(&definition.Name, &definition.ScriptPath, &definition.WorkingDirectory, &environmentJSON, &definition.ManuallyStopped, &definition.Interpreter); err != nil {
			return nil, fmt.Errorf("scan pm2 process row: %w", err)
		}
		if err := decodeDefinitionEnvironment(environmentJSON, &definition); err != nil {
			return nil, fmt.Errorf("decode pm2 process environment: %w", err)
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
INSERT INTO pm2_processes (position, name, script_path, working_directory, environment_json, manually_stopped, interpreter)
VALUES (?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return fmt.Errorf("prepare pm2 process insert: %w", err)
	}
	defer statement.Close()

	for index, definition := range definitions {
		if _, err := statement.ExecContext(ctx, index, definition.Name, definition.ScriptPath, definition.WorkingDirectory, encodeDefinitionEnvironment(definition), definition.ManuallyStopped, definition.Interpreter); err != nil {
			return fmt.Errorf("insert pm2 process at position %d: %w", index, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit pm2 process replace: %w", err)
	}

	return nil
}

func encodeDefinitionEnvironment(definition Definition) string {
	if len(definition.Environment) == 0 {
		return ""
	}

	payload, err := json.Marshal(definition.Environment)
	if err != nil {
		return ""
	}

	return string(payload)
}

func decodeDefinitionEnvironment(raw string, definition *Definition) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		definition.Environment = nil
		return nil
	}

	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return err
	}
	definition.Environment = cloneEnvironmentMap(values)
	return nil
}

func ensurePM2StoreColumn(ctx context.Context, db *sql.DB, table string, column string, definition string) error {
	if db == nil {
		return nil
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
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
			return fmt.Errorf("scan %s columns: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column, err)
	}

	return nil
}
