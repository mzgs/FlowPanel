package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) Ensure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS auth_sessions (
    token TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions (expires_at);
`); err != nil {
		return fmt.Errorf("ensure session storage: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return fmt.Errorf("remove expired sessions: %w", err)
	}
	return nil
}

func (s *SessionStore) Delete(token string) error {
	return s.DeleteCtx(context.Background(), token)
}

func (s *SessionStore) DeleteCtx(ctx context.Context, token string) error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *SessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}

func (s *SessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, nil
	}

	var data []byte
	var expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT data, expires_at FROM auth_sessions WHERE token = ?`, token).Scan(&data, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find session: %w", err)
	}
	if expiresAt <= time.Now().Unix() {
		if err := s.DeleteCtx(ctx, token); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	return data, true, nil
}

func (s *SessionStore) Commit(token string, data []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, data, expiry)
}

func (s *SessionStore) CommitCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO auth_sessions (token, data, expires_at)
VALUES (?, ?, ?)
ON CONFLICT(token) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at
`, token, data, expiry.Unix()); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	return nil
}
