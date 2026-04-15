package httpx

import (
	"context"
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerPM2Routes(r chi.Router) {
	pm2StatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"pm2": a.trackPM2Status(a.app.PM2.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/pm2", pm2StatusHandler)
	r.Method(stdhttp.MethodHead, "/pm2", pm2StatusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PM2 == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("pm2", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("pm2", action)
				a.app.Logger.Error(action+" pm2 failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("pm2", action)

			pastTense := map[string]string{
				"install": "Installed PM2.",
				"remove":  "Removed PM2.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"pm2": a.trackPM2Status(a.app.PM2.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/pm2/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.PM2.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.PM2.Remove(ctx)
	}))
}
