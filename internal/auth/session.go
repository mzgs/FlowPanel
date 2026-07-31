package auth

import (
	"net/http"

	"flowpanel/internal/config"

	"github.com/alexedwards/scs/v2"
)

func NewSessionManager(cfg config.Config, store scs.Store) *scs.SessionManager {
	sessionManager := scs.New()
	if store != nil {
		sessionManager.Store = store
	}
	sessionManager.Cookie.Name = cfg.Session.CookieName
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.Path = "/"
	sessionManager.Cookie.Persist = true
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = cfg.Session.CookieSecure
	sessionManager.Lifetime = cfg.Session.Lifetime
	sessionManager.IdleTimeout = 0

	return sessionManager
}
