package ftp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ftpAccountsTableName = "domain_ftp_accounts"

var ErrUsernameTaken = errors.New("ftp username already exists")

type Account struct {
	DomainID     string    `json:"domain_id"`
	Username     string    `json:"username"`
	Enabled      bool      `json:"enabled"`
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
CREATE TABLE IF NOT EXISTS domain_ftp_accounts (
    domain_id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure domain ftp accounts table: %w", err)
	}

	return nil
}

func (s *Store) GetByDomainID(ctx context.Context, domainID string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, sql.ErrNoRows
	}

	var (
		account     Account
		enabledInt  int64
		createdAtNS int64
		updatedAtNS int64
	)

	err := s.db.QueryRowContext(ctx, `
SELECT domain_id, username, password_hash, enabled, created_at, updated_at
FROM domain_ftp_accounts
WHERE domain_id = ?
`, strings.TrimSpace(domainID)).Scan(
		&account.DomainID,
		&account.Username,
		&account.PasswordHash,
		&enabledInt,
		&createdAtNS,
		&updatedAtNS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, sql.ErrNoRows
		}
		return Account{}, fmt.Errorf("get ftp account by domain id: %w", err)
	}

	account.Enabled = enabledInt != 0
	account.CreatedAt = time.Unix(0, createdAtNS).UTC()
	account.UpdatedAt = time.Unix(0, updatedAtNS).UTC()
	return account, nil
}

func (s *Store) GetByUsername(ctx context.Context, username string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, sql.ErrNoRows
	}

	var (
		account     Account
		enabledInt  int64
		createdAtNS int64
		updatedAtNS int64
	)

	err := s.db.QueryRowContext(ctx, `
SELECT domain_id, username, password_hash, enabled, created_at, updated_at
FROM domain_ftp_accounts
WHERE username = ?
`, strings.TrimSpace(username)).Scan(
		&account.DomainID,
		&account.Username,
		&account.PasswordHash,
		&enabledInt,
		&createdAtNS,
		&updatedAtNS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, sql.ErrNoRows
		}
		return Account{}, fmt.Errorf("get ftp account by username: %w", err)
	}

	account.Enabled = enabledInt != 0
	account.CreatedAt = time.Unix(0, createdAtNS).UTC()
	account.UpdatedAt = time.Unix(0, updatedAtNS).UTC()
	return account, nil
}

func (s *Store) Upsert(ctx context.Context, account Account) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO domain_ftp_accounts (
    domain_id,
    username,
    password_hash,
    enabled,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(domain_id) DO UPDATE SET
    username = excluded.username,
    password_hash = excluded.password_hash,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at
`,
		account.DomainID,
		account.Username,
		account.PasswordHash,
		boolToInt(account.Enabled),
		account.CreatedAt.UTC().UnixNano(),
		account.UpdatedAt.UTC().UnixNano(),
	)
	if err == nil {
		return nil
	}

	if isDuplicateUsernameError(err) {
		return ErrUsernameTaken
	}

	return fmt.Errorf("upsert ftp account for domain %q: %w", account.DomainID, err)
}

func (s *Store) DeleteByDomainID(ctx context.Context, domainID string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM domain_ftp_accounts
WHERE domain_id = ?
`, strings.TrimSpace(domainID)); err != nil {
		return fmt.Errorf("delete ftp account by domain id: %w", err)
	}

	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func isDuplicateUsernameError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") && strings.Contains(message, ".username")
}
