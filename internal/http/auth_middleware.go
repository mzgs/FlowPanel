package httpx

import (
	stdhttp "net/http"
	"strings"

	"flowpanel/internal/app"

	"go.uber.org/zap"
)

const panelUserIDSessionKey = "panel_user_id"

func RequirePanelAuth(app *app.App) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app == nil || app.Sessions == nil || app.Auth == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "panel auth is not configured"})
				return
			}

			userID := strings.TrimSpace(app.Sessions.GetString(r.Context(), panelUserIDSessionKey))
			if userID == "" {
				writeAuthRequired(w, r)
				return
			}

			if _, ok, err := app.Auth.GetUser(r.Context(), userID); err != nil {
				app.Logger.Error("load authenticated panel user failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to verify session"})
				return
			} else if !ok {
				app.Sessions.Remove(r.Context(), panelUserIDSessionKey)
				writeAuthRequired(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthRequired(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r != nil && !strings.HasPrefix(r.URL.Path, "/api/") && requestPrefersHTML(r) {
		stdhttp.Redirect(w, r, "/", stdhttp.StatusFound)
		return
	}

	writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{"error": "authentication required"})
}
