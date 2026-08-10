package httpx

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/alerts"
	"flowpanel/internal/app"
	"flowpanel/internal/caddy"
	eventlog "flowpanel/internal/events"
	"flowpanel/internal/golang"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/nodejs"
	"flowpanel/internal/packageruntime"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/pm2"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type apiRoutes struct {
	app                     *app.App
	runtimeActions          *runtimeActionTracker
	backupCreateMu          sync.Mutex
	backupCreateRunning     bool
	backupCreateJobs        map[string]backupCreateJob
	backupRestoreMu         sync.Mutex
	backupRestoreRunning    bool
	githubWebhookMu         sync.Mutex
	githubWebhookDeliveries map[string]time.Time
	githubWebhooksInFlight  map[string]struct{}
}

func (a *apiRoutes) beginBackupRestore() bool {
	a.backupRestoreMu.Lock()
	defer a.backupRestoreMu.Unlock()
	if a.backupRestoreRunning {
		return false
	}
	a.backupRestoreRunning = true
	return true
}

func (a *apiRoutes) endBackupRestore() {
	a.backupRestoreMu.Lock()
	a.backupRestoreRunning = false
	a.backupRestoreMu.Unlock()
}

func newAPIRoutes(app *app.App) *apiRoutes {
	return &apiRoutes{
		app:                     app,
		runtimeActions:          newRuntimeActionTracker(),
		backupCreateJobs:        make(map[string]backupCreateJob),
		githubWebhookDeliveries: make(map[string]time.Time),
		githubWebhooksInFlight:  make(map[string]struct{}),
	}
}

func (a *apiRoutes) beginGitHubWebhookDelivery(id string) (started, completed bool) {
	const (
		retention  = 24 * time.Hour
		maxEntries = 10_000
	)

	now := time.Now().UTC()
	a.githubWebhookMu.Lock()
	defer a.githubWebhookMu.Unlock()

	for deliveryID, seenAt := range a.githubWebhookDeliveries {
		if now.Sub(seenAt) >= retention {
			delete(a.githubWebhookDeliveries, deliveryID)
		}
	}
	if _, exists := a.githubWebhookDeliveries[id]; exists {
		return false, true
	}
	if _, exists := a.githubWebhooksInFlight[id]; exists {
		return false, false
	}
	a.githubWebhooksInFlight[id] = struct{}{}
	return true, false
}

func (a *apiRoutes) finishGitHubWebhookDelivery(id string, accepted bool) {
	const maxEntries = 10_000
	now := time.Now().UTC()
	a.githubWebhookMu.Lock()
	defer a.githubWebhookMu.Unlock()
	delete(a.githubWebhooksInFlight, id)
	if !accepted {
		return
	}
	for len(a.githubWebhookDeliveries) >= maxEntries {
		var oldestID string
		var oldest time.Time
		for deliveryID, seenAt := range a.githubWebhookDeliveries {
			if oldestID == "" || seenAt.Before(oldest) {
				oldestID, oldest = deliveryID, seenAt
			}
		}
		delete(a.githubWebhookDeliveries, oldestID)
	}
	a.githubWebhookDeliveries[id] = now
}

func (a *apiRoutes) register(r chi.Router) {
	if r == nil {
		return
	}

	a.registerBackupRoutes(r)
	a.registerAlertRoutes(r)
	a.registerApplicationRoutes(r)
	a.registerDomainRoutes(r)
	a.registerFileRoutes(r)
	a.registerFirewallRoutes(r)
	a.registerTaskManagerRoutes(r)
}

func (a *apiRoutes) syncDomainsWithCaddy(ctx context.Context) error {
	return syncDomainsWithCurrentSettings(ctx, a.app)
}

func (a *apiRoutes) refreshDomainRoutingAfterContentChange(ctx context.Context, hostnames ...string) error {
	if err := a.syncDomainsWithCaddy(ctx); err != nil {
		return err
	}
	if a == nil || a.app == nil || a.app.Domains == nil {
		return nil
	}

	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if err := a.app.Domains.InvalidatePreview(hostname); err != nil {
			a.app.Logger.Warn("invalidate domain preview after content change failed", zap.String("hostname", hostname), zap.Error(err))
		}
	}

	return nil
}

func (a *apiRoutes) recordEvent(ctx context.Context, input eventlog.CreateInput) {
	if a == nil || a.app == nil || a.app.Events == nil {
		return
	}
	if _, err := a.app.Events.Record(backgroundRequestContext(ctx), input); err != nil {
		a.app.Logger.Error("record event failed", zap.Error(err))
	}
}

func (a *apiRoutes) mutationEvent(ctx context.Context, category, action, resourceType, resourceID, resourceLabel, status, message string) {
	a.recordEvent(ctx, eventlog.CreateInput{
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

func (a *apiRoutes) triggerAlert(ctx context.Context, input alerts.TriggerInput) {
	if a == nil || a.app == nil || a.app.Alerts == nil {
		return
	}
	if err := a.app.Alerts.Trigger(backgroundRequestContext(ctx), input); err != nil {
		a.app.Logger.Error("trigger alert failed", zap.String("alert_key", input.Key), zap.Error(err))
	}
}

func (a *apiRoutes) resolveAlert(ctx context.Context, key string) {
	if a == nil || a.app == nil || a.app.Alerts == nil {
		return
	}
	if err := a.app.Alerts.Resolve(backgroundRequestContext(ctx), key); err != nil {
		a.app.Logger.Error("resolve alert failed", zap.String("alert_key", key), zap.Error(err))
	}
}

func (a *apiRoutes) currentCaddyStatus() caddy.Status {
	if a == nil || a.app == nil || a.app.Caddy == nil {
		return caddy.Status{
			State:   "missing",
			Message: "Caddy runtime is not configured.",
		}
	}

	status := a.app.Caddy.Status()
	if a.app.Domains != nil {
		status.ConfiguredDomains = len(a.app.Domains.List())
	}

	return a.trackCaddyStatus(status)
}

func (a *apiRoutes) startBackgroundRuntimeAction(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
	resource string,
	action string,
	resourceType string,
	resourceID string,
	resourceLabel string,
	successMessage string,
	status func(context.Context) map[string]any,
	run func(context.Context) error,
	after func(context.Context) error,
) bool {
	actionCtx := backgroundRequestContext(r.Context())
	if err := a.runtimeActions.Begin(resource, action); err != nil {
		writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
		return false
	}

	go func() {
		defer a.runtimeActions.End(resource, action)

		if err := run(actionCtx); err != nil {
			a.runtimeActions.Fail(resource, fmt.Sprintf("%s %s failed. Check Activity for details.", resourceLabel, action))
			a.app.Logger.Error(action+" "+resource+" failed", zap.Error(err))
			a.mutationEvent(actionCtx, "runtime", action, resourceType, resourceID, resourceLabel, "failed", err.Error())
			return
		}

		if after != nil {
			if err := after(actionCtx); err != nil {
				a.runtimeActions.Fail(resource, fmt.Sprintf("%s %s follow-up failed. Check Activity for details.", resourceLabel, action))
				a.app.Logger.Error(action+" "+resource+" follow-up failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, resourceType, resourceID, resourceLabel, "failed", err.Error())
				return
			}
		}

		a.mutationEvent(actionCtx, "runtime", action, resourceType, resourceID, resourceLabel, "succeeded", successMessage)
	}()

	writeJSON(w, stdhttp.StatusOK, status(actionCtx))
	return true
}

func (a *apiRoutes) trackCaddyStatus(status caddy.Status) caddy.Status {
	switch a.runtimeActions.Current("caddy") {
	case "restart":
		status.State = "restarting"
		status.Message = "Caddy restart and domain sync are running in the background."
		status.RestartAvailable = false
	}

	return status
}

func (a *apiRoutes) trackPHPStatus(status phpenv.Status) phpenv.Status {
	if failure := a.runtimeActions.Failure("php"); failure != "" {
		status.State, status.Message, status.Ready = "failed", failure, false
		return status
	}
	switch a.runtimeActions.Current("php") {
	case "install":
		status.State = "installing"
		status.Message = "PHP installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "PHP removal is running in the background."
	case "start":
		status.State = "starting"
		status.Message = "PHP-FPM is starting in the background."
	case "stop":
		status.State = "stopping"
		status.Message = "PHP-FPM is stopping in the background."
	case "restart":
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

func (a *apiRoutes) trackMariaDBStatus(status mariadb.Status) mariadb.Status {
	if failure := a.runtimeActions.Failure("mariadb"); failure != "" {
		status.State, status.Message, status.Ready = "failed", failure, false
		return status
	}
	switch a.runtimeActions.Current("mariadb") {
	case "install":
		status.State = "installing"
		status.Message = "MariaDB installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "MariaDB removal is running in the background."
	case "start":
		status.State = "starting"
		status.Message = "MariaDB is starting in the background."
	case "stop":
		status.State = "stopping"
		status.Message = "MariaDB is stopping in the background."
	case "restart":
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

func (a *apiRoutes) trackGoStatus(status golang.Status) golang.Status {
	if failure := a.runtimeActions.Failure("golang"); failure != "" {
		status.State, status.Message = "failed", failure
		return status
	}
	switch a.runtimeActions.Current("golang") {
	case "install":
		status.State = "installing"
		status.Message = "Go installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "Go removal is running in the background."
	default:
		return status
	}

	status.InstallAvailable = false
	status.RemoveAvailable = false
	return status
}

func (a *apiRoutes) trackNodeJSStatus(status nodejs.Status) nodejs.Status {
	if failure := a.runtimeActions.Failure("nodejs"); failure != "" {
		status.State, status.Message = "failed", failure
		return status
	}
	switch a.runtimeActions.Current("nodejs") {
	case "install":
		status.State = "installing"
		status.Message = "Node.js installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "Node.js removal is running in the background."
	default:
		return status
	}

	status.InstallAvailable = false
	status.RemoveAvailable = false
	return status
}

func (a *apiRoutes) trackPM2Status(status pm2.Status) pm2.Status {
	if failure := a.runtimeActions.Failure("pm2"); failure != "" {
		status.State, status.Message = "failed", failure
		return status
	}
	switch a.runtimeActions.Current("pm2") {
	case "install":
		status.State = "installing"
		status.Message = "PM2 installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "PM2 removal is running in the background."
	default:
		return status
	}

	status.InstallAvailable = false
	status.RemoveAvailable = false
	return status
}

func (a *apiRoutes) trackPHPMyAdminStatus(status phpmyadmin.Status) phpmyadmin.Status {
	if failure := a.runtimeActions.Failure("phpmyadmin"); failure != "" {
		status.State, status.Message = "failed", failure
		return status
	}
	switch a.runtimeActions.Current("phpmyadmin") {
	case "install":
		status.State = "installing"
		status.Message = "phpMyAdmin installation is running in the background."
	case "remove":
		status.State = "removing"
		status.Message = "phpMyAdmin removal is running in the background."
	default:
		return status
	}

	status.InstallAvailable = false
	status.RemoveAvailable = false
	return status
}

func (a *apiRoutes) trackPackageRuntimeStatus(key, label string, status packageruntime.Status) packageruntime.Status {
	if failure := a.runtimeActions.Failure(key); failure != "" {
		status.State, status.Message = "failed", failure
		return status
	}
	switch a.runtimeActions.Current(key) {
	case "install":
		status.State = "installing"
		status.Message = fmt.Sprintf("%s installation is running in the background.", label)
	case "update":
		status.State = "updating"
		status.Message = fmt.Sprintf("%s update is running.", label)
	case "remove":
		status.State = "removing"
		status.Message = fmt.Sprintf("%s removal is running in the background.", label)
	case "start":
		status.State = "starting"
		status.Message = fmt.Sprintf("%s is starting in the background.", label)
	case "stop":
		status.State = "stopping"
		status.Message = fmt.Sprintf("%s is stopping in the background.", label)
	case "restart":
		status.State = "restarting"
		status.Message = fmt.Sprintf("%s is restarting in the background.", label)
	default:
		return status
	}

	status.InstallAvailable = false
	status.UpdateAvailable = false
	status.RemoveAvailable = false
	status.StartAvailable = false
	status.StopAvailable = false
	status.RestartAvailable = false
	return status
}

func (a *apiRoutes) phpActionVersion(r *stdhttp.Request) string {
	return trimmedQuery(r, "version")
}

func (a *apiRoutes) phpActionExtension(r *stdhttp.Request) string {
	return trimmedQuery(r, "extension")
}

func formatPHPBool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func (a *apiRoutes) formatPHPActivityFailureLog(ctx context.Context, extension, requestedVersion string, err error) string {
	var builder strings.Builder

	normalizedVersion := phpenv.NormalizeVersion(requestedVersion)
	runtimeStatus := phpenv.RuntimeStatus{}
	if a != nil && a.app != nil && a.app.PHP != nil {
		if normalizedVersion != "" {
			runtimeStatus = a.app.PHP.StatusForVersion(ctx, normalizedVersion)
		} else {
			phpStatus := a.app.PHP.Status(ctx)
			if phpStatus.DefaultVersion != "" {
				runtimeStatus = a.app.PHP.StatusForVersion(ctx, phpStatus.DefaultVersion)
			}
		}
	}

	builder.WriteString("PHP extension installation failed.\n")
	builder.WriteString(fmt.Sprintf("Extension: %s\n", strings.TrimSpace(extension)))
	if runtimeStatus.Version != "" {
		builder.WriteString(fmt.Sprintf("PHP version: %s\n", runtimeStatus.Version))
	} else if normalizedVersion != "" {
		builder.WriteString(fmt.Sprintf("PHP version: %s\n", normalizedVersion))
	}
	builder.WriteString("\nError:\n")
	if err != nil {
		builder.WriteString(strings.TrimSpace(err.Error()))
	} else {
		builder.WriteString("No error details were returned.")
	}

	if runtimeStatus.Version == "" {
		return builder.String()
	}

	builder.WriteString("\n\nRuntime snapshot:\n")
	builder.WriteString(fmt.Sprintf("State: %s\n", runtimeStatus.State))
	builder.WriteString(fmt.Sprintf("Ready: %s\n", formatPHPBool(runtimeStatus.Ready)))
	builder.WriteString(fmt.Sprintf("PHP installed: %s\n", formatPHPBool(runtimeStatus.PHPInstalled)))
	builder.WriteString(fmt.Sprintf("PHP-FPM installed: %s\n", formatPHPBool(runtimeStatus.FPMInstalled)))
	builder.WriteString(fmt.Sprintf("Service running: %s\n", formatPHPBool(runtimeStatus.ServiceRunning)))
	if value := strings.TrimSpace(runtimeStatus.Message); value != "" {
		builder.WriteString(fmt.Sprintf("Status message: %s\n", value))
	}
	if value := strings.TrimSpace(runtimeStatus.PHPPath); value != "" {
		builder.WriteString(fmt.Sprintf("PHP binary: %s\n", value))
	}
	if value := strings.TrimSpace(runtimeStatus.FPMPath); value != "" {
		builder.WriteString(fmt.Sprintf("PHP-FPM binary: %s\n", value))
	}
	if value := strings.TrimSpace(runtimeStatus.LoadedConfigFile); value != "" {
		builder.WriteString(fmt.Sprintf("Loaded config: %s\n", value))
	}
	if value := strings.TrimSpace(runtimeStatus.ScanDir); value != "" {
		builder.WriteString(fmt.Sprintf("Scanned ini dir: %s\n", value))
	}
	if value := strings.TrimSpace(runtimeStatus.ManagedConfigFile); value != "" {
		builder.WriteString(fmt.Sprintf("Managed config: %s\n", value))
	}
	if len(runtimeStatus.Issues) > 0 {
		builder.WriteString("\nReported issues:\n")
		for _, issue := range runtimeStatus.Issues {
			issue = strings.TrimSpace(issue)
			if issue == "" {
				continue
			}
			builder.WriteString("- ")
			builder.WriteString(issue)
			builder.WriteString("\n")
		}
	}

	return strings.TrimSpace(builder.String())
}
