package httpx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	stdhttp "net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/backup"
	"flowpanel/internal/caddy"
	flowcron "flowpanel/internal/cron"
	"flowpanel/internal/domain"
	eventlog "flowpanel/internal/events"
	filesvc "flowpanel/internal/files"
	"flowpanel/internal/ftp"
	"flowpanel/internal/googledrive"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/settings"
	"flowpanel/internal/systemstatus"
	"flowpanel/web"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

const googleDriveOAuthStateSessionKey = "google_drive_oauth_state"

type runtimeActionTracker struct {
	mu      sync.Mutex
	actions map[string]string
}

func newRuntimeActionTracker() *runtimeActionTracker {
	return &runtimeActionTracker{
		actions: make(map[string]string),
	}
}

func (t *runtimeActionTracker) Begin(resource, action string) error {
	if t == nil {
		return nil
	}

	resource = strings.TrimSpace(resource)
	action = strings.TrimSpace(action)
	if resource == "" || action == "" {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if current := strings.TrimSpace(t.actions[resource]); current != "" {
		return fmt.Errorf("%s %s is already in progress", resource, current)
	}

	t.actions[resource] = action
	return nil
}

func (t *runtimeActionTracker) End(resource, action string) {
	if t == nil {
		return
	}

	resource = strings.TrimSpace(resource)
	action = strings.TrimSpace(action)
	if resource == "" || action == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.actions[resource] == action {
		delete(t.actions, resource)
	}
}

func (t *runtimeActionTracker) Current(resource string) string {
	if t == nil {
		return ""
	}

	resource = strings.TrimSpace(resource)
	if resource == "" {
		return ""
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return strings.TrimSpace(t.actions[resource])
}

func backgroundRequestContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return context.WithoutCancel(ctx)
}

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
		runtimeActions := newRuntimeActionTracker()
		syncDomainsWithCaddy := func(ctx context.Context) error {
			return syncDomainsWithCurrentSettings(ctx, app)
		}
		recordEvent := func(ctx context.Context, input eventlog.CreateInput) {
			if app == nil || app.Events == nil {
				return
			}
			if _, err := app.Events.Record(backgroundRequestContext(ctx), input); err != nil {
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
		trackPHPStatus := func(status phpenv.Status) phpenv.Status {
			switch runtimeActions.Current("php") {
			case "install":
				if status.PHPInstalled && status.FPMInstalled && status.ServiceRunning {
					runtimeActions.End("php", "install")
					return status
				}
				status.State = "installing"
				status.Message = "PHP installation is running in the background."
			case "remove":
				if !status.PHPInstalled && !status.FPMInstalled {
					runtimeActions.End("php", "remove")
					return status
				}
				status.State = "removing"
				status.Message = "PHP removal is running in the background."
			case "start":
				if status.ServiceRunning {
					runtimeActions.End("php", "start")
					return status
				}
				status.State = "starting"
				status.Message = "PHP-FPM is starting in the background."
			case "stop":
				if status.FPMInstalled && !status.ServiceRunning {
					runtimeActions.End("php", "stop")
					return status
				}
				status.State = "stopping"
				status.Message = "PHP-FPM is stopping in the background."
			case "restart":
				if status.ServiceRunning {
					runtimeActions.End("php", "restart")
					return status
				}
				status.State = "restarting"
				status.Message = "PHP-FPM is restarting in the background."
			default:
				return status
			}

			status.Ready = false
			status.InstallAvailable = false
			status.RemoveAvailable = false
			status.StartAvailable = false
			status.StopAvailable = false
			status.RestartAvailable = false
			return status
		}
		trackMariaDBStatus := func(status mariadb.Status) mariadb.Status {
			switch runtimeActions.Current("mariadb") {
			case "install":
				if status.ServerInstalled && status.ServiceRunning {
					runtimeActions.End("mariadb", "install")
					return status
				}
				status.State = "installing"
				status.Message = "MariaDB installation is running in the background."
			case "remove":
				if !status.ServerInstalled && !status.ClientInstalled {
					runtimeActions.End("mariadb", "remove")
					return status
				}
				status.State = "removing"
				status.Message = "MariaDB removal is running in the background."
			case "start":
				if status.ServiceRunning {
					runtimeActions.End("mariadb", "start")
					return status
				}
				status.State = "starting"
				status.Message = "MariaDB is starting in the background."
			case "stop":
				if status.ServerInstalled && !status.ServiceRunning {
					runtimeActions.End("mariadb", "stop")
					return status
				}
				status.State = "stopping"
				status.Message = "MariaDB is stopping in the background."
			case "restart":
				if status.ServiceRunning {
					runtimeActions.End("mariadb", "restart")
					return status
				}
				status.State = "restarting"
				status.Message = "MariaDB is restarting in the background."
			default:
				return status
			}

			status.Ready = false
			status.InstallAvailable = false
			status.RemoveAvailable = false
			status.StartAvailable = false
			status.StopAvailable = false
			status.RestartAvailable = false
			return status
		}
		trackPHPMyAdminStatus := func(status phpmyadmin.Status) phpmyadmin.Status {
			switch runtimeActions.Current("phpmyadmin") {
			case "install":
				if status.Installed {
					runtimeActions.End("phpmyadmin", "install")
					return status
				}
				status.State = "installing"
				status.Message = "phpMyAdmin installation is running in the background."
			case "remove":
				if !status.Installed {
					runtimeActions.End("phpmyadmin", "remove")
					return status
				}
				status.State = "removing"
				status.Message = "phpMyAdmin removal is running in the background."
			default:
				return status
			}

			status.InstallAvailable = false
			status.RemoveAvailable = false
			return status
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
		r.Method(stdhttp.MethodGet, "/terminal/ws", newTerminalWebSocketHandler(app))

		settingsGetHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			record, err := app.Settings.Get(r.Context())
			if err != nil {
				app.Logger.Error("load settings failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to load settings",
				})
				return
			}

			writeSettingsResponse(w, stdhttp.StatusOK, app, record)
		})
		r.Method(stdhttp.MethodGet, "/settings", settingsGetHandler)
		r.Method(stdhttp.MethodHead, "/settings", settingsGetHandler)

		settingsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			var input settings.UpdateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			previousPanelURL, err := currentPanelURL(r.Context(), app)
			if err != nil {
				app.Logger.Error("load settings before update failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to load settings",
				})
				return
			}

			record, err := app.Settings.Update(r.Context(), input)
			if err != nil {
				var validation settings.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("update settings failed", zap.Error(err))
				mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "failed", "Failed to update panel settings.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update settings",
				})
				return
			}

			if app.FTP != nil {
				if err := app.FTP.Apply(r.Context(), ftpConfigFromSettings(record)); err != nil {
					app.Logger.Error("apply ftp settings failed", zap.Error(err))
					mutationEvent(r.Context(), "settings", "update", "settings", "ftp", "FTP settings", "failed", "Saved settings but could not apply FTP runtime changes.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "settings saved but ftp runtime could not be updated",
					})
					return
				}
			}

			if previousPanelURL != record.PanelURL {
				if err := syncDomainsWithPanelURL(r.Context(), app, record.PanelURL); err != nil {
					if errors.Is(err, caddy.ErrRuntimeNotStarted) {
						mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "succeeded", "Updated panel settings.")
						writeSettingsResponse(w, stdhttp.StatusOK, app, record)
						return
					}
					app.Logger.Error("sync caddy runtime after settings update failed", zap.Error(err))
					mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "failed", "Saved panel settings but could not refresh panel routing.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "settings saved but panel routing could not be refreshed",
					})
					return
				}
			}

			mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "succeeded", "Updated panel settings.")
			writeSettingsResponse(w, stdhttp.StatusOK, app, record)
		})
		r.Method(stdhttp.MethodPut, "/settings", settingsUpdateHandler)

		settingsGoogleDriveOAuthCredentialsUploadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.GoogleDrive == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "google drive integration is unavailable",
				})
				return
			}

			r.Body = stdhttp.MaxBytesReader(w, r.Body, 2<<20)
			if err := r.ParseMultipartForm(2 << 20); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "upload a valid Google OAuth credentials JSON file",
				})
				return
			}

			file, _, err := r.FormFile("credentials")
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "upload a Google OAuth credentials JSON file in the credentials field",
				})
				return
			}
			defer file.Close()

			if err := app.GoogleDrive.SaveOAuthCredentialsJSON(file); err != nil {
				if errors.Is(err, googledrive.ErrInvalidOAuthConfigJSON) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": err.Error(),
					})
					return
				}

				app.Logger.Error("save google drive oauth credentials failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to save google drive oauth credentials",
				})
				return
			}

			record, err := app.Settings.ClearGoogleDriveConnection(r.Context())
			if err != nil {
				app.Logger.Error("clear google drive connection after credentials upload failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "google drive credentials were saved but the existing connection could not be cleared",
				})
				return
			}

			mutationEvent(r.Context(), "settings", "upload_google_drive_oauth_credentials", "settings", "google_drive", "Google Drive", "succeeded", "Uploaded Google Drive OAuth credentials.")
			writeSettingsResponse(w, stdhttp.StatusOK, app, record)
		})
		r.Method(stdhttp.MethodPost, "/settings/google-drive/oauth-client", settingsGoogleDriveOAuthCredentialsUploadHandler)

		settingsGoogleDriveConnectHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.GoogleDrive == nil || !app.GoogleDrive.Enabled() {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "google drive integration is not configured",
				})
				return
			}

			state, err := randomOAuthState()
			if err != nil {
				app.Logger.Error("generate google drive oauth state failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to start google drive sign-in",
				})
				return
			}

			app.Sessions.Put(r.Context(), googleDriveOAuthStateSessionKey, state)
			redirectURL, err := app.GoogleDrive.AuthURL(state, buildGoogleDriveRedirectURL(r))
			if err != nil {
				app.Logger.Error("build google drive auth url failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to start google drive sign-in",
				})
				return
			}

			stdhttp.Redirect(w, r, redirectURL, stdhttp.StatusFound)
		})
		r.Method(stdhttp.MethodGet, "/settings/google-drive/connect", settingsGoogleDriveConnectHandler)

		settingsGoogleDriveCallbackHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.GoogleDrive == nil || !app.GoogleDrive.Enabled() {
				writeOAuthPopupPage(w, stdhttp.StatusServiceUnavailable, "error", "Google Drive integration is not configured.", "")
				return
			}

			if errValue := strings.TrimSpace(r.URL.Query().Get("error")); errValue != "" {
				message := "Google sign-in was cancelled."
				if errValue == "access_denied" {
					message = "Google denied access. If the OAuth consent screen is in testing, add this account as a test user or publish the app."
				}
				if description := strings.TrimSpace(r.URL.Query().Get("error_description")); description != "" {
					message = fmt.Sprintf("%s (%s)", message, description)
				}
				writeOAuthPopupPage(w, stdhttp.StatusBadRequest, "error", message, "")
				return
			}

			expectedState := app.Sessions.GetString(r.Context(), googleDriveOAuthStateSessionKey)
			app.Sessions.Remove(r.Context(), googleDriveOAuthStateSessionKey)
			if expectedState == "" || expectedState != strings.TrimSpace(r.URL.Query().Get("state")) {
				writeOAuthPopupPage(w, stdhttp.StatusBadRequest, "error", "Google sign-in could not be verified.", "")
				return
			}

			connection, err := app.GoogleDrive.Exchange(r.Context(), buildGoogleDriveRedirectURL(r), strings.TrimSpace(r.URL.Query().Get("code")))
			if err != nil {
				app.Logger.Error("exchange google drive oauth code failed", zap.Error(err))
				writeOAuthPopupPage(w, stdhttp.StatusInternalServerError, "error", "Google Drive connection failed.", "")
				return
			}

			record, err := app.Settings.SetGoogleDriveConnection(r.Context(), connection.Email, connection.RefreshToken)
			if err != nil {
				app.Logger.Error("persist google drive connection failed", zap.Error(err))
				writeOAuthPopupPage(w, stdhttp.StatusInternalServerError, "error", "Google Drive connection could not be saved.", "")
				return
			}

			mutationEvent(r.Context(), "settings", "connect_google_drive", "settings", "google_drive", record.GoogleDriveEmail, "succeeded", "Connected a Google Drive account.")
			writeOAuthPopupPage(w, stdhttp.StatusOK, "success", "Google Drive connected.", record.GoogleDriveEmail)
		})
		r.Method(stdhttp.MethodGet, "/settings/google-drive/callback", settingsGoogleDriveCallbackHandler)

		settingsGoogleDriveDisconnectHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			record, err := app.Settings.ClearGoogleDriveConnection(r.Context())
			if err != nil {
				app.Logger.Error("clear google drive connection failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to disconnect google drive",
				})
				return
			}

			mutationEvent(r.Context(), "settings", "disconnect_google_drive", "settings", "google_drive", "Google Drive", "succeeded", "Disconnected the Google Drive account.")
			writeSettingsResponse(w, stdhttp.StatusOK, app, record)
		})
		r.Method(stdhttp.MethodDelete, "/settings/google-drive", settingsGoogleDriveDisconnectHandler)

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
				IncludePanelData *bool    `json:"include_panel_data"`
				IncludeSites     *bool    `json:"include_sites"`
				IncludeDatabases *bool    `json:"include_databases"`
				SiteHostnames    []string `json:"site_hostnames"`
				DatabaseNames    []string `json:"database_names"`
				Location         string   `json:"location"`
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
			input.SiteHostnames = payload.SiteHostnames
			input.DatabaseNames = payload.DatabaseNames
			input.Location = payload.Location

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
					"error": err.Error(),
				})
				return
			}

			mutationEvent(r.Context(), "backups", "create", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Created backup %q.", record.Name))

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"backup": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/backups", backupsCreateHandler)

		backupsScheduleListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			snapshot := app.Cron.Snapshot()
			schedules := make([]map[string]any, 0, len(snapshot.Jobs))
			for _, job := range snapshot.Jobs {
				scope, ok := backup.ParseScheduledCommand(job.Command)
				if !ok {
					continue
				}

				schedules = append(schedules, map[string]any{
					"id":                 job.ID,
					"name":               job.Name,
					"schedule":           job.Schedule,
					"created_at":         job.CreatedAt,
					"include_panel_data": scope.IncludePanelData,
					"include_sites":      scope.IncludeSites,
					"include_databases":  scope.IncludeDatabases,
					"location":           scope.Location,
				})
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"enabled":   snapshot.Enabled,
				"started":   snapshot.Started,
				"schedules": schedules,
			})
		})
		r.Method(stdhttp.MethodGet, "/backups/schedules", backupsScheduleListHandler)
		r.Method(stdhttp.MethodHead, "/backups/schedules", backupsScheduleListHandler)

		backupsScheduleCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			input := backup.CreateInput{
				IncludePanelData: true,
				IncludeSites:     true,
				IncludeDatabases: true,
			}
			var payload struct {
				Name             string `json:"name"`
				Schedule         string `json:"schedule"`
				IncludePanelData *bool  `json:"include_panel_data"`
				IncludeSites     *bool  `json:"include_sites"`
				IncludeDatabases *bool  `json:"include_databases"`
				Location         string `json:"location"`
			}
			if err := decodeJSON(r, &payload); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
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
			input.Location = payload.Location

			if !input.IncludePanelData && !input.IncludeSites && !input.IncludeDatabases {
				validation := backup.ValidationErrors{}
				validation["scope"] = "Select at least one backup source."
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error":        "validation failed",
					"field_errors": map[string]string(validation),
				})
				return
			}

			executablePath, err := os.Executable()
			if err != nil {
				app.Logger.Error("resolve executable path failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to resolve flowpanel executable",
				})
				return
			}

			command, err := backup.BuildScheduledCommand(executablePath, input)
			if err != nil {
				app.Logger.Error("build scheduled backup command failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create scheduled backup command",
				})
				return
			}

			record, err := app.Cron.Create(r.Context(), flowcron.CreateInput{
				Name:     payload.Name,
				Schedule: payload.Schedule,
				Command:  command,
			})
			if err != nil {
				var validation flowcron.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}
				app.Logger.Error("create scheduled backup failed", zap.Error(err))
				mutationEvent(r.Context(), "backups", "schedule", "backup_schedule", "backup_schedule", strings.TrimSpace(payload.Name), "failed", "Failed to create scheduled backup.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create scheduled backup",
				})
				return
			}

			mutationEvent(r.Context(), "backups", "schedule", "backup_schedule", record.ID, record.Name, "succeeded", fmt.Sprintf("Created scheduled backup %q.", record.Name))
			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"schedule": map[string]any{
					"id":                 record.ID,
					"name":               record.Name,
					"schedule":           record.Schedule,
					"created_at":         record.CreatedAt,
					"include_panel_data": input.IncludePanelData,
					"include_sites":      input.IncludeSites,
					"include_databases":  input.IncludeDatabases,
					"location":           input.Location,
				},
			})
		})
		r.Method(stdhttp.MethodPost, "/backups/schedules", backupsScheduleCreateHandler)

		backupsScheduleDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Cron == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "cron scheduler is not configured",
				})
				return
			}

			jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
			if jobID == "" {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "backup schedule id is required",
				})
				return
			}

			job := flowcron.Record{}
			found := false
			for _, candidate := range app.Cron.List() {
				if candidate.ID != jobID {
					continue
				}
				job = candidate
				found = true
				break
			}
			if !found {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "backup schedule not found",
				})
				return
			}
			if _, ok := backup.ParseScheduledCommand(job.Command); !ok {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "backup schedule not found",
				})
				return
			}

			record, deleted, err := app.Cron.Delete(r.Context(), jobID)
			if err != nil {
				app.Logger.Error("delete scheduled backup failed", zap.Error(err))
				mutationEvent(r.Context(), "backups", "delete_schedule", "backup_schedule", jobID, job.Name, "failed", "Failed to delete scheduled backup.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete scheduled backup",
				})
				return
			}
			if !deleted {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "backup schedule not found",
				})
				return
			}

			mutationEvent(r.Context(), "backups", "delete_schedule", "backup_schedule", record.ID, record.Name, "succeeded", fmt.Sprintf("Deleted scheduled backup %q.", record.Name))
			w.WriteHeader(stdhttp.StatusNoContent)
		})
		r.Method(stdhttp.MethodDelete, "/backups/schedules/{jobID}", backupsScheduleDeleteHandler)

		backupsImportHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid backup upload",
				})
				return
			}

			headers := r.MultipartForm.File["backup"]
			if len(headers) != 1 {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "provide exactly one backup file",
				})
				return
			}

			header := headers[0]
			file, err := header.Open()
			if err != nil {
				app.Logger.Error("open uploaded backup failed",
					zap.String("backup_name", header.Filename),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to read backup upload",
				})
				return
			}
			defer file.Close()

			record, err := app.Backups.Import(r.Context(), header.Filename, file)
			if err != nil {
				writeBackupError(w, err)
				if errors.Is(err, backup.ErrAlreadyExists) || errors.Is(err, backup.ErrInvalidName) || errors.Is(err, backup.ErrInvalidArchive) {
					return
				}
				app.Logger.Error("import backup failed",
					zap.String("backup_name", header.Filename),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "backups", "import", "backup", header.Filename, header.Filename, "failed", "Failed to import a backup archive.")
				return
			}

			mutationEvent(r.Context(), "backups", "import", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Imported backup %q.", record.Name))
			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"backup": record,
			})
		})
		r.Method(stdhttp.MethodPost, "/backups/import", backupsImportHandler)

		backupsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			name, err := decodeBackupNameParam(r)
			if err != nil {
				writeBackupError(w, err)
				return
			}
			location := readBackupLocation(r)
			if err := app.Backups.Delete(r.Context(), name, location); err != nil {
				switch {
				case errors.Is(err, backup.ErrInvalidName):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid backup name",
					})
					return
				case errors.Is(err, backup.ErrInvalidLocation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid backup location",
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

			name, err := decodeBackupNameParam(r)
			if err != nil {
				writeBackupError(w, err)
				return
			}
			location := readBackupLocation(r)
			download, err := app.Backups.OpenDownload(r.Context(), name, location)
			if err != nil {
				switch {
				case errors.Is(err, backup.ErrInvalidLocation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid backup location",
					})
				default:
					writeBackupError(w, err)
				}
				return
			}
			defer download.Reader.Close()

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", download.Name))
			w.Header().Set("Content-Type", "application/gzip")
			if download.Size > 0 {
				w.Header().Set("Content-Length", strconv.FormatInt(download.Size, 10))
			}
			if _, err := io.Copy(w, download.Reader); err != nil {
				app.Logger.Error("stream backup download failed",
					zap.String("backup_name", name),
					zap.Error(err),
				)
			}
		})
		r.Method(stdhttp.MethodGet, "/backups/{backupName}/download", backupsDownloadHandler)

		backupsRestoreHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Backups == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "backup service is not configured",
				})
				return
			}

			name, err := decodeBackupNameParam(r)
			if err != nil {
				writeBackupError(w, err)
				return
			}
			location := readBackupLocation(r)
			result, err := app.Backups.Restore(r.Context(), name, location)
			if err != nil {
				switch {
				case errors.Is(err, backup.ErrInvalidLocation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "invalid backup location",
					})
				default:
					writeBackupError(w, err)
				}
				app.Logger.Error("restore backup failed",
					zap.String("backup_name", name),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "failed", fmt.Sprintf("Failed to restore backup %q: %v", name, err))
				return
			}

			if err := syncBackupRestoreState(r.Context(), app, result); err != nil {
				app.Logger.Error("sync restored backup state failed",
					zap.String("backup_name", name),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "failed", "Restored backup archive but failed to reload runtime state.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "backup restored but runtime sync failed",
				})
				return
			}

			mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "succeeded", fmt.Sprintf("Restored backup %q.", name))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"restore": result,
			})
		})
		r.Method(stdhttp.MethodPost, "/backups/{backupName}/restore", backupsRestoreHandler)

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
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(r.Context())),
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

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("mariadb", "install"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.MariaDB.Install(actionCtx); err != nil {
				runtimeActions.End("mariadb", "install")
				app.Logger.Error("install mariadb failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "install", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("mariadb", "install")

			mutationEvent(actionCtx, "runtime", "install", "mariadb", "mariadb", "MariaDB", "succeeded", "Installed MariaDB.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(actionCtx)),
			})
		})

		mariaDBRemoveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("mariadb", "remove"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.MariaDB.Remove(actionCtx); err != nil {
				runtimeActions.End("mariadb", "remove")
				app.Logger.Error("remove mariadb failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "remove", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("mariadb", "remove")

			mutationEvent(actionCtx, "runtime", "remove", "mariadb", "mariadb", "MariaDB", "succeeded", "Removed MariaDB.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(actionCtx)),
			})
		})

		mariaDBStartHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("mariadb", "start"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.MariaDB.Start(actionCtx); err != nil {
				runtimeActions.End("mariadb", "start")
				app.Logger.Error("start mariadb failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "start", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("mariadb", "start")

			mutationEvent(actionCtx, "runtime", "start", "mariadb", "mariadb", "MariaDB", "succeeded", "Started MariaDB.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(actionCtx)),
			})
		})

		mariaDBStopHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("mariadb", "stop"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.MariaDB.Stop(actionCtx); err != nil {
				runtimeActions.End("mariadb", "stop")
				app.Logger.Error("stop mariadb failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "stop", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("mariadb", "stop")

			mutationEvent(actionCtx, "runtime", "stop", "mariadb", "mariadb", "MariaDB", "succeeded", "Stopped MariaDB.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(actionCtx)),
			})
		})

		mariaDBRestartHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("mariadb", "restart"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.MariaDB.Restart(actionCtx); err != nil {
				runtimeActions.End("mariadb", "restart")
				app.Logger.Error("restart mariadb failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "restart", "mariadb", "mariadb", "MariaDB", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("mariadb", "restart")

			mutationEvent(actionCtx, "runtime", "restart", "mariadb", "mariadb", "MariaDB", "succeeded", "Restarted MariaDB.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"mariadb": trackMariaDBStatus(app.MariaDB.Status(actionCtx)),
			})
		})
		r.Method(stdhttp.MethodPost, "/mariadb/install", mariaDBInstallHandler)
		r.Method(stdhttp.MethodPost, "/mariadb/remove", mariaDBRemoveHandler)
		r.Method(stdhttp.MethodPost, "/mariadb/start", mariaDBStartHandler)
		r.Method(stdhttp.MethodPost, "/mariadb/stop", mariaDBStopHandler)
		r.Method(stdhttp.MethodPost, "/mariadb/restart", mariaDBRestartHandler)

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

		mariaDBDatabaseBackupHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.MariaDB == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "mariadb runtime is not configured",
				})
				return
			}

			databaseName := chi.URLParam(r, "databaseName")
			dump, err := app.MariaDB.DumpDatabase(r.Context(), databaseName)
			if err != nil {
				var validation mariadb.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				default:
					app.Logger.Error("dump mariadb database failed",
						zap.String("database_name", databaseName),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to back up database",
					})
					return
				}
			}

			fileName := fmt.Sprintf("%s-%s.sql", strings.TrimSpace(databaseName), time.Now().UTC().Format("20060102-150405"))
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			w.Header().Set("Content-Type", "application/sql; charset=utf-8")
			stdhttp.ServeContent(w, r, fileName, time.Now().UTC(), bytes.NewReader(dump))
		})
		r.Method(stdhttp.MethodGet, "/mariadb/databases/{databaseName}/backup", mariaDBDatabaseBackupHandler)

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

		phpActionVersion := func(r *stdhttp.Request) string {
			return strings.TrimSpace(r.URL.Query().Get("version"))
		}

		phpStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": trackPHPStatus(app.PHP.Status(r.Context())),
			})
		})

		phpInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			version := phpActionVersion(r)
			if err := runtimeActions.Begin("php", "install"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			var err error
			if strings.TrimSpace(version) != "" {
				err = app.PHP.InstallVersion(actionCtx, version)
			} else {
				err = app.PHP.Install(actionCtx)
			}
			if err != nil {
				runtimeActions.End("php", "install")
				app.Logger.Error("install php failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "install", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("php", "install")

			status := trackPHPStatus(app.PHP.Status(actionCtx))
			shouldSync := status.Ready
			if strings.TrimSpace(version) != "" {
				shouldSync = app.PHP.StatusForVersion(actionCtx, version).Ready
			}
			if shouldSync {
				if err := syncDomainsWithCaddy(actionCtx); err != nil {
					app.Logger.Error("sync domains after php install failed", zap.Error(err))
					mutationEvent(actionCtx, "runtime", "install", "php", "php", "PHP", "failed", "PHP installed but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php installed but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(actionCtx, "runtime", "install", "php", "php", "PHP", "succeeded", "Installed PHP.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		phpRemoveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			version := phpActionVersion(r)
			if err := runtimeActions.Begin("php", "remove"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			var err error
			if strings.TrimSpace(version) != "" {
				err = app.PHP.RemoveVersion(actionCtx, version)
			} else {
				err = app.PHP.Remove(actionCtx)
			}
			if err != nil {
				runtimeActions.End("php", "remove")
				app.Logger.Error("remove php failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "remove", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("php", "remove")

			status := trackPHPStatus(app.PHP.Status(actionCtx))
			if err := syncDomainsWithCaddy(actionCtx); err != nil {
				app.Logger.Error("sync domains after php remove failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "remove", "php", "php", "PHP", "failed", "PHP removed but failed to republish domains.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "php removed but failed to republish domains",
				})
				return
			}

			mutationEvent(actionCtx, "runtime", "remove", "php", "php", "PHP", "succeeded", "Removed PHP.")

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

			actionCtx := backgroundRequestContext(r.Context())
			version := phpActionVersion(r)
			if err := runtimeActions.Begin("php", "start"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			var err error
			if strings.TrimSpace(version) != "" {
				err = app.PHP.StartVersion(actionCtx, version)
			} else {
				err = app.PHP.Start(actionCtx)
			}
			if err != nil {
				runtimeActions.End("php", "start")
				app.Logger.Error("start php failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "start", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("php", "start")

			status := trackPHPStatus(app.PHP.Status(actionCtx))
			shouldSync := status.Ready
			if strings.TrimSpace(version) != "" {
				shouldSync = app.PHP.StatusForVersion(actionCtx, version).Ready
			}
			if shouldSync {
				if err := syncDomainsWithCaddy(actionCtx); err != nil {
					app.Logger.Error("sync domains after php start failed", zap.Error(err))
					mutationEvent(actionCtx, "runtime", "start", "php", "php", "PHP", "failed", "PHP started but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php started but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(actionCtx, "runtime", "start", "php", "php", "PHP", "succeeded", "Started PHP.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		phpStopHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			version := phpActionVersion(r)
			if err := runtimeActions.Begin("php", "stop"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			var err error
			if strings.TrimSpace(version) != "" {
				err = app.PHP.StopVersion(actionCtx, version)
			} else {
				err = app.PHP.Stop(actionCtx)
			}
			if err != nil {
				runtimeActions.End("php", "stop")
				app.Logger.Error("stop php failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "stop", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("php", "stop")

			mutationEvent(actionCtx, "runtime", "stop", "php", "php", "PHP", "succeeded", "Stopped PHP.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": trackPHPStatus(app.PHP.Status(actionCtx)),
			})
		})

		phpRestartHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			version := phpActionVersion(r)
			if err := runtimeActions.Begin("php", "restart"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			var err error
			if strings.TrimSpace(version) != "" {
				err = app.PHP.RestartVersion(actionCtx, version)
			} else {
				err = app.PHP.Restart(actionCtx)
			}
			if err != nil {
				runtimeActions.End("php", "restart")
				app.Logger.Error("restart php failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "restart", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("php", "restart")

			status := trackPHPStatus(app.PHP.Status(actionCtx))
			shouldSync := status.Ready
			if strings.TrimSpace(version) != "" {
				shouldSync = app.PHP.StatusForVersion(actionCtx, version).Ready
			}
			if shouldSync {
				if err := syncDomainsWithCaddy(actionCtx); err != nil {
					app.Logger.Error("sync domains after php restart failed", zap.Error(err))
					mutationEvent(actionCtx, "runtime", "restart", "php", "php", "PHP", "failed", "PHP restarted but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php restarted but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(actionCtx, "runtime", "restart", "php", "php", "PHP", "succeeded", "Restarted PHP.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		phpSettingsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHP == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "php runtime is not configured",
				})
				return
			}

			version := phpActionVersion(r)

			var input phpenv.UpdateSettingsInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			var (
				status phpenv.Status
				err    error
			)
			if strings.TrimSpace(version) != "" {
				_, err = app.PHP.UpdateSettingsForVersion(r.Context(), version, input)
				status = app.PHP.Status(r.Context())
			} else {
				status, err = app.PHP.UpdateSettings(r.Context(), input)
			}
			if err != nil {
				var validation phpenv.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("update php settings failed", zap.Error(err))
				mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			shouldSync := status.Ready
			if strings.TrimSpace(version) != "" {
				shouldSync = app.PHP.StatusForVersion(r.Context(), version).Ready
			}
			if shouldSync {
				if err := syncDomainsWithCaddy(r.Context()); err != nil {
					app.Logger.Error("sync domains after php settings update failed", zap.Error(err))
					mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "failed", "PHP settings saved but failed to republish domains.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "php settings saved but failed to republish domains",
					})
					return
				}
			}

			mutationEvent(r.Context(), "runtime", "update", "php", "php", "PHP", "succeeded", "Updated PHP settings.")
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"php": status,
			})
		})

		r.Method(stdhttp.MethodGet, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodHead, "/php", phpStatusHandler)
		r.Method(stdhttp.MethodPost, "/php/install", phpInstallHandler)
		r.Method(stdhttp.MethodPost, "/php/remove", phpRemoveHandler)
		r.Method(stdhttp.MethodPost, "/php/start", phpStartHandler)
		r.Method(stdhttp.MethodPost, "/php/stop", phpStopHandler)
		r.Method(stdhttp.MethodPost, "/php/restart", phpRestartHandler)
		r.Method(stdhttp.MethodPut, "/php/settings", phpSettingsUpdateHandler)

		phpMyAdminStatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": trackPHPMyAdminStatus(app.PHPMyAdmin.Status(r.Context())),
			})
		})

		phpMyAdminInstallHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("phpmyadmin", "install"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.PHPMyAdmin.Install(actionCtx); err != nil {
				runtimeActions.End("phpmyadmin", "install")
				app.Logger.Error("install phpmyadmin failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("phpmyadmin", "install")

			status := trackPHPMyAdminStatus(app.PHPMyAdmin.Status(actionCtx))
			if status.Installed && app.PHP != nil {
				phpStatus := trackPHPStatus(app.PHP.Status(actionCtx))
				if phpStatus.Ready {
					if err := syncDomainsWithCaddy(actionCtx); err != nil {
						app.Logger.Error("sync domains after phpmyadmin install failed", zap.Error(err))
						mutationEvent(actionCtx, "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", "phpMyAdmin installed but failed to republish routes.")
						writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
							"error": "phpmyadmin installed but failed to republish routes",
						})
						return
					}
				}
			}

			mutationEvent(actionCtx, "runtime", "install", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", "Installed phpMyAdmin.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": status,
			})
		})

		phpMyAdminRemoveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := runtimeActions.Begin("phpmyadmin", "remove"); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if err := app.PHPMyAdmin.Remove(actionCtx); err != nil {
				runtimeActions.End("phpmyadmin", "remove")
				app.Logger.Error("remove phpmyadmin failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "remove", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}
			runtimeActions.End("phpmyadmin", "remove")

			status := trackPHPMyAdminStatus(app.PHPMyAdmin.Status(actionCtx))
			if err := syncDomainsWithCaddy(actionCtx); err != nil {
				app.Logger.Error("sync domains after phpmyadmin remove failed", zap.Error(err))
				mutationEvent(actionCtx, "runtime", "remove", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "failed", "phpMyAdmin removed but failed to republish routes.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "phpmyadmin removed but failed to republish routes",
				})
				return
			}

			mutationEvent(actionCtx, "runtime", "remove", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", "Removed phpMyAdmin.")

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": status,
			})
		})

		phpMyAdminThemeImportHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.PHPMyAdmin == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "phpmyadmin runtime is not configured",
				})
				return
			}

			r.Body = stdhttp.MaxBytesReader(w, r.Body, 64<<20)
			if err := r.ParseMultipartForm(64 << 20); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "upload a valid theme zip file",
				})
				return
			}

			file, _, err := r.FormFile("theme")
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "upload a theme zip file in the theme field",
				})
				return
			}
			defer file.Close()

			status, err := app.PHPMyAdmin.ImportTheme(r.Context(), file)
			if err != nil {
				if errors.Is(err, phpmyadmin.ErrThemeImportRequiresInstall) || errors.Is(err, phpmyadmin.ErrInvalidThemeArchive) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": err.Error(),
					})
					return
				}

				app.Logger.Error("import phpmyadmin theme failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to import phpmyadmin theme",
				})
				return
			}

			mutationEvent(r.Context(), "runtime", "import_theme", "phpmyadmin", "phpmyadmin", "phpMyAdmin", "succeeded", "Imported a phpMyAdmin theme.")
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"phpmyadmin": trackPHPMyAdminStatus(status),
			})
		})

		r.Method(stdhttp.MethodGet, "/phpmyadmin", phpMyAdminStatusHandler)
		r.Method(stdhttp.MethodHead, "/phpmyadmin", phpMyAdminStatusHandler)
		r.Method(stdhttp.MethodPost, "/phpmyadmin/install", phpMyAdminInstallHandler)
		r.Method(stdhttp.MethodPost, "/phpmyadmin/remove", phpMyAdminRemoveHandler)
		r.Method(stdhttp.MethodPost, "/phpmyadmin/theme", phpMyAdminThemeImportHandler)

		ftpAccountsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			records, err := app.FTPAccounts.ListAccounts(r.Context())
			if err != nil {
				app.Logger.Error("list ftp accounts failed", zap.Error(err))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to list ftp accounts",
				})
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"accounts": records,
			})
		})

		ftpAccountsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			var input ftp.CreateAccountInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			record, err := app.FTPAccounts.CreateAccount(r.Context(), input)
			if err != nil {
				var validation ftp.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("create ftp account failed", zap.Error(err))
				mutationEvent(r.Context(), "ftp", "create", "ftp_account", input.Username, input.Username, "failed", "Failed to create the FTP account.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create ftp account",
				})
				return
			}

			mutationEvent(r.Context(), "ftp", "create", "ftp_account", record.ID, record.Username, "succeeded", fmt.Sprintf("Created FTP account %q.", record.Username))
			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"account": record,
			})
		})

		ftpAccountsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			var input ftp.UpdateAccountInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			accountID := chi.URLParam(r, "accountID")
			record, err := app.FTPAccounts.UpdateAccount(r.Context(), accountID, input)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "ftp account not found",
					})
					return
				}

				var validation ftp.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("update ftp account failed",
					zap.String("account_id", accountID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "ftp", "update", "ftp_account", accountID, accountID, "failed", "Failed to update the FTP account.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update ftp account",
				})
				return
			}

			mutationEvent(r.Context(), "ftp", "update", "ftp_account", record.ID, record.Username, "succeeded", fmt.Sprintf("Updated FTP account %q.", record.Username))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"account": record,
			})
		})

		ftpAccountsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			accountID := chi.URLParam(r, "accountID")
			if err := app.FTPAccounts.DeleteAccount(r.Context(), accountID); err != nil {
				app.Logger.Error("delete ftp account failed",
					zap.String("account_id", accountID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "ftp", "delete", "ftp_account", accountID, accountID, "failed", "Failed to delete the FTP account.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to delete ftp account",
				})
				return
			}

			mutationEvent(r.Context(), "ftp", "delete", "ftp_account", accountID, accountID, "succeeded", "Deleted the FTP account.")
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"deleted": true,
			})
		})

		domainsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"sites_base_path": app.Domains.BasePath(),
				"domains":         app.Domains.List(),
			})
		})

		domainsLogsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostnameFilter := normalizeDomainLogHostname(r.URL.Query().Get("hostname"))
			typeFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
			if typeFilter == "" {
				typeFilter = "all"
			}
			switch typeFilter {
			case "all", "access", "error":
			default:
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "type must be one of all, access, or error",
				})
				return
			}

			limit := 200
			if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
				parsedLimit, err := strconv.Atoi(rawLimit)
				if err != nil || parsedLimit < 1 || parsedLimit > 1000 {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "limit must be an integer between 1 and 1000",
					})
					return
				}
				limit = parsedLimit
			}

			search := strings.TrimSpace(r.URL.Query().Get("search"))

			records := app.Domains.List()
			hostnames := make([]string, 0, len(records))
			logs := make([]domainLogResponse, 0, len(records)*2)
			for _, record := range records {
				hostnames = append(hostnames, record.Hostname)
				if hostnameFilter != "" && record.Hostname != hostnameFilter {
					continue
				}

				if typeFilter == "all" || typeFilter == "access" {
					logs = append(logs, readDomainLog(record.Hostname, "access", record.Logs.Access, search, limit))
				}
				if typeFilter == "all" || typeFilter == "error" {
					logs = append(logs, readDomainLog(record.Hostname, "error", record.Logs.Error, search, limit))
				}
			}
			sort.Strings(hostnames)

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"hostnames": hostnames,
				"filters": map[string]any{
					"hostname": hostnameFilter,
					"type":     typeFilter,
					"search":   search,
					"limit":    limit,
				},
				"logs": logs,
			})
		})

		domainsPreviewHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			refreshRequested := false
			switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("refresh"))) {
			case "1", "true", "yes":
				refreshRequested = true
			}

			var (
				previewPath string
				err         error
			)
			if refreshRequested {
				previewPath, err = app.Domains.RefreshPreview(r.Context(), hostname)
			} else {
				previewPath, err = app.Domains.EnsurePreview(r.Context(), hostname)
			}
			if err != nil {
				switch {
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
				default:
					app.Logger.Error("load domain preview failed",
						zap.String("hostname", hostname),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
						"error": "failed to load domain preview",
					})
				}
				return
			}

			w.Header().Set("Cache-Control", "private, max-age=3600")
			w.Header().Set("Content-Type", "image/png")
			stdhttp.ServeFile(w, r, previewPath)
		})

		domainsWebsiteCopyHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			sourceRecord, ok := app.Domains.FindByHostname(hostname)
			if !ok {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "domain not found",
				})
				return
			}

			var input struct {
				TargetHostname     string `json:"target_hostname"`
				ReplaceTargetFiles bool   `json:"replace_target_files"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			validation := domain.ValidationErrors{}
			if !isSiteBackedDomainRecord(sourceRecord) {
				validation["kind"] = "Website copying is available only for Static site and Php site domains."
			}

			targetHostname := strings.TrimSpace(input.TargetHostname)
			var targetRecord domain.Record
			if targetHostname == "" {
				validation["target_hostname"] = "Select a destination domain."
			} else {
				record, exists := app.Domains.FindByHostname(targetHostname)
				if !exists {
					validation["target_hostname"] = "Select a valid destination domain."
				} else {
					targetRecord = record
					if !isSiteBackedDomainRecord(targetRecord) {
						validation["target_hostname"] = "Destination domain must be a Static site or Php site."
					}
					if targetRecord.Hostname == sourceRecord.Hostname {
						validation["target_hostname"] = "Choose a different destination domain."
					}
				}
			}

			if len(validation) > 0 {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error":        "validation failed",
					"field_errors": map[string]string(validation),
				})
				return
			}

			if err := copyDomainDocumentRoot(
				sourceRecord,
				targetRecord,
				app.Domains.BasePath(),
				input.ReplaceTargetFiles,
			); err != nil {
				switch {
				case errors.Is(err, errDomainCopyConflict):
					writeJSON(w, stdhttp.StatusConflict, map[string]any{
						"error": "target directory already contains files that would be replaced",
					})
				case errors.Is(err, errDomainCopyInvalidTarget):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": "validation failed",
						"field_errors": map[string]string{
							"target_hostname": "Destination domain must use a different document root.",
						},
					})
				default:
					app.Logger.Error("copy website failed",
						zap.String("source_hostname", sourceRecord.Hostname),
						zap.String("target_hostname", targetRecord.Hostname),
						zap.Error(err),
					)
					mutationEvent(
						r.Context(),
						"domains",
						"copy",
						"website",
						sourceRecord.Hostname,
						sourceRecord.Hostname,
						"failed",
						"Failed to copy website files.",
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to copy website",
					})
				}
				return
			}

			if err := app.Domains.InvalidatePreview(targetRecord.Hostname); err != nil {
				app.Logger.Warn("invalidate copied domain preview failed",
					zap.String("hostname", targetRecord.Hostname),
					zap.Error(err),
				)
			}

			mutationEvent(
				r.Context(),
				"domains",
				"copy",
				"website",
				sourceRecord.Hostname,
				sourceRecord.Hostname,
				"succeeded",
				fmt.Sprintf(`Copied website files from %q to %q.`, sourceRecord.Hostname, targetRecord.Hostname),
			)

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"ok":              true,
				"source_hostname": sourceRecord.Hostname,
				"target_hostname": targetRecord.Hostname,
			})
		})

		domainsComposerActionHandler := func(action string) stdhttp.HandlerFunc {
			return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				hostname := chi.URLParam(r, "hostname")
				record, err := runDomainComposerAction(r.Context(), app.Domains, hostname, action)
				if err != nil {
					switch {
					case errors.Is(err, domain.ErrNotFound):
						writeJSON(w, stdhttp.StatusNotFound, map[string]any{
							"error": "domain not found",
						})
					case errors.Is(err, errComposerUnsupportedDomain), errors.Is(err, errComposerMissingManifest):
						writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
							"error": err.Error(),
						})
					case errors.Is(err, errComposerUnavailable):
						writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
							"error": err.Error(),
						})
					default:
						app.Logger.Error("run composer command failed",
							zap.String("hostname", hostname),
							zap.String("action", action),
							zap.Error(err),
						)
						mutationEvent(
							r.Context(),
							"domains",
							"composer_"+action,
							"domain",
							record.ID,
							record.Hostname,
							"failed",
							fmt.Sprintf("Failed to run composer %s for %q.", action, record.Hostname),
						)
						writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
							"error": err.Error(),
						})
					}
					return
				}

				mutationEvent(
					r.Context(),
					"domains",
					"composer_"+action,
					"domain",
					record.ID,
					record.Hostname,
					"succeeded",
					fmt.Sprintf("Ran composer %s for %q.", action, record.Hostname),
				)
				writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
			}
		}

		domainsGitHubUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			record, ok := app.Domains.FindByHostname(hostname)
			if !ok {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "domain not found",
				})
				return
			}
			if err := ensureGitHubIntegrationSupported(record); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": err.Error(),
				})
				return
			}

			var input domainGitHubIntegrationInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			repositoryURL := strings.TrimSpace(input.RepositoryURL)
			postFetchScript := strings.TrimSpace(input.PostFetchScript)
			existingIntegration := record.GitHub
			if repositoryURL == "" {
				if existingIntegration != nil && existingIntegration.WebhookID > 0 {
					if token, err := getGitHubToken(r.Context(), app.Settings); err == nil {
						if ref, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
							if err := deleteGitHubWebhook(r.Context(), token, ref, existingIntegration.WebhookID); err != nil {
								app.Logger.Warn("delete github webhook failed",
									zap.String("hostname", record.Hostname),
									zap.Error(err),
								)
							}
						}
					}
				}

				updatedRecord, err := app.Domains.DeleteGitHubIntegration(r.Context(), hostname)
				if err != nil {
					app.Logger.Error("delete github integration failed",
						zap.String("hostname", hostname),
						zap.Error(err),
					)
					mutationEvent(r.Context(), "domains", "github_disconnect", "domain", record.ID, record.Hostname, "failed", "Failed to remove the GitHub integration.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to remove github integration",
					})
					return
				}

				mutationEvent(r.Context(), "domains", "github_disconnect", "domain", updatedRecord.ID, updatedRecord.Hostname, "succeeded", fmt.Sprintf("Removed the GitHub integration for %q.", updatedRecord.Hostname))
				writeJSON(w, stdhttp.StatusOK, map[string]any{
					"domain": updatedRecord,
				})
				return
			}

			token, err := getGitHubToken(r.Context(), app.Settings)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": err.Error(),
					"field_errors": map[string]string{
						"repository_url": "Add a GitHub token in Settings first.",
					},
				})
				return
			}

			repoRef, err := parseGitHubRepositoryURL(repositoryURL)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": err.Error(),
					"field_errors": map[string]string{
						"repository_url": err.Error(),
					},
				})
				return
			}

			metadata, err := loadGitHubRepositoryMetadata(r.Context(), token, repoRef)
			if err != nil {
				app.Logger.Error("load github repository metadata failed",
					zap.String("hostname", hostname),
					zap.String("repository", repositoryURL),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
					"error": err.Error(),
				})
				return
			}

			now := time.Now().UTC()
			createdAt := now
			webhookID := int64(0)
			webhookSecret := ""
			if existingIntegration != nil {
				createdAt = existingIntegration.CreatedAt
				if createdAt.IsZero() {
					createdAt = now
				}
				webhookID = existingIntegration.WebhookID
				webhookSecret = existingIntegration.WebhookSecret
			}

			if existingIntegration != nil && existingIntegration.WebhookID > 0 && !sameGitHubRepository(existingIntegration.RepositoryURL, metadata.CloneURL) {
				if previousRef, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
					if err := deleteGitHubWebhook(r.Context(), token, previousRef, existingIntegration.WebhookID); err != nil {
						app.Logger.Error("delete previous github webhook failed",
							zap.String("hostname", hostname),
							zap.Error(err),
						)
						writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
							"error": err.Error(),
						})
						return
					}
				}
				webhookID = 0
				webhookSecret = ""
			}

			if input.AutoDeployOnPush {
				if webhookSecret == "" {
					webhookSecret, err = generateGitHubWebhookSecret()
					if err != nil {
						app.Logger.Error("generate github webhook secret failed",
							zap.String("hostname", hostname),
							zap.Error(err),
						)
						writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
							"error": "failed to generate webhook secret",
						})
						return
					}
				}

				panelURL, panelURLErr := currentPanelURL(r.Context(), app)
				if panelURLErr != nil {
					app.Logger.Error("load panel url for github webhook failed",
						zap.String("hostname", hostname),
						zap.Error(panelURLErr),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to load panel url",
					})
					return
				}

				webhookURL, err := buildGitHubWebhookURL(r, record.Hostname, panelURL)
				if err != nil {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error": err.Error(),
					})
					return
				}

				webhookID, err = upsertGitHubWebhook(r.Context(), token, repoRef, webhookID, webhookURL, webhookSecret)
				if err != nil {
					app.Logger.Error("configure github webhook failed",
						zap.String("hostname", hostname),
						zap.String("repository", metadata.CloneURL),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
						"error": err.Error(),
					})
					return
				}
			} else if existingIntegration != nil && existingIntegration.WebhookID > 0 {
				if existingRef, refErr := parseGitHubRepositoryURL(existingIntegration.RepositoryURL); refErr == nil {
					if err := deleteGitHubWebhook(r.Context(), token, existingRef, existingIntegration.WebhookID); err != nil {
						app.Logger.Error("delete github webhook failed",
							zap.String("hostname", hostname),
							zap.Error(err),
						)
						writeJSON(w, stdhttp.StatusBadGateway, map[string]any{
							"error": err.Error(),
						})
						return
					}
				}
				webhookID = 0
				webhookSecret = ""
			}

			integration := domain.GitHubIntegration{
				RepositoryURL:    strings.TrimSpace(metadata.CloneURL),
				AutoDeployOnPush: input.AutoDeployOnPush,
				DefaultBranch:    strings.TrimSpace(metadata.DefaultBranch),
				PostFetchScript:  postFetchScript,
				WebhookSecret:    webhookSecret,
				WebhookID:        webhookID,
				CreatedAt:        createdAt,
				UpdatedAt:        now,
			}

			updatedRecord, err := app.Domains.UpsertGitHubIntegration(r.Context(), hostname, integration)
			if err != nil {
				app.Logger.Error("save github integration failed",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "github_update", "domain", record.ID, record.Hostname, "failed", "Failed to save the GitHub integration.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to save github integration",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "github_update", "domain", updatedRecord.ID, updatedRecord.Hostname, "succeeded", fmt.Sprintf("Updated the GitHub integration for %q.", updatedRecord.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain": updatedRecord,
			})
		})

		domainsGitHubDeployHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			record, ok := app.Domains.FindByHostname(hostname)
			if !ok {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "domain not found",
				})
				return
			}
			if err := ensureGitHubIntegrationSupported(record); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": err.Error(),
				})
				return
			}
			if record.GitHub == nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": errGitHubIntegrationNotConfigured.Error(),
				})
				return
			}

			token, err := getGitHubToken(r.Context(), app.Settings)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": err.Error(),
				})
				return
			}

			result, err := runDomainGitHubDeploy(r.Context(), record, *record.GitHub, token)
			if err != nil {
				app.Logger.Error("github deploy failed",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "github_deploy", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Failed to deploy %q from GitHub.", record.Hostname))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			mutationEvent(r.Context(), "domains", "github_deploy", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Deployed %q from GitHub.", record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"ok":     true,
				"action": result.Action,
			})
		})

		domainsGitHubWebhookHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")
			record, ok := app.Domains.FindByHostname(hostname)
			if !ok || record.GitHub == nil || !record.GitHub.AutoDeployOnPush || strings.TrimSpace(record.GitHub.WebhookSecret) == "" {
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{
					"error": "github webhook not configured",
				})
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "failed to read webhook payload",
				})
				return
			}

			signature := r.Header.Get("X-Hub-Signature-256")
			if !verifyGitHubWebhookSignature(record.GitHub.WebhookSecret, body, signature) {
				writeJSON(w, stdhttp.StatusUnauthorized, map[string]any{
					"error": errGitHubInvalidWebhookSignature.Error(),
				})
				return
			}

			eventName := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
			switch eventName {
			case "ping":
				writeJSON(w, stdhttp.StatusAccepted, map[string]any{
					"ok": true,
				})
				return
			case "push":
			default:
				writeJSON(w, stdhttp.StatusAccepted, map[string]any{
					"ok": true,
				})
				return
			}

			var payload gitHubWebhookPushPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid webhook payload",
				})
				return
			}

			if payload.Repository.CloneURL != "" && !sameGitHubRepository(payload.Repository.CloneURL, record.GitHub.RepositoryURL) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "webhook repository does not match this domain integration",
				})
				return
			}

			defaultBranch := strings.TrimSpace(record.GitHub.DefaultBranch)
			if defaultBranch == "" {
				defaultBranch = strings.TrimSpace(payload.Repository.DefaultBranch)
			}
			if defaultBranch != "" && payload.Ref != "refs/heads/"+defaultBranch {
				writeJSON(w, stdhttp.StatusAccepted, map[string]any{
					"ok": true,
				})
				return
			}

			token, err := getGitHubToken(r.Context(), app.Settings)
			if err != nil {
				app.Logger.Error("github webhook deploy blocked by missing token",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "failed", "GitHub webhook was received but no GitHub token is configured.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			result, err := runDomainGitHubDeploy(r.Context(), record, *record.GitHub, token)
			if err != nil {
				app.Logger.Error("github webhook deploy failed",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "failed", fmt.Sprintf("Push webhook deployment failed for %q.", record.Hostname))
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			mutationEvent(r.Context(), "domains", "github_webhook_deploy", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Push webhook deployed %q.", record.Hostname))
			writeJSON(w, stdhttp.StatusAccepted, map[string]any{
				"ok":     true,
				"action": result.Action,
			})
		})

		domainsPHPSettingsUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			hostname := chi.URLParam(r, "hostname")

			var input domain.UpdatePHPInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			record, err := app.Domains.UpdatePHPSettings(r.Context(), hostname, input)
			if err != nil {
				var validation domain.ValidationErrors
				switch {
				case errors.As(err, &validation):
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				case errors.Is(err, domain.ErrNotFound):
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
					return
				default:
					app.Logger.Error("update domain php settings failed",
						zap.String("hostname", hostname),
						zap.Error(err),
					)
					mutationEvent(r.Context(), "domains", "update_php_settings", "domain", hostname, hostname, "failed", "Failed to update domain PHP settings.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to update domain php settings",
					})
					return
				}
			}

			if err := syncDomainsWithCaddy(r.Context()); err != nil {
				app.Logger.Error("sync domains after php settings update failed",
					zap.String("hostname", hostname),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "update_php_settings", "domain", record.ID, record.Hostname, "failed", "Saved domain PHP settings but failed to republish routes.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "domain php settings saved but routes could not be refreshed",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "update_php_settings", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Updated PHP settings for %q.", record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain": record,
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

			if app.FTPAccounts != nil {
				if err := app.FTPAccounts.ReconcileDomain(r.Context(), record); err != nil {
					_, removed, rollbackErr := app.Domains.Delete(r.Context(), record.ID)
					if rollbackErr != nil {
						app.Logger.Error("rollback created domain after ftp setup failed",
							zap.String("domain_id", record.ID),
							zap.Error(rollbackErr),
						)
					} else if !removed {
						app.Logger.Error("rollback created domain after ftp setup missing", zap.String("domain_id", record.ID))
					}
					app.Logger.Error("create default ftp account failed",
						zap.String("domain_id", record.ID),
						zap.String("hostname", record.Hostname),
						zap.Error(err),
					)
					mutationEvent(r.Context(), "domains", "create", "domain", record.ID, record.Hostname, "failed", "Created domain record but failed to provision its FTP account.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "failed to provision ftp account",
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
				if app.FTPAccounts != nil {
					if cleanupErr := app.FTPAccounts.DeleteDomain(r.Context(), record.ID); cleanupErr != nil {
						app.Logger.Warn("cleanup ftp account after domain publish failure failed",
							zap.String("domain_id", record.ID),
							zap.String("hostname", record.Hostname),
							zap.Error(cleanupErr),
						)
					}
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

			if app.FTPAccounts != nil {
				if err := app.FTPAccounts.ReconcileDomain(r.Context(), record); err != nil {
					app.Logger.Error("reconcile ftp account after domain update failed",
						zap.String("domain_id", record.ID),
						zap.String("hostname", record.Hostname),
						zap.Error(err),
					)
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "domain updated but ftp account could not be reconciled",
					})
					return
				}
			}

			mutationEvent(r.Context(), "domains", "update", "domain", record.ID, record.Hostname, "succeeded", fmt.Sprintf("Updated domain %q.", record.Hostname))
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain": record,
			})
		})

		domainsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			domainID := chi.URLParam(r, "domainID")
			deleteDatabase := queryEnabled(r, "delete_database")
			deleteDocumentRoot := queryEnabled(r, "delete_document_root")
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

			warnings := make([]string, 0, 2)
			if deleteDatabase {
				cleanupWarnings, cleanupErr := deleteLinkedDomainDatabases(r.Context(), app.MariaDB, record.Hostname)
				warnings = append(warnings, cleanupWarnings...)
				if cleanupErr != nil {
					app.Logger.Warn("delete linked databases failed",
						zap.String("domain_id", record.ID),
						zap.String("hostname", record.Hostname),
						zap.Error(cleanupErr),
					)
				}
			}
			if deleteDocumentRoot {
				if warning, cleanupErr := deleteDomainDocumentRoot(record, app.Domains.BasePath()); cleanupErr != nil {
					warnings = append(warnings, warning)
					app.Logger.Warn("delete domain document root failed",
						zap.String("domain_id", record.ID),
						zap.String("hostname", record.Hostname),
						zap.Error(cleanupErr),
					)
				}
			}

			if app.FTPAccounts != nil {
				if cleanupErr := app.FTPAccounts.DeleteDomain(r.Context(), record.ID); cleanupErr != nil {
					warnings = append(warnings, "The FTP account could not be removed.")
					app.Logger.Warn("delete domain ftp account failed",
						zap.String("domain_id", record.ID),
						zap.String("hostname", record.Hostname),
						zap.Error(cleanupErr),
					)
				}
			}

			message := fmt.Sprintf("Deleted domain %q.", record.Hostname)
			if len(warnings) > 0 {
				message = fmt.Sprintf(`Deleted domain %q with cleanup warnings.`, record.Hostname)
			}
			mutationEvent(r.Context(), "domains", "delete", "domain", record.ID, record.Hostname, "succeeded", message)

			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"domain":   record,
				"warnings": warnings,
			})
		})

		domainFTPGetHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			domainID := chi.URLParam(r, "domainID")
			status, err := app.FTPAccounts.GetDomainStatus(r.Context(), domainID)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
					return
				}

				app.Logger.Error("load domain ftp status failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to load ftp account",
				})
				return
			}

			if err := writeDomainFTPResponse(w, stdhttp.StatusOK, app, r, status); err != nil {
				app.Logger.Error("load ftp connection settings failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to load ftp connection settings",
				})
				return
			}
		})

		domainFTPUpdateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			var input ftp.UpdateInput
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			domainID := chi.URLParam(r, "domainID")
			status, err := app.FTPAccounts.UpdateDomain(r.Context(), domainID, input)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
					return
				}

				var validation ftp.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("update domain ftp account failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "update_ftp", "domain", domainID, domainID, "failed", "Failed to update the domain FTP account.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update ftp account",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "update_ftp", "domain", domainID, status.Username, "succeeded", "Updated the domain FTP account.")
			if err := writeDomainFTPResponse(w, stdhttp.StatusOK, app, r, status); err != nil {
				app.Logger.Error("load ftp connection settings after update failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "ftp account updated but connection settings could not be loaded",
				})
				return
			}
		})

		domainFTPResetPasswordHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.FTPAccounts == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "ftp accounts are not configured",
				})
				return
			}

			domainID := chi.URLParam(r, "domainID")
			status, password, err := app.FTPAccounts.ResetPassword(r.Context(), domainID)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					writeJSON(w, stdhttp.StatusNotFound, map[string]any{
						"error": "domain not found",
					})
					return
				}

				var validation ftp.ValidationErrors
				if errors.As(err, &validation) {
					writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
						"error":        "validation failed",
						"field_errors": map[string]string(validation),
					})
					return
				}

				app.Logger.Error("reset domain ftp password failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				mutationEvent(r.Context(), "domains", "reset_ftp_password", "domain", domainID, domainID, "failed", "Failed to reset the domain FTP password.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to reset ftp password",
				})
				return
			}

			mutationEvent(r.Context(), "domains", "reset_ftp_password", "domain", domainID, status.Username, "succeeded", "Reset the domain FTP password.")
			payload, err := domainFTPResponsePayload(r, app, status)
			if err != nil {
				app.Logger.Error("load ftp connection settings after password reset failed",
					zap.String("domain_id", domainID),
					zap.Error(err),
				)
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "ftp password reset but connection settings could not be loaded",
				})
				return
			}
			payload["password"] = password
			writeJSON(w, stdhttp.StatusOK, payload)
		})

		r.Method(stdhttp.MethodGet, "/domains", domainsListHandler)
		r.Method(stdhttp.MethodHead, "/domains", domainsListHandler)
		r.Method(stdhttp.MethodGet, "/domains/logs", domainsLogsHandler)
		r.Method(stdhttp.MethodHead, "/domains/logs", domainsLogsHandler)
		r.Method(stdhttp.MethodGet, "/domains/{hostname}/preview", domainsPreviewHandler)
		r.Method(stdhttp.MethodHead, "/domains/{hostname}/preview", domainsPreviewHandler)
		r.Method(stdhttp.MethodPost, "/domains/{hostname}/copy", domainsWebsiteCopyHandler)
		r.Method(stdhttp.MethodPost, "/domains/{hostname}/composer/install", domainsComposerActionHandler("install"))
		r.Method(stdhttp.MethodPost, "/domains/{hostname}/composer/update", domainsComposerActionHandler("update"))
		r.Method(stdhttp.MethodPut, "/domains/{hostname}/php-settings", domainsPHPSettingsUpdateHandler)
		r.Method(stdhttp.MethodPut, "/domains/{hostname}/github", domainsGitHubUpdateHandler)
		r.Method(stdhttp.MethodPost, "/domains/{hostname}/github/deploy", domainsGitHubDeployHandler)
		r.Method(stdhttp.MethodPost, "/domains/{hostname}/github/webhook", domainsGitHubWebhookHandler)
		r.Method(stdhttp.MethodPost, "/domains", domainsCreateHandler)
		r.Method(stdhttp.MethodPut, "/domains/{domainID}", domainsUpdateHandler)
		r.Method(stdhttp.MethodDelete, "/domains/{domainID}", domainsDeleteHandler)
		r.Method(stdhttp.MethodGet, "/domains/{domainID}/ftp", domainFTPGetHandler)
		r.Method(stdhttp.MethodPut, "/domains/{domainID}/ftp", domainFTPUpdateHandler)
		r.Method(stdhttp.MethodPost, "/domains/{domainID}/ftp/reset-password", domainFTPResetPasswordHandler)
		r.Method(stdhttp.MethodGet, "/ftp/accounts", ftpAccountsListHandler)
		r.Method(stdhttp.MethodHead, "/ftp/accounts", ftpAccountsListHandler)
		r.Method(stdhttp.MethodPost, "/ftp/accounts", ftpAccountsCreateHandler)
		r.Method(stdhttp.MethodPut, "/ftp/accounts/{accountID}", ftpAccountsUpdateHandler)
		r.Method(stdhttp.MethodDelete, "/ftp/accounts/{accountID}", ftpAccountsDeleteHandler)

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

		filesUpdatePermissionsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path        string `json:"path"`
				Permissions string `json:"permissions"`
				Recursive   bool   `json:"recursive"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.SetPermissions(input.Path, input.Permissions, input.Recursive); err != nil {
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

			absolutePath, name, cleanup, err := app.Files.DownloadPath(r.URL.Query().Get("path"))
			if err != nil {
				writeFileError(w, err)
				return
			}
			defer cleanup()

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			stdhttp.ServeFile(w, r, absolutePath)
		})

		filesDownloadArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Paths []string `json:"paths"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			name, writeArchive, err := app.Files.PrepareDownloadPaths(input.Paths)
			if err != nil {
				writeFileError(w, err)
				return
			}

			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
			w.Header().Set("Content-Type", "application/gzip")
			if err := writeArchive(w); err != nil {
				app.Logger.Error("stream file archive failed", zap.Error(err))
			}
		})

		filesCreateArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Paths       []string `json:"paths"`
				Destination string   `json:"destination"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			archivePath, err := app.Files.CreateArchive(input.Paths, input.Destination)
			if err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusCreated, map[string]any{
				"path": archivePath,
			})
		})

		filesExtractArchiveHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if app.Files == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "file manager is not configured",
				})
				return
			}

			var input struct {
				Path string `json:"path"`
			}
			if err := decodeJSON(r, &input); err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "invalid request body",
				})
				return
			}

			if err := app.Files.ExtractArchive(input.Path); err != nil {
				writeFileError(w, err)
				return
			}

			writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
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
		r.Method(stdhttp.MethodPut, "/files/permissions", filesUpdatePermissionsHandler)
		r.Method(stdhttp.MethodPost, "/files/upload", filesUploadHandler)
		r.Method(stdhttp.MethodGet, "/files/download", filesDownloadHandler)
		r.Method(stdhttp.MethodPost, "/files/download", filesDownloadArchiveHandler)
		r.Method(stdhttp.MethodPost, "/files/archive", filesCreateArchiveHandler)
		r.Method(stdhttp.MethodPost, "/files/extract", filesExtractArchiveHandler)
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
	return syncDomainsWithCurrentSettings(ctx, app)
}

func syncDomainsWithCurrentSettings(ctx context.Context, app *app.App) error {
	panelURL, err := currentPanelURL(ctx, app)
	if err != nil {
		return err
	}

	return syncDomainsWithPanelURL(ctx, app, panelURL)
}

func syncDomainsWithPanelURL(ctx context.Context, app *app.App, panelURL string) error {
	if app == nil || app.Caddy == nil || app.Domains == nil {
		return nil
	}

	return app.Caddy.Sync(ctx, app.Domains.List(), panelURL)
}

func currentPanelURL(ctx context.Context, app *app.App) (string, error) {
	if app == nil || app.Settings == nil {
		return "", nil
	}

	record, err := app.Settings.Get(ctx)
	if err != nil {
		return "", err
	}

	return record.PanelURL, nil
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

var errInvalidPanelAssets = errors.New("panel bundle is invalid")

var panelAssetPattern = regexp.MustCompile(`(?:src|href)=["']([^"']+)["']`)

var loadEmbeddedPanelFS = web.DistFS

var loadLocalPanelFS = func() (fs.FS, error) {
	root, err := findFlowPanelRoot()
	if err != nil {
		return nil, err
	}

	return os.DirFS(filepath.Join(root, "web", "dist")), nil
}

var buildLocalPanelAssets = func() error {
	root, err := findFlowPanelRoot()
	if err != nil {
		return err
	}

	cmd := exec.Command("npm", "run", "build")
	cmd.Dir = filepath.Join(root, "web", "panel")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build panel assets: %w", err)
	}

	return nil
}

func newPanelHandler() (*panelHandler, error) {
	distFS, err := loadEmbeddedPanelFS()
	if err != nil {
		return nil, err
	}

	handler, err := newPanelHandlerWithFS(distFS)
	if err == nil || !errors.Is(err, errInvalidPanelAssets) {
		return handler, err
	}

	if buildErr := buildLocalPanelAssets(); buildErr != nil {
		return nil, fmt.Errorf("%w; automatic rebuild failed: %v", err, buildErr)
	}

	localDistFS, localErr := loadLocalPanelFS()
	if localErr != nil {
		return nil, fmt.Errorf("%w; built local assets but could not load web/dist: %v", err, localErr)
	}

	handler, retryErr := newPanelHandlerWithFS(localDistFS)
	if retryErr != nil {
		return nil, fmt.Errorf("%w; rebuilt local assets but validation still failed: %v", err, retryErr)
	}

	return handler, nil
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

	if cleanPath != "index.html" && path.Ext(cleanPath) != "" && !requestPrefersHTML(r) {
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

func requestPrefersHTML(r *stdhttp.Request) bool {
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return strings.Contains(accept, "text/html")
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
			return fmt.Errorf("%w: missing asset %q referenced by index.html: %v", errInvalidPanelAssets, assetPath, err)
		}
		if stat.IsDir() {
			return fmt.Errorf("%w: asset %q referenced by index.html is a directory", errInvalidPanelAssets, assetPath)
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

func findFlowPanelRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	for current := wd; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(current, "web", "panel", "package.json")); err == nil {
				return current, nil
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	return "", errors.New("could not locate FlowPanel project root")
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
	case errors.Is(err, filesvc.ErrInvalidPermissions):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid permissions value",
		})
	case errors.Is(err, filesvc.ErrUnsupportedArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "unsupported archive format",
		})
	case errors.Is(err, filesvc.ErrInvalidArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid archive contents",
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

type domainLogResponse struct {
	Hostname     string     `json:"hostname"`
	Type         string     `json:"type"`
	Path         string     `json:"path"`
	Available    bool       `json:"available"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
	SizeBytes    int64      `json:"size_bytes"`
	TotalMatches int        `json:"total_matches"`
	Truncated    bool       `json:"truncated"`
	ReadError    string     `json:"read_error,omitempty"`
	Lines        []string   `json:"lines"`
}

func normalizeDomainLogHostname(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func readDomainLog(hostname string, logType string, filePath string, search string, limit int) domainLogResponse {
	response := domainLogResponse{
		Hostname: hostname,
		Type:     logType,
		Path:     filePath,
		Lines:    []string{},
	}

	if strings.TrimSpace(filePath) == "" {
		return response
	}

	info, err := os.Stat(filePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			response.ReadError = err.Error()
		}
		return response
	}

	response.Available = true
	response.SizeBytes = info.Size()
	modifiedAt := info.ModTime().UTC()
	response.ModifiedAt = &modifiedAt

	lines, totalMatches, truncated, err := tailMatchingLogLines(filePath, search, limit)
	if err != nil {
		response.ReadError = err.Error()
		return response
	}

	response.TotalMatches = totalMatches
	response.Truncated = truncated
	response.Lines = lines

	return response
}

func tailMatchingLogLines(filePath string, search string, limit int) ([]string, int, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, false, err
	}
	defer file.Close()

	search = strings.ToLower(strings.TrimSpace(search))

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lines := make([]string, 0, limit)
	totalMatches := 0
	for scanner.Scan() {
		line := scanner.Text()
		if search != "" && !strings.Contains(strings.ToLower(line), search) {
			continue
		}

		totalMatches++
		if len(lines) < limit {
			lines = append(lines, line)
			continue
		}

		copy(lines, lines[1:])
		lines[len(lines)-1] = line
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, false, err
	}

	return lines, totalMatches, totalMatches > limit, nil
}

func queryEnabled(r *stdhttp.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func deleteLinkedDomainDatabases(ctx context.Context, manager mariadb.Manager, hostname string) ([]string, error) {
	if manager == nil {
		err := errors.New("mariadb runtime is not configured")
		return []string{"MariaDB runtime is not configured, so linked databases were not deleted."}, err
	}

	databases, err := manager.ListDatabases(ctx)
	if err != nil {
		return []string{"Failed to load linked databases for deletion."}, err
	}

	var warnings []string
	for _, database := range databases {
		if strings.TrimSpace(database.Domain) != hostname {
			continue
		}

		if err := manager.DeleteDatabase(ctx, database.Name, mariadb.DeleteDatabaseInput{
			Username: database.Username,
		}); err != nil {
			warnings = append(warnings, fmt.Sprintf(`Failed to delete linked database %q.`, database.Name))
		}
	}

	if len(warnings) > 0 {
		return warnings, errors.New(strings.Join(warnings, " "))
	}

	return nil, nil
}

var (
	errDomainCopyConflict      = errors.New("target directory already contains conflicting files")
	errDomainCopyInvalidTarget = errors.New("source and destination domains share the same document root")
)

func isSiteBackedDomainRecord(record domain.Record) bool {
	return record.Kind == domain.KindStaticSite || record.Kind == domain.KindPHP
}

func resolveDomainDocumentRoot(record domain.Record, basePath string) (string, error) {
	if !isSiteBackedDomainRecord(record) {
		return "", errors.New("domain is not site-backed")
	}

	normalizedBasePath := filepath.Clean(strings.TrimSpace(basePath))
	if normalizedBasePath == "." || normalizedBasePath == "" {
		return "", errors.New("domain sites base path is not configured")
	}

	targetPath := filepath.Clean(strings.TrimSpace(record.Target))
	if targetPath == "." || targetPath == "" {
		return "", errors.New("domain document root is not configured")
	}

	relativePath, err := filepath.Rel(normalizedBasePath, targetPath)
	if err != nil {
		return "", err
	}
	if relativePath == "." {
		return "", errors.New("refusing to use the sites base path as a document root")
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("document root %q is outside the sites base path", targetPath)
	}

	return targetPath, nil
}

func copyDomainDocumentRoot(
	source domain.Record,
	target domain.Record,
	basePath string,
	replaceTargetFiles bool,
) error {
	sourcePath, err := resolveDomainDocumentRoot(source, basePath)
	if err != nil {
		return err
	}

	targetPath, err := resolveDomainDocumentRoot(target, basePath)
	if err != nil {
		return err
	}

	if sourcePath == targetPath {
		return errDomainCopyInvalidTarget
	}

	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return fmt.Errorf("ensure target document root: %w", err)
	}

	if replaceTargetFiles {
		if err := clearDocumentRootContents(targetPath); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return fmt.Errorf("read source document root: %w", err)
	}

	for _, entry := range entries {
		sourceEntryPath := filepath.Join(sourcePath, entry.Name())
		targetEntryPath := filepath.Join(targetPath, entry.Name())
		if err := filesvc.CopyPath(sourceEntryPath, targetEntryPath); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return errDomainCopyConflict
			}
			return fmt.Errorf("copy document root entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func clearDocumentRootContents(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(path, 0o755)
		}
		return fmt.Errorf("read target document root: %w", err)
	}

	for _, entry := range entries {
		entryPath := filepath.Join(path, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("clear target document root entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func deleteDomainDocumentRoot(record domain.Record, basePath string) (string, error) {
	if !isSiteBackedDomainRecord(record) {
		return "", nil
	}

	targetPath, err := resolveDomainDocumentRoot(record, basePath)
	if err != nil {
		return "The domain document root could not be deleted.", err
	}

	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Sprintf(`Failed to delete the document root for %q.`, record.Hostname), err
	}

	return "", nil
}

func syncBackupRestoreState(ctx context.Context, app *app.App, result backup.RestoreResult) error {
	if !result.RestoredPanelDatabase {
		return nil
	}

	if app.Domains != nil {
		if err := app.Domains.Load(ctx); err != nil {
			return fmt.Errorf("reload domains: %w", err)
		}
	}

	if app.Cron != nil {
		snapshot := app.Cron.Snapshot()
		if snapshot.Started {
			if err := app.Cron.Stop(ctx); err != nil {
				return fmt.Errorf("stop cron scheduler: %w", err)
			}
		}
		if err := app.Cron.Load(ctx); err != nil {
			return fmt.Errorf("reload cron jobs: %w", err)
		}
		if snapshot.Started {
			app.Cron.Start()
		}
	}

	if app.Caddy != nil && app.Domains != nil {
		if err := syncDomainsWithCurrentSettings(ctx, app); err != nil {
			return fmt.Errorf("sync caddy runtime: %w", err)
		}
	}

	return nil
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
	case errors.Is(err, backup.ErrInvalidArchive):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid backup archive",
		})
	case errors.Is(err, backup.ErrAlreadyExists):
		writeJSON(w, stdhttp.StatusConflict, map[string]any{
			"error": "backup already exists",
		})
	case errors.Is(err, backup.ErrInvalidLocation):
		writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
			"error": "invalid backup location",
		})
	default:
		writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
			"error": err.Error(),
		})
	}
}

func writeSettingsResponse(w stdhttp.ResponseWriter, statusCode int, app *app.App, record settings.Record) {
	w.Header().Set("Cache-Control", "no-store")
	settingsPayload := map[string]any{
		"panel_name":             record.PanelName,
		"panel_url":              record.PanelURL,
		"github_token":           record.GitHubToken,
		"ftp_enabled":            record.FTPEnabled,
		"ftp_port":               record.FTPPort,
		"ftp_passive_ports":      record.FTPPassivePorts,
		"google_drive_email":     record.GoogleDriveEmail,
		"google_drive_connected": record.GoogleDriveConnected,
		"google_drive_available": app != nil && app.GoogleDrive != nil && app.GoogleDrive.Enabled(),
	}

	writeJSON(w, statusCode, map[string]any{
		"settings": settingsPayload,
	})
}

func ftpConfigFromSettings(record settings.Record) ftp.Config {
	return ftp.Config{
		Enabled:      record.FTPEnabled,
		Host:         record.FTPHost,
		Port:         record.FTPPort,
		PublicIP:     "",
		PassivePorts: record.FTPPassivePorts,
	}
}

func writeDomainFTPResponse(w stdhttp.ResponseWriter, statusCode int, app *app.App, r *stdhttp.Request, ftpStatus ftp.DomainStatus) error {
	payload, err := domainFTPResponsePayload(r, app, ftpStatus)
	if err != nil {
		return err
	}

	writeJSON(w, statusCode, payload)
	return nil
}

func domainFTPResponsePayload(r *stdhttp.Request, app *app.App, ftpStatus ftp.DomainStatus) (map[string]any, error) {
	record := settings.Record{
		FTPPort: ftp.DefaultPort(),
	}
	if app != nil && app.Settings != nil {
		var err error
		record, err = app.Settings.Get(r.Context())
		if err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"ftp": map[string]any{
			"supported":    ftpStatus.Supported,
			"enabled":      ftpStatus.Enabled,
			"username":     ftpStatus.Username,
			"root_path":    ftpStatus.RootPath,
			"has_password": ftpStatus.HasPassword,
			"host":         ftpConnectionHost(r, record),
			"port":         record.FTPPort,
		},
	}, nil
}

func ftpConnectionHost(r *stdhttp.Request, record settings.Record) string {
	if host := hostNameFromURL(record.PanelURL); host != "" {
		return host
	}
	if r == nil {
		return ""
	}

	return hostNameFromURL(requestBaseURL(r))
}

func hostNameFromURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil {
		return ""
	}

	return strings.TrimSpace(parsed.Hostname())
}

func decodeBackupNameParam(r *stdhttp.Request) (string, error) {
	name, err := url.PathUnescape(chi.URLParam(r, "backupName"))
	if err != nil {
		return "", backup.ErrInvalidName
	}

	return name, nil
}

func readBackupLocation(r *stdhttp.Request) string {
	if r == nil {
		return backup.LocationLocal
	}

	return strings.TrimSpace(r.URL.Query().Get("location"))
}

func randomOAuthState() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func buildGoogleDriveRedirectURL(r *stdhttp.Request) string {
	return strings.TrimRight(requestBaseURL(r), "/") + "/api/settings/google-drive/callback"
}

func writeOAuthPopupPage(w stdhttp.ResponseWriter, statusCode int, status string, message string, email string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = fmt.Fprintf(
		w,
		`<!doctype html>
<html>
<body>
<script>
window.opener && window.opener.postMessage({ type: "flowpanel-google-drive-oauth", status: %q, message: %q, email: %q }, window.location.origin);
window.close();
</script>
</body>
</html>`,
		status,
		message,
		email,
	)
}
