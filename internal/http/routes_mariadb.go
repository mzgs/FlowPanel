package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strings"
	"time"

	"flowpanel/internal/mariadb"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

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
