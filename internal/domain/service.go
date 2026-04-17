package domain

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/config"
	"flowpanel/internal/phpenv"
)

const (
	linuxSitesBasePath   = "/var/www"
	darwinSitesBasePath  = "/Users/Shared/www"
	windowsSitesBasePath = `C:\Sites`
)

var ErrDuplicateHostname = errors.New("duplicate hostname")
var ErrNotFound = errors.New("domain not found")

var hostnamePattern = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])$`)

type Kind string

const (
	KindStaticSite   Kind = "Static site"
	KindPHP          Kind = "Php site"
	KindNodeJS       Kind = "Node.js"
	KindPython       Kind = "Python"
	KindReverseProxy Kind = "Reverse proxy"
)

type Record struct {
	ID           string             `json:"id"`
	Hostname     string             `json:"hostname"`
	Kind         Kind               `json:"kind"`
	Target       string             `json:"target"`
	NodeJSScript string             `json:"nodejs_script_path,omitempty"`
	PHPVersion   string             `json:"php_version,omitempty"`
	PHPSettings  phpenv.Settings    `json:"php_settings"`
	Logs         LogPaths           `json:"logs"`
	GitHub       *GitHubIntegration `json:"github_integration,omitempty"`
	CacheEnabled bool               `json:"cache_enabled"`
	CreatedAt    time.Time          `json:"created_at"`
}

type GitHubIntegration struct {
	RepositoryURL    string    `json:"repository_url"`
	AutoDeployOnPush bool      `json:"auto_deploy_on_push"`
	DefaultBranch    string    `json:"default_branch"`
	PostFetchScript  string    `json:"post_fetch_script"`
	WebhookSecret    string    `json:"-"`
	WebhookID        int64     `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type LogPaths struct {
	Directory string `json:"directory"`
	Access    string `json:"access"`
	Error     string `json:"error"`
}

type CreateInput struct {
	Hostname     string `json:"hostname"`
	Kind         Kind   `json:"kind"`
	Target       string `json:"target"`
	NodeJSScript string `json:"nodejs_script_path"`
	CacheEnabled bool   `json:"cache_enabled"`
}

type UpdateInput struct {
	Hostname     string `json:"hostname"`
	Kind         Kind   `json:"kind"`
	Target       string `json:"target"`
	NodeJSScript string `json:"nodejs_script_path"`
	CacheEnabled bool   `json:"cache_enabled"`
}

type UpdatePHPInput struct {
	PHPVersion string `json:"php_version"`
	phpenv.UpdateSettingsInput
}

type ValidationErrors map[string]string

func (e ValidationErrors) Error() string {
	return "validation failed"
}

type Service struct {
	basePath         string
	logsBasePath     string
	store            *Store
	previewCachePath string
	previewTTL       time.Duration
	previewGenerator PreviewGenerator
	now              func() time.Time
	mu               sync.RWMutex
	previewMu        sync.Mutex
	records          []Record
	githubByDomainID map[string]GitHubIntegration
}

func NewService(store *Store) *Service {
	return NewServiceWithBasePath(defaultSitesBasePath(), store)
}

func NewServiceWithBasePath(basePath string, store *Store) *Service {
	return newService(basePath, store)
}

func newService(basePath string, store *Store) *Service {
	return &Service{
		basePath:         strings.TrimSpace(basePath),
		logsBasePath:     defaultLogsBasePath(),
		store:            store,
		previewCachePath: defaultPreviewCachePath(),
		previewTTL:       defaultPreviewTTL(),
		previewGenerator: defaultPreviewGenerator(),
		now:              time.Now,
		records:          make([]Record, 0),
		githubByDomainID: make(map[string]GitHubIntegration),
	}
}

func defaultLogsBasePath() string {
	return filepath.Join(config.FlowPanelDataPath(), "logs", "sites")
}

func defaultSitesBasePath() string {
	switch runtime.GOOS {
	case "windows":
		return windowsSitesBasePath
	case "darwin":
		return darwinSitesBasePath
	default:
		return linuxSitesBasePath
	}
}

func SupportsManagedDocumentRoot(kind Kind) bool {
	switch kind {
	case KindStaticSite, KindPHP, KindNodeJS, KindPython, KindReverseProxy:
		return true
	default:
		return false
	}
}

func usesUpstreamTarget(kind Kind) bool {
	switch kind {
	case KindNodeJS, KindPython, KindReverseProxy:
		return true
	default:
		return false
	}
}

func ResolveDocumentRoot(basePath string, record Record) (string, error) {
	if !SupportsManagedDocumentRoot(record.Kind) {
		return "", fmt.Errorf("unsupported domain kind %q", record.Kind)
	}

	normalizedBasePath := filepath.Clean(strings.TrimSpace(basePath))
	if normalizedBasePath == "." || normalizedBasePath == "" {
		return "", errors.New("domain sites base path is not configured")
	}

	hostname := normalizeHostname(record.Hostname)
	if hostname == "" {
		return "", errors.New("domain hostname is required")
	}

	rootPath := filepath.Join(normalizedBasePath, hostname)
	switch record.Kind {
	case KindStaticSite, KindPHP:
		targetPath := filepath.Clean(strings.TrimSpace(record.Target))
		if targetPath != "." && targetPath != "" {
			if !filepath.IsAbs(targetPath) {
				targetPath = filepath.Join(normalizedBasePath, targetPath)
			}
			rootPath = targetPath
		}
	}

	relativePath, err := filepath.Rel(normalizedBasePath, rootPath)
	if err != nil {
		return "", err
	}
	if relativePath == "." {
		return "", errors.New("refusing to use the sites base path as a document root")
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("document root %q is outside the sites base path", rootPath)
	}

	return rootPath, nil
}

func (s *Service) BasePath() string {
	return s.basePath
}

func (s *Service) SetLogsBasePath(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logsBasePath = strings.TrimSpace(path)
	for i, record := range s.records {
		s.records[i] = s.withLogPaths(record)
	}
}

func (s *Service) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, len(s.records))
	for i, record := range s.records {
		records[i] = s.withTransientFields(record)
	}

	return records
}

func (s *Service) Load(ctx context.Context) error {
	if s.store == nil {
		return nil
	}

	records, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	githubIntegrations, err := s.store.ListGitHubIntegrations(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = make([]Record, len(records))
	s.githubByDomainID = make(map[string]GitHubIntegration, len(githubIntegrations))
	for _, integration := range githubIntegrations {
		s.githubByDomainID[integration.DomainID] = integration.GitHubIntegration
	}
	for i, record := range records {
		s.records[i] = s.withTransientFields(record)
	}

	return nil
}

func (s *Service) Delete(ctx context.Context, id string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, record := range s.records {
		if record.ID != id {
			continue
		}

		if s.store != nil {
			if err := s.store.DeleteGitHubIntegration(ctx, record.ID); err != nil {
				return Record{}, false, err
			}
			if err := s.store.Delete(ctx, record.ID); err != nil {
				return Record{}, false, err
			}
		}

		delete(s.githubByDomainID, record.ID)
		s.records = append(s.records[:i], s.records[i+1:]...)
		return record, true, nil
	}

	return Record{}, false, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Record, error) {
	hostname, kind, target, nodeJSScript, err := normalizeAndValidateInput(input.Hostname, input.Kind, input.Target, input.NodeJSScript)
	if err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range s.records {
		if record.Hostname == hostname {
			return Record{}, ErrDuplicateHostname
		}
	}

	resolvedTarget, err := s.deriveTarget(hostname, kind, target)
	if err != nil {
		return Record{}, err
	}

	record := Record{
		ID:           fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()),
		Hostname:     hostname,
		Kind:         kind,
		Target:       resolvedTarget,
		NodeJSScript: nodeJSScript,
		CacheEnabled: input.CacheEnabled,
		CreatedAt:    time.Now().UTC(),
	}
	record = s.withTransientFields(record)

	if s.store != nil {
		if err := s.store.Insert(ctx, record); err != nil {
			return Record{}, err
		}
	}

	s.insertRecordLocked(record)

	return record, nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Record, Record, error) {
	hostname, kind, target, nodeJSScript, err := normalizeAndValidateInput(input.Hostname, input.Kind, input.Target, input.NodeJSScript)
	if err != nil {
		return Record{}, Record{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, current, ok := s.findRecordLocked(id)
	if !ok {
		return Record{}, Record{}, ErrNotFound
	}

	if hostname != current.Hostname {
		return Record{}, Record{}, ValidationErrors{
			"hostname": "Domain cannot be changed after creation.",
		}
	}

	resolvedTarget, err := s.deriveTarget(current.Hostname, kind, target)
	if err != nil {
		return Record{}, Record{}, err
	}

	updated := current
	updated.Kind = kind
	updated.Target = resolvedTarget
	updated.NodeJSScript = nodeJSScript
	updated.CacheEnabled = input.CacheEnabled
	updated = s.withTransientFields(updated)

	if s.store != nil {
		if err := s.store.Update(ctx, updated); err != nil {
			return Record{}, Record{}, err
		}
	}

	s.records[index] = updated

	return updated, current, nil
}

func (s *Service) Restore(ctx context.Context, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record.Kind, record.Target = normalizePersistedKindAndTarget(record.Kind, record.Target)
	record.NodeJSScript = normalizePersistedNodeJSScript(record.Kind, record.NodeJSScript)
	record = s.withTransientFields(record)

	index, _, exists := s.findRecordLocked(record.ID)
	for _, existing := range s.records {
		if existing.ID != record.ID && existing.Hostname == record.Hostname {
			return ErrDuplicateHostname
		}
	}

	if s.store != nil {
		var err error
		if exists {
			err = s.store.Update(ctx, record)
		} else {
			err = s.store.Insert(ctx, record)
		}
		if err != nil {
			return err
		}
		if record.GitHub == nil {
			if err := s.store.DeleteGitHubIntegration(ctx, record.ID); err != nil {
				return err
			}
		} else {
			if err := s.store.UpsertGitHubIntegration(ctx, record.ID, *record.GitHub); err != nil {
				return err
			}
		}
	}

	if record.GitHub == nil {
		delete(s.githubByDomainID, record.ID)
	} else {
		s.githubByDomainID[record.ID] = *record.GitHub
	}

	if exists {
		s.records = append(s.records[:index], s.records[index+1:]...)
	}
	s.insertRecordLocked(record)

	return nil
}

func normalizeHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func validateKind(kind Kind) string {
	switch kind {
	case KindStaticSite, KindPHP, KindNodeJS, KindPython, KindReverseProxy:
		return ""
	default:
		return "Select a valid domain type."
	}
}

func validateHostname(value string) string {
	if value == "" {
		return "Domain is required."
	}

	if strings.Contains(value, "://") {
		return "Enter a domain, not a full URL."
	}

	if strings.ContainsAny(value, "/ \t\n\r") {
		return "Domain must not contain spaces or paths."
	}

	for _, char := range value {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if isLetter || isDigit || char == '.' || char == '-' {
			continue
		}

		return "Domain can contain only letters, numbers, dots, and hyphens."
	}

	if len(value) > 253 {
		return "Enter a valid domain like example.com."
	}

	if !hostnamePattern.MatchString(value) {
		return "Enter a valid domain like example.com."
	}

	return ""
}

func validateTarget(kind Kind, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "Target is required."
	}

	switch kind {
	case KindNodeJS, KindPython:
		if _, err := normalizeNodeJSTarget(trimmed); err != nil {
			return err.Error()
		}
	case KindReverseProxy:
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "Enter a full upstream URL starting with http:// or https://."
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "Enter a full upstream URL starting with http:// or https://."
		}
		if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "Enter an upstream origin without credentials, paths, queries, or fragments."
		}
	}

	return ""
}

func normalizeAndValidateInput(hostname string, kind Kind, target string, nodeJSScript string) (string, Kind, string, string, error) {
	normalizedHostname := normalizeHostname(hostname)
	normalizedKind, trimmedTarget := normalizePersistedKindAndTarget(kind, target)
	normalizedNodeJSScript := normalizePersistedNodeJSScript(normalizedKind, nodeJSScript)

	validation := ValidationErrors{}

	if message := validateKind(normalizedKind); message != "" {
		validation["kind"] = message
	}

	if message := validateHostname(normalizedHostname); message != "" {
		validation["hostname"] = message
	}

	if usesUpstreamTarget(normalizedKind) {
		if message := validateTarget(normalizedKind, trimmedTarget); message != "" {
			validation["target"] = message
		}
	}
	if usesScriptPath(normalizedKind) {
		if message := validateNodeJSScript(normalizedNodeJSScript); message != "" {
			validation["nodejs_script_path"] = message
		}
	}

	if len(validation) > 0 {
		return "", "", "", "", validation
	}

	return normalizedHostname, normalizedKind, trimmedTarget, normalizedNodeJSScript, nil
}

func (s *Service) findRecordLocked(id string) (int, Record, bool) {
	for i, record := range s.records {
		if record.ID == id {
			return i, record, true
		}
	}

	return -1, Record{}, false
}

func (s *Service) insertRecordLocked(record Record) {
	record = s.withLogPaths(record)

	index := len(s.records)
	for i, existing := range s.records {
		if record.CreatedAt.After(existing.CreatedAt) ||
			(record.CreatedAt.Equal(existing.CreatedAt) && record.ID > existing.ID) {
			index = i
			break
		}
	}

	s.records = append(s.records, Record{})
	copy(s.records[index+1:], s.records[index:])
	s.records[index] = record
}

func (s *Service) withTransientFields(record Record) Record {
	record = s.withLogPaths(record)
	if integration, ok := s.githubByDomainID[record.ID]; ok {
		copyIntegration := integration
		record.GitHub = &copyIntegration
	} else {
		record.GitHub = nil
	}

	return record
}

func (s *Service) withLogPaths(record Record) Record {
	host := normalizeHostname(record.Hostname)
	if host == "" {
		record.Logs = LogPaths{}
		return record
	}

	logDir := filepath.Join(s.logsBasePath, host)
	record.Logs = LogPaths{
		Directory: logDir,
		Access:    filepath.Join(logDir, "access.log"),
		Error:     filepath.Join(logDir, "error.log"),
	}

	return record
}

func (s *Service) UpsertGitHubIntegration(
	ctx context.Context,
	hostname string,
	integration GitHubIntegration,
) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, record, ok := s.findRecordByHostnameLocked(hostname)
	if !ok {
		return Record{}, ErrNotFound
	}

	if s.store != nil {
		if err := s.store.UpsertGitHubIntegration(ctx, record.ID, integration); err != nil {
			return Record{}, err
		}
	}

	s.githubByDomainID[record.ID] = integration
	updated := s.withTransientFields(record)
	s.records[index] = updated

	return updated, nil
}

func (s *Service) DeleteGitHubIntegration(ctx context.Context, hostname string) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, record, ok := s.findRecordByHostnameLocked(hostname)
	if !ok {
		return Record{}, ErrNotFound
	}

	if s.store != nil {
		if err := s.store.DeleteGitHubIntegration(ctx, record.ID); err != nil {
			return Record{}, err
		}
	}

	delete(s.githubByDomainID, record.ID)
	updated := s.withTransientFields(record)
	s.records[index] = updated

	return updated, nil
}

func (s *Service) UpdatePHPSettings(
	ctx context.Context,
	hostname string,
	input UpdatePHPInput,
) (Record, error) {
	if s == nil {
		return Record{}, ErrNotFound
	}

	validation := phpenv.ValidateUpdateSettingsInput(input.UpdateSettingsInput)
	if len(validation) > 0 {
		return Record{}, ValidationErrors(validation)
	}
	if normalizedVersion := strings.TrimSpace(input.PHPVersion); normalizedVersion != "" {
		if phpenv.NormalizeVersion(normalizedVersion) == "" {
			return Record{}, ValidationErrors{
				"php_version": "Select a supported PHP version.",
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, record, ok := s.findRecordByHostnameLocked(hostname)
	if !ok {
		return Record{}, ErrNotFound
	}
	if record.Kind != KindPHP {
		return Record{}, ValidationErrors{
			"kind": "PHP settings are available only for PHP site domains.",
		}
	}

	record.PHPVersion = phpenv.NormalizeVersion(input.PHPVersion)
	record.PHPSettings = phpenv.NormalizeUpdateSettingsInput(input.UpdateSettingsInput)
	record = s.withTransientFields(record)

	if s.store != nil {
		if err := s.store.Update(ctx, record); err != nil {
			return Record{}, err
		}
	}

	s.records[index] = record
	return record, nil
}

func (s *Service) findRecordByHostnameLocked(hostname string) (int, Record, bool) {
	normalizedHostname := normalizeHostname(hostname)
	for i, record := range s.records {
		if normalizeHostname(record.Hostname) == normalizedHostname {
			return i, record, true
		}
	}

	return -1, Record{}, false
}

func (s *Service) deriveTarget(hostname string, kind Kind, target string) (string, error) {
	siteRoot, err := s.ensureSiteRoot(hostname)
	if err != nil {
		return "", err
	}

	switch kind {
	case KindStaticSite:
		if err := ensureStaticSiteIndex(siteRoot, hostname); err != nil {
			return "", err
		}
		return siteRoot, nil
	case KindPHP:
		if err := ensurePHPSiteIndex(siteRoot, hostname); err != nil {
			return "", err
		}
		return siteRoot, nil
	case KindNodeJS, KindPython, KindReverseProxy:
		return target, nil
	default:
		return "", fmt.Errorf("unsupported domain kind %q", kind)
	}
}

func normalizePersistedKindAndTarget(kind Kind, target string) (Kind, string) {
	trimmedTarget := strings.TrimSpace(target)
	if usesScriptPath(kind) {
		if normalizedTarget, err := normalizeNodeJSTarget(trimmedTarget); err == nil {
			return kind, normalizedTarget
		}
		return kind, trimmedTarget
	}
	if kind != "App" {
		return kind, trimmedTarget
	}

	port, err := strconv.Atoi(trimmedTarget)
	if err != nil || port < 1 || port > 65535 {
		return KindReverseProxy, trimmedTarget
	}

	return KindReverseProxy, fmt.Sprintf("http://127.0.0.1:%d", port)
}

func validateNodeJSScript(value string) string {
	if value == "" {
		return "Script path is required."
	}

	return ""
}

func normalizePersistedNodeJSScript(kind Kind, value string) string {
	if !usesScriptPath(kind) {
		return ""
	}

	return normalizeNodeJSScript(value)
}

func normalizeNodeJSScript(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	normalized := filepath.ToSlash(filepath.Clean(trimmed))
	switch normalized {
	case ".", "/", "":
		return ""
	}
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return ""
	}
	if strings.HasPrefix(normalized, "/") {
		return ""
	}

	return normalized
}

func normalizeNodeJSTarget(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return "", errors.New("Port is required.")
	}

	port, err := strconv.Atoi(trimmed)
	if err == nil {
		if port < 1 || port > 65535 {
			return "", errors.New("Enter a port between 1 and 65535.")
		}
		return fmt.Sprintf("http://127.0.0.1:%d", port), nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("Enter a port between 1 and 65535.")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("Enter a port between 1 and 65535.")
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("Enter a port between 1 and 65535.")
	}

	return trimmed, nil
}

func ResolveNodeJSScriptPath(basePath string, record Record) (string, error) {
	if !usesScriptPath(record.Kind) {
		return "", fmt.Errorf("unsupported domain kind %q", record.Kind)
	}

	scriptPath := normalizePersistedNodeJSScript(record.Kind, record.NodeJSScript)
	if scriptPath == "" {
		return "", errors.New("nodejs script path is not configured")
	}

	documentRoot, err := ResolveDocumentRoot(basePath, record)
	if err != nil {
		return "", err
	}

	resolved := filepath.Clean(filepath.Join(documentRoot, filepath.FromSlash(scriptPath)))
	relativePath, err := filepath.Rel(documentRoot, resolved)
	if err != nil {
		return "", err
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", errors.New("nodejs script path must stay inside the domain root")
	}

	return resolved, nil
}

func usesScriptPath(kind Kind) bool {
	return kind == KindNodeJS || kind == KindPython
}

func (s *Service) ensureSiteRoot(hostname string) (string, error) {
	siteRoot := filepath.Join(s.basePath, hostname)
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		return "", fmt.Errorf("create site directory: %w", err)
	}

	return siteRoot, nil
}

func ensureStaticSiteIndex(siteRoot string, hostname string) error {
	indexPath := filepath.Join(siteRoot, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat site index: %w", err)
	}

	if err := os.WriteFile(indexPath, []byte(staticSiteIndexContent(hostname)), 0o644); err != nil {
		return fmt.Errorf("create site index: %w", err)
	}

	return nil
}

func ensurePHPSiteIndex(siteRoot string, hostname string) error {
	indexPath := filepath.Join(siteRoot, "index.php")
	if _, err := os.Stat(indexPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat php site index: %w", err)
	}

	if err := os.WriteFile(indexPath, []byte(phpSiteIndexContent(hostname)), 0o644); err != nil {
		return fmt.Errorf("create php site index: %w", err)
	}

	return nil
}

func staticSiteIndexContent(hostname string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s</title>
  <style>
    :root {
      color-scheme: dark;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0b1220;
      color: #e2e8f0;
    }

    * {
      box-sizing: border-box;
    }

    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background:
        radial-gradient(circle at top, rgba(37, 99, 235, 0.35), transparent 40%%),
        linear-gradient(180deg, #0f172a 0%%, #020617 100%%);
      padding: 24px;
    }

    main {
      width: min(720px, 100%%);
      border: 1px solid rgba(148, 163, 184, 0.2);
      border-radius: 20px;
      padding: 32px;
      background: rgba(15, 23, 42, 0.82);
      box-shadow: 0 24px 80px rgba(15, 23, 42, 0.45);
      backdrop-filter: blur(12px);
    }

    p {
      margin: 0 0 12px;
      line-height: 1.6;
      color: #cbd5e1;
    }

    .eyebrow {
      text-transform: uppercase;
      letter-spacing: 0.12em;
      font-size: 12px;
      color: #93c5fd;
    }

    h1 {
      margin: 0 0 16px;
      font-size: clamp(2rem, 4vw, 3.5rem);
      line-height: 1;
    }

    code {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.95em;
      color: #bfdbfe;
    }
  </style>
</head>
<body>
  <main>
    <p class="eyebrow">FlowPanel static site</p>
    <h1>%s</h1>
    <p>This domain is ready to serve static content.</p>
    <p>Replace <code>index.html</code> in this folder with your own site files.</p>
  </main>
</body>
</html>
`, hostname, hostname)
}

func phpSiteIndexContent(hostname string) string {
	return fmt.Sprintf(`<?php
declare(strict_types=1);

$hostname = %q;
?>
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title><?= htmlspecialchars($hostname, ENT_QUOTES, 'UTF-8') ?></title>
</head>
<body>
  <h1><?= htmlspecialchars($hostname, ENT_QUOTES, 'UTF-8') ?></h1>
  <p>PHP is working.</p>
</body>
</html>
`, hostname)
}
