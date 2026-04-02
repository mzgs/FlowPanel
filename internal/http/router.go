package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	stdhttp "net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/backup"
	flowcron "flowpanel/internal/cron"
	"flowpanel/internal/domain"
	eventlog "flowpanel/internal/events"
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
		recordEvent := func(ctx context.Context, input eventlog.CreateInput) {
			if app == nil || app.Events == nil {
				return
			}
			if _, err := app.Events.Record(ctx, input); err != nil {
				app.Logger.Error("record event failed", zap.Error(err))
			}
		}
		mutationEvent := func(ctx context.Context, category, action, resourceType, resourceID, resourceLabel, status, message string) {
			recordEvent(ctx, eventlog.CreateInput{
				Actor:         "panel",
				Category:      category,
				Action:        action,
				ResourceType:  resourceType,
				ResourceID:    resourceID,
				ResourceLabel: resourceLabel,
				Status:        status,
				Message:       message,
			})
		}

		bootstrapHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"name":              "FlowPanel",
				"status":            "ok",
				"environment":       app.Config.Env,
				"admin_listen_addr": app.Config.AdminListenAddr,
				"phpmyadmin_addr":   app.Config.PHPMyAdminAddr,
				"cron_enabled":      app.Config.Cron.Enabled,
			})
		})
		r.Method(stdhttp.MethodGet, "/bootstrap", bootstrapHandler)
		r.Method(stdhttp.MethodHead, "/bootstrap", bootstrapHandler)

		eventsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Events == nil {
				writeJSON(w, stdhttp.StatusOK, map[string]any{
					"events": []eventlog.Record{},
				})
				return
			}

			limit := 100
			if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
				parsedLimit, err := strconv.Atoi(rawLimit)
				if err != nil {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "limit must be a valid integer",
					})
					return
				}
				limit = parsedLimit
			}

			records, err := app.Events.List(r.Context(), limit)
			if err != nil {
				app.Logger.Error("list events failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to list events",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"events": records,
			})
		})
		r.Method(stdhttp.MethodGet, "/events", eventsListHandler)
		r.Method(stdhttp.MethodHead, "/events", eventsListHandler)

		backupsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			records, err := app.Backups.List(r.Context())
			if err != nil {
				app.Logger.Error("list backups failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to list backups",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"backups": records,
			})
		})
		r.Method(stdhttp.MethodGet, "/backups", backupsListHandler)
		r.Method(stdhttp.MethodHead, "/backups", backupsListHandler)

		backupsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			input := backup.CreateInput{
				IncludePanelData: true,
				IncludeSites:     true,
				IncludeDatabases: true,
			}
			var payload struct {
				IncludePanelData *bool `json:"include_panel_data"`
				IncludeSites     *bool `json:"include_sites"`
				IncludeDatabases *bool `json:"include_databases"`
			}
			if r.Body != nil {
				if err := decodeJSON(r, &payload); err != nil && !errors.Is(err, io.EOF) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid request body",
					})
					return
				}
			}
			if payload.IncludePanelData != nil {
				input.IncludePanelData = *payload.IncludePanelData
			}
			if payload.IncludeSites != nil {
				input.IncludeSites = *payload.IncludeSites
			}
			if payload.IncludeDatabases != nil {
				input.IncludeDatabases = *payload.IncludeDatabases
			}

			record, err := app.Backups.Create(r.Context(), input)
			if err != nil {
				var validation backup.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}
				app.Logger.Error("create backup failed", zap.Error(err))
				mutationEvent(r.Context(), "backups", "create", "backup", "backup", "FlowPanel backup", "failed", "Failed to create a backup archive.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create backup",
				})
				return
			}

			mutationEvent(r.Context(), "backups", "create", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Created backup %q.", record.Name))

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"backup": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/backups", backupsCreateHandler)

		backupsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			name := chi.URLParam(r, "backupName")
			if err := app.Backups.Delete(r.Context(), name); err != nil {
				switch {
				case errors.Is(err, backup.ErrInvalidName):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid backup name",
					})
					return
				case errors.Is(err, backup.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "backup not found",
					})
					return
				default:
					app.Logger.Error("delete backup failed",
						zap.String("backup_name", name),
						zap.Error(err),
					)
					mutationEvent(r.Context(), "backups", "delete", "backup", name, name, "failed", "Failed to delete a backup archive.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to delete backup",
					})
					return
				}
			}

			mutationEvent(r.Context(), "backups", "delete", "backup", name, name, "succeeded", fmt.Sprintf("Deleted backup %q.", name))

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"ok": true,
			})
		})
		r.Method(stdhttp.MethodDelete, "/backups/{backupName}", backupsDeleteHandler)

		backupsDownloadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			absolutePath, name, err := app.Backups.DownloadPath(chi.URLParam(r, "backupName"))
			if err != nil {
				writeBackupError(w, err)
				return
			}

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			stdhttp.ServeFile(w, r, absolutePath)
		})
		r.Method(stdhttp.MethodGet, "/backups/{backupName}/download", backupsDownloadHandler)

		cronListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			snapshot := app.Cron.Snapshot()
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"enabled": snapshot.Enabled,
				"started": snapshot.Started,
				"jobs":    snapshot.Jobs,
			})
		})
		r.Method(stdhttp.MethodGet, "/cron", cronListHandler)
		r.Method(stdhttp.MethodHead, "/cron", cronListHandler)

		cronCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			var input flowcron.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			record, err := app.Cron.Create(r.Context(), input)
			if err != nil {
				var validation flowcron.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("create cron job failed", zap.Error(err))
				mutationEvent(r.Context(), "cron", "create", "cron_job", strings.TrimSpace(input.Name), strings.TrimSpace(input.Name), "failed", "Failed to create cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create cron job",
				})
				return
			}

			mutationEvent(r.Context(), "cron", "create", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Created cron job %q.", record.Name))

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"job": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/cron", cronCreateHandler)

		cronUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			jobID := chi.URLParam(r, "jobID")

			var input flowcron.UpdateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			record, err := app.Cron.Update(r.Context(), jobID, input)
			if err != nil {
				var validation flowcron.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}
				if errors.Is(err, flowcron.ErrNotFound) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "cron job not found",
					})
					return
				}

				app.Logger.Error("update cron job failed",
					zap.String("job_id", jobID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "cron", "update", "cron_job", jobID, jobID, "failed", "Failed to update cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update cron job",
				})
				return
			}

			mutationEvent(r.Context(), "cron", "update", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Updated cron job %q.", record.Name))

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"job": record,
			})
		})
		r.Method(stdhttp.MethodPut, "/cron/{jobID}", cronUpdateHandler)

		cronRunHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			jobID := chi.URLParam(r, "jobID")
			record, err := app.Cron.RunNow(jobID)
			if err != nil {
				if errors.Is(err, flowcron.ErrNotFound) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "cron job not found",
					})
					return
				}

				app.Logger.Error("run cron job failed",
					zap.String("job_id", jobID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "cron", "run", "cron_job", jobID, jobID, "failed", "Failed to run cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to run cron job",
				})
				return
			}

			mutationEvent(r.Context(), "cron", "run", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Triggered cron job %q.", record.Name))

			writeJSON(w, stdhttp.StatusAccepted, map[string]any{
				"job": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/cron/{jobID}/run", cronRunHandler)

		cronDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			jobID := chi.URLParam(r, "jobID")
			_, deleted, err := app.Cron.Delete(r.Context(), jobID)
			if err != nil {
				app.Logger.Error("delete cron job failed",
					zap.String("job_id", jobID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "cron", "delete", "cron_job", jobID, jobID, "failed", "Failed to delete cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete cron job",
				})
				return
			}
			if !deleted {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "cron job not found",
				})
				return
			}

			mutationEvent(r.Context(), "cron", "delete", "cron_job", jobID, jobID, "succeeded", fmt.Sprintf("Deleted cron job %q.", jobID))

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"ok": true,
			})
		})
		r.Method(stdhttp.MethodDelete, "/cron/{jobID}", cronDeleteHandler)

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
				mutationEvent(r.Context(), "database", "update", "mariadb", "root-password", "MariaDB root password", "failed", "Failed to update the MariaDB root password.")
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

			mutationEvent(r.Context(), "database", "update", "mariadb", "root-password", "MariaDB root password", "succeeded", "Updated the MariaDB root password.")

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
				mutationEvent(r.Context(), "runtime", "install", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			mutationEvent(r.Context(), "runtime", "install", "mariadb", "mariadb", "MariaDB", "succeeded", "Installed MariaDB.")

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
					mutationEvent(r.Context(), "database", "create", "database", strings.TrimSpace(input.Name), strings.TrimSpace(input.Name), "failed", "Failed to create database.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to create database",
					})
					return
				}
			}

			mutationEvent(r.Context(), "database", "create", "database", record.Name, record.Name, "succeeded", fmt.Sprintf("Created database %q for %q.", record.Name, record.Username))

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
					mutationEvent(r.Context(), "database", "update", "database", databaseName, databaseName, "failed", "Failed to update database.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to update database",
					})
					return
				}
			}

			mutationEvent(r.Context(), "database", "update", "database", record.Name, record.Name, "succeeded", fmt.Sprintf("Updated database %q.", record.Name))

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
					mutationEvent(r.Context(), "database", "delete", "database", databaseName, databaseName, "failed", "Failed to delete database.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to delete database",
					})
					return
				}
			}

			mutationEvent(r.Context(), "database", "delete", "database", databaseName, databaseName, "succeeded", fmt.Sprintf("Deleted database %q.", databaseName))

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
				mutationEvent(r.Context(), "runtime", "install", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			status := app.PHP.Status(r.Context())
			if status.Ready {
				if err := syncDomainsWithCaddy(r.Context()); err != nil {
					app.Logger.Error("sync domains after php install failed", zap.Error(err))
					mutationEvent(r.Context(), "runtime", "install", "php", "php", "PHP", "failed", "PHP installed but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php installed but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(r.Context(), "runtime", "install", "php", "php", "PHP", "succeeded", "Installed PHP.")

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
				mutationEvent(r.Context(), "runtime", "start", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			status := app.PHP.Status(r.Context())
			if status.Ready {
				if err := syncDomainsWithCaddy(r.Context()); err != nil {
					app.Logger.Error("sync domains after php start failed", zap.Error(err))
					mutationEvent(r.Context(), "runtime", "start", "php", "php", "PHP", "failed", "PHP started but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php started but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(r.Context(), "runtime", "start", "php", "php", "PHP", "succeeded", "Started PHP.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		r.Method(stdhttp.MethodGet, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodHead, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodPost, "/php/install", phpInstallHandler)
		r.Method(stdhttp.MethodPost, "/php/start", phpStartHandler)

		phpMyAdminStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": app.PHPMyAdmin.Status(r.Context()),
			})
		})

		phpMyAdminInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			if err := app.PHPMyAdmin.Install(r.Context()); err != nil {
				app.Logger.Error("install phpmyadmin failed", zap.Error(err))
				mutationEvent(r.Context(), "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			status := app.PHPMyAdmin.Status(r.Context())
			if status.Installed && app.PHP != nil {
				phpStatus := app.PHP.Status(r.Context())
				if phpStatus.Ready {
					if err := syncDomainsWithCaddy(r.Context()); err != nil {
						app.Logger.Error("sync domains after phpmyadmin install failed", zap.Error(err))
						mutationEvent(r.Context(), "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", "phpMyAdmin installed but failed to republish routes.")
						writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
							"error": "phpmyadmin installed but failed to republish routes",
						})
						return
					}
				}
			}

			mutationEvent(r.Context(), "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", "Installed phpMyAdmin.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": status,
			})
		})

		r.Method(stdhttp.MethodGet, "/phpmyadmin", phpMyAdminStatusHandler)
		r.Method(stdhttp.MethodHead, "/phpmyadmin", phpMyAdminStatusHandler)
		r.Method(stdhttp.MethodPost, "/phpmyadmin/install", phpMyAdminInstallHandler)

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
					mutationEvent(r.Context(), "domains", "create", "domain", strings.TrimSpace(input.Hostname), strings.TrimSpace(input.Hostname), "failed", "Failed to create domain.")
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
				mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "failed", "Created domain record but failed to publish it.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to publish domain",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Created domain %q.", record.Hostname))

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
					mutationEvent(r.Context(), "domains", "update", "domain", domainID, domainID, "failed", "Failed to update domain.")
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
				mutationEvent(r.Context(), "domains", "update", "domain", record.ID, record.Hostname, "failed", "Updated domain record but failed to publish it.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update domain",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "update", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Updated domain %q.", record.Hostname))

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
				mutationEvent(r.Context(), "domains", "delete", "domain", domainID, domainID, "failed", "Failed to delete domain.")
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
				mutationEvent(r.Context(), "domains", "delete", "domain", record.ID, record.Hostname, "failed", "Deleted domain record but failed to republish routes.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete domain",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "delete", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Deleted domain %q.", record.Hostname))

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

			targetPath := strings.TrimSpace(r.URL.Query().Get("path"))
			if err := app.Files.Delete(targetPath); err != nil {
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

	router.Handle("/phpmyadmin", newPHPMyAdminRedirectHandler(app))
	router.Handle("/phpmyadmin/*", newPHPMyAdminRedirectHandler(app))
	router.Method(stdhttp.MethodGet, "/", panelHandler)
	router.Method(stdhttp.MethodHead, "/", panelHandler)
	router.Method(stdhttp.MethodGet, "/*", panelHandler)
	router.Method(stdhttp.MethodHead, "/*", panelHandler)

	return router, nil
}

func newPHPMyAdminRedirectHandler(app *app.App) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if app.PHPMyAdmin == nil {
			stdhttp.Error(w, "phpMyAdmin is not configured.", stdhttp.StatusServiceUnavailable)
			return
		}

		status := app.PHPMyAdmin.Status(r.Context())
		if !status.Installed {
			stdhttp.NotFound(w, r)
			return
		}

		if app.PHP == nil {
			stdhttp.Error(w, "PHP is not configured for phpMyAdmin.", stdhttp.StatusServiceUnavailable)
			return
		}

		phpStatus := app.PHP.Status(r.Context())
		if !phpStatus.Ready {
			stdhttp.Error(w, phpStatus.Message, stdhttp.StatusServiceUnavailable)
			return
		}
		if err := syncPHPMyAdminRoute(r.Context(), app); err != nil {
			app.Logger.Error("sync phpmyadmin route failed", zap.Error(err))
			stdhttp.Error(w, "phpMyAdmin route could not be published.", stdhttp.StatusInternalServerError)
			return
		}

		target, err := phpMyAdminExternalURL(app.Config.PHPMyAdminAddr, r.Host, strings.TrimPrefix(r.URL.Path, "/phpmyadmin"))
		if err != nil {
			stdhttp.Error(w, "phpMyAdmin URL is not configured.", stdhttp.StatusInternalServerError)
			return
		}
		target.RawQuery = r.URL.RawQuery
		stdhttp.Redirect(w, r, target.String(), stdhttp.StatusTemporaryRedirect)
	})
}

func syncPHPMyAdminRoute(ctx context.Context, app *app.App) error {
	if app == nil || app.Caddy == nil || app.Domains == nil {
		return nil
	}

	return app.Caddy.Sync(ctx, app.Domains.List())
}

func phpMyAdminExternalURL(listenAddr, requestHost, requestPath string) (*url.URL, error) {
	listenHost, listenPort, err := splitHostPortDefault(listenAddr)
	if err != nil {
		return nil, err
	}
	requestHostOnly, _, err := splitHostPortDefault(requestHost)
	if err != nil {
		requestHostOnly = strings.TrimSpace(requestHost)
	}

	host := listenHost
	switch host {
	case "", "0.0.0.0", "::":
		host = requestHostOnly
	}
	if host == "" {
		host = "localhost"
	}

	pathValue := "/" + strings.TrimPrefix(strings.TrimSpace(requestPath), "/")
	if pathValue == "/" {
		pathValue = "/"
	}

	return &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, listenPort),
		Path:   pathValue,
	}, nil
}

func splitHostPortDefault(address string) (string, string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", fmt.Errorf("address is empty")
	}
	if strings.HasPrefix(address, ":") {
		return "", strings.TrimPrefix(address, ":"), nil
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", "", err
	}

	return strings.Trim(strings.TrimSpace(host), "[]"), strings.TrimSpace(port), nil
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

func writeBackupError(w stdhttp.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeJSON(w, stdhttp.StatusNotFound, map[string]any{
			"error": "backup not found",
		})
	case errors.Is(err, backup.ErrInvalidName):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid backup name",
		})
	default:
		writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
			"error": "backup operation failed",
		})
	}
}
