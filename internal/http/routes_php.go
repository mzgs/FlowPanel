package httpx

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strings"
	"time"

	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/settings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

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

			actionCtx := backgroundRequestContext(r.Context())
			version := a.phpActionVersion(r)
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
