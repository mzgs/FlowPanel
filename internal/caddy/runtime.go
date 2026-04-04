package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"

	httpcache "github.com/caddyserver/cache-handler"
	caddyv2 "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	fastcgi "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy/fastcgi"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/rewrite"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
	caddylogging "github.com/caddyserver/caddy/v2/modules/logging"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	"github.com/darkweak/souin/configurationtypes"
	"go.uber.org/zap"
)

type Runtime struct {
	logger          *zap.Logger
	publicHTTPAddr  string
	publicHTTPSAddr string
	php             phpenv.Manager
	phpMyAdmin      phpmyadmin.Manager
	phpMyAdminAddr  string

	mu      sync.Mutex
	started bool
	rawJSON []byte
}

type configSummary struct {
	configuredDomains int
	activeRoutes      int
	placeholderRoutes int
}

type phpRouteConfig struct {
	fastCGIAddress string
}

type phpMyAdminRouteConfig struct {
	fastCGIAddress string
	root           string
}

type runtimeSyncMode int

const (
	runtimeSyncModeStandard runtimeSyncMode = iota
	runtimeSyncModeHTTPSOnly
)

const defaultCacheTTL = 120 * time.Second

var loggerNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func NewRuntime(
	logger *zap.Logger,
	publicHTTPAddr,
	publicHTTPSAddr string,
	phpManager phpenv.Manager,
	phpMyAdminManager phpmyadmin.Manager,
	phpMyAdminAddr string,
) *Runtime {
	return &Runtime{
		logger:          logger,
		publicHTTPAddr:  strings.TrimSpace(publicHTTPAddr),
		publicHTTPSAddr: strings.TrimSpace(publicHTTPSAddr),
		php:             phpManager,
		phpMyAdmin:      phpMyAdminManager,
		phpMyAdminAddr:  strings.TrimSpace(phpMyAdminAddr),
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

	cfg, summary, err := buildConfig(
		r.publicHTTPAddr,
		r.publicHTTPSAddr,
		r.phpMyAdminAddr,
		nil,
		nil,
		nil,
		runtimeSyncModeStandard,
	)
	if err != nil {
		return fmt.Errorf("build caddy config: %w", err)
	}
	rawConfig, err := encodeAndValidateConfig(cfg)
	if err != nil {
		return err
	}
	if err := loadRawConfig(rawConfig, true); err != nil {
		return err
	}
	r.rawJSON = append(r.rawJSON[:0], rawConfig...)

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
	phpMyAdminConfig, err := r.resolvePHPMyAdminRouteConfig(ctx)
	if err != nil {
		return err
	}

	mode := runtimeSyncModeStandard
	for {
		cfg, summary, err := buildConfig(
			r.publicHTTPAddr,
			r.publicHTTPSAddr,
			r.phpMyAdminAddr,
			records,
			phpConfig,
			phpMyAdminConfig,
			mode,
		)
		if err != nil {
			return fmt.Errorf("build caddy config: %w", err)
		}
		rawConfig, err := encodeAndValidateConfig(cfg)
		if err != nil {
			return err
		}
		if err := r.applyConfigWithFallback(rawConfig); err != nil {
			if mode == runtimeSyncModeStandard && isPublicHTTPListenerConflict(err, r.publicHTTPAddr) {
				r.logger.Warn("public HTTP listener is unavailable; retrying with HTTPS-only Caddy config",
					zap.String("public_http_addr", r.publicHTTPAddr),
					zap.Error(err),
				)
				mode = runtimeSyncModeHTTPSOnly
				continue
			}
			if mode == runtimeSyncModeHTTPSOnly {
				return fmt.Errorf("apply https-only caddy config: %w", err)
			}
			return err
		}

		r.rawJSON = append(r.rawJSON[:0], rawConfig...)

		fields := []zap.Field{
			zap.Int("configured_domains", summary.configuredDomains),
			zap.Int("active_routes", summary.activeRoutes),
			zap.Int("placeholder_routes", summary.placeholderRoutes),
		}
		if mode == runtimeSyncModeHTTPSOnly {
			fields = append(fields, zap.Bool("https_only_mode", true))
			r.logger.Warn("embedded caddy runtime synchronized without a public HTTP listener", fields...)
		} else {
			r.logger.Info("embedded caddy runtime synchronized", fields...)
		}

		return nil
	}
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
	r.rawJSON = nil
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

func (r *Runtime) resolvePHPMyAdminRouteConfig(ctx context.Context) (*phpMyAdminRouteConfig, error) {
	if r.phpMyAdmin == nil {
		return nil, nil
	}

	status := r.phpMyAdmin.Status(ctx)
	if !status.Installed || strings.TrimSpace(status.InstallPath) == "" {
		return nil, nil
	}

	if r.php == nil {
		return nil, fmt.Errorf("php-fpm support is not configured")
	}

	phpStatus := r.php.Status(ctx)
	if !phpStatus.Ready {
		return nil, fmt.Errorf("php-fpm is not ready: %s", phpStatus.Message)
	}
	if strings.TrimSpace(phpStatus.ListenAddress) == "" {
		return nil, fmt.Errorf("php-fpm listen address is not configured")
	}

	return &phpMyAdminRouteConfig{
		fastCGIAddress: phpStatus.ListenAddress,
		root:           status.InstallPath,
	}, nil
}

func buildConfig(
	publicHTTPAddr,
	publicHTTPSAddr,
	phpMyAdminAddr string,
	records []domain.Record,
	phpConfig *phpRouteConfig,
	phpMyAdminConfig *phpMyAdminRouteConfig,
	mode runtimeSyncMode,
) (*caddyv2.Config, configSummary, error) {
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
	if loggingConfig := domainLoggingConfig(records); loggingConfig != nil {
		cfg.Logging = loggingConfig
	}

	if len(records) == 0 && phpMyAdminConfig == nil {
		return cfg, summary, nil
	}

	httpApp := caddyhttp.App{
		Servers: map[string]*caddyhttp.Server{},
	}
	if len(records) > 0 {
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

		httpApp.HTTPSPort = httpsPort
		httpApp.Servers["public"] = &caddyhttp.Server{
			Listen:            []string{publicHTTPSAddr},
			ReadHeaderTimeout: caddyv2.Duration(10 * time.Second),
			IdleTimeout:       caddyv2.Duration(2 * time.Minute),
			MaxHeaderBytes:    1024 * 10,
			Routes:            routes,
			Logs:              domainServerLogConfig(records),
		}
		if mode == runtimeSyncModeStandard {
			httpPort, err := parseTCPPort(publicHTTPAddr)
			if err != nil {
				return nil, configSummary{}, fmt.Errorf("parse public HTTP listener: %w", err)
			}
			httpApp.HTTPPort = httpPort
		} else {
			httpApp.Servers["public"].AutoHTTPS = &caddyhttp.AutoHTTPSConfig{
				DisableRedir: true,
			}
		}
	}
	if phpMyAdminConfig != nil {
		httpApp.Servers["phpmyadmin"] = &caddyhttp.Server{
			Listen:            []string{phpMyAdminAddr},
			ReadHeaderTimeout: caddyv2.Duration(10 * time.Second),
			IdleTimeout:       caddyv2.Duration(2 * time.Minute),
			MaxHeaderBytes:    1024 * 10,
			Routes:            caddyhttp.RouteList{routeForPHPMyAdmin(*phpMyAdminConfig)},
			AutoHTTPS: &caddyhttp.AutoHTTPSConfig{
				Disabled: true,
			},
			Logs: &caddyhttp.ServerLogConfig{},
		}
		summary.activeRoutes++
	}

	cfg.AppsRaw = caddyv2.ModuleMap{
		"http": caddyconfig.JSON(httpApp, nil),
	}
	if hasCacheEnabledRecords(records) {
		cfg.AppsRaw["cache"] = caddyconfig.JSON(cacheAppConfig(), nil)
	}
	if _, ok := httpApp.Servers["public"]; ok && mode == runtimeSyncModeHTTPSOnly {
		cfg.AppsRaw["tls"] = caddyconfig.JSON(httpsOnlyTLSApp(httpApp.HTTPSPort), nil)
	}

	return cfg, summary, nil
}

func httpsOnlyTLSApp(httpsPort int) caddytls.TLS {
	return caddytls.TLS{
		Automation: &caddytls.AutomationConfig{
			Policies: []*caddytls.AutomationPolicy{{
				IssuersRaw: []json.RawMessage{
					caddyconfig.JSONModuleObject(caddytls.ACMEIssuer{
						Challenges: &caddytls.ChallengesConfig{
							HTTP: &caddytls.HTTPChallengeConfig{
								Disabled: true,
							},
							TLSALPN: &caddytls.TLSALPNChallengeConfig{
								AlternatePort: httpsPort,
							},
						},
					}, "module", "acme", nil),
				},
			}},
		},
	}
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
	originHandlers := make([]json.RawMessage, 0, 2)

	switch record.Kind {
	case domain.KindStaticSite:
		originHandlers = append(originHandlers,
			caddyconfig.JSONModuleObject(fileserver.FileServer{
				Root: record.Target,
			}, "handler", "file_server", nil),
		)
	case domain.KindPHP:
		if phpConfig == nil || strings.TrimSpace(phpConfig.fastCGIAddress) == "" {
			return nil, false, fmt.Errorf("php-fpm is not configured for %q", record.Hostname)
		}

		originHandlers = append(originHandlers,
			caddyconfig.JSONModuleObject(caddyhttp.Subroute{
				Routes: phpSubrouteRoutes(record.Target, phpConfig.fastCGIAddress),
			}, "handler", "subroute", nil),
		)
	case domain.KindApp:
		originHandlers = append(originHandlers,
			caddyconfig.JSONModuleObject(reverseproxy.Handler{
				Upstreams: reverseproxy.UpstreamPool{
					&reverseproxy.Upstream{
						Dial: net.JoinHostPort("localhost", record.Target),
					},
				},
			}, "handler", "reverse_proxy", nil),
		)
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

		originHandlers = append(originHandlers,
			caddyconfig.JSONModuleObject(handler, "handler", "reverse_proxy", nil),
		)
	default:
		return nil, false, fmt.Errorf("unsupported domain kind %q", record.Kind)
	}

	if !record.CacheEnabled {
		return originHandlers, false, nil
	}

	handlers := make([]json.RawMessage, 0, len(originHandlers)+1)
	handlers = append(handlers, caddyconfig.JSONModuleObject(cacheHandlerConfig(), "handler", "cache", nil))
	handlers = append(handlers, originHandlers...)

	return handlers, false, nil
}

func routeForPHPMyAdmin(config phpMyAdminRouteConfig) caddyhttp.Route {
	return caddyhttp.Route{
		HandlersRaw: []json.RawMessage{
			caddyconfig.JSONModuleObject(caddyhttp.Subroute{
				Routes: phpSubrouteRoutes(config.root, config.fastCGIAddress),
			}, "handler", "subroute", nil),
		},
		Terminal: true,
	}
}

func phpSubrouteRoutes(root, fastCGIAddress string) caddyhttp.RouteList {
	return caddyhttp.RouteList{
		{
			MatcherSetsRaw: []caddyv2.ModuleMap{{
				"file": caddyconfig.JSON(fileserver.MatchFile{
					Root:     root,
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
					Root:      root,
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
						Root:      root,
						SplitPath: []string{".php"},
					}, "protocol", "fastcgi", nil),
					Upstreams: reverseproxy.UpstreamPool{
						&reverseproxy.Upstream{
							Dial: fastCGIDialAddress(fastCGIAddress),
						},
					},
				}, "handler", "reverse_proxy", nil),
			},
		},
		{
			HandlersRaw: []json.RawMessage{
				caddyconfig.JSONModuleObject(fileserver.FileServer{
					Root: root,
				}, "handler", "file_server", nil),
			},
			Terminal: true,
		},
	}
}

func fastCGIDialAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(address), "unix:") {
		address = strings.TrimSpace(address[len("unix:"):])
	}

	if strings.HasPrefix(strings.ToLower(address), "unix/") {
		return address
	}
	if strings.HasPrefix(address, "/") {
		return "unix/" + address
	}

	return address
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

func (r *Runtime) applyConfigWithFallback(rawConfig []byte) error {
	if err := loadRawConfig(rawConfig, false); err == nil {
		return nil
	} else if !isAddressInUseError(err) {
		return err
	}

	r.logger.Warn("embedded caddy reload hit listener conflict, retrying with full restart")

	previousConfig := append([]byte(nil), r.rawJSON...)
	if err := caddyv2.Stop(); err != nil {
		return fmt.Errorf("stop embedded caddy runtime before retry: %w", err)
	}

	if err := loadRawConfig(rawConfig, true); err == nil {
		return nil
	} else if len(previousConfig) == 0 {
		return err
	} else {
		restoreErr := loadRawConfig(previousConfig, true)
		if restoreErr != nil {
			return fmt.Errorf("load caddy config after restart: %v; restore previous config: %w", err, restoreErr)
		}
		return err
	}
}

func encodeAndValidateConfig(cfg *caddyv2.Config) ([]byte, error) {
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal caddy config: %w", err)
	}

	var validateCfg caddyv2.Config
	if err := json.Unmarshal(rawConfig, &validateCfg); err != nil {
		return nil, fmt.Errorf("decode caddy config for validation: %w", err)
	}

	if err := caddyv2.Validate(&validateCfg); err != nil {
		return nil, fmt.Errorf("validate caddy config: %w", err)
	}

	return rawConfig, nil
}

func loadRawConfig(rawConfig []byte, forceReload bool) error {
	if err := caddyv2.Load(rawConfig, forceReload); err != nil {
		return fmt.Errorf("load caddy config: %w", err)
	}

	return nil
}

func isAddressInUseError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}

func isPublicHTTPListenerConflict(err error, address string) bool {
	if !isAddressInUseError(err) {
		return false
	}

	normalizedAddress := strings.ToLower(strings.TrimSpace(address))
	message := strings.ToLower(err.Error())
	if normalizedAddress != "" && strings.Contains(message, "listen tcp "+normalizedAddress) {
		return true
	}

	port, parseErr := parseTCPPort(address)
	if parseErr != nil {
		return false
	}

	return strings.Contains(message, fmt.Sprintf("listen tcp :%d", port)) ||
		strings.Contains(message, fmt.Sprintf("listening on :%d", port))
}

func boolPtr(value bool) *bool {
	return &value
}

func hasCacheEnabledRecords(records []domain.Record) bool {
	for _, record := range records {
		if record.CacheEnabled {
			return true
		}
	}

	return false
}

func cacheAppConfig() httpcache.SouinApp {
	return httpcache.SouinApp{
		DefaultCache: httpcache.DefaultCache{
			TTL:       configurationtypes.Duration{Duration: defaultCacheTTL},
			CacheName: "FlowPanel",
		},
	}
}

func cacheHandlerConfig() httpcache.SouinCaddyMiddleware {
	return httpcache.SouinCaddyMiddleware{
		Configuration: httpcache.Configuration{
			DefaultCache: httpcache.DefaultCache{
				TTL:       configurationtypes.Duration{Duration: defaultCacheTTL},
				CacheName: "FlowPanel",
			},
		},
	}
}

func domainLoggingConfig(records []domain.Record) *caddyv2.Logging {
	if len(records) == 0 {
		return nil
	}

	logs := make(map[string]*caddyv2.CustomLog, len(records)*2)
	for _, record := range records {
		if strings.TrimSpace(record.Logs.Access) == "" || strings.TrimSpace(record.Logs.Error) == "" {
			continue
		}

		accessLoggerName, errorLoggerName := domainLoggerNames(record)
		logs[accessLoggerName] = &caddyv2.CustomLog{
			BaseLog: caddyv2.BaseLog{
				WriterRaw: caddyconfig.JSONModuleObject(caddylogging.FileWriter{
					Filename: record.Logs.Access,
					DirMode:  "0755",
				}, "output", "file", nil),
				Level: "INFO",
			},
			Include: []string{"http.log.access." + accessLoggerName},
		}
		logs[errorLoggerName] = &caddyv2.CustomLog{
			BaseLog: caddyv2.BaseLog{
				WriterRaw: caddyconfig.JSONModuleObject(caddylogging.FileWriter{
					Filename: record.Logs.Error,
					DirMode:  "0755",
				}, "output", "file", nil),
				Level: "INFO",
			},
			Include: []string{"http.log.error." + errorLoggerName},
		}
	}

	if len(logs) == 0 {
		return nil
	}

	return &caddyv2.Logging{
		Logs: logs,
	}
}

func domainServerLogConfig(records []domain.Record) *caddyhttp.ServerLogConfig {
	config := &caddyhttp.ServerLogConfig{
		LoggerNames:       make(map[string]caddyhttp.StringArray, len(records)),
		SkipUnmappedHosts: true,
	}

	for _, record := range records {
		accessLoggerName, errorLoggerName := domainLoggerNames(record)
		config.LoggerNames[record.Hostname] = caddyhttp.StringArray{accessLoggerName, errorLoggerName}
	}

	return config
}

func domainLoggerNames(record domain.Record) (string, string) {
	name := strings.TrimSpace(record.ID)
	if name == "" {
		name = record.Hostname
	}
	name = loggerNameSanitizer.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		name = "domain"
	}

	return name + "_access", name + "_error"
}
