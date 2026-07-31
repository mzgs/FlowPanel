package httpx

import (
	"fmt"
	stdhttp "net/http"
	"strings"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
)

func panelAuthUserID(app *app.App, r *stdhttp.Request) string {
	if app == nil || app.Sessions == nil || r == nil {
		return ""
	}
	return strings.TrimSpace(app.Sessions.GetString(r.Context(), panelUserIDSessionKey))
}

func renewPanelSession(app *app.App, r *stdhttp.Request, user auth.PublicUser) error {
	if err := app.Sessions.RenewToken(r.Context()); err != nil {
		return fmt.Errorf("renew panel session token: %w", err)
	}

	app.Sessions.Put(r.Context(), panelUserIDSessionKey, user.ID)
	app.Sessions.Put(r.Context(), panelSessionVersionKey, user.SessionVersion)
	app.Sessions.Put(r.Context(), panelLastActivitySessionKey, time.Now().Unix())
	return nil
}
