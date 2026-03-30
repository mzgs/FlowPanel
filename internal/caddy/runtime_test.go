package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"

	"flowpanel/internal/domain"
	"flowpanel/internal/phpenv"

	caddyv2 "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddytls"
	"go.uber.org/zap"
)

func TestBuildConfigValidatesStaticAndAppDomains(t *testing.T) {
	staticRoot := t.TempDir()

	cfg, summary, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "static.example.com",
			Kind:     domain.KindStaticSite,
			Target:   staticRoot,
		},
		{
			Hostname: "app.example.com",
			Kind:     domain.KindApp,
			Target:   "3000",
		},
	}, nil, runtimeSyncModeStandard)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	if summary.configuredDomains != 2 {
		t.Fatalf("configured domains = %d, want 2", summary.configuredDomains)
	}
	if summary.activeRoutes != 2 {
		t.Fatalf("active routes = %d, want 2", summary.activeRoutes)
	}
	if summary.placeholderRoutes != 0 {
		t.Fatalf("placeholder routes = %d, want 0", summary.placeholderRoutes)
	}

	var httpApp caddyhttp.App
	if err := json.Unmarshal(cfg.AppsRaw["http"], &httpApp); err != nil {
		t.Fatalf("unmarshal http app: %v", err)
	}

	if httpApp.HTTPPort != 9080 {
		t.Fatalf("http port = %d, want 9080", httpApp.HTTPPort)
	}
	if httpApp.HTTPSPort != 9443 {
		t.Fatalf("https port = %d, want 9443", httpApp.HTTPSPort)
	}

	server := httpApp.Servers["public"]
	if server == nil {
		t.Fatal("expected public server")
	}
	if len(server.Listen) != 1 || server.Listen[0] != ":9443" {
		t.Fatalf("listen = %#v, want [:9443]", server.Listen)
	}
	if len(server.Routes) != 2 {
		t.Fatalf("route count = %d, want 2", len(server.Routes))
	}

	if err := caddyv2.Validate(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}

func TestBuildConfigBuildsFastCGIRouteForPHPDomains(t *testing.T) {
	cfg, summary, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "php.example.com",
			Kind:     domain.KindPHP,
			Target:   "/var/www/php.example.com/public",
		},
	}, &phpRouteConfig{fastCGIAddress: "127.0.0.1:9000"}, runtimeSyncModeStandard)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	if summary.activeRoutes != 1 {
		t.Fatalf("active routes = %d, want 1", summary.activeRoutes)
	}
	if summary.placeholderRoutes != 0 {
		t.Fatalf("placeholder routes = %d, want 0", summary.placeholderRoutes)
	}

	var httpApp caddyhttp.App
	if err := json.Unmarshal(cfg.AppsRaw["http"], &httpApp); err != nil {
		t.Fatalf("unmarshal http app: %v", err)
	}

	server := httpApp.Servers["public"]
	if server == nil || len(server.Routes) != 1 || len(server.Routes[0].HandlersRaw) != 1 {
		t.Fatalf("unexpected routes: %#v", server)
	}

	var handler map[string]any
	if err := json.Unmarshal(server.Routes[0].HandlersRaw[0], &handler); err != nil {
		t.Fatalf("unmarshal handler: %v", err)
	}

	if handler["handler"] != "subroute" {
		t.Fatalf("handler = %#v, want subroute", handler["handler"])
	}

	if err := caddyv2.Validate(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}

func TestBuildConfigNormalizesUnixSocketFastCGIAddress(t *testing.T) {
	cfg, _, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "php.example.com",
			Kind:     domain.KindPHP,
			Target:   "/var/www/php.example.com/public",
		},
	}, &phpRouteConfig{fastCGIAddress: "/run/php/php8.3-fpm.sock"}, runtimeSyncModeStandard)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	if !bytes.Contains(rawConfig, []byte(`"dial":"unix//run/php/php8.3-fpm.sock"`)) {
		t.Fatalf("raw config = %s, want unix socket FastCGI dial address", string(rawConfig))
	}
}

func TestBuildConfigRejectsReverseProxyTargetsWithPaths(t *testing.T) {
	_, _, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "proxy.example.com",
			Kind:     domain.KindReverseProxy,
			Target:   "https://backend.example.com/base",
		},
	}, nil, runtimeSyncModeStandard)
	if err == nil {
		t.Fatal("expected build config to fail")
	}
}

func TestConfigMarshalRemainsLoadableAfterValidationClone(t *testing.T) {
	cfg, _, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "static.example.com",
			Kind:     domain.KindStaticSite,
			Target:   t.TempDir(),
		},
	}, nil, runtimeSyncModeStandard)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	var validateCfg caddyv2.Config
	if err := json.Unmarshal(rawConfig, &validateCfg); err != nil {
		t.Fatalf("unmarshal validate config: %v", err)
	}

	if err := caddyv2.Validate(&validateCfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if bytes.Contains(rawConfig, []byte(`"http":null`)) {
		t.Fatalf("raw config unexpectedly contains a null http app: %s", string(rawConfig))
	}
}

func TestBuildConfigHTTPSOnlyModeDisablesRedirectsAndHTTPChallenge(t *testing.T) {
	cfg, summary, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "static.example.com",
			Kind:     domain.KindStaticSite,
			Target:   t.TempDir(),
		},
	}, nil, runtimeSyncModeHTTPSOnly)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	if summary.activeRoutes != 1 {
		t.Fatalf("active routes = %d, want 1", summary.activeRoutes)
	}

	var httpApp caddyhttp.App
	if err := json.Unmarshal(cfg.AppsRaw["http"], &httpApp); err != nil {
		t.Fatalf("unmarshal http app: %v", err)
	}

	server := httpApp.Servers["public"]
	if server == nil {
		t.Fatal("expected public server")
	}
	if server.AutoHTTPS == nil || !server.AutoHTTPS.DisableRedir {
		t.Fatalf("public server automatic HTTPS = %#v, want redirects disabled", server.AutoHTTPS)
	}

	var tlsApp caddytls.TLS
	if err := json.Unmarshal(cfg.AppsRaw["tls"], &tlsApp); err != nil {
		t.Fatalf("unmarshal tls app: %v", err)
	}

	if tlsApp.Automation == nil || len(tlsApp.Automation.Policies) != 1 {
		t.Fatalf("unexpected tls automation policies: %#v", tlsApp.Automation)
	}
	if len(tlsApp.Automation.Policies[0].IssuersRaw) != 1 {
		t.Fatalf("issuer count = %d, want 1", len(tlsApp.Automation.Policies[0].IssuersRaw))
	}

	var issuer struct {
		Module     string `json:"module"`
		Challenges struct {
			HTTP struct {
				Disabled bool `json:"disabled"`
			} `json:"http"`
			TLSALPN struct {
				AlternatePort int `json:"alternate_port"`
			} `json:"tls-alpn"`
		} `json:"challenges"`
	}
	if err := json.Unmarshal(tlsApp.Automation.Policies[0].IssuersRaw[0], &issuer); err != nil {
		t.Fatalf("unmarshal acme issuer: %v", err)
	}

	if issuer.Module != "acme" {
		t.Fatalf("issuer module = %q, want acme", issuer.Module)
	}
	if !issuer.Challenges.HTTP.Disabled {
		t.Fatal("expected ACME HTTP challenge to be disabled")
	}
	if issuer.Challenges.TLSALPN.AlternatePort != 9443 {
		t.Fatalf("tls-alpn alternate port = %d, want 9443", issuer.Challenges.TLSALPN.AlternatePort)
	}

	if err := caddyv2.Validate(cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}

func TestRuntimeSyncCanReloadMultipleDomainSets(t *testing.T) {
	httpAddr := freeTCPAddress(t)
	httpsAddr := freeTCPAddress(t)

	runtime := NewRuntime(zap.NewNop(), httpAddr, httpsAddr, fakePHPManager{})
	ctx := context.Background()

	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	defer func() {
		if err := runtime.Stop(ctx); err != nil {
			t.Fatalf("stop runtime: %v", err)
		}
	}()

	if err := runtime.Sync(ctx, []domain.Record{
		{
			Hostname: "one.localhost",
			Kind:     domain.KindApp,
			Target:   "3000",
		},
	}); err != nil {
		t.Fatalf("sync first config: %v", err)
	}

	if err := runtime.Sync(ctx, []domain.Record{
		{
			Hostname: "one.localhost",
			Kind:     domain.KindApp,
			Target:   "3000",
		},
		{
			Hostname: "two.localhost",
			Kind:     domain.KindApp,
			Target:   "3001",
		},
	}); err != nil {
		t.Fatalf("sync second config: %v", err)
	}
}

func TestRuntimeSyncFallsBackToHTTPSOnlyWhenPublicHTTPPortIsBusy(t *testing.T) {
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on occupied HTTP port: %v", err)
	}
	defer func() {
		_ = httpListener.Close()
	}()

	httpAddr := httpListener.Addr().String()
	httpsAddr := freeTCPAddress(t)

	runtime := NewRuntime(zap.NewNop(), httpAddr, httpsAddr, fakePHPManager{})
	ctx := context.Background()

	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	defer func() {
		if err := runtime.Stop(ctx); err != nil {
			t.Fatalf("stop runtime: %v", err)
		}
	}()

	if err := runtime.Sync(ctx, []domain.Record{
		{
			Hostname: "one.localhost",
			Kind:     domain.KindApp,
			Target:   "3000",
		},
	}); err != nil {
		t.Fatalf("sync config with occupied HTTP port: %v", err)
	}

	var cfg caddyv2.Config
	if err := json.Unmarshal(runtime.rawJSON, &cfg); err != nil {
		t.Fatalf("unmarshal runtime config: %v", err)
	}

	var httpApp caddyhttp.App
	if err := json.Unmarshal(cfg.AppsRaw["http"], &httpApp); err != nil {
		t.Fatalf("unmarshal http app: %v", err)
	}
	server := httpApp.Servers["public"]
	if server == nil || server.AutoHTTPS == nil || !server.AutoHTTPS.DisableRedir {
		t.Fatalf("public server automatic HTTPS = %#v, want redirects disabled", server)
	}

	var tlsApp caddytls.TLS
	if err := json.Unmarshal(cfg.AppsRaw["tls"], &tlsApp); err != nil {
		t.Fatalf("unmarshal tls app: %v", err)
	}
	if tlsApp.Automation == nil || len(tlsApp.Automation.Policies) != 1 {
		t.Fatalf("unexpected tls automation policies: %#v", tlsApp.Automation)
	}
}

type fakePHPManager struct{}

func (fakePHPManager) Status(context.Context) phpenv.Status {
	return phpenv.Status{
		Ready:         true,
		ListenAddress: "127.0.0.1:9000",
	}
}

func (fakePHPManager) Install(context.Context) error {
	return nil
}

func (fakePHPManager) Start(context.Context) error {
	return nil
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr type = %T, want *net.TCPAddr", listener.Addr())
	}

	return fmt.Sprintf("127.0.0.1:%d", addr.Port)
}
