package httpx

import (
	"context"
	"fmt"
	stdhttp "net/http"

	"flowpanel/internal/packageruntime"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerPackageRuntimeRoutes(r chi.Router, key, label string, manager packageruntime.Manager) {
	statusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if manager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": fmt.Sprintf("%s runtime is not configured", key)})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			key: a.trackPackageRuntimeStatus(key, label, manager.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/"+key, statusHandler)
	r.Method(stdhttp.MethodHead, "/"+key, statusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if manager == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": fmt.Sprintf("%s runtime is not configured", key)})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin(key, action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End(key, action)
				a.app.Logger.Error(action+" "+key+" failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, key, key, label, "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End(key, action)

			pastTense := map[string]string{
				"install": fmt.Sprintf("Installed %s.", label),
				"remove":  fmt.Sprintf("Removed %s.", label),
				"start":   fmt.Sprintf("Started %s.", label),
				"stop":    fmt.Sprintf("Stopped %s.", label),
				"restart": fmt.Sprintf("Restarted %s.", label),
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, key, key, label, "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				key: a.trackPackageRuntimeStatus(key, label, manager.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/"+key+"/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return manager.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return manager.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/start", registerRuntimeAction("start", func(ctx context.Context) error {
		return manager.Start(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/stop", registerRuntimeAction("stop", func(ctx context.Context) error {
		return manager.Stop(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/restart", registerRuntimeAction("restart", func(ctx context.Context) error {
		return manager.Restart(ctx)
	}))
}
