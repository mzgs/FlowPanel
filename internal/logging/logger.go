package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DeRuina/timberjack"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const MaxFileSizeMB = 500

var (
	sharedWriterMu sync.RWMutex
	sharedWriter   io.Writer = os.Stderr
	securityMu     sync.RWMutex
	securityHandle func(SecurityEvent)
	securityOnce   sync.Once
	securityQueue  = make(chan SecurityEvent, 1024)
	securityState  = securityStateManager{
		rateLimitDomains: make(map[string]string),
		autoBanPolicies:  make(map[string]AutoBanPolicy),
		autoBans:         make(map[string]autoBanState),
	}
)

type CaddyWriter struct{}
type SecurityWriter struct{}
type SecurityBlockHandler struct {
	Hostname string `json:"hostname"`
	Action   string `json:"action"`
}
type AutoBanHandler struct {
	Hostname string `json:"hostname"`
}

type SecurityEvent struct {
	Action        string
	Hostname      string
	URI           string
	ClientIP      string
	TransactionID string
	ExpiresAt     time.Time
}

type AutoBanPolicy struct {
	BlockedRequests int
	Window          time.Duration
	Ban             time.Duration
	Allowed         []string
}

type autoBanState struct {
	Attempts    []time.Time
	BannedUntil time.Time
}

type securityStateManager struct {
	mu               sync.Mutex
	rateLimitDomains map[string]string
	autoBanPolicies  map[string]AutoBanPolicy
	autoBans         map[string]autoBanState
}

type sharedWriteCloser struct {
	io.Writer
}

func init() {
	caddy.RegisterModule(CaddyWriter{})
	caddy.RegisterModule(SecurityWriter{})
	caddy.RegisterModule(SecurityBlockHandler{})
	caddy.RegisterModule(AutoBanHandler{})
}

func (CaddyWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.flowpanel",
		New: func() caddy.Module { return new(CaddyWriter) },
	}
}

func (CaddyWriter) String() string {
	return "FlowPanel rotating log"
}

func (CaddyWriter) WriterKey() string {
	return "flowpanel"
}

func (CaddyWriter) OpenWriter() (io.WriteCloser, error) {
	sharedWriterMu.RLock()
	writer := sharedWriter
	sharedWriterMu.RUnlock()
	return sharedWriteCloser{Writer: writer}, nil
}

func (sharedWriteCloser) Close() error {
	return nil
}

func (SecurityWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.flowpanel_security",
		New: func() caddy.Module { return new(SecurityWriter) },
	}
}

func (SecurityWriter) String() string {
	return "FlowPanel security events"
}

func (SecurityWriter) WriterKey() string {
	return "flowpanel-security"
}

func (SecurityWriter) OpenWriter() (io.WriteCloser, error) {
	return securityWriteCloser{}, nil
}

type securityWriteCloser struct{}

func (securityWriteCloser) Write(data []byte) (int, error) {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var entry struct {
			Message       string `json:"msg"`
			Hostname      string `json:"hostname"`
			URI           string `json:"uri"`
			ClientIP      string `json:"client_ip"`
			TransactionID string `json:"unique_id"`
			Zone          string `json:"zone"`
			RemoteIP      string `json:"remote_ip"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}

		event := SecurityEvent{Action: "waf_blocked", Hostname: entry.Hostname, URI: entry.URI, ClientIP: entry.ClientIP, TransactionID: entry.TransactionID}
		if entry.Message == "rate limit exceeded" {
			event.Action = "rate_limited"
			event.Hostname = securityState.rateLimitDomain(entry.Zone)
			event.ClientIP = entry.RemoteIP
		} else if entry.Message != "WAF rule violation detected" {
			continue
		}

		event.Hostname = strings.ToLower(strings.TrimSpace(event.Hostname))
		hostname := event.Hostname
		if host, _, err := net.SplitHostPort(hostname); err == nil {
			event.Hostname = host
		}
		clientIP := strings.TrimSpace(event.ClientIP)
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}
		event.ClientIP = clientIP
		publishSecurityEvent(event)
	}
	return len(data), nil
}

func (securityWriteCloser) Close() error {
	return nil
}

func SetSecurityEventHandler(handler func(SecurityEvent)) {
	securityMu.Lock()
	securityHandle = handler
	securityMu.Unlock()
	securityOnce.Do(func() {
		go func() {
			for event := range securityQueue {
				securityMu.RLock()
				handler := securityHandle
				securityMu.RUnlock()
				if handler != nil {
					handler(event)
				}
			}
		}()
	})
}

func publishSecurityEvent(event SecurityEvent) {
	if event.Hostname == "" {
		return
	}
	select {
	case securityQueue <- event:
	default:
	}
	if event.Action == "waf_blocked" || event.Action == "rate_limited" {
		if banned, ok := securityState.recordBlocked(event); ok {
			select {
			case securityQueue <- banned:
			default:
			}
		}
	}
}

func ConfigureSecurityPolicies(rateLimitDomains map[string]string, autoBanPolicies map[string]AutoBanPolicy) {
	securityState.mu.Lock()
	defer securityState.mu.Unlock()
	securityState.rateLimitDomains = rateLimitDomains
	securityState.autoBanPolicies = autoBanPolicies
	for key := range securityState.autoBans {
		hostname, clientIP, _ := strings.Cut(key, "\x00")
		policy, ok := autoBanPolicies[hostname]
		if !ok || policy.allows(clientIP) {
			delete(securityState.autoBans, key)
		}
	}
}

func (s *securityStateManager) rateLimitDomain(zone string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rateLimitDomains[zone]
}

func (s *securityStateManager) recordBlocked(event SecurityEvent) (SecurityEvent, bool) {
	if event.ClientIP == "" {
		return SecurityEvent{}, false
	}
	now := time.Now().UTC()
	key := event.Hostname + "\x00" + event.ClientIP
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.autoBanPolicies[event.Hostname]
	if !ok || policy.allows(event.ClientIP) {
		return SecurityEvent{}, false
	}
	state := s.autoBans[key]
	if state.BannedUntil.After(now) {
		return SecurityEvent{}, false
	}
	cutoff := now.Add(-policy.Window)
	attempts := state.Attempts[:0]
	for _, attempt := range state.Attempts {
		if attempt.After(cutoff) {
			attempts = append(attempts, attempt)
		}
	}
	state.Attempts = append(attempts, now)
	if len(state.Attempts) < policy.BlockedRequests {
		s.autoBans[key] = state
		return SecurityEvent{}, false
	}
	state.Attempts = nil
	state.BannedUntil = now.Add(policy.Ban)
	s.autoBans[key] = state
	event.Action = "auto_banned"
	event.ExpiresAt = state.BannedUntil
	return event, true
}

func (s *securityStateManager) bannedUntil(hostname, clientIP string) time.Time {
	now := time.Now().UTC()
	key := strings.ToLower(strings.TrimSpace(hostname)) + "\x00" + clientIP
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy, ok := s.autoBanPolicies[strings.ToLower(strings.TrimSpace(hostname))]; !ok || policy.allows(clientIP) {
		return time.Time{}
	}
	state := s.autoBans[key]
	if !state.BannedUntil.After(now) {
		delete(s.autoBans, key)
		return time.Time{}
	}
	return state.BannedUntil
}

func (p AutoBanPolicy) allows(clientIP string) bool {
	address, err := netip.ParseAddr(clientIP)
	if err != nil {
		return false
	}
	for _, value := range p.Allowed {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			if allowed, parseErr := netip.ParseAddr(value); parseErr == nil && allowed == address {
				return true
			}
			continue
		}
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (SecurityBlockHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.flowpanel_security_block", New: func() caddy.Module { return new(SecurityBlockHandler) }}
}

func (h SecurityBlockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, _ caddyhttp.Handler) error {
	publishSecurityEvent(SecurityEvent{Action: h.Action, Hostname: h.Hostname, URI: r.RequestURI, ClientIP: requestIP(r.RemoteAddr)})
	return forbidden(w)
}

func (AutoBanHandler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.flowpanel_auto_ban", New: func() caddy.Module { return new(AutoBanHandler) }}
}

func (h AutoBanHandler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	clientIP := requestIP(r.RemoteAddr)
	expiresAt := securityState.bannedUntil(h.Hostname, clientIP)
	if expiresAt.IsZero() {
		return next.ServeHTTP(w, r)
	}
	publishSecurityEvent(SecurityEvent{Action: "auto_ban_blocked", Hostname: h.Hostname, URI: r.RequestURI, ClientIP: clientIP, ExpiresAt: expiresAt})
	return forbidden(w)
}

func requestIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}

func forbidden(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, "Forbidden")
	return nil
}

func New(env string) (*zap.Logger, error) {
	return newLogger(env)
}

func NewRotating(env, path string) (*zap.Logger, io.Closer, error) {
	writer := &timberjack.Logger{
		Filename:    path,
		MaxSize:     MaxFileSizeMB,
		MaxBackups:  10,
		Compression: "gzip",
		FileMode:    0o644,
	}

	sharedWriterMu.Lock()
	sharedWriter = writer
	sharedWriterMu.Unlock()

	logger, err := newLogger(env, zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return zapcore.NewCore(logEncoder(env), zapcore.AddSync(writer), logLevel(env))
	}))
	if err != nil {
		_ = writer.Close()
		return nil, nil, fmt.Errorf("build rotating logger: %w", err)
	}

	return logger, writer, nil
}

func newLogger(env string, options ...zap.Option) (*zap.Logger, error) {
	var cfg zap.Config

	if env == "production" {
		cfg = zap.NewProductionConfig()
	} else {
		cfg = zap.NewDevelopmentConfig()
	}

	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return cfg.Build(options...)
}

func logEncoder(env string) zapcore.Encoder {
	if env == "production" {
		cfg := zap.NewProductionEncoderConfig()
		cfg.EncodeTime = zapcore.ISO8601TimeEncoder
		return zapcore.NewJSONEncoder(cfg)
	}

	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return zapcore.NewConsoleEncoder(cfg)
}

func logLevel(env string) zapcore.LevelEnabler {
	if env == "production" {
		return zap.InfoLevel
	}
	return zap.DebugLevel
}
