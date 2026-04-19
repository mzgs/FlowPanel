package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbutil "flowpanel/internal/db"
)

type Store struct {
	db *sql.DB
}

type GitHubIntegrationRecord struct {
	DomainID string
	GitHubIntegration
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
CREATE TABLE IF NOT EXISTS domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,
    target TEXT NOT NULL,
    nodejs_script_path TEXT NOT NULL DEFAULT '',
    php_version TEXT NOT NULL DEFAULT '',
    php_settings TEXT NOT NULL DEFAULT '',
    environment_variables TEXT NOT NULL DEFAULT '',
    cache_enabled INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
`
	if err := dbutil.ExecStatements(
		ctx,
		s.db,
		dbutil.Statement{
			SQL:          statement,
			ErrorContext: "ensure domains table",
		},
		dbutil.Statement{
			SQL: `
CREATE TABLE IF NOT EXISTS domain_github_integrations (
    domain_id TEXT PRIMARY KEY,
    repository_url TEXT NOT NULL,
    auto_deploy_on_push INTEGER NOT NULL DEFAULT 0,
    default_branch TEXT NOT NULL DEFAULT '',
    post_fetch_script TEXT NOT NULL DEFAULT '',
    webhook_secret TEXT NOT NULL DEFAULT '',
    webhook_id INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
`,
			ErrorContext: "ensure domain github integrations table",
		},
	); err != nil {
		return err
	}

	return ensureDomainStoreColumn(ctx, s.db, "domains", "environment_variables", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) List(ctx context.Context) ([]Record, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, hostname, kind, target, nodejs_script_path, php_version, php_settings, environment_variables, cache_enabled, created_at
FROM domains
ORDER BY created_at DESC, id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0)
	for rows.Next() {
		var (
			record          Record
			kind            string
			nodeJSScript    string
			phpVersion      string
			phpSettingsJSON string
			environmentJSON string
			cacheEnabledInt int64
			createdAtUnix   int64
		)

		if err := rows.Scan(&record.ID, &record.Hostname, &kind, &record.Target, &nodeJSScript, &phpVersion, &phpSettingsJSON, &environmentJSON, &cacheEnabledInt, &createdAtUnix); err != nil {
			return nil, fmt.Errorf("scan domain row: %w", err)
		}

		record.Kind, record.Target = normalizeKindAndTarget(Kind(kind), record.Target)
		record.NodeJSScript = normalizeNodeJSScriptForKind(record.Kind, nodeJSScript)
		record.PHPVersion = strings.TrimSpace(phpVersion)
		record.CacheEnabled = cacheEnabledInt != 0
		if message := validateKind(record.Kind); message != "" {
			return nil, fmt.Errorf("invalid persisted domain kind %q for %q", kind, record.Hostname)
		}

		record.CreatedAt = time.Unix(0, createdAtUnix).UTC()
		if err := decodePHPSettings(phpSettingsJSON, &record); err != nil {
			return nil, fmt.Errorf("decode php settings for %q: %w", record.Hostname, err)
		}
		if err := decodeEnvironmentVariables(environmentJSON, &record); err != nil {
			return nil, fmt.Errorf("decode environment variables for %q: %w", record.Hostname, err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domain rows: %w", err)
	}

	return records, nil
}

func (s *Store) Insert(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO domains (id, hostname, kind, target, nodejs_script_path, php_version, php_settings, environment_variables, cache_enabled, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, record.ID, record.Hostname, string(record.Kind), record.Target, record.NodeJSScript, record.PHPVersion, encodePHPSettings(record), encodeEnvironmentVariables(record), boolToInt(record.CacheEnabled), record.CreatedAt.UTC().UnixNano())
	if err == nil {
		return nil
	}

	if isDuplicateHostnameError(err) {
		return ErrDuplicateHostname
	}

	return fmt.Errorf("insert domain %q: %w", record.Hostname, err)
}

func (s *Store) ListGitHubIntegrations(ctx context.Context) ([]GitHubIntegrationRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT domain_id, repository_url, auto_deploy_on_push, default_branch, post_fetch_script, webhook_secret, webhook_id, created_at, updated_at
FROM domain_github_integrations
`)
	if err != nil {
		return nil, fmt.Errorf("list domain github integrations: %w", err)
	}
	defer rows.Close()

	records := make([]GitHubIntegrationRecord, 0)
	for rows.Next() {
		var (
			record        GitHubIntegrationRecord
			autoDeployInt int64
			webhookID     int64
			createdAtUnix int64
			updatedAtUnix int64
		)

		if err := rows.Scan(
			&record.DomainID,
			&record.RepositoryURL,
			&autoDeployInt,
			&record.DefaultBranch,
			&record.PostFetchScript,
			&record.WebhookSecret,
			&webhookID,
			&createdAtUnix,
			&updatedAtUnix,
		); err != nil {
			return nil, fmt.Errorf("scan domain github integration row: %w", err)
		}

		record.AutoDeployOnPush = autoDeployInt != 0
		record.WebhookID = webhookID
		record.CreatedAt = time.Unix(0, createdAtUnix).UTC()
		record.UpdatedAt = time.Unix(0, updatedAtUnix).UTC()
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domain github integration rows: %w", err)
	}

	return records, nil
}

func (s *Store) Update(ctx context.Context, record Record) error {
	if s == nil || s.db == nil {
		return nil
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE domains
SET hostname = ?, kind = ?, target = ?, nodejs_script_path = ?, php_version = ?, php_settings = ?, environment_variables = ?, cache_enabled = ?, created_at = ?
WHERE id = ?
`, record.Hostname, string(record.Kind), record.Target, record.NodeJSScript, record.PHPVersion, encodePHPSettings(record), encodeEnvironmentVariables(record), boolToInt(record.CacheEnabled), record.CreatedAt.UTC().UnixNano(), record.ID)
	if err == nil {
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return fmt.Errorf("update domain %q: %w", record.ID, rowsErr)
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	}

	if isDuplicateHostnameError(err) {
		return ErrDuplicateHostname
	}

	return fmt.Errorf("update domain %q: %w", record.ID, err)
}

func (s *Store) UpsertGitHubIntegration(
	ctx context.Context,
	domainID string,
	integration GitHubIntegration,
) error {
	if s == nil || s.db == nil {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO domain_github_integrations (
	domain_id,
	repository_url,
	auto_deploy_on_push,
	default_branch,
	post_fetch_script,
	webhook_secret,
	webhook_id,
	created_at,
	updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(domain_id) DO UPDATE SET
	repository_url = excluded.repository_url,
	auto_deploy_on_push = excluded.auto_deploy_on_push,
	default_branch = excluded.default_branch,
	post_fetch_script = excluded.post_fetch_script,
	webhook_secret = excluded.webhook_secret,
	webhook_id = excluded.webhook_id,
	created_at = excluded.created_at,
	updated_at = excluded.updated_at
`, domainID, integration.RepositoryURL, boolToInt(integration.AutoDeployOnPush), integration.DefaultBranch, integration.PostFetchScript, integration.WebhookSecret, integration.WebhookID, integration.CreatedAt.UTC().UnixNano(), integration.UpdatedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("upsert domain github integration %q: %w", domainID, err)
	}

	return nil
}

func (s *Store) DeleteGitHubIntegration(ctx context.Context, domainID string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM domain_github_integrations WHERE domain_id = ?`, domainID); err != nil {
		return fmt.Errorf("delete domain github integration %q: %w", domainID, err)
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete domain %q: %w", id, err)
	}

	return nil
}

func isDuplicateHostnameError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "hostname")
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func encodePHPSettings(record Record) string {
	if record.PHPSettings == (Record{}.PHPSettings) {
		return ""
	}

	payload, err := json.Marshal(record.PHPSettings)
	if err != nil {
		return ""
	}

	return string(payload)
}

func decodePHPSettings(raw string, record *Record) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	return json.Unmarshal([]byte(raw), &record.PHPSettings)
}

func encodeEnvironmentVariables(record Record) string {
	if len(record.EnvironmentVariables) == 0 {
		return ""
	}

	payload, err := json.Marshal(record.EnvironmentVariables)
	if err != nil {
		return ""
	}

	return string(payload)
}

func decodeEnvironmentVariables(raw string, record *Record) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		record.EnvironmentVariables = nil
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &record.EnvironmentVariables); err != nil {
		return err
	}

	record.EnvironmentVariables = normalizeEnvironmentVariables(record.EnvironmentVariables)
	return nil
}

func ensureDomainStoreColumn(ctx context.Context, db *sql.DB, table string, column string, definition string) error {
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
