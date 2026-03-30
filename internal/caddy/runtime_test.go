package caddy

import (
	"bytes"
	"encoding/json"
	"testing"

	"flowpanel/internal/domain"

	caddyv2 "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
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
	})
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

func TestBuildConfigUsesPlaceholderRouteForPHPDomains(t *testing.T) {
	cfg, summary, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "php.example.com",
			Kind:     domain.KindPHP,
			Target:   "/var/www/php.example.com/public",
		},
	})
	if err != nil {
		t.Fatalf("build config: %v", err)
	}

	if summary.activeRoutes != 0 {
		t.Fatalf("active routes = %d, want 0", summary.activeRoutes)
	}
	if summary.placeholderRoutes != 1 {
		t.Fatalf("placeholder routes = %d, want 1", summary.placeholderRoutes)
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

	if handler["handler"] != "static_response" {
		t.Fatalf("handler = %#v, want static_response", handler["handler"])
	}
	if handler["status_code"] != float64(503) {
		t.Fatalf("status_code = %#v, want 503", handler["status_code"])
	}
}

func TestBuildConfigRejectsReverseProxyTargetsWithPaths(t *testing.T) {
	_, _, err := buildConfig(":9080", ":9443", []domain.Record{
		{
			Hostname: "proxy.example.com",
			Kind:     domain.KindReverseProxy,
			Target:   "https://backend.example.com/base",
		},
	})
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
	})
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
