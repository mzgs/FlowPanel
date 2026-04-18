package ftp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const ftpAccountsTableName = "ftp_accounts"

var ErrUsernameTaken = errors.New("ftp username already exists")

type Account struct {
	ID           string    `json:"id"`
	DomainID     string    `json:"domain_id"`
	Username     string    `json:"username"`
	RootPath     string    `json:"root_path"`
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
CREATE TABLE IF NOT EXISTS ftp_accounts (
    id TEXT PRIMARY KEY,
    domain_id TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL UNIQUE,
    root_path TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`

	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("ensure ftp accounts table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
CREATE INDEX IF NOT EXISTS idx_ftp_accounts_domain_id
ON ftp_accounts(domain_id)
`); err != nil {
		return fmt.Errorf("ensure ftp accounts domain index: %w", err)
	}

	return nil
}

func (s *Store) List(ctx context.Context) ([]Account, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain_id, username, root_path, password_hash, enabled, created_at, updated_at
FROM ftp_accounts
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list ftp accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]Account, 0)
	for rows.Next() {
		account, err := scanAccount(rows.Scan)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ftp accounts: %w", err)
	}

	return accounts, nil
}

func (s *Store) GetByID(ctx context.Context, id string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, sql.ErrNoRows
	}

	account, err := scanAccount(s.db.QueryRowContext(ctx, `
SELECT id, domain_id, username, root_path, password_hash, enabled, created_at, updated_at
FROM ftp_accounts
WHERE id = ?
`, strings.TrimSpace(id)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, sql.ErrNoRows
		}
		return Account{}, fmt.Errorf("get ftp account by id: %w", err)
	}

	return account, nil
}

func (s *Store) GetPrimaryByDomainID(ctx context.Context, domainID string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, sql.ErrNoRows
	}

	account, err := scanAccount(s.db.QueryRowContext(ctx, `
SELECT id, domain_id, username, root_path, password_hash, enabled, created_at, updated_at
FROM ftp_accounts
WHERE domain_id = ?
ORDER BY created_at ASC, id ASC
LIMIT 1
`, strings.TrimSpace(domainID)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, sql.ErrNoRows
		}
		return Account{}, fmt.Errorf("get primary ftp account by domain id: %w", err)
	}

	return account, nil
}

func (s *Store) GetByUsername(ctx context.Context, username string) (Account, error) {
	if s == nil || s.db == nil {
		return Account{}, sql.ErrNoRows
	}

	account, err := scanAccount(s.db.QueryRowContext(ctx, `
SELECT id, domain_id, username, root_path, password_hash, enabled, created_at, updated_at
FROM ftp_accounts
WHERE username = ?
`, strings.TrimSpace(username)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, sql.ErrNoRows
		}
		return Account{}, fmt.Errorf("get ftp account by username: %w", err)
	}

	return account, nil
}

func (s *Store) Upsert(ctx context.Context, account Account) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO ftp_accounts (
    id,
    domain_id,
    username,
    root_path,
    password_hash,
    enabled,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    domain_id = excluded.domain_id,
    username = excluded.username,
    root_path = excluded.root_path,
    password_hash = excluded.password_hash,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at
`,
		account.ID,
		strings.TrimSpace(account.DomainID),
		account.Username,
		account.RootPath,
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

	return fmt.Errorf("upsert ftp account %q: %w", account.ID, err)
}

func (s *Store) DeleteByID(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM ftp_accounts
WHERE id = ?
`, strings.TrimSpace(id)); err != nil {
		return fmt.Errorf("delete ftp account by id: %w", err)
	}

	return nil
}

func (s *Store) DeleteByDomainID(ctx context.Context, domainID string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
DELETE FROM ftp_accounts
WHERE domain_id = ?
`, strings.TrimSpace(domainID)); err != nil {
		return fmt.Errorf("delete ftp accounts by domain id: %w", err)
	}

	return nil
}
func scanAccount(scan func(dest ...any) error) (Account, error) {
	var (
		account     Account
		enabledInt  int64
		createdAtNS int64
		updatedAtNS int64
	)

	if err := scan(
		&account.ID,
		&account.DomainID,
		&account.Username,
		&account.RootPath,
		&account.PasswordHash,
		&enabledInt,
		&createdAtNS,
		&updatedAtNS,
	); err != nil {
		return Account{}, err
	}

	account.Enabled = enabledInt != 0
	account.CreatedAt = time.Unix(0, createdAtNS).UTC()
	account.UpdatedAt = time.Unix(0, updatedAtNS).UTC()
	return account, nil
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
