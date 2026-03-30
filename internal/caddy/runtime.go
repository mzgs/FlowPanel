package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/phpenv"

	caddyv2 "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	fastcgi "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy/fastcgi"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/rewrite"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"go.uber.org/zap"
)

type Runtime struct {
	logger          *zap.Logger
	publicHTTPAddr  string
	publicHTTPSAddr string
	php             phpenv.Manager

	mu      sync.Mutex
	started bool
}

type configSummary struct {
	configuredDomains int
	activeRoutes      int
	placeholderRoutes int
}

type phpRouteConfig struct {
	fastCGIAddress string
}

func NewRuntime(logger *zap.Logger, publicHTTPAddr, publicHTTPSAddr string, phpManager phpenv.Manager) *Runtime {
	return &Runtime{
		logger:          logger,
		publicHTTPAddr:  strings.TrimSpace(publicHTTPAddr),
		publicHTTPSAddr: strings.TrimSpace(publicHTTPSAddr),
		php:             phpManager,
	}
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return nil
	}

	cfg, summary, err := buildConfig(r.publicHTTPAddr, r.publicHTTPSAddr, nil, nil)
	if err != nil {
		return fmt.Errorf("build caddy config: %w", err)
	}
	if err := loadConfig(cfg, true); err != nil {
		return err
	}

	r.started = true
	r.logger.Info("embedded caddy runtime started",
		zap.String("public_http_addr", r.publicHTTPAddr),
		zap.String("public_https_addr", r.publicHTTPSAddr),
		zap.Int("configured_domains", summary.configuredDomains),
	)

	return nil
}

func (r *Runtime) Sync(ctx context.Context, records []domain.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return fmt.Errorf("embedded caddy runtime is not started")
	}

	phpConfig, err := r.resolvePHPRouteConfig(ctx, records)
	if err != nil {
		return err
	}

	cfg, summary, err := buildConfig(r.publicHTTPAddr, r.publicHTTPSAddr, records, phpConfig)
	if err != nil {
		return fmt.Errorf("build caddy config: %w", err)
	}
	if err := loadConfig(cfg, false); err != nil {
		return err
	}

	r.logger.Info("embedded caddy runtime synchronized",
		zap.Int("configured_domains", summary.configuredDomains),
		zap.Int("active_routes", summary.activeRoutes),
		zap.Int("placeholder_routes", summary.placeholderRoutes),
	)

	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return nil
	}

	if err := caddyv2.Stop(); err != nil {
		return fmt.Errorf("stop embedded caddy runtime: %w", err)
	}

	r.started = false
	r.logger.Info("embedded caddy runtime stopped")

	return nil
}

func (r *Runtime) resolvePHPRouteConfig(ctx context.Context, records []domain.Record) (*phpRouteConfig, error) {
	for _, record := range records {
		if record.Kind != domain.KindPHP {
			continue
		}

		if r.php == nil {
			return nil, fmt.Errorf("php-fpm support is not configured")
		}

		status := r.php.Status(ctx)
		if !status.Ready {
			return nil, fmt.Errorf("php-fpm is not ready: %s", status.Message)
		}
		if strings.TrimSpace(status.ListenAddress) == "" {
			return nil, fmt.Errorf("php-fpm listen address is not configured")
		}

		return &phpRouteConfig{
			fastCGIAddress: status.ListenAddress,
		}, nil
	}

	return nil, nil
}

func buildConfig(publicHTTPAddr, publicHTTPSAddr string, records []domain.Record, phpConfig *phpRouteConfig) (*caddyv2.Config, configSummary, error) {
	summary := configSummary{
		configuredDomains: len(records),
	}

	cfg := &caddyv2.Config{
		Admin: &caddyv2.AdminConfig{
			Disabled: true,
			Config: &caddyv2.ConfigSettings{
				Persist: boolPtr(false),
			},
		},
	}

	if len(records) == 0 {
		return cfg, summary, nil
	}

	httpPort, err := parseTCPPort(publicHTTPAddr)
	if err != nil {
		return nil, configSummary{}, fmt.Errorf("parse public HTTP listener: %w", err)
	}

	httpsPort, err := parseTCPPort(publicHTTPSAddr)
	if err != nil {
		return nil, configSummary{}, fmt.Errorf("parse public HTTPS listener: %w", err)
	}

	routes := make(caddyhttp.RouteList, 0, len(records))
	for _, record := range records {
		route, placeholder, err := routeForRecord(record, phpConfig)
		if err != nil {
			return nil, configSummary{}, err
		}

		routes = append(routes, route)
		if placeholder {
			summary.placeholderRoutes++
			continue
		}
		summary.activeRoutes++
	}

	httpApp := caddyhttp.App{
		HTTPPort:  httpPort,
		HTTPSPort: httpsPort,
		Servers: map[string]*caddyhttp.Server{
			"public": {
				Listen:            []string{publicHTTPSAddr},
				ReadHeaderTimeout: caddyv2.Duration(10 * time.Second),
				IdleTimeout:       caddyv2.Duration(2 * time.Minute),
				MaxHeaderBytes:    1024 * 10,
				Routes:            routes,
				Logs:              &caddyhttp.ServerLogConfig{},
			},
		},
	}

	cfg.AppsRaw = caddyv2.ModuleMap{
		"http": caddyconfig.JSON(httpApp, nil),
	}

	return cfg, summary, nil
}

func routeForRecord(record domain.Record, phpConfig *phpRouteConfig) (caddyhttp.Route, bool, error) {
	handlers, placeholder, err := handlersForRecord(record, phpConfig)
	if err != nil {
		return caddyhttp.Route{}, false, err
	}

	return caddyhttp.Route{
		MatcherSetsRaw: []caddyv2.ModuleMap{{
			"host": caddyconfig.JSON(caddyhttp.MatchHost{record.Hostname}, nil),
		}},
		HandlersRaw: handlers,
		Terminal:    true,
	}, placeholder, nil
}

func handlersForRecord(record domain.Record, phpConfig *phpRouteConfig) ([]json.RawMessage, bool, error) {
	switch record.Kind {
	case domain.KindStaticSite:
		return []json.RawMessage{
			caddyconfig.JSONModuleObject(fileserver.FileServer{
				Root: record.Target,
			}, "handler", "file_server", nil),
		}, false, nil
	case domain.KindPHP:
		if phpConfig == nil || strings.TrimSpace(phpConfig.fastCGIAddress) == "" {
			return nil, false, fmt.Errorf("php-fpm is not configured for %q", record.Hostname)
		}

		return []json.RawMessage{
			caddyconfig.JSONModuleObject(caddyhttp.Subroute{
				Routes: caddyhttp.RouteList{
					{
						MatcherSetsRaw: []caddyv2.ModuleMap{{
							"file": caddyconfig.JSON(fileserver.MatchFile{
								Root:     record.Target,
								TryFiles: []string{"{http.request.uri.path}/index.php"},
							}, nil),
							"not": caddyconfig.JSON(caddyhttp.MatchNot{
								MatcherSetsRaw: []caddyv2.ModuleMap{{
									"path": caddyconfig.JSON(caddyhttp.MatchPath{"*/"}, nil),
								}},
							}, nil),
						}},
						HandlersRaw: []json.RawMessage{
							caddyconfig.JSONModuleObject(caddyhttp.StaticResponse{
								StatusCode: caddyhttp.WeakString("308"),
								Headers: http.Header{
									"Location": []string{"{http.request.orig_uri.path}/{http.request.orig_uri.prefixed_query}"},
								},
							}, "handler", "static_response", nil),
						},
					},
					{
						Group: "php-rewrite",
						MatcherSetsRaw: []caddyv2.ModuleMap{{
							"file": caddyconfig.JSON(fileserver.MatchFile{
								Root:      record.Target,
								TryFiles:  []string{"{http.request.uri.path}", "{http.request.uri.path}/index.php", "index.php"},
								TryPolicy: "first_exist_fallback",
								SplitPath: []string{".php"},
							}, nil),
						}},
						HandlersRaw: []json.RawMessage{
							caddyconfig.JSONModuleObject(rewrite.Rewrite{
								URI: "{http.matchers.file.relative}",
							}, "handler", "rewrite", nil),
						},
					},
					{
						MatcherSetsRaw: []caddyv2.ModuleMap{{
							"path": caddyconfig.JSON(caddyhttp.MatchPath{"*.php"}, nil),
						}},
						HandlersRaw: []json.RawMessage{
							caddyconfig.JSONModuleObject(reverseproxy.Handler{
								TransportRaw: caddyconfig.JSONModuleObject(fastcgi.Transport{
									Root:      record.Target,
									SplitPath: []string{".php"},
								}, "protocol", "fastcgi", nil),
								Upstreams: reverseproxy.UpstreamPool{
									&reverseproxy.Upstream{
										Dial: phpConfig.fastCGIAddress,
									},
								},
							}, "handler", "reverse_proxy", nil),
						},
					},
					{
						HandlersRaw: []json.RawMessage{
							caddyconfig.JSONModuleObject(fileserver.FileServer{
								Root: record.Target,
							}, "handler", "file_server", nil),
						},
						Terminal: true,
					},
				},
			}, "handler", "subroute", nil),
		}, false, nil
	case domain.KindApp:
		return []json.RawMessage{
			caddyconfig.JSONModuleObject(reverseproxy.Handler{
				Upstreams: reverseproxy.UpstreamPool{
					&reverseproxy.Upstream{
						Dial: net.JoinHostPort("127.0.0.1", record.Target),
					},
				},
			}, "handler", "reverse_proxy", nil),
		}, false, nil
	case domain.KindReverseProxy:
		targetURL, err := parseUpstreamURL(record)
		if err != nil {
			return nil, false, err
		}

		handler := reverseproxy.Handler{
			Upstreams: reverseproxy.UpstreamPool{
				&reverseproxy.Upstream{
					Dial: upstreamDialAddress(targetURL),
				},
			},
		}
		if targetURL.Scheme == "https" {
			handler.TransportRaw = caddyconfig.JSONModuleObject(reverseproxy.HTTPTransport{
				TLS: &reverseproxy.TLSConfig{
					ServerName: targetURL.Hostname(),
				},
			}, "protocol", "http", nil)
		}

		return []json.RawMessage{
			caddyconfig.JSONModuleObject(handler, "handler", "reverse_proxy", nil),
		}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported domain kind %q", record.Kind)
	}
}

func parseUpstreamURL(record domain.Record) (*url.URL, error) {
	targetURL, err := url.Parse(record.Target)
	if err != nil {
		return nil, fmt.Errorf("parse reverse proxy target for %q: %w", record.Hostname, err)
	}
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		return nil, fmt.Errorf("reverse proxy target for %q must start with http:// or https://", record.Hostname)
	}
	if targetURL.Host == "" {
		return nil, fmt.Errorf("reverse proxy target for %q must include a host", record.Hostname)
	}
	if targetURL.User != nil || (targetURL.Path != "" && targetURL.Path != "/") || targetURL.RawQuery != "" || targetURL.Fragment != "" {
		return nil, fmt.Errorf("reverse proxy target for %q must not include credentials, paths, queries, or fragments", record.Hostname)
	}

	return targetURL, nil
}

func upstreamDialAddress(targetURL *url.URL) string {
	host := targetURL.Hostname()
	port := targetURL.Port()
	if port == "" {
		switch targetURL.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
}

func parseTCPPort(address string) (int, error) {
	parsed, err := caddyv2.ParseNetworkAddress(strings.TrimSpace(address))
	if err != nil {
		return 0, err
	}
	if parsed.Network != "" && parsed.Network != "tcp" && parsed.Network != "tcp4" && parsed.Network != "tcp6" {
		return 0, fmt.Errorf("listener %q must use a TCP network", address)
	}
	if parsed.StartPort == 0 || parsed.EndPort == 0 || parsed.StartPort != parsed.EndPort {
		return 0, fmt.Errorf("listener %q must specify exactly one TCP port", address)
	}

	return int(parsed.StartPort), nil
}

func loadConfig(cfg *caddyv2.Config, forceReload bool) error {
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal caddy config: %w", err)
	}

	var validateCfg caddyv2.Config
	if err := json.Unmarshal(rawConfig, &validateCfg); err != nil {
		return fmt.Errorf("decode caddy config for validation: %w", err)
	}

	if err := caddyv2.Validate(&validateCfg); err != nil {
		return fmt.Errorf("validate caddy config: %w", err)
	}

	if err := caddyv2.Load(rawConfig, forceReload); err != nil {
		return fmt.Errorf("load caddy config: %w", err)
	}

	return nil
}

func boolPtr(value bool) *bool {
	return &value
}
