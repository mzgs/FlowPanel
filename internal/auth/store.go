package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbutil "flowpanel/internal/db"
)

var ErrUsernameTaken = errors.New("panel username already exists")

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

	const statement = `
CREATE TABLE IF NOT EXISTS panel_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`

	return dbutil.ExecStatements(ctx, s.db, dbutil.Statement{
		SQL:          statement,
		ErrorContext: "ensure panel users table",
	})
}

func (s *Store) Count(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}

	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM panel_users`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count panel users: %w", err)
	}

	return count, nil
}

func (s *Store) GetByID(ctx context.Context, id string) (User, error) {
	if s == nil || s.db == nil {
		return User{}, sql.ErrNoRows
	}

	user, err := scanUser(s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, created_at, updated_at
FROM panel_users
WHERE id = ?
`, strings.TrimSpace(id)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, sql.ErrNoRows
		}
		return User{}, fmt.Errorf("get panel user by id: %w", err)
	}

	return user, nil
}

func (s *Store) GetByUsername(ctx context.Context, username string) (User, error) {
	if s == nil || s.db == nil {
		return User{}, sql.ErrNoRows
	}

	user, err := scanUser(s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, created_at, updated_at
FROM panel_users
WHERE username = ?
`, NormalizeUsername(username)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, sql.ErrNoRows
		}
		return User{}, fmt.Errorf("get panel user by username: %w", err)
	}

	return user, nil
}

func (s *Store) First(ctx context.Context) (User, error) {
	if s == nil || s.db == nil {
		return User{}, sql.ErrNoRows
	}

	user, err := scanUser(s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, created_at, updated_at
FROM panel_users
ORDER BY created_at ASC
LIMIT 1
`).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, sql.ErrNoRows
		}
		return User{}, fmt.Errorf("get first panel user: %w", err)
	}

	return user, nil
}

func (s *Store) Insert(ctx context.Context, user User) error {
	if s == nil || s.db == nil {
		return nil
	}

	return insertUser(ctx, s.db, user)
}

func (s *Store) InsertInitial(ctx context.Context, user User) error {
	if s == nil || s.db == nil {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin initial panel user insert: %w", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM panel_users`).Scan(&count); err != nil {
		return fmt.Errorf("count panel users before initial insert: %w", err)
	}
	if count > 0 {
		return ErrUsernameTaken
	}

	if err := insertUser(ctx, tx, user); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit initial panel user insert: %w", err)
	}

	return nil
}

func insertUser(ctx context.Context, db execer, user User) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO panel_users (id, username, password_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
`,
		user.ID,
		user.Username,
		user.PasswordHash,
		user.CreatedAt.UTC().UnixNano(),
		user.UpdatedAt.UTC().UnixNano(),
	)
	if err != nil {
		if isDuplicateUsernameError(err) {
			return ErrUsernameTaken
		}
		return fmt.Errorf("insert panel user %q: %w", user.ID, err)
	}

	return nil
}

func (s *Store) Update(ctx context.Context, user User) error {
	if s == nil || s.db == nil {
		return nil
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE panel_users
SET username = ?, password_hash = ?, updated_at = ?
WHERE id = ?
`,
		user.Username,
		user.PasswordHash,
		user.UpdatedAt.UTC().UnixNano(),
		user.ID,
	)
	if err != nil {
		if isDuplicateUsernameError(err) {
			return ErrUsernameTaken
		}
		return fmt.Errorf("update panel user %q: %w", user.ID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect panel user update %q: %w", user.ID, err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func scanUser(scan func(dest ...any) error) (User, error) {
	var (
		user          User
		createdAtUnix int64
		updatedAtUnix int64
	)

	if err := scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&createdAtUnix,
		&updatedAtUnix,
	); err != nil {
		return User{}, err
	}

	user.CreatedAt = time.Unix(0, createdAtUnix).UTC()
	user.UpdatedAt = time.Unix(0, updatedAtUnix).UTC()
	return user, nil
}

func isDuplicateUsernameError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") && strings.Contains(message, ".username")
}
