package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/backup"
	"flowpanel/internal/caddy"
	flowcron "flowpanel/internal/cron"
	eventlog "flowpanel/internal/events"
	"flowpanel/internal/ftp"
	"flowpanel/internal/googledrive"
	"flowpanel/internal/settings"
	"flowpanel/internal/systemstatus"
	"flowpanel/web"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

const googleDriveOAuthStateSessionKey = "google_drive_oauth_state"

var runPHPInfoCommand = func(ctx context.Context, phpPath string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "flowpanel-phpinfo-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	scriptPath := filepath.Join(tempDir, "index.php")
	if err := os.WriteFile(scriptPath, []byte("<?php phpinfo();\n"), 0o644); err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, phpPath, "-S", address, "-t", tempDir)
	var serverLog bytes.Buffer
	cmd.Stdout = &serverLog
	cmd.Stderr = &serverLog
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	stopServer := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-waitDone:
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer stopServer()

	client := stdhttp.Client{Timeout: 2 * time.Second}
	targetURL := "http://" + address + "/"

	for {
		req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}

		response, err := client.Do(req)
		if err == nil {
			defer response.Body.Close()
			body, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				return nil, readErr
			}
			if response.StatusCode != stdhttp.StatusOK {
				return nil, fmt.Errorf("php info server returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
			}
			return body, nil
		}

		select {
		case waitErr := <-waitDone:
			if waitErr != nil {
				logOutput := strings.TrimSpace(serverLog.String())
				if logOutput != "" {
					return nil, fmt.Errorf("php info server exited: %w: %s", waitErr, logOutput)
				}
				return nil, fmt.Errorf("php info server exited: %w", waitErr)
			}
			return nil, errors.New("php info server exited before responding")
		default:
		}

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		time.Sleep(100 * time.Millisecond)
	}
}

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
		api := newAPIRoutes(app)
		r.Use(RequirePanelAuth(app))

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
				api.mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "failed", "Failed to update panel settings.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update settings",
				})
				return
			}

			if app.FTP != nil {
				if err := app.FTP.Apply(r.Context(), ftpConfigFromSettings(record)); err != nil {
					app.Logger.Error("apply ftp settings failed", zap.Error(err))
					api.mutationEvent(r.Context(), "settings", "update", "settings", "ftp", "FTP settings", "failed", "Saved settings but could not apply FTP runtime changes.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "settings saved but ftp runtime could not be updated",
					})
					return
				}
			}

			if previousPanelURL != record.PanelURL {
				if err := syncDomainsWithPanelURL(r.Context(), app, record.PanelURL); err != nil {
					if errors.Is(err, caddy.ErrRuntimeNotStarted) {
						api.mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "succeeded", "Updated panel settings.")
						writeSettingsResponse(w, stdhttp.StatusOK, app, record)
						return
					}
					app.Logger.Error("sync caddy runtime after settings update failed", zap.Error(err))
					api.mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "failed", "Saved panel settings but could not refresh panel routing.")
					writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
						"error": "settings saved but panel routing could not be refreshed",
					})
					return
				}
			}

			api.mutationEvent(r.Context(), "settings", "update", "settings", "panel", "Panel settings", "succeeded", "Updated panel settings.")
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

			api.mutationEvent(r.Context(), "settings", "upload_google_drive_oauth_credentials", "settings", "google_drive", "Google Drive", "succeeded", "Uploaded Google Drive OAuth credentials.")
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

			api.mutationEvent(r.Context(), "settings", "connect_google_drive", "settings", "google_drive", record.GoogleDriveEmail, "succeeded", "Connected a Google Drive account.")
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

			api.mutationEvent(r.Context(), "settings", "disconnect_google_drive", "settings", "google_drive", "Google Drive", "succeeded", "Disconnected the Google Drive account.")
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

		api.registerBackupRoutes(r)

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
				api.mutationEvent(r.Context(), "cron", "create", "cron_job", strings.TrimSpace(input.Name), strings.TrimSpace(input.Name), "failed", "Failed to create cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to create cron job",
				})
				return
			}

			api.mutationEvent(r.Context(), "cron", "create", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Created cron job %q.", record.Name))

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
				api.mutationEvent(r.Context(), "cron", "update", "cron_job", jobID, jobID, "failed", "Failed to update cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to update cron job",
				})
				return
			}

			api.mutationEvent(r.Context(), "cron", "update", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Updated cron job %q.", record.Name))

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
				api.mutationEvent(r.Context(), "cron", "run", "cron_job", jobID, jobID, "failed", "Failed to run cron job.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": "failed to run cron job",
				})
				return
			}

			api.mutationEvent(r.Context(), "cron", "run", "cron_job", record.ID, record.Name, "succeeded", fmt.Sprintf("Triggered cron job %q.", record.Name))

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
				api.mutationEvent(r.Context(), "cron", "delete", "cron_job", jobID, jobID, "failed", "Failed to delete cron job.")
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

			api.mutationEvent(r.Context(), "cron", "delete", "cron_job", jobID, jobID, "succeeded", fmt.Sprintf("Deleted cron job %q.", jobID))

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

		api.registerGoRoutes(r)
		api.registerMariaDBRoutes(r)

		api.registerPHPRoutes(r)
		api.registerDomainRoutes(r)
		api.registerFileRoutes(r)

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

func writeSettingsResponse(w stdhttp.ResponseWriter, statusCode int, app *app.App, record settings.Record) {
	w.Header().Set("Cache-Control", "no-store")
	settingsPayload := map[string]any{
		"panel_name":             record.PanelName,
		"panel_url":              record.PanelURL,
		"github_token":           record.GitHubToken,
		"default_php_version":    record.DefaultPHPVersion,
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
