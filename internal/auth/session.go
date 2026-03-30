package auth

import (
	"net/http"
	"time"

	"flowpanel/internal/config"

	"github.com/alexedwards/scs/v2"
)

func NewSessionManager(cfg config.Config) *scs.SessionManager {
	sessionManager := scs.New()
	sessionManager.Cookie.Name = cfg.Session.CookieName
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.Path = "/"
	sessionManager.Cookie.Persist = true
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = cfg.IsProduction()
	sessionManager.Lifetime = cfg.Session.Lifetime
	sessionManager.IdleTimeout = 12 * time.Hour

	return sessionManager
}
