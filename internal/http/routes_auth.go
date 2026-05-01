package httpx

import (
	"errors"
	stdhttp "net/http"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func registerPanelAuthRoutes(router chi.Router, app *app.App) {
	sessionHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if app.Auth == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
				"authenticated": false,
				"error":         "panel auth is not configured",
			})
			return
		}

		hasUsers, err := app.Auth.HasUsers(r.Context())
		if err != nil {
			app.Logger.Error("check panel setup failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load auth session"})
			return
		}

		userID := strings.TrimSpace(app.Sessions.GetString(r.Context(), panelUserIDSessionKey))
		if userID == "" {
			writeAuthSession(w, false, !hasUsers, auth.PublicUser{})
			return
		}

		user, ok, err := app.Auth.GetUser(r.Context(), userID)
		if err != nil {
			app.Logger.Error("load panel auth session failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to load auth session"})
			return
		}
		if !ok {
			app.Sessions.Remove(r.Context(), panelUserIDSessionKey)
			writeAuthSession(w, false, !hasUsers, auth.PublicUser{})
			return
		}

		writeAuthSession(w, true, !hasUsers, user)
	})
	router.Method(stdhttp.MethodGet, "/api/auth/session", sessionHandler)
	router.Method(stdhttp.MethodHead, "/api/auth/session", sessionHandler)

	router.Method(stdhttp.MethodPost, "/api/auth/setup", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if app.Auth == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "panel auth is not configured"})
			return
		}

		var input auth.CreateInitialAdminInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}

		user, err := app.Auth.CreateInitialAdmin(r.Context(), input)
		if err != nil {
			var validation auth.ValidationErrors
			if errors.As(err, &validation) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error":        "validation failed",
					"field_errors": map[string]string(validation),
				})
				return
			}

			app.Logger.Error("create initial panel admin failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create admin user"})
			return
		}

		if err := renewPanelSession(app, r, user.ID); err != nil {
			app.Logger.Error("create panel setup session failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "admin user created but sign-in failed"})
			return
		}

		writeAuthSession(w, true, false, user)
	}))

	router.Method(stdhttp.MethodPost, "/api/auth/login", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if app.Auth == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "panel auth is not configured"})
			return
		}

		var input auth.LoginInput
		if err := decodeJSON(r, &input); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid request body"})
			return
		}

		user, err := app.Auth.Authenticate(r.Context(), input)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{"error": "invalid username or password"})
				return
			}

			app.Logger.Error("panel login failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to sign in"})
			return
		}

		if err := renewPanelSession(app, r, user.ID); err != nil {
			app.Logger.Error("create panel login session failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to sign in"})
			return
		}

		writeAuthSession(w, true, false, user)
	}))

	router.Method(stdhttp.MethodPost, "/api/auth/logout", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if err := app.Sessions.Destroy(r.Context()); err != nil {
			app.Logger.Error("destroy panel session failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to sign out"})
			return
		}

		writeAuthSession(w, false, false, auth.PublicUser{})
	}))
}

func renewPanelSession(app *app.App, r *stdhttp.Request, userID string) error {
	if err := app.Sessions.RenewToken(r.Context()); err != nil {
		return err
	}

	app.Sessions.Put(r.Context(), panelUserIDSessionKey, userID)
	return nil
}

func writeAuthSession(w stdhttp.ResponseWriter, authenticated bool, setupRequired bool, user auth.PublicUser) {
	w.Header().Set("Cache-Control", "no-store")
	payload := map[string]any{
		"authenticated":  authenticated,
		"setup_required": setupRequired,
	}
	if authenticated {
		payload["user"] = user
	}

	writeJSON(w, stdhttp.StatusOK, payload)
}
