package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/caddy"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	domainPreviewPrefix       = "/domain-preview/"
	domainPreviewMaxBodyBytes = 8 << 20
)

var (
	domainPreviewHTMLRootURL = regexp.MustCompile(`(?i)(\b(?:href|src|action|poster)=['"])(/[^'"]*)`)
	domainPreviewCSSRootURL  = regexp.MustCompile(`(?i)(url\(\s*['"]?)(/[^)'"\s]*)`)
)

func newDomainLivePreviewHandler(app *app.App) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "hostname")))
		if app == nil || app.Domains == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "domains are not configured"})
			return
		}
		if _, ok := app.Domains.FindByHostname(hostname); !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "domain not found"})
			return
		}
		if app.Caddy == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "embedded caddy runtime is not configured"})
			return
		}

		previewAddress, err := app.Caddy.PreviewAddress()
		if err != nil {
			status := stdhttp.StatusBadGateway
			if errorsIsRuntimeNotStarted(err) {
				status = stdhttp.StatusServiceUnavailable
			}
			writeJSON(w, status, map[string]any{"error": "domain preview is unavailable"})
			return
		}

		prefix := domainPreviewPrefix + url.PathEscape(hostname)
		if r.URL.Path == prefix {
			stdhttp.Redirect(w, r, prefix+"/", stdhttp.StatusTemporaryRedirect)
			return
		}

		target, _ := url.Parse("http://" + previewAddress)
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = &stdhttp.Transport{DisableCompression: true}
		proxy.ErrorHandler = func(w stdhttp.ResponseWriter, _ *stdhttp.Request, proxyErr error) {
			app.Logger.Error("proxy domain preview failed", zap.String("hostname", hostname), zap.Error(proxyErr))
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": "failed to load domain preview"})
		}
		proxy.ModifyResponse = func(response *stdhttp.Response) error {
			return rewriteDomainPreviewResponse(response, hostname, prefix)
		}

		originalDirector := proxy.Director
		proxy.Director = func(request *stdhttp.Request) {
			originalDirector(request)
			request.Host = hostname
			request.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
			if request.URL.Path == "" {
				request.URL.Path = "/"
			}
			request.URL.RawPath = ""
			request.Header.Del("Authorization")
			request.Header.Del("Cookie")
			request.Header.Del("Origin")
			request.Header.Del("Referer")
			request.Header.Del("Accept-Encoding")
		}
		proxy.ServeHTTP(w, r)
	})
}

func errorsIsRuntimeNotStarted(err error) bool {
	return errors.Is(err, caddy.ErrRuntimeNotStarted)
}

func rewriteDomainPreviewResponse(response *stdhttp.Response, hostname, prefix string) error {
	response.Header.Del("Set-Cookie")
	response.Header.Set("Cache-Control", "no-store")
	response.Header.Set("Content-Security-Policy", "sandbox allow-scripts; default-src * data: blob: 'unsafe-inline' 'unsafe-eval'; connect-src * data: blob:; img-src * data: blob:; media-src * data: blob:; style-src * 'unsafe-inline'; frame-ancestors 'self';")
	response.Header.Set("X-Frame-Options", "SAMEORIGIN")
	if location := strings.TrimSpace(response.Header.Get("Location")); location != "" {
		response.Header.Set("Location", rewriteDomainPreviewLocation(location, hostname, prefix))
	}

	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if (response.Request != nil && response.Request.Method == stdhttp.MethodHead) || response.Body == nil || !isDomainPreviewTextContent(contentType) {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, domainPreviewMaxBodyBytes+1))
	if err != nil {
		return err
	}
	if len(body) > domainPreviewMaxBodyBytes {
		response.Body = &domainPreviewReadCloser{
			Reader: io.MultiReader(bytes.NewReader(body), response.Body),
			Closer: response.Body,
		}
		return nil
	}
	_ = response.Body.Close()

	rewritten := rewriteDomainPreviewBody(string(body), hostname, prefix, strings.Contains(contentType, "text/html"), strings.Contains(contentType, "text/css"))
	response.Body = io.NopCloser(strings.NewReader(rewritten))
	response.ContentLength = int64(len(rewritten))
	response.Header.Set("Content-Length", fmt.Sprintf("%d", len(rewritten)))
	response.Header.Del("Content-Encoding")
	return nil
}

type domainPreviewReadCloser struct {
	io.Reader
	io.Closer
}

func isDomainPreviewTextContent(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "javascript") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml")
}

func rewriteDomainPreviewBody(body, hostname, prefix string, html, css bool) string {
	for _, origin := range []string{"https://" + hostname, "http://" + hostname, "//" + hostname} {
		body = strings.ReplaceAll(body, origin, prefix)
	}
	if html {
		body = domainPreviewHTMLRootURL.ReplaceAllStringFunc(body, func(value string) string {
			quoteIndex := strings.IndexAny(value, "'\"")
			if quoteIndex < 0 {
				return value
			}
			path := value[quoteIndex+1:]
			if strings.HasPrefix(path, "//") || strings.HasPrefix(path, prefix+"/") {
				return value
			}
			return value[:quoteIndex+1] + prefix + value[quoteIndex+1:]
		})
		base := `<base href="` + prefix + `/">`
		lower := strings.ToLower(body)
		if index := strings.Index(lower, "<head>"); index >= 0 {
			body = body[:index+len("<head>")] + base + body[index+len("<head>"):]
		} else {
			body = base + body
		}
	}
	if css || html {
		body = domainPreviewCSSRootURL.ReplaceAllStringFunc(body, func(value string) string {
			slashIndex := strings.Index(value, "/")
			if slashIndex < 0 {
				return value
			}
			path := value[slashIndex:]
			if strings.HasPrefix(path, "//") || strings.HasPrefix(path, prefix+"/") {
				return value
			}
			return value[:slashIndex] + prefix + value[slashIndex:]
		})
	}
	return body
}

func rewriteDomainPreviewLocation(location, hostname, prefix string) string {
	parsed, err := url.Parse(location)
	if err != nil {
		return location
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Hostname(), hostname) {
		return location
	}
	if parsed.Host == "" && !strings.HasPrefix(parsed.Path, "/") {
		return location
	}
	if parsed.Host == "" && strings.HasPrefix(parsed.Path, prefix+"/") {
		return location
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return prefix + path + parsedQueryAndFragment(parsed)
}

func parsedQueryAndFragment(value *url.URL) string {
	suffix := ""
	if value.RawQuery != "" {
		suffix += "?" + value.RawQuery
	}
	if value.Fragment != "" {
		suffix += "#" + value.EscapedFragment()
	}
	return suffix
}
