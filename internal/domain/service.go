package domain

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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

type Kind string

const (
	KindStaticSite   Kind = "Static site"
	KindPHP          Kind = "Php site"
	KindApp          Kind = "App"
	KindReverseProxy Kind = "Reverse proxy"
)

type Record struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Kind      Kind      `json:"kind"`
	Target    string    `json:"target"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	Hostname string `json:"hostname"`
	Kind     Kind   `json:"kind"`
	Target   string `json:"target"`
}

type ValidationErrors map[string]string

func (e ValidationErrors) Error() string {
	return "validation failed"
}

type Service struct {
	basePath string
	mu       sync.RWMutex
	records  []Record
}

func NewService() *Service {
	return newService(defaultSitesBasePath())
}

func newService(basePath string) *Service {
	return &Service{
		basePath: strings.TrimSpace(basePath),
		records:  make([]Record, 0),
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

func (s *Service) Create(input CreateInput) (Record, error) {
	hostname := normalizeHostname(input.Hostname)
	target := strings.TrimSpace(input.Target)

	validation := ValidationErrors{}

	if message := validateKind(input.Kind); message != "" {
		validation["kind"] = message
	}

	if message := validateHostname(hostname); message != "" {
		validation["hostname"] = message
	}

	if input.Kind == KindApp || input.Kind == KindReverseProxy {
		if message := validateTarget(input.Kind, target); message != "" {
			validation["target"] = message
		}
	}

	if len(validation) > 0 {
		return Record{}, validation
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
		ID:        fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano()),
		Hostname:  hostname,
		Kind:      input.Kind,
		Target:    resolvedTarget,
		CreatedAt: time.Now().UTC(),
	}

	s.records = append([]Record{record}, s.records...)

	return record, nil
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
		return "Hostname is required."
	}

	if strings.Contains(value, "://") {
		return "Enter a hostname, not a full URL."
	}

	if strings.ContainsAny(value, "/ \t\n\r") {
		return "Hostname must not contain spaces or paths."
	}

	for _, char := range value {
		isLetter := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'
		if isLetter || isDigit || char == '.' || char == '-' {
			continue
		}

		return "Hostname can contain only letters, numbers, dots, and hyphens."
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
	}

	return ""
}

func (s *Service) deriveTarget(hostname string, kind Kind, target string) (string, error) {
	switch kind {
	case KindStaticSite:
		siteRoot := filepath.Join(s.basePath, hostname)
		if err := os.MkdirAll(siteRoot, 0o755); err != nil {
			return "", fmt.Errorf("create site directory: %w", err)
		}
		return siteRoot, nil
	case KindPHP:
		publicRoot := filepath.Join(s.basePath, hostname, "public")
		if err := os.MkdirAll(publicRoot, 0o755); err != nil {
			return "", fmt.Errorf("create php public directory: %w", err)
		}
		return publicRoot, nil
	case KindApp, KindReverseProxy:
		return target, nil
	default:
		return "", fmt.Errorf("unsupported domain kind %q", kind)
	}
}
