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
	KindApp          Kind = "App"
	KindReverseProxy Kind = "Reverse proxy"
)

type Record struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	Kind         Kind      `json:"kind"`
	Target       string    `json:"target"`
	CacheEnabled bool      `json:"cache_enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateInput struct {
	Hostname     string `json:"hostname"`
	Kind         Kind   `json:"kind"`
	Target       string `json:"target"`
	CacheEnabled bool   `json:"cache_enabled"`
}

type UpdateInput struct {
	Hostname     string `json:"hostname"`
	Kind         Kind   `json:"kind"`
	Target       string `json:"target"`
	CacheEnabled bool   `json:"cache_enabled"`
}

type ValidationErrors map[string]string

func (e ValidationErrors) Error() string {
	return "validation failed"
}

type Service struct {
	basePath         string
	store            *Store
	previewCachePath string
	previewTTL       time.Duration
	previewGenerator PreviewGenerator
	now              func() time.Time
	mu               sync.RWMutex
	previewMu        sync.Mutex
	records          []Record
}

func NewService(store *Store) *Service {
	return newService(defaultSitesBasePath(), store)
}

func newService(basePath string, store *Store) *Service {
	return &Service{
		basePath:         strings.TrimSpace(basePath),
		store:            store,
		previewCachePath: defaultPreviewCachePath(),
		previewTTL:       defaultPreviewTTL(),
		previewGenerator: defaultPreviewGenerator(),
		now:              time.Now,
		records:          make([]Record, 0),
	}
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

func (s *Service) BasePath() string {
	return s.basePath
}

func (s *Service) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]Record, len(s.records))
	copy(records, s.records)

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

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = append([]Record(nil), records...)

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
			if err := s.store.Delete(ctx, record.ID); err != nil {
				return Record{}, false, err
			}
		}

		s.records = append(s.records[:i], s.records[i+1:]...)
		return record, true, nil
	}

	return Record{}, false, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Record, error) {
	hostname, target, err := normalizeAndValidateInput(input.Hostname, input.Kind, input.Target)
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

	resolvedTarget, err := s.deriveTarget(hostname, input.Kind, target)
	if err != nil {
		return Record{}, err
	}

	record := Record{
		ID:           fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()),
		Hostname:     hostname,
		Kind:         input.Kind,
		Target:       resolvedTarget,
		CacheEnabled: input.CacheEnabled,
		CreatedAt:    time.Now().UTC(),
	}

	if s.store != nil {
		if err := s.store.Insert(ctx, record); err != nil {
			return Record{}, err
		}
	}

	s.insertRecordLocked(record)

	return record, nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Record, Record, error) {
	hostname, target, err := normalizeAndValidateInput(input.Hostname, input.Kind, input.Target)
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

	resolvedTarget, err := s.deriveTarget(current.Hostname, input.Kind, target)
	if err != nil {
		return Record{}, Record{}, err
	}

	updated := current
	updated.Kind = input.Kind
	updated.Target = resolvedTarget
	updated.CacheEnabled = input.CacheEnabled

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
	case KindStaticSite, KindPHP, KindApp, KindReverseProxy:
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
	case KindApp:
		port, err := strconv.Atoi(trimmed)
		if err != nil || port < 1 || port > 65535 {
			return "Enter a valid port between 1 and 65535."
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

func normalizeAndValidateInput(hostname string, kind Kind, target string) (string, string, error) {
	normalizedHostname := normalizeHostname(hostname)
	trimmedTarget := strings.TrimSpace(target)

	validation := ValidationErrors{}

	if message := validateKind(kind); message != "" {
		validation["kind"] = message
	}

	if message := validateHostname(normalizedHostname); message != "" {
		validation["hostname"] = message
	}

	if kind == KindApp || kind == KindReverseProxy {
		if message := validateTarget(kind, trimmedTarget); message != "" {
			validation["target"] = message
		}
	}

	if len(validation) > 0 {
		return "", "", validation
	}

	return normalizedHostname, trimmedTarget, nil
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

func (s *Service) deriveTarget(hostname string, kind Kind, target string) (string, error) {
	switch kind {
	case KindStaticSite:
		siteRoot := filepath.Join(s.basePath, hostname)
		if err := os.MkdirAll(siteRoot, 0o755); err != nil {
			return "", fmt.Errorf("create site directory: %w", err)
		}
		if err := ensureStaticSiteIndex(siteRoot, hostname); err != nil {
			return "", err
		}
		return siteRoot, nil
	case KindPHP:
		siteRoot := filepath.Join(s.basePath, hostname)
		if err := os.MkdirAll(siteRoot, 0o755); err != nil {
			return "", fmt.Errorf("create php site directory: %w", err)
		}
		if err := ensurePHPSiteIndex(siteRoot, hostname); err != nil {
			return "", err
		}
		return siteRoot, nil
	case KindApp, KindReverseProxy:
		return target, nil
	default:
		return "", fmt.Errorf("unsupported domain kind %q", kind)
	}
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
