package httpx

import (
	"fmt"
	"net"
	stdhttp "net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"flowpanel/internal/app"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func Recoverer(logger *zap.Logger) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						zap.String("request_id", chimiddleware.GetReqID(r.Context())),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
						zap.Any("panic", recovered),
						zap.ByteString("stack", debug.Stack()),
					)

					stdhttp.Error(w, fmt.Sprintf("%d %s", stdhttp.StatusInternalServerError, stdhttp.StatusText(stdhttp.StatusInternalServerError)), stdhttp.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func SameOriginProtection(app *app.App) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if isSafeRequestMethod(r.Method) || requestHasTrustedOrigin(app, r) ||
				(requestHasOpaqueOrigin(r) && validDomainPreviewRequest(app, r)) {
				next.ServeHTTP(w, r)
				return
			}

			writeJSON(w, stdhttp.StatusForbidden, map[string]any{"error": "request origin is not trusted"})
		})
	}
}

func requestHasOpaqueOrigin(r *stdhttp.Request) bool {
	return r != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("Origin")), "null")
}

func isSafeRequestMethod(method string) bool {
	switch method {
	case stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodOptions:
		return true
	default:
		return false
	}
}

func requestHasTrustedOrigin(app *app.App, r *stdhttp.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if origin == "" {
		return true
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return false
	}
	if isDevelopmentLoopbackOrigin(app, r, parsedOrigin) {
		return true
	}

	allowed := map[string]struct{}{}
	if requestOrigin := normalizedRequestOrigin(r); requestOrigin != "" {
		allowed[requestOrigin] = struct{}{}
	}
	if panelOrigin := configuredPanelOrigin(app, r); panelOrigin != "" {
		allowed[panelOrigin] = struct{}{}
	}

	_, ok := allowed[normalizedURLOrigin(parsedOrigin)]
	return ok
}

func isDevelopmentLoopbackOrigin(app *app.App, r *stdhttp.Request, origin *url.URL) bool {
	if app != nil && app.Config.IsProduction() {
		return false
	}

	return isLoopbackHostname(origin.Hostname()) && isLoopbackHostname(requestHostname(r))
}

func normalizedRequestOrigin(r *stdhttp.Request) string {
	if r == nil {
		return ""
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	}

	return normalizedURLOrigin(&url.URL{
		Scheme: scheme,
		Host:   r.Host,
	})
}

func configuredPanelOrigin(app *app.App, r *stdhttp.Request) string {
	if app == nil || app.Settings == nil || r == nil {
		return ""
	}

	record, err := app.Settings.Get(r.Context())
	if err != nil || strings.TrimSpace(record.PanelURL) == "" {
		return ""
	}

	parsed, err := url.Parse(record.PanelURL)
	if err != nil {
		return ""
	}

	return normalizedURLOrigin(parsed)
}

func normalizedURLOrigin(value *url.URL) string {
	if value == nil {
		return ""
	}

	scheme := strings.ToLower(strings.TrimSpace(value.Scheme))
	hostname := strings.ToLower(strings.Trim(strings.TrimSpace(value.Hostname()), "[]"))
	if scheme == "" || hostname == "" {
		return ""
	}

	port := value.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	if port == "" {
		return scheme + "://" + hostname
	}

	return scheme + "://" + net.JoinHostPort(hostname, port)
}

func requestHostname(r *stdhttp.Request) string {
	if r == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.Host)); err == nil {
		return host
	}

	return strings.Trim(strings.TrimSpace(r.Host), "[]")
}

func isLoopbackHostname(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]"))
	if value == "localhost" {
		return true
	}
	if ip := net.ParseIP(value); ip != nil {
		return ip.IsLoopback()
	}

	return false
}

func firstForwardedValue(value string) string {
	if index := strings.Index(value, ","); index >= 0 {
		value = value[:index]
	}

	return strings.ToLower(strings.TrimSpace(value))
}
