package httpx

import (
	"context"
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerNodeJSRoutes(r chi.Router) {
	nodeJSStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.NodeJS == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "nodejs runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"nodejs": a.trackNodeJSStatus(a.app.NodeJS.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/nodejs", nodeJSStatusHandler)
	r.Method(stdhttp.MethodHead, "/nodejs", nodeJSStatusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.NodeJS == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "nodejs runtime is not configured"})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("nodejs", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("nodejs", action)
				a.app.Logger.Error(action+" nodejs failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "nodejs", "nodejs", "Node.js", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("nodejs", action)

			pastTense := map[string]string{
				"install": "Installed Node.js.",
				"remove":  "Removed Node.js.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "nodejs", "nodejs", "Node.js", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"nodejs": a.trackNodeJSStatus(a.app.NodeJS.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/nodejs/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.NodeJS.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/nodejs/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.NodeJS.Remove(ctx)
	}))
}
