package httpx

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/url"
	"strings"

	"flowpanel/internal/alerts"
	"flowpanel/internal/app"
	"flowpanel/internal/auth"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func registerPanelAuthRoutes(router chi.Router, app *app.App) {
	loginRateLimiter := newAuthRateLimiter()

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

		userID := panelAuthUserID(app, r)
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
		if !ok || !panelSessionMatches(app, r, user.SessionVersion) {
			clearPanelAuthSession(app, r)
			writeAuthSession(w, false, !hasUsers, auth.PublicUser{})
			return
		}

		writeAuthSession(w, true, !hasUsers, user)
	})
	router.Method(stdhttp.MethodGet, "/api/auth/session", sessionHandler)
	router.Method(stdhttp.MethodHead, "/api/auth/session", sessionHandler)

	router.Method(stdhttp.MethodPost, "/api/auth/login", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		formRequest := strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
		writeError := func(status int, message string) {
			if formRequest {
				stdhttp.Redirect(w, r, "/?auth_error="+url.QueryEscape(message), stdhttp.StatusSeeOther)
				return
			}
			writeJSON(w, status, map[string]any{"error": message})
		}

		if app.Auth == nil {
			writeError(stdhttp.StatusServiceUnavailable, "panel auth is not configured")
			return
		}

		var input auth.LoginInput
		var err error
		if formRequest {
			err = r.ParseForm()
			input = auth.LoginInput{Username: r.PostFormValue("username"), Password: r.PostFormValue("password")}
		} else {
			err = decodeJSON(r, &input)
		}
		if err != nil {
			writeError(stdhttp.StatusBadRequest, "invalid request body")
			return
		}

		rateLimitKeys := authRateLimitKeys(r, input)
		if retryAfter, ok := loginRateLimiter.Allow(rateLimitKeys...); !ok {
			if formRequest {
				writeError(stdhttp.StatusTooManyRequests, "too many sign-in attempts; try again later")
			} else {
				writeAuthRateLimited(w, retryAfter)
			}
			return
		}

		user, err := app.Auth.Authenticate(r.Context(), input)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				attempts := loginRateLimiter.RecordFailure(rateLimitKeys...)
				if app.Alerts != nil {
					config, configErr := app.Alerts.Config(r.Context())
					if configErr != nil {
						app.Logger.Error("load login alert settings failed", zap.Error(configErr))
					} else if config.Enabled && attempts >= config.LoginFailureThreshold {
						address := clientIPAddress(r)
						key := "login:" + address + ":" + auth.NormalizeUsername(input.Username)
						if alertErr := app.Alerts.Trigger(r.Context(), alerts.TriggerInput{
							Key: key, Severity: "critical", Title: "Repeated login failures",
							Message: fmt.Sprintf("%d failed sign-in attempts for %q from %s.", attempts, auth.NormalizeUsername(input.Username), address),
						}); alertErr != nil {
							app.Logger.Error("trigger login failure alert failed", zap.Error(alertErr))
						}
					}
				}
				writeError(stdhttp.StatusUnauthorized, "invalid username or password")
				return
			}

			app.Logger.Error("panel login failed", zap.Error(err))
			writeError(stdhttp.StatusInternalServerError, "failed to sign in")
			return
		}

		if err := renewPanelSession(app, r, user); err != nil {
			app.Logger.Error("create panel login session failed", zap.Error(err))
			writeError(stdhttp.StatusInternalServerError, "failed to sign in")
			return
		}

		loginRateLimiter.Clear(rateLimitKeys...)
		if app.Alerts != nil {
			key := "login:" + clientIPAddress(r) + ":" + auth.NormalizeUsername(input.Username)
			if err := app.Alerts.Resolve(r.Context(), key); err != nil {
				app.Logger.Error("resolve login failure alert failed", zap.Error(err))
			}
		}
		if formRequest {
			stdhttp.Redirect(w, r, "/", stdhttp.StatusSeeOther)
		} else {
			writeAuthSession(w, true, false, user)
		}
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
