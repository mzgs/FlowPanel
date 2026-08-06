package settings

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const websiteImportSettingPrefix = "website_import."

var websiteImportProfileFields = []string{"host", "port", "username", "secret", "auth_type", "verify_tls"}

type WebsiteImportProfile struct {
	Provider  string `json:"provider"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Secret    string `json:"secret"`
	AuthType  string `json:"auth_type"`
	VerifyTLS bool   `json:"verify_tls"`
}

func (s *Service) WebsiteImportProfiles(ctx context.Context) (map[string]WebsiteImportProfile, error) {
	if s == nil || s.store == nil || s.store.db == nil {
		return map[string]WebsiteImportProfile{}, nil
	}
	rows, err := s.store.db.QueryContext(ctx, fmt.Sprintf(`SELECT key, value FROM %s WHERE key LIKE ?`, settingsTableName), websiteImportSettingPrefix+"%")
	if err != nil {
		return nil, fmt.Errorf("get website import profiles: %w", err)
	}
	defer rows.Close()
	values := map[string]map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan website import profile: %w", err)
		}
		parts := strings.Split(strings.TrimPrefix(key, websiteImportSettingPrefix), ".")
		if len(parts) != 2 || !validWebsiteImportProvider(parts[0]) {
			continue
		}
		if values[parts[0]] == nil {
			values[parts[0]] = map[string]string{}
		}
		values[parts[0]][parts[1]] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate website import profiles: %w", err)
	}
	profiles := map[string]WebsiteImportProfile{}
	for provider, fields := range values {
		port, _ := strconv.Atoi(fields["port"])
		profiles[provider] = WebsiteImportProfile{
			Provider: provider, Host: fields["host"], Port: port, Username: fields["username"], Secret: fields["secret"],
			AuthType: fields["auth_type"], VerifyTLS: fields["verify_tls"] == "1",
		}
	}
	return profiles, nil
}

func (s *Service) SaveWebsiteImportProfile(ctx context.Context, profile WebsiteImportProfile) error {
	profile.Provider = strings.ToLower(strings.TrimSpace(profile.Provider))
	if !validWebsiteImportProvider(profile.Provider) {
		return fmt.Errorf("unsupported website import provider %q", profile.Provider)
	}
	if s == nil || s.store == nil || s.store.db == nil {
		return nil
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin website import profile update: %w", err)
	}
	if err := ensureKeyValueTable(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	statement := fmt.Sprintf(`INSERT INTO %s (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingsTableName)
	values := map[string]string{
		"host": strings.TrimSpace(profile.Host), "port": strconv.Itoa(profile.Port), "username": strings.TrimSpace(profile.Username),
		"secret": profile.Secret, "auth_type": strings.ToLower(strings.TrimSpace(profile.AuthType)), "verify_tls": boolString(profile.VerifyTLS),
	}
	for _, field := range websiteImportProfileFields {
		if _, err := tx.ExecContext(ctx, statement, websiteImportSettingPrefix+profile.Provider+"."+field, values[field]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("save website import profile: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit website import profile update: %w", err)
	}
	return nil
}

func validWebsiteImportProvider(provider string) bool {
	return provider == "cpanel" || provider == "plesk"
}
