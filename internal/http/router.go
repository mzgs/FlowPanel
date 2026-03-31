package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	stdhttp "net/http"
	"path"
	"regexp"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/domain"
	filesvc "flowpanel/internal/files"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/systemstatus"
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

		systemStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"system": systemstatus.Inspect(r.Context()),
			})
		})
		r.Method(stdhttp.MethodGet, "/system", systemStatusHandler)
		r.Method(stdhttp.MethodHead, "/system", systemStatusHandler)

		mariaDBStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": app.MariaDB.Status(r.Context()),
			})
		})
		r.Method(stdhttp.MethodGet, "/mariadb", mariaDBStatusHandler)
		r.Method(stdhttp.MethodHead, "/mariadb", mariaDBStatusHandler)

		mariaDBRootPasswordHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			password, configured, err := app.MariaDB.RootPassword(r.Context())
			if err != nil {
				app.Logger.Error("read mariadb root password failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to read mariadb root password",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"root_password": password,
				"configured":    configured,
			})
		})
		r.Method(stdhttp.MethodGet, "/mariadb/root-password", mariaDBRootPasswordHandler)
		r.Method(stdhttp.MethodHead, "/mariadb/root-password", mariaDBRootPasswordHandler)

		mariaDBRootPasswordUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			var input struct {
				Password string `json:"password"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.MariaDB.SetRootPassword(r.Context(), input.Password); err != nil {
				var validation mariadb.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("update mariadb root password failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update mariadb root password",
				})
				return
			}

			password, configured, err := app.MariaDB.RootPassword(r.Context())
			if err != nil {
				app.Logger.Error("read mariadb root password failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to read mariadb root password",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"root_password": password,
				"configured":    configured,
			})
		})
		r.Method(stdhttp.MethodPut, "/mariadb/root-password", mariaDBRootPasswordUpdateHandler)

		mariaDBInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			if err := app.MariaDB.Install(r.Context()); err != nil {
				app.Logger.Error("install mariadb failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": app.MariaDB.Status(r.Context()),
			})
		})
		r.Method(stdhttp.MethodPost, "/mariadb/install", mariaDBInstallHandler)

		mariaDBDatabasesListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			records, err := app.MariaDB.ListDatabases(r.Context())
			if err != nil {
				app.Logger.Error("list mariadb databases failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to list databases",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"databases": records,
			})
		})
		r.Method(stdhttp.MethodGet, "/mariadb/databases", mariaDBDatabasesListHandler)
		r.Method(stdhttp.MethodHead, "/mariadb/databases", mariaDBDatabasesListHandler)

		mariaDBDatabaseCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			var input mariadb.CreateDatabaseInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			record, err := app.MariaDB.CreateDatabase(r.Context(), input)
			if err != nil {
				var validation mariadb.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, mariadb.ErrDatabaseAlreadyExists):
					writeJSON(w, stdhttp.StatusConflict, map[string]any{
						"error": "database already exists",
						"field_errors": map[string]string{
							"name": "This database already exists.",
						},
					})
					return
				default:
					app.Logger.Error("create mariadb database failed", zap.Error(err))
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to create database",
					})
					return
				}
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"database": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/mariadb/databases", mariaDBDatabaseCreateHandler)

		mariaDBDatabaseUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			var input mariadb.UpdateDatabaseInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			databaseName := chi.URLParam(r, "databaseName")
			record, err := app.MariaDB.UpdateDatabase(r.Context(), databaseName, input)
			if err != nil {
				var validation mariadb.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, mariadb.ErrDatabaseNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "database not found",
					})
					return
				default:
					app.Logger.Error("update mariadb database failed",
						zap.String("database_name", databaseName),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to update database",
					})
					return
				}
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"database": record,
			})
		})
		r.Method(stdhttp.MethodPut, "/mariadb/databases/{databaseName}", mariaDBDatabaseUpdateHandler)

		mariaDBDatabaseDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			databaseName := chi.URLParam(r, "databaseName")
			input := mariadb.DeleteDatabaseInput{
				Username: strings.TrimSpace(r.URL.Query().Get("username")),
			}

			if err := app.MariaDB.DeleteDatabase(r.Context(), databaseName, input); err != nil {
				var validation mariadb.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, mariadb.ErrDatabaseNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "database not found",
					})
					return
				default:
					app.Logger.Error("delete mariadb database failed",
						zap.String("database_name", databaseName),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to delete database",
					})
					return
				}
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"ok": true,
			})
		})
		r.Method(stdhttp.MethodDelete, "/mariadb/databases/{databaseName}", mariaDBDatabaseDeleteHandler)

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
							"hostname": "This domain already exists.",
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
							"hostname": "This domain already exists.",
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

		filesListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			listing, err := app.Files.List(r.URL.Query().Get("path"))
			if err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, listing)
		})

		filesCreateDirectoryHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.CreateDirectory(input.Path, input.Name); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
		})

		filesCreateFileHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.CreateFile(input.Path, input.Name); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
		})

		filesRenameHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			newPath, err := app.Files.Rename(input.Path, input.Name)
			if err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"path": newPath,
			})
		})

		filesDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			if err := app.Files.Delete(r.URL.Query().Get("path")); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
		})

		filesContentHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			content, err := app.Files.ReadTextFile(r.URL.Query().Get("path"))
			if err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, content)
		})

		filesUpdateContentHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.WriteTextFile(input.Path, input.Content); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
		})

		filesUploadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			if err := r.ParseMultipartForm(64 << 20); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid upload payload",
				})
				return
			}

			if err := app.Files.Upload(r.FormValue("path"), r.MultipartForm.File["files"]); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{"ok": true})
		})

		filesDownloadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			absolutePath, name, err := app.Files.DownloadPath(r.URL.Query().Get("path"))
			if err != nil {
				writeFileError(w, err)
				return
			}

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			stdhttp.ServeFile(w, r, absolutePath)
		})

		filesTransferHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Mode   string   `json:"mode"`
				Paths  []string `json:"paths"`
				Target string   `json:"target"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.Transfer(input.Mode, input.Paths, input.Target); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
		})

		r.Method(stdhttp.MethodGet, "/files", filesListHandler)
		r.Method(stdhttp.MethodPost, "/files/directories", filesCreateDirectoryHandler)
		r.Method(stdhttp.MethodPost, "/files/documents", filesCreateFileHandler)
		r.Method(stdhttp.MethodPost, "/files/rename", filesRenameHandler)
		r.Method(stdhttp.MethodDelete, "/files", filesDeleteHandler)
		r.Method(stdhttp.MethodGet, "/files/content", filesContentHandler)
		r.Method(stdhttp.MethodPut, "/files/content", filesUpdateContentHandler)
		r.Method(stdhttp.MethodPost, "/files/upload", filesUploadHandler)
		r.Method(stdhttp.MethodGet, "/files/download", filesDownloadHandler)
		r.Method(stdhttp.MethodPost, "/files/transfer", filesTransferHandler)

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

var panelAssetPattern = regexp.MustCompile(`(?:src|href)=["']([^"']+)["']`)

func newPanelHandler() (*panelHandler, error) {
	distFS, err := web.DistFS()
	if err != nil {
		return nil, err
	}

	return newPanelHandlerWithFS(distFS)
}

func newPanelHandlerWithFS(distFS fs.FS) (*panelHandler, error) {
	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, err
	}

	if err := validatePanelAssets(distFS, index); err != nil {
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

	if cleanPath != "index.html" && path.Ext(cleanPath) != "" {
		stdhttp.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(stdhttp.StatusOK)
	if r.Method == stdhttp.MethodHead {
		return
	}

	_, _ = w.Write(h.index)
}

func validatePanelAssets(distFS fs.FS, index []byte) error {
	matches := panelAssetPattern.FindAllSubmatch(index, -1)
	for _, match := range matches {
		assetPath, ok := normalizePanelAssetPath(string(match[1]))
		if !ok {
			continue
		}

		stat, err := fs.Stat(distFS, assetPath)
		if err != nil {
			return fmt.Errorf("panel bundle is invalid: missing asset %q referenced by index.html: %w", assetPath, err)
		}
		if stat.IsDir() {
			return fmt.Errorf("panel bundle is invalid: asset %q referenced by index.html is a directory", assetPath)
		}
	}

	return nil
}

func normalizePanelAssetPath(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" ||
		strings.HasPrefix(ref, "#") ||
		strings.HasPrefix(ref, "http://") ||
		strings.HasPrefix(ref, "https://") ||
		strings.HasPrefix(ref, "//") ||
		strings.HasPrefix(ref, "data:") {
		return "", false
	}

	ref = strings.SplitN(ref, "#", 2)[0]
	ref = strings.SplitN(ref, "?", 2)[0]
	ref = strings.TrimPrefix(ref, "./")
	ref = strings.TrimPrefix(ref, "/")
	if ref == "" || ref == "index.html" {
		return "", false
	}

	return ref, true
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

func writeFileError(w stdhttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filesvc.ErrNotFound):
		writeJSON(w, stdhttp.StatusNotFound, map[string]any{
			"error": "file or directory not found",
		})
	case errors.Is(err, filesvc.ErrInvalidPath):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid file path",
		})
	case errors.Is(err, filesvc.ErrDirectoryExpected):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "directory expected",
		})
	case errors.Is(err, filesvc.ErrFileExpected):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "file expected",
		})
	case errors.Is(err, filesvc.ErrUnsupportedEntry):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "symlinks are not supported",
		})
	case errors.Is(err, filesvc.ErrBinaryFile):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "file is not editable as text",
		})
	case errors.Is(err, filesvc.ErrEditableFileTooBig):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "file is too large to edit in the panel",
		})
	case errors.Is(err, filesvc.ErrInvalidTransfer):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid move or copy operation",
		})
	case errors.Is(err, fs.ErrExist):
		writeJSON(w, stdhttp.StatusConflict, map[string]any{
			"error": "file already exists",
		})
	default:
		writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
			"error": "file operation failed",
		})
	}
}
