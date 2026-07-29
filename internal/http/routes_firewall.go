package httpx

import (
	"context"
	"errors"
	stdhttp "net/http"

	"flowpanel/internal/firewall"
	"flowpanel/internal/settings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type updateFirewallRequest struct {
	Enabled bool `json:"enabled"`
}

func (a *apiRoutes) registerFirewallRoutes(r chi.Router) {
	statusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Firewall == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "managed firewall is unavailable"})
			return
		}
		cfg, err := a.firewallConfig(r.Context())
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load firewall settings"})
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"firewall": a.app.Firewall.Status(r.Context(), cfg)})
	})

	updateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Firewall == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "managed firewall is unavailable"})
			return
		}
		var input updateFirewallRequest
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		cfg, err := a.firewallConfig(r.Context())
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load firewall settings"})
			return
		}
		status, err := a.app.Firewall.SetEnabled(r.Context(), input.Enabled, cfg)
		if err != nil {
			a.app.Logger.Error("update managed firewall failed", zap.Bool("enabled", input.Enabled), zap.Error(err))
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}
		state := "disabled"
		if input.Enabled {
			state = "enabled"
		}
		a.mutationEvent(r.Context(), "security", "update", "firewall", "managed", "Managed firewall", "succeeded", "Managed firewall "+state+".")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"firewall": status})
	})

	reconcileHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := a.reconcileFirewall(r.Context()); err != nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": err.Error()})
			return
		}
		cfg, _ := a.firewallConfig(r.Context())
		writeJSON(w, stdhttp.StatusOK, map[string]any{"firewall": a.app.Firewall.Status(r.Context(), cfg)})
	})

	r.Method(stdhttp.MethodGet, "/firewall", statusHandler)
	r.Method(stdhttp.MethodHead, "/firewall", statusHandler)
	r.Method(stdhttp.MethodPut, "/firewall", updateHandler)
	r.Method(stdhttp.MethodPost, "/firewall/reconcile", reconcileHandler)
}

func (a *apiRoutes) firewallConfig(ctx context.Context) (firewall.Config, error) {
	if a == nil || a.app == nil || a.app.Settings == nil {
		return firewall.Config{}, errors.New("settings are unavailable")
	}
	record, err := a.app.Settings.Get(ctx)
	if err != nil {
		return firewall.Config{}, err
	}
	return firewallConfigFromSettings(a.app.Config.AdminListenAddr, a.app.Config.PublicHTTPAddr, a.app.Config.PublicHTTPSAddr, record), nil
}

func firewallConfigFromSettings(adminAddr, httpAddr, httpsAddr string, record settings.Record) firewall.Config {
	return firewall.Config{
		AdminAddr:       adminAddr,
		HTTPAddr:        httpAddr,
		HTTPSAddr:       httpsAddr,
		FTPEnabled:      record.FTPEnabled,
		FTPPort:         record.FTPPort,
		FTPPassivePorts: record.FTPPassivePorts,
	}
}

func (a *apiRoutes) reconcileFirewall(ctx context.Context) error {
	if a == nil || a.app == nil || a.app.Firewall == nil {
		return nil
	}
	cfg, err := a.firewallConfig(ctx)
	if err != nil {
		return err
	}
	if err := a.app.Firewall.Reconcile(ctx, cfg); err != nil {
		a.app.Logger.Error("reconcile managed firewall failed", zap.Error(err))
		return err
	}
	return nil
}
