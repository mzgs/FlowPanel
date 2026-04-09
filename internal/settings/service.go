package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"flowpanel/internal/ftp"
	"flowpanel/internal/phpenv"
)

const (
	defaultPanelName     = "FlowPanel"
	maxPanelNameLength   = 80
	maxPanelURLLength    = 512
	maxGitHubTokenLength = 4096
	maxPassivePortsLen   = 32
)

type Record struct {
	PanelName               string `json:"panel_name"`
	PanelURL                string `json:"panel_url"`
	GitHubToken             string `json:"github_token"`
	DefaultPHPVersion       string `json:"default_php_version"`
	FTPEnabled              bool   `json:"ftp_enabled"`
	FTPHost                 string
	FTPPort                 int `json:"ftp_port"`
	FTPPublicIP             string
	FTPPassivePorts         string `json:"ftp_passive_ports"`
	GoogleDriveEmail        string `json:"google_drive_email"`
	GoogleDriveConnected    bool   `json:"google_drive_connected"`
	GoogleDriveRefreshToken string `json:"-"`
}

type UpdateInput struct {
	PanelName       string `json:"panel_name"`
	PanelURL        string `json:"panel_url"`
	GitHubToken     string `json:"github_token"`
	FTPEnabled      bool   `json:"ftp_enabled"`
	FTPPort         int    `json:"ftp_port"`
	FTPPassivePorts string `json:"ftp_passive_ports"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "settings validation failed"
}

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) Get(ctx context.Context) (Record, error) {
	if s == nil || s.store == nil {
		return defaultRecord(), nil
	}

	record, err := s.store.Get(ctx)
	if err == nil {
		record.FTPHost = ftp.DefaultHost()
		record.FTPPublicIP = ""
		record.DefaultPHPVersion = phpenv.NormalizeVersion(record.DefaultPHPVersion)
		record.FTPPassivePorts = normalizeFTPPassivePorts(record.FTPPassivePorts)
		return record, nil
	}
	if err == sql.ErrNoRows {
		return defaultRecord(), nil
	}

	return Record{}, err
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (Record, error) {
	validation := validateUpdateInput(input)
	if len(validation) > 0 {
		return Record{}, validation
	}

	record := Record{
		PanelName:       normalizeSingleLine(input.PanelName, defaultPanelName, maxPanelNameLength),
		PanelURL:        normalizePanelURL(input.PanelURL),
		GitHubToken:     normalizeOptionalSingleLine(input.GitHubToken, maxGitHubTokenLength),
		FTPEnabled:      input.FTPEnabled,
		FTPHost:         ftp.DefaultHost(),
		FTPPort:         normalizeFTPPort(input.FTPPort),
		FTPPublicIP:     "",
		FTPPassivePorts: normalizeFTPPassivePorts(input.FTPPassivePorts),
	}

	if s == nil || s.store == nil {
		return record, nil
	}

	current, err := s.Get(ctx)
	if err != nil {
		return Record{}, err
	}
	record.GoogleDriveEmail = current.GoogleDriveEmail
	record.GoogleDriveRefreshToken = current.GoogleDriveRefreshToken
	record.GoogleDriveConnected = strings.TrimSpace(record.GoogleDriveRefreshToken) != ""
	record.DefaultPHPVersion = current.DefaultPHPVersion

	if err := s.store.Upsert(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (s *Service) SetGoogleDriveConnection(ctx context.Context, email string, refreshToken string) (Record, error) {
	email = normalizeOptionalSingleLine(email, 320)
	refreshToken = normalizeOptionalSingleLine(refreshToken, 4096)
	if refreshToken == "" {
		return Record{}, errors.New("google drive refresh token is required")
	}

	record, err := s.Get(ctx)
	if err != nil {
		return Record{}, err
	}
	record.GoogleDriveEmail = email
	record.GoogleDriveRefreshToken = refreshToken
	record.GoogleDriveConnected = true

	if s == nil || s.store == nil {
		return record, nil
	}
	if err := s.store.Upsert(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (s *Service) ClearGoogleDriveConnection(ctx context.Context) (Record, error) {
	record, err := s.Get(ctx)
	if err != nil {
		return Record{}, err
	}
	record.GoogleDriveEmail = ""
	record.GoogleDriveRefreshToken = ""
	record.GoogleDriveConnected = false

	if s == nil || s.store == nil {
		return record, nil
	}
	if err := s.store.Upsert(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func (s *Service) SetDefaultPHPVersion(ctx context.Context, version string) (Record, error) {
	normalizedVersion := phpenv.NormalizeVersion(version)
	if normalizedVersion == "" {
		return Record{}, ValidationErrors{
			"default_php_version": "Select a supported PHP version.",
		}
	}

	record, err := s.Get(ctx)
	if err != nil {
		return Record{}, err
	}
	record.DefaultPHPVersion = normalizedVersion

	if s == nil || s.store == nil {
		return record, nil
	}
	if err := s.store.Upsert(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func defaultRecord() Record {
	return Record{
		PanelName:            defaultPanelName,
		PanelURL:             "",
		GitHubToken:          "",
		DefaultPHPVersion:    "",
		FTPEnabled:           false,
		FTPHost:              "0.0.0.0",
		FTPPort:              2121,
		FTPPublicIP:          "",
		FTPPassivePorts:      ftp.DefaultPassivePorts(),
		GoogleDriveEmail:     "",
		GoogleDriveConnected: false,
	}
}

func validateUpdateInput(input UpdateInput) ValidationErrors {
	validation := ValidationErrors{}

	panelName := strings.TrimSpace(input.PanelName)
	if panelName == "" {
		validation["panel_name"] = "Panel name is required."
	} else if len(panelName) > maxPanelNameLength {
		validation["panel_name"] = fmt.Sprintf("Panel name must be %d characters or fewer.", maxPanelNameLength)
	}

	panelURL := strings.TrimSpace(input.PanelURL)
	if len(panelURL) > maxPanelURLLength {
		validation["panel_url"] = fmt.Sprintf("Panel URL must be %d characters or fewer.", maxPanelURLLength)
	} else if panelURL != "" {
		if _, err := parsePanelURL(panelURL); err != nil {
			validation["panel_url"] = err.Error()
		}
	}

	if len(strings.TrimSpace(input.GitHubToken)) > maxGitHubTokenLength {
		validation["github_token"] = fmt.Sprintf("GitHub token must be %d characters or fewer.", maxGitHubTokenLength)
	}

	ftpPort := normalizeFTPPort(input.FTPPort)
	if ftpPort < 1 || ftpPort > 65535 {
		validation["ftp_port"] = "FTP port must be between 1 and 65535."
	}

	ftpPassivePorts := strings.TrimSpace(input.FTPPassivePorts)
	if len(ftpPassivePorts) > maxPassivePortsLen {
		validation["ftp_passive_ports"] = fmt.Sprintf("FTP passive ports must be %d characters or fewer.", maxPassivePortsLen)
	} else if ftpPassivePorts != "" {
		minPort, maxPort, ok := parsePassivePorts(ftpPassivePorts)
		if !ok || minPort < 1 || maxPort > 65535 || minPort > maxPort {
			validation["ftp_passive_ports"] = "Enter a passive port range like 30000-30100."
		}
	}

	return validation
}

func normalizeSingleLine(value, fallback string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if maxLen > 0 && len(value) > maxLen {
		value = strings.TrimSpace(value[:maxLen])
	}
	if value == "" {
		return fallback
	}

	return value
}

func normalizeOptionalSingleLine(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen > 0 && len(value) > maxLen {
		value = strings.TrimSpace(value[:maxLen])
	}

	return value
}

func normalizePanelURL(value string) string {
	parsed, err := parsePanelURL(value)
	if err != nil || parsed == nil {
		return ""
	}

	return parsed.String()
}

func normalizeFTPPort(value int) int {
	if value == 0 {
		return ftp.DefaultPort()
	}

	return value
}

func normalizeFTPPassivePorts(value string) string {
	value = normalizeOptionalSingleLine(value, maxPassivePortsLen)
	if value == "" {
		return ftp.DefaultPassivePorts()
	}

	return value
}

func parsePassivePorts(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}

	minPort, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	maxPort, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, false
	}

	return minPort, maxPort, true
}

func parsePanelURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return nil, errors.New("Enter a valid panel URL or hostname.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Panel URL must start with http:// or https:// when a scheme is provided.")
	}
	if parsed.Host == "" {
		return nil, errors.New("Panel URL must include a hostname.")
	}
	if parsed.User != nil {
		return nil, errors.New("Panel URL must not include a username or password.")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Panel URL must not include a query string or fragment.")
	}
	if path := strings.TrimSpace(parsed.EscapedPath()); path != "" && path != "/" {
		return nil, errors.New("Panel URL must not include a path.")
	}

	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed, nil
}
