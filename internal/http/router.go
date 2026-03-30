package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	stdhttp "net/http"
	"path"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/domain"
	"flowpanel/web"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func NewRouter(app *app.App) (stdhttp.Handler, error) {
	panelHandler, err := newPanelHandler()
	if err != nil {
		return nil, err
	}

	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)
	router.Use(RequestLogger(app.Logger.Named("http")))
	router.Use(Recoverer(app.Logger.Named("panic")))
	router.Use(app.Sessions.LoadAndSave)

	healthHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	router.Method(stdhttp.MethodGet, "/healthz", healthHandler)
	router.Method(stdhttp.MethodHead, "/healthz", healthHandler)

	router.Route("/api", func(r chi.Router) {
		syncDomainsWithCaddy := func(ctx context.Context) error {
			return app.Caddy.Sync(ctx, app.Domains.List())
		}

		bootstrapHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"name":              "FlowPanel",
				"status":            "ok",
				"environment":       app.Config.Env,
				"admin_listen_addr": app.Config.AdminListenAddr,
				"cron_enabled":      app.Config.Cron.Enabled,
			})
		})
		r.Method(stdhttp.MethodGet, "/bootstrap", bootstrapHandler)
		r.Method(stdhttp.MethodHead, "/bootstrap", bootstrapHandler)

		phpStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": app.PHP.Status(r.Context()),
			})
		})

		phpInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			if err := app.PHP.Install(r.Context()); err != nil {
				app.Logger.Error("install php failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			status := app.PHP.Status(r.Context())
			if status.Ready {
				if err := syncDomainsWithCaddy(r.Context()); err != nil {
					app.Logger.Error("sync domains after php install failed", zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php installed but failed to republish domains",
					})
					return
				}
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		phpStartHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			if err := app.PHP.Start(r.Context()); err != nil {
				app.Logger.Error("start php failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			status := app.PHP.Status(r.Context())
			if status.Ready {
				if err := syncDomainsWithCaddy(r.Context()); err != nil {
					app.Logger.Error("sync domains after php start failed", zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php started but failed to republish domains",
					})
					return
				}
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		r.Method(stdhttp.MethodGet, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodHead, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodPost, "/php/install", phpInstallHandler)
		r.Method(stdhttp.MethodPost, "/php/start", phpStartHandler)

		domainsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"sites_base_path": app.Domains.BasePath(),
				"domains":         app.Domains.List(),
			})
		})

		domainsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input domain.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if input.Kind == domain.KindPHP {
				if app.PHP == nil {
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
						"error": "php runtime is not configured",
					})
					return
				}

				phpStatus := app.PHP.Status(r.Context())
				if !phpStatus.Ready {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "php runtime is not ready",
						"field_errors": map[string]string{
							"kind": phpStatus.Message,
						},
					})
					return
				}
			}

			record, err := app.Domains.Create(r.Context(), input)
			if err != nil {
				var validation domain.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, domain.ErrDuplicateHostname):
					writeJSON(w, stdhttp.StatusConflict, map[string]any{
						"error": "domain already exists",
						"field_errors": map[string]string{
							"hostname": "This hostname already exists.",
						},
					})
					return
				default:
					app.Logger.Error("create domain failed", zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to create domain",
					})
					return
				}
			}

			if err := syncDomainsWithCaddy(r.Context()); err != nil {
				_, removed, rollbackErr := app.Domains.Delete(r.Context(), record.ID)
				if rollbackErr != nil {
					app.Logger.Error("rollback created domain failed",
						zap.String("domain_id", record.ID),
						zap.Error(rollbackErr),
					)
				} else if !removed {
					app.Logger.Error("rollback created domain missing", zap.String("domain_id", record.ID))
				}
				app.Logger.Error("publish domain failed",
					zap.String("domain_id", record.ID),
					zap.String("hostname", record.Hostname),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to publish domain",
				})
				return
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"domain": record,
			})
		})

		domainsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input domain.UpdateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if input.Kind == domain.KindPHP {
				if app.PHP == nil {
					writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
						"error": "php runtime is not configured",
					})
					return
				}

				phpStatus := app.PHP.Status(r.Context())
				if !phpStatus.Ready {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "php runtime is not ready",
						"field_errors": map[string]string{
							"kind": phpStatus.Message,
						},
					})
					return
				}
			}

			domainID := chi.URLParam(r, "domainID")
			record, previous, err := app.Domains.Update(r.Context(), domainID, input)
			if err != nil {
				var validation domain.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, domain.ErrDuplicateHostname):
					writeJSON(w, stdhttp.StatusConflict, map[string]any{
						"error": "domain already exists",
						"field_errors": map[string]string{
							"hostname": "This hostname already exists.",
						},
					})
					return
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
					return
				default:
					app.Logger.Error("update domain failed",
						zap.String("domain_id", domainID),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to update domain",
					})
					return
				}
			}

			if err := syncDomainsWithCaddy(r.Context()); err != nil {
				if rollbackErr := app.Domains.Restore(r.Context(), previous); rollbackErr != nil {
					app.Logger.Error("rollback updated domain failed",
						zap.String("domain_id", previous.ID),
						zap.Error(rollbackErr),
					)
				}
				app.Logger.Error("publish updated domain failed",
					zap.String("domain_id", record.ID),
					zap.String("hostname", record.Hostname),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update domain",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain": record,
			})
		})

		domainsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			domainID := chi.URLParam(r, "domainID")
			record, removed, err := app.Domains.Delete(r.Context(), domainID)
			if err != nil {
				app.Logger.Error("delete domain failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete domain",
				})
				return
			}
			if !removed {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "domain not found",
				})
				return
			}

			if err := syncDomainsWithCaddy(r.Context()); err != nil {
				if rollbackErr := app.Domains.Restore(r.Context(), record); rollbackErr != nil {
					app.Logger.Error("rollback deleted domain failed",
						zap.String("domain_id", record.ID),
						zap.Error(rollbackErr),
					)
				}
				app.Logger.Error("publish deleted domain failed",
					zap.String("domain_id", record.ID),
					zap.String("hostname", record.Hostname),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete domain",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain": record,
			})
		})

		r.Method(stdhttp.MethodGet, "/domains", domainsListHandler)
		r.Method(stdhttp.MethodHead, "/domains", domainsListHandler)
		r.Method(stdhttp.MethodPost, "/domains", domainsCreateHandler)
		r.Method(stdhttp.MethodPut, "/domains/{domainID}", domainsUpdateHandler)
		r.Method(stdhttp.MethodDelete, "/domains/{domainID}", domainsDeleteHandler)

		r.NotFound(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{
				"error": "api route not found",
			})
		})
	})

	router.Method(stdhttp.MethodGet, "/", panelHandler)
	router.Method(stdhttp.MethodHead, "/", panelHandler)
	router.Method(stdhttp.MethodGet, "/*", panelHandler)
	router.Method(stdhttp.MethodHead, "/*", panelHandler)

	return router, nil
}

type panelHandler struct {
	index      []byte
	distFS     fs.FS
	fileServer stdhttp.Handler
}

func newPanelHandler() (*panelHandler, error) {
	distFS, err := web.DistFS()
	if err != nil {
		return nil, err
	}

	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, err
	}

	return &panelHandler{
		index:      index,
		distFS:     distFS,
		fileServer: stdhttp.FileServer(stdhttp.FS(distFS)),
	}, nil
}

func (h *panelHandler) ServeHTTP(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet && r.Method != stdhttp.MethodHead {
		stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}

	cleanPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if cleanPath == "." || cleanPath == "" {
		cleanPath = "index.html"
	}

	if file, err := h.distFS.Open(cleanPath); err == nil {
		if stat, statErr := file.Stat(); statErr == nil && !stat.IsDir() {
			_ = file.Close()
			h.fileServer.ServeHTTP(w, r)
			return
		}
		_ = file.Close()
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(stdhttp.StatusOK)
	if r.Method == stdhttp.MethodHead {
		return
	}

	_, _ = w.Write(h.index)
}

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeJSON(r *stdhttp.Request, payload any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(payload)
}
