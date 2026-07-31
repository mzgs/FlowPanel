package httpx

import (
	stdhttp "net/http"
	"strings"
	"time"

	"flowpanel/internal/app"

	"go.uber.org/zap"
)

const (
	panelUserIDSessionKey       = "panel_user_id"
	panelSessionVersionKey      = "panel_session_version"
	panelLastActivitySessionKey = "panel_last_activity"
)

func RequirePanelAuth(app *app.App) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app == nil || app.Sessions == nil || app.Auth == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "panel auth is not configured"})
				return
			}

			userID := panelAuthUserID(app, r)
			if userID == "" {
				writeAuthRequired(w, r)
				return
			}

			if user, ok, err := app.Auth.GetUser(r.Context(), userID); err != nil {
				app.Logger.Error("load authenticated panel user failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to verify session"})
				return
			} else if !ok || !panelSessionMatches(app, r, user.SessionVersion) {
				clearPanelAuthSession(app, r)
				writeAuthRequired(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func panelSessionMatches(app *app.App, r *stdhttp.Request, version string) bool {
	if app == nil || app.Sessions == nil || r == nil || strings.TrimSpace(version) == "" ||
		app.Sessions.GetString(r.Context(), panelSessionVersionKey) != version {
		return false
	}

	now := time.Now()
	lastActivity := app.Sessions.GetInt64(r.Context(), panelLastActivitySessionKey)
	timeout := 12 * time.Hour
	if app.Settings != nil {
		timeout = app.Settings.LoginTimeout()
	}
	if lastActivity > 0 && now.Sub(time.Unix(lastActivity, 0)) > timeout {
		return false
	}
	app.Sessions.Put(r.Context(), panelLastActivitySessionKey, now.Unix())
	return true
}

func clearPanelAuthSession(app *app.App, r *stdhttp.Request) {
	if app == nil || app.Sessions == nil || r == nil {
		return
	}
	app.Sessions.Remove(r.Context(), panelUserIDSessionKey)
	app.Sessions.Remove(r.Context(), panelSessionVersionKey)
	app.Sessions.Remove(r.Context(), panelLastActivitySessionKey)
}

func writeAuthRequired(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r != nil && !strings.HasPrefix(r.URL.Path, "/api/") && requestPrefersHTML(r) {
		stdhttp.Redirect(w, r, "/", stdhttp.StatusFound)
		return
	}

	writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{"error": "authentication required"})
}
