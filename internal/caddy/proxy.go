package caddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	caddyv2 "github.com/caddyserver/caddy/v2"
)

// RunProxy owns the public listeners independently from the FlowPanel API.
// FlowPanel updates its configuration through Caddy's local administration
// socket, so restarting the control plane does not interrupt hosted traffic.
func RunProxy(ctx context.Context, adminAddress string) error {
	adminAddress = strings.TrimSpace(adminAddress)
	if adminAddress == "" {
		return errors.New("FLOWPANEL_CADDY_ADMIN_ADDR is required")
	}

	cfg := caddyv2.Config{Admin: &caddyv2.AdminConfig{Listen: adminAddress}}
	if raw, err := os.ReadFile(caddyv2.ConfigAutosavePath); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("decode saved caddy config: %w", err)
		}
		cfg.Admin = &caddyv2.AdminConfig{Listen: adminAddress}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read saved caddy config: %w", err)
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode caddy proxy config: %w", err)
	}
	if err := caddyv2.Load(raw, true); err != nil {
		return fmt.Errorf("start caddy proxy: %w", err)
	}

	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-signalCtx.Done()
	if err := caddyv2.Stop(); err != nil {
		return fmt.Errorf("stop caddy proxy: %w", err)
	}
	return nil
}
