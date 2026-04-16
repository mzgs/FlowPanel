package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/mariadb"
	"flowpanel/internal/packageruntime"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/pm2"
	"flowpanel/internal/settings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerApplicationRoutes(r chi.Router) {
	if r == nil {
		return
	}

	a.registerGoRoutes(r)
	a.registerNodeJSRoutes(r)
	a.registerPM2Routes(r)
	a.registerMariaDBRoutes(r)
	a.registerPackageRuntimeRoutes(r, "redis", "Redis", a.app.Redis)
	a.registerPackageRuntimeRoutes(r, "mongodb", "MongoDB", a.app.MongoDB)
	a.registerPackageRuntimeRoutes(r, "postgresql", "PostgreSQL", a.app.PostgreSQL)
	a.registerPHPRoutes(r)
}

func (a *apiRoutes) registerGoRoutes(r chi.Router) {
	goStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Golang == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "golang runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"golang": a.trackGoStatus(a.app.Golang.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/golang", goStatusHandler)
	r.Method(stdhttp.MethodHead, "/golang", goStatusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.Golang == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "golang runtime is not configured"})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"golang",
					action,
					"golang",
					"golang",
					"Go",
					"Removed Go.",
					func(ctx context.Context) map[string]any {
						return map[string]any{
							"golang": a.trackGoStatus(a.app.Golang.Status(ctx)),
						}
					},
					run,
					nil,
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("golang", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("golang", action)
				a.app.Logger.Error(action+" golang failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "golang", "golang", "Go", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("golang", action)

			pastTense := map[string]string{
				"install": "Installed Go.",
				"remove":  "Removed Go.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "golang", "golang", "Go", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"golang": a.trackGoStatus(a.app.Golang.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/golang/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.Golang.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/golang/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.Golang.Remove(ctx)
	}))
}

func (a *apiRoutes) registerMariaDBRoutes(r chi.Router) {
	mariaDBStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"mariadb": a.trackMariaDBStatus(a.app.MariaDB.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/mariadb", mariaDBStatusHandler)
	r.Method(stdhttp.MethodHead, "/mariadb", mariaDBStatusHandler)

	mariaDBRootPasswordHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		password, configured, err := a.app.MariaDB.RootPassword(r.Context())
		if err != nil {
			a.app.Logger.Error("read mariadb root password failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to read mariadb root password"})
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
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		var input struct {
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		if err := a.app.MariaDB.SetRootPassword(r.Context(), input.Password); err != nil {
			var validation mariadb.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("update mariadb root password failed", zap.Error(err))
			a.mutationEvent(r.Context(), "database", "update", "mariadb", "root-password", "MariaDB root password", "failed", "Failed to update the MariaDB root password.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update mariadb root password"})
			return
		}

		password, configured, err := a.app.MariaDB.RootPassword(r.Context())
		if err != nil {
			a.app.Logger.Error("read mariadb root password failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to read mariadb root password"})
			return
		}

		a.mutationEvent(r.Context(), "database", "update", "mariadb", "root-password", "MariaDB root password", "succeeded", "Updated the MariaDB root password.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"root_password": password,
			"configured":    configured,
		})
	})
	r.Method(stdhttp.MethodPut, "/mariadb/root-password", mariaDBRootPasswordUpdateHandler)

	registerRuntimeAction := func(action string, run func(context *apiRoutes, actionCtx context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"mariadb",
					action,
					"mariadb",
					"mariadb",
					"MariaDB",
					"Removed MariaDB.",
					func(ctx context.Context) map[string]any {
						return map[string]any{
							"mariadb": a.trackMariaDBStatus(a.app.MariaDB.Status(ctx)),
						}
					},
					func(ctx context.Context) error {
						return run(a, ctx)
					},
					nil,
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("mariadb", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(a, actionCtx); err != nil {
				a.runtimeActions.End("mariadb", action)
				a.app.Logger.Error(action+" mariadb failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("mariadb", action)

			pastTense := map[string]string{
				"install": "Installed MariaDB.",
				"remove":  "Removed MariaDB.",
				"start":   "Started MariaDB.",
				"stop":    "Stopped MariaDB.",
				"restart": "Restarted MariaDB.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "mariadb", "mariadb", "MariaDB", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": a.trackMariaDBStatus(a.app.MariaDB.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/mariadb/install", registerRuntimeAction("install", func(context *apiRoutes, actionCtx context.Context) error {
		return context.app.MariaDB.Install(actionCtx)
	}))
	r.Method(stdhttp.MethodPost, "/mariadb/remove", registerRuntimeAction("remove", func(context *apiRoutes, actionCtx context.Context) error {
		return context.app.MariaDB.Remove(actionCtx)
	}))
	r.Method(stdhttp.MethodPost, "/mariadb/start", registerRuntimeAction("start", func(context *apiRoutes, actionCtx context.Context) error {
		return context.app.MariaDB.Start(actionCtx)
	}))
	r.Method(stdhttp.MethodPost, "/mariadb/stop", registerRuntimeAction("stop", func(context *apiRoutes, actionCtx context.Context) error {
		return context.app.MariaDB.Stop(actionCtx)
	}))
	r.Method(stdhttp.MethodPost, "/mariadb/restart", registerRuntimeAction("restart", func(context *apiRoutes, actionCtx context.Context) error {
		return context.app.MariaDB.Restart(actionCtx)
	}))

	mariaDBDatabasesListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		records, err := a.app.MariaDB.ListDatabases(r.Context())
		if err != nil {
			a.app.Logger.Error("list mariadb databases failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to list databases"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"databases": records})
	})
	r.Method(stdhttp.MethodGet, "/mariadb/databases", mariaDBDatabasesListHandler)
	r.Method(stdhttp.MethodHead, "/mariadb/databases", mariaDBDatabasesListHandler)

	mariaDBBackupHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		dump, err := a.app.MariaDB.DumpAllDatabasesArchive(r.Context())
		if err != nil {
			var validation mariadb.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("dump mariadb databases archive failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to back up databases"})
			return
		}

		fileName := fmt.Sprintf("mariadb-all-databases-%s.tar.gz", time.Now().UTC().Format("20060102-150405"))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		w.Header().Set("Content-Type", "application/gzip")
		stdhttp.ServeContent(w, r, fileName, time.Now().UTC(), bytes.NewReader(dump))
	})
	r.Method(stdhttp.MethodGet, "/mariadb/backup", mariaDBBackupHandler)

	mariaDBDatabaseBackupHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		databaseName := chi.URLParam(r, "databaseName")
		dump, err := a.app.MariaDB.DumpDatabase(r.Context(), databaseName)
		if err != nil {
			var validation mariadb.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("dump mariadb database failed", zap.String("database_name", databaseName), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to back up database"})
			return
		}

		fileName := fmt.Sprintf("%s-%s.sql", strings.TrimSpace(databaseName), time.Now().UTC().Format("20060102-150405"))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
		w.Header().Set("Content-Type", "application/sql; charset=utf-8")
		stdhttp.ServeContent(w, r, fileName, time.Now().UTC(), bytes.NewReader(dump))
	})
	r.Method(stdhttp.MethodGet, "/mariadb/databases/{databaseName}/backup", mariaDBDatabaseBackupHandler)

	mariaDBDatabaseCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		var input mariadb.CreateDatabaseInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		record, err := a.app.MariaDB.CreateDatabase(r.Context(), input)
		if err != nil {
			var validation mariadb.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, mariadb.ErrDatabaseAlreadyExists):
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error":        "database already exists",
					"field_errors": map[string]string{"name": "This database already exists."},
				})
			default:
				a.app.Logger.Error("create mariadb database failed", zap.Error(err))
				a.mutationEvent(r.Context(), "database", "create", "database", strings.TrimSpace(input.Name), strings.TrimSpace(input.Name), "failed", "Failed to create database.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create database"})
			}
			return
		}

		a.mutationEvent(r.Context(), "database", "create", "database", record.Name, record.Name, "succeeded", fmt.Sprintf("Created database %q for %q.", record.Name, record.Username))
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"database": record})
	})
	r.Method(stdhttp.MethodPost, "/mariadb/databases", mariaDBDatabaseCreateHandler)

	mariaDBDatabaseUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		var input mariadb.UpdateDatabaseInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		databaseName := chi.URLParam(r, "databaseName")
		record, err := a.app.MariaDB.UpdateDatabase(r.Context(), databaseName, input)
		if err != nil {
			var validation mariadb.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, mariadb.ErrDatabaseNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "database not found"})
			default:
				a.app.Logger.Error("update mariadb database failed", zap.String("database_name", databaseName), zap.Error(err))
				a.mutationEvent(r.Context(), "database", "update", "database", databaseName, databaseName, "failed", "Failed to update database.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update database"})
			}
			return
		}

		a.mutationEvent(r.Context(), "database", "update", "database", record.Name, record.Name, "succeeded", fmt.Sprintf("Updated database %q.", record.Name))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"database": record})
	})
	r.Method(stdhttp.MethodPut, "/mariadb/databases/{databaseName}", mariaDBDatabaseUpdateHandler)

	mariaDBDatabaseDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.MariaDB == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "mariadb runtime is not configured"})
			return
		}

		databaseName := chi.URLParam(r, "databaseName")
		input := mariadb.DeleteDatabaseInput{
			Username: strings.TrimSpace(r.URL.Query().Get("username")),
		}

		if err := a.app.MariaDB.DeleteDatabase(r.Context(), databaseName, input); err != nil {
			var validation mariadb.ValidationErrors
			switch {
			case errors.As(err, &validation):
				writeValidationFailed(w, map[string]string(validation))
			case errors.Is(err, mariadb.ErrDatabaseNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "database not found"})
			default:
				a.app.Logger.Error("delete mariadb database failed", zap.String("database_name", databaseName), zap.Error(err))
				a.mutationEvent(r.Context(), "database", "delete", "database", databaseName, databaseName, "failed", "Failed to delete database.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete database"})
			}
			return
		}

		a.mutationEvent(r.Context(), "database", "delete", "database", databaseName, databaseName, "succeeded", fmt.Sprintf("Deleted database %q.", databaseName))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})
	r.Method(stdhttp.MethodDelete, "/mariadb/databases/{databaseName}", mariaDBDatabaseDeleteHandler)
}

func (a *apiRoutes) registerNodeJSRoutes(r chi.Router) {
	nodeJSStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.NodeJS == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "nodejs runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"nodejs": a.trackNodeJSStatus(a.app.NodeJS.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/nodejs", nodeJSStatusHandler)
	r.Method(stdhttp.MethodHead, "/nodejs", nodeJSStatusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.NodeJS == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "nodejs runtime is not configured"})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"nodejs",
					action,
					"nodejs",
					"nodejs",
					"Node.js",
					"Removed Node.js.",
					func(ctx context.Context) map[string]any {
						return map[string]any{
							"nodejs": a.trackNodeJSStatus(a.app.NodeJS.Status(ctx)),
						}
					},
					run,
					nil,
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("nodejs", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("nodejs", action)
				a.app.Logger.Error(action+" nodejs failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "nodejs", "nodejs", "Node.js", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("nodejs", action)

			pastTense := map[string]string{
				"install": "Installed Node.js.",
				"remove":  "Removed Node.js.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "nodejs", "nodejs", "Node.js", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"nodejs": a.trackNodeJSStatus(a.app.NodeJS.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/nodejs/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.NodeJS.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/nodejs/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.NodeJS.Remove(ctx)
	}))
}

func (a *apiRoutes) registerPHPRoutes(r chi.Router) {
	phpStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"php": a.trackPHPStatus(a.app.PHP.Status(r.Context())),
		})
	})

	phpDefaultHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil || a.app.Settings == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime settings are not configured"})
			return
		}

		version := a.phpActionVersion(r)
		if version == "" {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "php version is required"})
			return
		}
		version = phpenv.NormalizeVersion(version)
		if version == "" {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "select a supported php version"})
			return
		}

		runtimeStatus := a.app.PHP.StatusForVersion(r.Context(), version)
		if !runtimeStatus.Ready {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
				"error": fmt.Sprintf("PHP %s must be installed and running before it can be the default.", runtimeStatus.Version),
			})
			return
		}

		if _, err := a.app.Settings.SetDefaultPHPVersion(r.Context(), runtimeStatus.Version); err != nil {
			var validation settings.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("set default php failed", zap.Error(err))
			a.mutationEvent(r.Context(), "runtime", "update", "php", runtimeStatus.Version, "Default PHP version", "failed", "Failed to update the default PHP version.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to update the default PHP version"})
			return
		}

		if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
			a.app.Logger.Error("sync domains after default php update failed", zap.Error(err))
			a.mutationEvent(r.Context(), "runtime", "update", "php", runtimeStatus.Version, "Default PHP version", "failed", "Default PHP version saved but failed to republish domains.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "default php version saved but failed to republish domains"})
			return
		}

		a.mutationEvent(r.Context(), "runtime", "update", "php", runtimeStatus.Version, "Default PHP version", "succeeded", fmt.Sprintf("Set PHP %s as the default runtime.", runtimeStatus.Version))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"php": a.trackPHPStatus(a.app.PHP.Status(r.Context()))})
	})

	phpInfoHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeHTML(w, stdhttp.StatusServiceUnavailable, renderPHPInfoErrorDocument("PHP runtime is not configured."))
			return
		}

		version := a.phpActionVersion(r)
		status := a.app.PHP.StatusForVersion(r.Context(), version)
		if !status.PHPInstalled || strings.TrimSpace(status.PHPPath) == "" {
			message := "The selected PHP runtime is not installed."
			if strings.TrimSpace(status.Version) != "" {
				message = fmt.Sprintf("PHP %s is not installed.", status.Version)
			}
			writeHTML(w, stdhttp.StatusServiceUnavailable, renderPHPInfoErrorDocument(message))
			return
		}

		runCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		output, err := runPHPInfoCommand(runCtx, status.PHPPath)
		if err != nil {
			a.app.Logger.Error("generate php info failed", zap.String("version", status.Version), zap.String("php_path", status.PHPPath), zap.Error(err))
			writeHTML(w, stdhttp.StatusInternalServerError, renderPHPInfoErrorDocument("PHP info could not be generated."))
			return
		}

		writeHTML(w, stdhttp.StatusOK, renderPHPInfoDocument(status.Version, output))
	})

	phpRuntimeHandler := func(action string, run func(context.Context, string) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
				return
			}

			version := a.phpActionVersion(r)
			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"php",
					action,
					"php",
					"php",
					"PHP",
					"Removed PHP.",
					func(ctx context.Context) map[string]any {
						return map[string]any{"php": a.trackPHPStatus(a.app.PHP.Status(ctx))}
					},
					func(ctx context.Context) error {
						return run(ctx, version)
					},
					func(ctx context.Context) error {
						if err := a.syncDomainsWithCaddy(ctx); err != nil {
							return fmt.Errorf("php removed but failed to republish domains: %w", err)
						}
						return nil
					},
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("php", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx, version); err != nil {
				a.runtimeActions.End("php", action)
				a.app.Logger.Error(action+" php failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("php", action)

			status := a.trackPHPStatus(a.app.PHP.Status(actionCtx))
			shouldSync := status.Ready
			if strings.TrimSpace(version) != "" {
				shouldSync = a.app.PHP.StatusForVersion(actionCtx, version).Ready
			}
			if action != "stop" && shouldSync || action == "remove" {
				if err := a.syncDomainsWithCaddy(actionCtx); err != nil {
					a.app.Logger.Error("sync domains after php "+action+" failed", zap.Error(err))
					failureMessage := map[string]string{
						"install": "PHP installed but failed to republish domains.",
						"remove":  "PHP removed but failed to republish domains.",
						"start":   "PHP started but failed to republish domains.",
						"restart": "PHP restarted but failed to republish domains.",
					}[action]
					a.mutationEvent(actionCtx, "runtime", action, "php", "php", "PHP", "failed", failureMessage)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": strings.ToLower(failureMessage),
					})
					return
				}
			}

			messages := map[string]string{
				"install": "Installed PHP.",
				"remove":  "Removed PHP.",
				"start":   "Started PHP.",
				"stop":    "Stopped PHP.",
				"restart": "Restarted PHP.",
			}
			a.mutationEvent(actionCtx, "runtime", action, "php", "php", "PHP", "succeeded", messages[action])
			writeJSON(w, stdhttp.StatusOK, map[string]any{"php": status})
		}
	}

	phpSettingsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
			return
		}

		version := a.phpActionVersion(r)
		var input phpenv.UpdateSettingsInput
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		var (
			status phpenv.Status
			err    error
		)
		if strings.TrimSpace(version) != "" {
			_, err = a.app.PHP.UpdateSettingsForVersion(r.Context(), version, input)
			status = a.app.PHP.Status(r.Context())
		} else {
			status, err = a.app.PHP.UpdateSettings(r.Context(), input)
		}
		if err != nil {
			var validation phpenv.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}

			a.app.Logger.Error("update php settings failed", zap.Error(err))
			a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		shouldSync := status.Ready
		if strings.TrimSpace(version) != "" {
			shouldSync = a.app.PHP.StatusForVersion(r.Context(), version).Ready
		}
		if shouldSync {
			if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
				a.app.Logger.Error("sync domains after php settings update failed", zap.Error(err))
				a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", "PHP settings saved but failed to republish domains.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "php settings saved but failed to republish domains"})
				return
			}
		}

		a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "succeeded", "Updated PHP settings.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"php": status})
	})

	phpINIHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
			return
		}

		ini, err := a.app.PHP.ReadManagedConfigForVersion(r.Context(), a.phpActionVersion(r))
		if err != nil {
			a.app.Logger.Error("read php ini failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"ini": ini})
	})

	phpINIUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
			return
		}

		version := a.phpActionVersion(r)
		var input struct {
			Content string `json:"content"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}

		runtimeStatus, err := a.app.PHP.UpdateManagedConfigForVersion(r.Context(), version, input.Content)
		if err != nil {
			a.app.Logger.Error("update php ini failed", zap.Error(err))
			a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		status := a.app.PHP.Status(r.Context())
		ini := phpenv.ManagedConfig{Path: runtimeStatus.LoadedConfigFile, Content: input.Content}

		shouldSync := runtimeStatus.Ready
		if shouldSync {
			if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
				a.app.Logger.Error("sync domains after php ini update failed", zap.Error(err))
				a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", "PHP ini saved but failed to republish domains.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "php ini saved but failed to republish domains"})
				return
			}
		}

		a.mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "succeeded", "Updated PHP ini.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"php": status, "ini": ini})
	})

	phpExtensionInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHP == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "php runtime is not configured"})
			return
		}

		version := a.phpActionVersion(r)
		extension := a.phpActionExtension(r)
		if extension == "" {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "extension query parameter is required"})
			return
		}

		var (
			status phpenv.Status
			err    error
		)
		if strings.TrimSpace(version) != "" {
			_, err = a.app.PHP.InstallExtensionForVersion(r.Context(), version, extension)
			status = a.app.PHP.Status(r.Context())
		} else {
			status, err = a.app.PHP.InstallExtension(r.Context(), extension)
		}
		if err != nil {
			a.app.Logger.Error("install php extension failed", zap.String("version", version), zap.String("extension", extension), zap.Error(err))
			a.mutationEvent(r.Context(), "runtime", "install", "php_extension", extension, extension, "failed", a.formatPHPActivityFailureLog(r.Context(), extension, version, err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		shouldSync := status.Ready
		if strings.TrimSpace(version) != "" {
			shouldSync = a.app.PHP.StatusForVersion(r.Context(), version).Ready
		}
		if shouldSync {
			if err := a.syncDomainsWithCaddy(r.Context()); err != nil {
				a.app.Logger.Error("sync domains after php extension install failed", zap.Error(err))
				a.mutationEvent(r.Context(), "runtime", "install", "php_extension", extension, extension, "failed", "PHP extension installed but failed to republish domains.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "php extension installed but failed to republish domains"})
				return
			}
		}

		a.mutationEvent(r.Context(), "runtime", "install", "php_extension", extension, extension, "succeeded", fmt.Sprintf("Installed PHP extension %s.", extension))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"php": status})
	})

	r.Method(stdhttp.MethodGet, "/php", phpStatusHandler)
	r.Method(stdhttp.MethodHead, "/php", phpStatusHandler)
	r.Method(stdhttp.MethodGet, "/php/info", phpInfoHandler)
	r.Method(stdhttp.MethodPut, "/php/default", phpDefaultHandler)
	r.Method(stdhttp.MethodPost, "/php/install", phpRuntimeHandler("install", func(ctx context.Context, version string) error {
		if strings.TrimSpace(version) != "" {
			return a.app.PHP.InstallVersion(ctx, version)
		}
		return a.app.PHP.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/php/remove", phpRuntimeHandler("remove", func(ctx context.Context, version string) error {
		if strings.TrimSpace(version) != "" {
			return a.app.PHP.RemoveVersion(ctx, version)
		}
		return a.app.PHP.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/php/start", phpRuntimeHandler("start", func(ctx context.Context, version string) error {
		if strings.TrimSpace(version) != "" {
			return a.app.PHP.StartVersion(ctx, version)
		}
		return a.app.PHP.Start(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/php/stop", phpRuntimeHandler("stop", func(ctx context.Context, version string) error {
		if strings.TrimSpace(version) != "" {
			return a.app.PHP.StopVersion(ctx, version)
		}
		return a.app.PHP.Stop(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/php/restart", phpRuntimeHandler("restart", func(ctx context.Context, version string) error {
		if strings.TrimSpace(version) != "" {
			return a.app.PHP.RestartVersion(ctx, version)
		}
		return a.app.PHP.Restart(ctx)
	}))
	r.Method(stdhttp.MethodGet, "/php/ini", phpINIHandler)
	r.Method(stdhttp.MethodPut, "/php/settings", phpSettingsUpdateHandler)
	r.Method(stdhttp.MethodPut, "/php/ini", phpINIUpdateHandler)
	r.Method(stdhttp.MethodPost, "/php/extensions/install", phpExtensionInstallHandler)

	phpMyAdminStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHPMyAdmin == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "phpmyadmin runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"phpmyadmin": a.trackPHPMyAdminStatus(a.app.PHPMyAdmin.Status(r.Context())),
		})
	})

	phpMyAdminActionHandler := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "phpmyadmin runtime is not configured"})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"phpmyadmin",
					action,
					"phpmyadmin",
					"phpmyadmin",
					"phpMyAdmin",
					"Removed phpMyAdmin.",
					func(ctx context.Context) map[string]any {
						return map[string]any{
							"phpmyadmin": a.trackPHPMyAdminStatus(a.app.PHPMyAdmin.Status(ctx)),
						}
					},
					run,
					func(ctx context.Context) error {
						if err := a.syncDomainsWithCaddy(ctx); err != nil {
							return fmt.Errorf("phpmyadmin removed but failed to republish routes: %w", err)
						}
						return nil
					},
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("phpmyadmin", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("phpmyadmin", action)
				a.app.Logger.Error(action+" phpmyadmin failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("phpmyadmin", action)

			status := a.trackPHPMyAdminStatus(a.app.PHPMyAdmin.Status(actionCtx))
			if (action == "install" && status.Installed && a.app.PHP != nil && a.trackPHPStatus(a.app.PHP.Status(actionCtx)).Ready) || action == "remove" {
				if err := a.syncDomainsWithCaddy(actionCtx); err != nil {
					a.app.Logger.Error("sync domains after phpmyadmin "+action+" failed", zap.Error(err))
					failureMessage := map[string]string{
						"install": "phpMyAdmin installed but failed to republish routes.",
						"remove":  "phpMyAdmin removed but failed to republish routes.",
					}[action]
					a.mutationEvent(actionCtx, "runtime", action, "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", failureMessage)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": strings.ToLower(failureMessage),
					})
					return
				}
			}

			messages := map[string]string{
				"install": "Installed phpMyAdmin.",
				"remove":  "Removed phpMyAdmin.",
			}
			a.mutationEvent(actionCtx, "runtime", action, "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", messages[action])
			writeJSON(w, stdhttp.StatusOK, map[string]any{"phpmyadmin": status})
		}
	}

	phpMyAdminThemeImportHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PHPMyAdmin == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "phpmyadmin runtime is not configured"})
			return
		}

		r.Body = stdhttp.MaxBytesReader(w, r.Body, 64<<20)
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "upload a valid theme zip file"})
			return
		}

		file, _, err := r.FormFile("theme")
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "upload a theme zip file in the theme field"})
			return
		}
		defer file.Close()

		status, err := a.app.PHPMyAdmin.ImportTheme(r.Context(), file)
		if err != nil {
			if errors.Is(err, phpmyadmin.ErrThemeImportRequiresInstall) || errors.Is(err, phpmyadmin.ErrInvalidThemeArchive) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}

			a.app.Logger.Error("import phpmyadmin theme failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to import phpmyadmin theme"})
			return
		}

		a.mutationEvent(r.Context(), "runtime", "import_theme", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", "Imported a phpMyAdmin theme.")
		writeJSON(w, stdhttp.StatusOK, map[string]any{"phpmyadmin": a.trackPHPMyAdminStatus(status)})
	})

	r.Method(stdhttp.MethodGet, "/phpmyadmin", phpMyAdminStatusHandler)
	r.Method(stdhttp.MethodHead, "/phpmyadmin", phpMyAdminStatusHandler)
	r.Method(stdhttp.MethodPost, "/phpmyadmin/install", phpMyAdminActionHandler("install", func(ctx context.Context) error {
		return a.app.PHPMyAdmin.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/phpmyadmin/remove", phpMyAdminActionHandler("remove", func(ctx context.Context) error {
		return a.app.PHPMyAdmin.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/phpmyadmin/theme", phpMyAdminThemeImportHandler)
}

func (a *apiRoutes) registerPM2Routes(r chi.Router) {
	pm2StatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"pm2": a.trackPM2Status(a.app.PM2.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/pm2", pm2StatusHandler)
	r.Method(stdhttp.MethodHead, "/pm2", pm2StatusHandler)

	parseProcessID := func(r *stdhttp.Request) (int, error) {
		processID, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "processID")))
		if err != nil {
			return 0, fmt.Errorf("invalid PM2 process ID")
		}

		return processID, nil
	}

	r.Method(stdhttp.MethodGet, "/pm2/processes", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		processes, err := a.app.PM2.List(r.Context())
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"processes": processes})
	}))
	r.Method(stdhttp.MethodGet, "/pm2/processes/{processID}/logs", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		processID, err := parseProcessID(r)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		output, err := a.app.PM2.Logs(r.Context(), processID)
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"output": output})
	}))

	registerProcessAction := func(action, successMessage string, run func(context.Context, int) ([]pm2.Process, error)) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PM2 == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
				return
			}

			processID, err := parseProcessID(r)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			processes, err := run(actionCtx, processID)
			if err != nil {
				a.app.Logger.Error(action+" pm2 process failed", zap.Int("process_id", processID), zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "pm2_process", strconv.Itoa(processID), fmt.Sprintf("PM2 process %d", processID), "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}

			processLabel := fmt.Sprintf("PM2 process %d", processID)
			for _, process := range processes {
				if process.ID == processID {
					processLabel = process.Name
					break
				}
			}

			a.mutationEvent(actionCtx, "runtime", action, "pm2_process", strconv.Itoa(processID), processLabel, "succeeded", successMessage)
			writeJSON(w, stdhttp.StatusOK, map[string]any{"processes": processes})
		}
	}

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PM2 == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					"pm2",
					action,
					"pm2",
					"pm2",
					"PM2",
					"Removed PM2.",
					func(ctx context.Context) map[string]any {
						return map[string]any{
							"pm2": a.trackPM2Status(a.app.PM2.Status(ctx)),
						}
					},
					run,
					nil,
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("pm2", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("pm2", action)
				a.app.Logger.Error(action+" pm2 failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("pm2", action)

			pastTense := map[string]string{
				"install": "Installed PM2.",
				"remove":  "Removed PM2.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"pm2": a.trackPM2Status(a.app.PM2.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/pm2/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.PM2.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.PM2.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/start", registerProcessAction("start", "Started the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.StartProcess(ctx, processID)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/stop", registerProcessAction("stop", "Stopped the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.StopProcess(ctx, processID)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/restart", registerProcessAction("restart", "Restarted the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.RestartProcess(ctx, processID)
	}))
}

func (a *apiRoutes) registerPackageRuntimeRoutes(r chi.Router, key, label string, manager packageruntime.Manager) {
	statusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if manager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": fmt.Sprintf("%s runtime is not configured", key)})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			key: a.trackPackageRuntimeStatus(key, label, manager.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/"+key, statusHandler)
	r.Method(stdhttp.MethodHead, "/"+key, statusHandler)

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if manager == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": fmt.Sprintf("%s runtime is not configured", key)})
				return
			}

			if action == "remove" {
				a.startBackgroundRuntimeAction(
					w,
					r,
					key,
					action,
					key,
					key,
					label,
					fmt.Sprintf("Removed %s.", label),
					func(ctx context.Context) map[string]any {
						return map[string]any{
							key: a.trackPackageRuntimeStatus(key, label, manager.Status(ctx)),
						}
					},
					run,
					nil,
				)
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin(key, action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End(key, action)
				a.app.Logger.Error(action+" "+key+" failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, key, key, label, "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End(key, action)

			pastTense := map[string]string{
				"install": fmt.Sprintf("Installed %s.", label),
				"remove":  fmt.Sprintf("Removed %s.", label),
				"start":   fmt.Sprintf("Started %s.", label),
				"stop":    fmt.Sprintf("Stopped %s.", label),
				"restart": fmt.Sprintf("Restarted %s.", label),
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, key, key, label, "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				key: a.trackPackageRuntimeStatus(key, label, manager.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/"+key+"/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return manager.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return manager.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/start", registerRuntimeAction("start", func(ctx context.Context) error {
		return manager.Start(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/stop", registerRuntimeAction("stop", func(ctx context.Context) error {
		return manager.Stop(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/"+key+"/restart", registerRuntimeAction("restart", func(ctx context.Context) error {
		return manager.Restart(ctx)
	}))
}
