package httpx

import (
	"context"
	stdhttp "net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerGoRoutes(r chi.Router) {
	goStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Golang == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "golang runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"golang": a.trackGoStatus(a.app.Golang.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/golang", goStatusHandler)
	r.Method(stdhttp.MethodHead, "/golang", goStatusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.Golang == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "golang runtime is not configured"})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("golang", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("golang", action)
				a.app.Logger.Error(action+" golang failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "golang", "golang", "Go", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("golang", action)

			pastTense := map[string]string{
				"install": "Installed Go.",
				"remove":  "Removed Go.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "golang", "golang", "Go", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"golang": a.trackGoStatus(a.app.Golang.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/golang/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.Golang.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/golang/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.Golang.Remove(ctx)
	}))
}
