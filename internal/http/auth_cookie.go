package httpx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/app"
)

const panelAuthCookieSuffix = "_auth"

func panelAuthCookieName(app *app.App) string {
	name := strings.TrimSpace(app.Config.Session.CookieName)
	if name == "" {
		name = "flowpanel_session"
	}
	return name + panelAuthCookieSuffix
}

func panelAuthUserID(app *app.App, r *stdhttp.Request) string {
	if app == nil || app.Sessions == nil {
		return ""
	}
	if userID := strings.TrimSpace(app.Sessions.GetString(r.Context(), panelUserIDSessionKey)); userID != "" {
		return userID
	}

	cookie, err := r.Cookie(panelAuthCookieName(app))
	if err != nil {
		return ""
	}

	return verifyPanelAuthCookie(app, cookie.Value)
}

func writePanelAuthCookie(app *app.App, w stdhttp.ResponseWriter, userID string) {
	expires := time.Now().Add(app.Config.Session.Lifetime).UTC()
	stdhttp.SetCookie(w, panelCookie(app, signedPanelAuthCookieValue(app, userID, expires), expires, int(time.Until(expires).Seconds())))
}

func clearPanelAuthCookie(app *app.App, w stdhttp.ResponseWriter) {
	stdhttp.SetCookie(w, panelCookie(app, "", time.Unix(1, 0), -1))
}

func panelCookie(app *app.App, value string, expires time.Time, maxAge int) *stdhttp.Cookie {
	return &stdhttp.Cookie{
		Name:     panelAuthCookieName(app),
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: stdhttp.SameSiteLaxMode,
		Secure:   app.Config.Session.CookieSecure,
	}
}

func signedPanelAuthCookieValue(app *app.App, userID string, expires time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(userID))) + "." + strconv.FormatInt(expires.Unix(), 10)
	return payload + "." + panelAuthSignature(app, payload)
}

func verifyPanelAuthCookie(app *app.App, value string) string {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(panelAuthSignature(app, parts[0]+"."+parts[1])), []byte(parts[2])) != 1 {
		return ""
	}

	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().After(time.Unix(expires, 0)) {
		return ""
	}

	userID, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(userID))
}

func panelAuthSignature(app *app.App, payload string) string {
	mac := hmac.New(sha256.New, []byte(app.Config.Session.Secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func renewPanelSession(app *app.App, r *stdhttp.Request, w stdhttp.ResponseWriter, userID string) error {
	if err := app.Sessions.RenewToken(r.Context()); err != nil {
		return fmt.Errorf("renew panel session token: %w", err)
	}

	writePanelAuthCookie(app, w, userID)
	return nil
}
