package httpx

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/caddy"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const (
	domainPreviewPrefix       = "/domain-preview/"
	domainPreviewMaxBodyBytes = 8 << 20
	domainPreviewTokenTTL     = 30 * time.Minute
)

var (
	domainPreviewHTMLRootURL = regexp.MustCompile(`(?i)(\b(?:href|src|action|poster)=['"])(/[^'"]*)`)
	domainPreviewCSSRootURL  = regexp.MustCompile(`(?i)(url\(\s*['"]?)(/[^)'"\s]*)`)
)

func newDomainLivePreviewHandler(app *app.App) stdhttp.Handler {
	serve := func(w stdhttp.ResponseWriter, r *stdhttp.Request, hostname, prefix, path string) {
		opaqueOrigin := requestHasOpaqueOrigin(r)
		if opaqueOrigin && r.Method == stdhttp.MethodOptions {
			setDomainPreviewCORSHeaders(w.Header())
			w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			w.WriteHeader(stdhttp.StatusNoContent)
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

		target, _ := url.Parse("http://" + previewAddress)
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.Transport = &stdhttp.Transport{DisableCompression: true}
		proxy.ErrorHandler = func(w stdhttp.ResponseWriter, _ *stdhttp.Request, proxyErr error) {
			app.Logger.Error("proxy domain preview failed", zap.String("hostname", hostname), zap.Error(proxyErr))
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": "failed to load domain preview"})
		}
		proxy.ModifyResponse = func(response *stdhttp.Response) error {
			if opaqueOrigin {
				setDomainPreviewCORSHeaders(response.Header)
			}
			rewriteDomainPreviewResponseCookies(response, hostname, prefix)
			return rewriteDomainPreviewResponse(response, hostname, prefix)
		}

		originalDirector := proxy.Director
		proxy.Director = func(request *stdhttp.Request) {
			hasOrigin := strings.TrimSpace(request.Header.Get("Origin")) != ""
			originalDirector(request)
			request.Host = hostname
			request.URL.Path = path
			request.URL.RawPath = ""
			request.Header.Del("Authorization")
			rewriteDomainPreviewRequestCookies(request, hostname)
			if hasOrigin {
				request.Header.Set("Origin", target.Scheme+"://"+hostname)
			} else {
				request.Header.Del("Origin")
			}
			request.Header.Del("Referer")
			request.Header.Del("Accept-Encoding")
		}
		proxy.ServeHTTP(w, r)
	}

	var entryHandler stdhttp.Handler
	entryHandler = RequirePanelAuth(app)(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		hostname := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "hostname")))
		prefix := domainPreviewPrefix + url.PathEscape(hostname)
		if r.URL.Path == prefix {
			stdhttp.Redirect(w, r, prefix+"/", stdhttp.StatusTemporaryRedirect)
			return
		}
		token := newDomainPreviewToken(hostname, app.Config.Session.Secret, time.Now().Add(domainPreviewTokenTTL))
		stdhttp.Redirect(w, r, prefix+"/"+token+"/", stdhttp.StatusTemporaryRedirect)
	}))

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

		basePrefix := domainPreviewPrefix + url.PathEscape(hostname)
		remainder := strings.TrimPrefix(r.URL.Path, basePrefix)
		if remainder == "" || remainder == "/" {
			entryHandler.ServeHTTP(w, r)
			return
		}

		token, path, found := strings.Cut(strings.TrimPrefix(remainder, "/"), "/")
		if !validDomainPreviewToken(token, hostname, app.Config.Session.Secret, time.Now()) {
			writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{"error": "domain preview authorization expired"})
			return
		}
		prefix := basePrefix + "/" + token
		if !found {
			stdhttp.Redirect(w, r, prefix+"/", stdhttp.StatusTemporaryRedirect)
			return
		}
		serve(w, r, hostname, prefix, "/"+path)
	})
}

func setDomainPreviewCORSHeaders(header stdhttp.Header) {
	header.Set("Access-Control-Allow-Origin", "null")
	header.Set("Access-Control-Allow-Credentials", "true")
	header.Add("Vary", "Origin")
}

func validDomainPreviewRequest(app *app.App, r *stdhttp.Request) bool {
	if app == nil || r == nil || !strings.HasPrefix(r.URL.Path, domainPreviewPrefix) {
		return false
	}
	hostname, remainder, found := strings.Cut(strings.TrimPrefix(r.URL.Path, domainPreviewPrefix), "/")
	if !found {
		return false
	}
	token, _, found := strings.Cut(remainder, "/")
	return found && validDomainPreviewToken(token, strings.ToLower(strings.TrimSpace(hostname)), app.Config.Session.Secret, time.Now())
}

func newDomainPreviewToken(hostname, secret string, expiresAt time.Time) string {
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	signature := hmac.New(sha256.New, []byte(secret))
	_, _ = signature.Write([]byte(hostname + "\n" + expires))
	return expires + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func validDomainPreviewToken(token, hostname, secret string, now time.Time) bool {
	expires, encodedSignature, found := strings.Cut(token, ".")
	unix, err := strconv.ParseInt(expires, 10, 64)
	if !found || err != nil || now.After(time.Unix(unix, 0)) {
		return false
	}
	expected := newDomainPreviewToken(hostname, secret, time.Unix(unix, 0))
	return hmac.Equal([]byte(token), []byte(expected)) && encodedSignature != ""
}

func errorsIsRuntimeNotStarted(err error) bool {
	return errors.Is(err, caddy.ErrRuntimeNotStarted)
}

func rewriteDomainPreviewResponse(response *stdhttp.Response, hostname, prefix string) error {
	response.Header.Set("Cache-Control", "no-store")
	response.Header.Set("Content-Security-Policy", "sandbox allow-forms allow-scripts; default-src * data: blob: 'unsafe-inline' 'unsafe-eval'; connect-src * data: blob:; img-src * data: blob:; media-src * data: blob:; style-src * 'unsafe-inline'; frame-ancestors 'self';")
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

func domainPreviewCookiePrefix(hostname string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(hostname))))
	return fmt.Sprintf("__fp_preview_%x_", sum[:8])
}

func rewriteDomainPreviewRequestCookies(request *stdhttp.Request, hostname string) {
	prefix := domainPreviewCookiePrefix(hostname)
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, prefix) {
			cookie.Name = strings.TrimPrefix(cookie.Name, prefix)
			request.AddCookie(cookie)
		}
	}
}

func rewriteDomainPreviewResponseCookies(response *stdhttp.Response, hostname, pathPrefix string) {
	if len(response.Header.Values("Set-Cookie")) == 0 {
		return
	}
	cookies := response.Cookies()
	response.Header.Del("Set-Cookie")
	namePrefix := domainPreviewCookiePrefix(hostname)
	for _, cookie := range cookies {
		cookie.Name = namePrefix + cookie.Name
		cookie.Domain = ""
		cookie.Path = pathPrefix + "/" + strings.TrimPrefix(cookie.Path, "/")
		response.Header.Add("Set-Cookie", cookie.String())
	}
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
		base := `<base href="` + prefix + `/">` + domainPreviewRuntimeScript(prefix)
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

func domainPreviewRuntimeScript(prefix string) string {
	encodedPrefix, _ := json.Marshal(prefix)
	return `<script>(function(){const p=` + string(encodedPrefix) + `;function s(){const v=Object.create(null),a={get length(){return Object.keys(v).length},key(i){return Object.keys(v)[i]??null},getItem(k){k=String(k);return k in v?v[k]:null},setItem(k,x){v[String(k)]=String(x)},removeItem(k){delete v[String(k)]},clear(){for(const k in v)delete v[k]}};return new Proxy(a,{get:(t,k)=>k in t?t[k]:v[k],set:(t,k,x)=>(v[k]=String(x),true),deleteProperty:(t,k)=>(delete v[k],true)})}for(const n of ["localStorage","sessionStorage"])try{Object.defineProperty(window,n,{value:s(),configurable:true})}catch{}const c=Object.create(null);try{Object.defineProperty(document,"cookie",{configurable:true,get(){return Object.entries(c).map(([k,v])=>k+"="+v).join("; ")},set(x){const a=String(x).split(";"),q=a.shift(),i=q.indexOf("=");if(i<1)return;const k=q.slice(0,i).trim(),v=q.slice(i+1).trim(),gone=a.some(x=>/^\s*max-age\s*=\s*0\s*$/i.test(x)||/^\s*expires\s*=\s*Thu,\s*01\s*Jan\s*1970/i.test(x));gone?delete c[k]:c[k]=v}})}catch{}function r(v){try{const u=new URL(String(v),location.href);if((u.protocol==="http:"||u.protocol==="https:"||u.protocol==="ws:"||u.protocol==="wss:")&&u.host===location.host&&u.pathname!==p&&!u.pathname.startsWith(p+"/"))u.pathname=p+(u.pathname.startsWith("/")?u.pathname:"/"+u.pathname);return u.href}catch{return v}}const o=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(m,u,...a){return o.call(this,m,r(u),...a)};if(window.fetch){const f=window.fetch;window.fetch=function(i,n){return f.call(this,i instanceof Request?new Request(r(i.url),i):r(i),n)}}if(window.EventSource){const E=window.EventSource;window.EventSource=function(u,c){return new E(r(u),c)};window.EventSource.prototype=E.prototype;Object.setPrototypeOf(window.EventSource,E)}if(window.WebSocket){const W=window.WebSocket;window.WebSocket=function(u,p){return p===undefined?new W(r(u)):new W(r(u),p)};window.WebSocket.prototype=W.prototype;Object.setPrototypeOf(window.WebSocket,W)}})();</script>`
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
