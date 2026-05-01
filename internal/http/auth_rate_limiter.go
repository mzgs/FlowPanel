package httpx

import (
	"net"
	stdhttp "net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/auth"
)

const (
	authRateLimitMaxAttempts = 10
	authRateLimitWindow      = 15 * time.Minute
	authRateLimitLockout     = 15 * time.Minute
)

type authRateLimitEntry struct {
	attempts     int
	firstAttempt time.Time
	blockedUntil time.Time
}

type authRateLimiter struct {
	mu          sync.Mutex
	entries     map[string]authRateLimitEntry
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
	now         func() time.Time
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{
		entries:     make(map[string]authRateLimitEntry),
		maxAttempts: authRateLimitMaxAttempts,
		window:      authRateLimitWindow,
		lockout:     authRateLimitLockout,
		now:         time.Now,
	}
}

func (l *authRateLimiter) Allow(keys ...string) (time.Duration, bool) {
	if l == nil {
		return 0, true
	}

	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	var retryAfter time.Duration
	for _, key := range keys {
		entry, ok := l.entries[key]
		if !ok {
			continue
		}
		if entry.blockedUntil.After(now) {
			if wait := entry.blockedUntil.Sub(now); wait > retryAfter {
				retryAfter = wait
			}
			continue
		}
		if now.Sub(entry.firstAttempt) > l.window {
			delete(l.entries, key)
		}
	}

	return retryAfter, retryAfter <= 0
}

func (l *authRateLimiter) RecordFailure(keys ...string) {
	if l == nil {
		return
	}

	now := l.now().UTC()
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, key := range keys {
		entry := l.entries[key]
		if entry.firstAttempt.IsZero() || now.Sub(entry.firstAttempt) > l.window {
			entry = authRateLimitEntry{firstAttempt: now}
		}

		entry.attempts++
		if entry.attempts >= l.maxAttempts {
			entry.blockedUntil = now.Add(l.lockout)
		}
		l.entries[key] = entry
	}
}

func (l *authRateLimiter) Clear(keys ...string) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, key := range keys {
		delete(l.entries, key)
	}
}

func authRateLimitKeys(r *stdhttp.Request, input auth.LoginInput) []string {
	address := clientIPAddress(r)
	username := auth.NormalizeUsername(input.Username)
	if username == "" {
		return []string{"auth-ip:" + address}
	}

	return []string{
		"auth-ip:" + address,
		"auth-login:" + address + ":" + username,
	}
}

func clientIPAddress(r *stdhttp.Request) string {
	if r == nil {
		return "unknown"
	}

	address := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	if address == "" {
		return "unknown"
	}

	return address
}

func writeAuthRateLimited(w stdhttp.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}

	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, stdhttp.StatusTooManyRequests, map[string]any{
		"error": "too many sign-in attempts; try again later",
	})
}
