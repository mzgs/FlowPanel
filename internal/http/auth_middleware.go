package httpx

import (
	stdhttp "net/http"

	"flowpanel/internal/app"
)

func RequirePanelAuth(_ *app.App) func(stdhttp.Handler) stdhttp.Handler {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			next.ServeHTTP(w, r)
		})
	}
}
