package httpx

import (
	"errors"
	stdhttp "net/http"

	"flowpanel/internal/alerts"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerAlertRoutes(r chi.Router) {
	settingsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Alerts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "alert service is not configured"})
			return
		}
		config, err := a.app.Alerts.PublicConfig(r.Context())
		if err != nil {
			a.app.Logger.Error("load alert settings failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load alert settings"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"settings": config})
	})
	r.Method(stdhttp.MethodGet, "/alerts/settings", settingsHandler)
	r.Method(stdhttp.MethodHead, "/alerts/settings", settingsHandler)

	r.Method(stdhttp.MethodPut, "/alerts/settings", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Alerts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "alert service is not configured"})
			return
		}
		var input alerts.UpdateInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		config, err := a.app.Alerts.UpdateConfig(r.Context(), input)
		if err != nil {
			var validation alerts.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("update alert settings failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update alert settings"})
			return
		}
		a.mutationEvent(r.Context(), "settings", "update_notifications", "settings", "notifications", "Notifications", "succeeded", "Updated notification settings.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"settings": config})
	}))

	r.Method(stdhttp.MethodPost, "/alerts/test", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Alerts == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "alert service is not configured"})
			return
		}
		if err := a.app.Alerts.SendTest(r.Context()); err != nil {
			var validation alerts.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("send test notification failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"sent": true})
	}))
}
