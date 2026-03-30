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

	caddyv2 "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"go.uber.org/zap"
)

type Runtime struct {
	logger          *zap.Logger
	publicHTTPAddr  string
	publicHTTPSAddr string

	mu      sync.Mutex
	started bool
}

type configSummary struct {
	configuredDomains int
	activeRoutes      int
	placeholderRoutes int
}

func NewRuntime(logger *zap.Logger, publicHTTPAddr, publicHTTPSAddr string) *Runtime {
	return &Runtime{
		logger:          logger,
		publicHTTPAddr:  strings.TrimSpace(publicHTTPAddr),
		publicHTTPSAddr: strings.TrimSpace(publicHTTPSAddr),
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

	cfg, summary, err := buildConfig(r.publicHTTPAddr, r.publicHTTPSAddr, nil)
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

	cfg, summary, err := buildConfig(r.publicHTTPAddr, r.publicHTTPSAddr, records)
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

func buildConfig(publicHTTPAddr, publicHTTPSAddr string, records []domain.Record) (*caddyv2.Config, configSummary, error) {
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
		route, placeholder, err := routeForRecord(record)
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

func routeForRecord(record domain.Record) (caddyhttp.Route, bool, error) {
	handlers, placeholder, err := handlersForRecord(record)
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

func handlersForRecord(record domain.Record) ([]json.RawMessage, bool, error) {
	switch record.Kind {
	case domain.KindStaticSite:
		return []json.RawMessage{
			caddyconfig.JSONModuleObject(fileserver.FileServer{
				Root: record.Target,
			}, "handler", "file_server", nil),
		}, false, nil
	case domain.KindPHP:
		return []json.RawMessage{
			caddyconfig.JSONModuleObject(caddyhttp.StaticResponse{
				StatusCode: caddyhttp.WeakString("503"),
				Headers: http.Header{
					"Content-Type": []string{"text/plain; charset=utf-8"},
				},
				Body: "PHP site execution is not implemented yet.\n",
			}, "handler", "static_response", nil),
		}, true, nil
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
