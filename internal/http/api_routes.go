package httpx

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strings"

	"flowpanel/internal/app"
	eventlog "flowpanel/internal/events"
	"flowpanel/internal/golang"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/nodejs"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type apiRoutes struct {
	app            *app.App
	runtimeActions *runtimeActionTracker
}

func newAPIRoutes(app *app.App) *apiRoutes {
	return &apiRoutes{
		app:            app,
		runtimeActions: newRuntimeActionTracker(),
	}
}

func (a *apiRoutes) register(r chi.Router) {
	if r == nil {
		return
	}

	a.registerBackupRoutes(r)
	a.registerGoRoutes(r)
	a.registerNodeJSRoutes(r)
	a.registerMariaDBRoutes(r)
	a.registerPHPRoutes(r)
	a.registerDomainRoutes(r)
	a.registerFileRoutes(r)
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

func (a *apiRoutes) trackPHPStatus(status phpenv.Status) phpenv.Status {
	switch a.runtimeActions.Current("php") {
	case "install":
		if status.PHPInstalled && status.FPMInstalled && status.ServiceRunning {
			a.runtimeActions.End("php", "install")
			return status
		}
		status.State = "installing"
		status.Message = "PHP installation is running in the background."
	case "remove":
		if !status.PHPInstalled && !status.FPMInstalled {
			a.runtimeActions.End("php", "remove")
			return status
		}
		status.State = "removing"
		status.Message = "PHP removal is running in the background."
	case "start":
		if status.ServiceRunning {
			a.runtimeActions.End("php", "start")
			return status
		}
		status.State = "starting"
		status.Message = "PHP-FPM is starting in the background."
	case "stop":
		if status.FPMInstalled && !status.ServiceRunning {
			a.runtimeActions.End("php", "stop")
			return status
		}
		status.State = "stopping"
		status.Message = "PHP-FPM is stopping in the background."
	case "restart":
		if status.ServiceRunning {
			a.runtimeActions.End("php", "restart")
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

func (a *apiRoutes) trackMariaDBStatus(status mariadb.Status) mariadb.Status {
	switch a.runtimeActions.Current("mariadb") {
	case "install":
		if status.ServerInstalled && status.ServiceRunning {
			a.runtimeActions.End("mariadb", "install")
			return status
		}
		status.State = "installing"
		status.Message = "MariaDB installation is running in the background."
	case "remove":
		if !status.ServerInstalled && !status.ClientInstalled {
			a.runtimeActions.End("mariadb", "remove")
			return status
		}
		status.State = "removing"
		status.Message = "MariaDB removal is running in the background."
	case "start":
		if status.ServiceRunning {
			a.runtimeActions.End("mariadb", "start")
			return status
		}
		status.State = "starting"
		status.Message = "MariaDB is starting in the background."
	case "stop":
		if status.ServerInstalled && !status.ServiceRunning {
			a.runtimeActions.End("mariadb", "stop")
			return status
		}
		status.State = "stopping"
		status.Message = "MariaDB is stopping in the background."
	case "restart":
		if status.ServiceRunning {
			a.runtimeActions.End("mariadb", "restart")
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

func (a *apiRoutes) trackGoStatus(status golang.Status) golang.Status {
	switch a.runtimeActions.Current("golang") {
	case "install":
		if status.Installed {
			a.runtimeActions.End("golang", "install")
			return status
		}
		status.State = "installing"
		status.Message = "Go installation is running in the background."
	case "remove":
		if !status.Installed {
			a.runtimeActions.End("golang", "remove")
			return status
		}
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
	switch a.runtimeActions.Current("nodejs") {
	case "install":
		if status.Installed {
			a.runtimeActions.End("nodejs", "install")
			return status
		}
		status.State = "installing"
		status.Message = "Node.js installation is running in the background."
	case "remove":
		if !status.Installed {
			a.runtimeActions.End("nodejs", "remove")
			return status
		}
		status.State = "removing"
		status.Message = "Node.js removal is running in the background."
	default:
		return status
	}

	status.InstallAvailable = false
	status.RemoveAvailable = false
	return status
}

func (a *apiRoutes) trackPHPMyAdminStatus(status phpmyadmin.Status) phpmyadmin.Status {
	switch a.runtimeActions.Current("phpmyadmin") {
	case "install":
		if status.Installed {
			a.runtimeActions.End("phpmyadmin", "install")
			return status
		}
		status.State = "installing"
		status.Message = "phpMyAdmin installation is running in the background."
	case "remove":
		if !status.Installed {
			a.runtimeActions.End("phpmyadmin", "remove")
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
