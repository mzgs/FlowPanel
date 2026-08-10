package httpx

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"flowpanel/internal/app"
	"flowpanel/internal/backup"
)

type restoreRequirementStatus struct {
	Kind             string `json:"kind"`
	Name             string `json:"name"`
	Version          string `json:"version,omitempty"`
	State            string `json:"state"`
	Message          string `json:"message"`
	InstallAvailable bool   `json:"install_available"`
}

type restorePreflightResponse struct {
	Requirements    []restoreRequirementStatus `json:"requirements"`
	Warnings        []string                   `json:"warnings,omitempty"`
	ChangesRequired bool                       `json:"changes_required"`
	CanPrepare      bool                       `json:"can_prepare"`
}

func inspectRestorePreflight(ctx context.Context, application *app.App, source backup.RestorePreflight) restorePreflightResponse {
	result := restorePreflightResponse{
		Requirements: make([]restoreRequirementStatus, 0, len(source.Requirements)),
		Warnings:     source.Warnings,
		CanPrepare:   true,
	}
	for _, requirement := range source.Requirements {
		status := inspectRestoreRequirement(ctx, application, requirement)
		duplicate := false
		for _, current := range result.Requirements {
			if current.Kind == status.Kind && current.Version == status.Version {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result.Requirements = append(result.Requirements, status)
		}
	}
	for i, status := range result.Requirements {
		if status.Kind == backup.RequirementPM2 && status.State == "unavailable" && requirementWillBePrepared(result.Requirements, backup.RequirementNodeJS) {
			result.Requirements[i].State = "install"
			result.Requirements[i].Message = "Will be installed after Node.js"
			result.Requirements[i].InstallAvailable = true
		}
	}
	for _, status := range result.Requirements {
		if status.State != "ready" {
			result.ChangesRequired = true
		}
		if status.State == "unavailable" {
			result.CanPrepare = false
		}
	}
	return result
}

func inspectRestoreRequirement(ctx context.Context, application *app.App, requirement backup.RestoreRequirement) restoreRequirementStatus {
	status := restoreRequirementStatus{Kind: requirement.Kind, Version: requirement.Version}
	switch requirement.Kind {
	case backup.RequirementDocker:
		status.Name = "Docker"
		current := application.Docker.Status(ctx)
		return runtimeRequirementStatus(status, current.Installed, current.ServiceRunning, current.InstallAvailable, current.StartAvailable)
	case backup.RequirementGolang:
		status.Name = "Go"
		current := application.Golang.Status(ctx)
		return runtimeRequirementStatus(status, current.Installed, true, current.InstallAvailable, false)
	case backup.RequirementMariaDB:
		status.Name = "MariaDB"
		current := application.MariaDB.Status(ctx)
		return runtimeRequirementStatus(status, current.ServerInstalled && current.ClientInstalled, current.Ready, current.InstallAvailable, current.StartAvailable)
	case backup.RequirementNodeJS:
		status.Name = "Node.js"
		current := application.NodeJS.Status(ctx)
		return runtimeRequirementStatus(status, current.Installed, true, current.InstallAvailable, false)
	case backup.RequirementPHP:
		status.Name = "PHP"
		if requirement.Version != "" {
			supported := false
			for _, version := range application.PHP.Status(ctx).AvailableVersions {
				if version == requirement.Version {
					supported = true
					break
				}
			}
			if !supported {
				status.State, status.Message = "unavailable", "This PHP version is not supported by this FlowPanel release."
				return status
			}
		}
		current := application.PHP.StatusForVersion(ctx, requirement.Version)
		status.Version = current.Version
		return runtimeRequirementStatus(status, current.PHPInstalled && current.FPMInstalled, current.Ready, current.InstallAvailable, current.StartAvailable)
	case backup.RequirementPM2:
		status.Name = "PM2"
		current := application.PM2.Status(ctx)
		return runtimeRequirementStatus(status, current.Installed, true, current.InstallAvailable, false)
	case backup.RequirementPython:
		status.Name = "Python"
		if _, err := exec.LookPath("python3"); err == nil {
			status.State, status.Message = "ready", "Installed"
		} else {
			status.State, status.Message = "unavailable", "Install Python manually before restoring this backup."
		}
		return status
	default:
		status.Name = requirement.Kind
		status.State, status.Message = "unavailable", "This runtime is not supported by automatic restore preparation."
		return status
	}
}

func requirementWillBePrepared(requirements []restoreRequirementStatus, kind string) bool {
	for _, requirement := range requirements {
		if requirement.Kind == kind && (requirement.State == "ready" || requirement.InstallAvailable) {
			return true
		}
	}
	return false
}

func runtimeRequirementStatus(status restoreRequirementStatus, installed, running, installAvailable, startAvailable bool) restoreRequirementStatus {
	switch {
	case !installed && installAvailable:
		status.State, status.Message, status.InstallAvailable = "install", "Will be installed", true
	case !installed:
		status.State, status.Message = "unavailable", "Automatic installation is unavailable on this server."
	case !running && startAvailable:
		status.State, status.Message, status.InstallAvailable = "start", "Will be started", true
	case !running:
		status.State, status.Message = "unavailable", "Installed, but automatic startup is unavailable."
	default:
		status.State, status.Message = "ready", "Ready"
	}
	return status
}

func prepareRestoreRequirements(ctx context.Context, application *app.App, source backup.RestorePreflight, report func(backup.RestoreProgress)) error {
	for _, kind := range []string{backup.RequirementMariaDB, backup.RequirementDocker, backup.RequirementPHP, backup.RequirementGolang, backup.RequirementNodeJS, backup.RequirementPM2, backup.RequirementPython} {
		for _, requirement := range source.Requirements {
			if requirement.Kind != kind {
				continue
			}
			status := inspectRestoreRequirement(ctx, application, requirement)
			if status.State == "ready" {
				continue
			}
			if status.State == "unavailable" {
				return fmt.Errorf("%s: %s", requirementLabel(status), strings.ToLower(status.Message))
			}
			report(backup.RestoreProgress{Label: fmt.Sprintf("%s %s…", titleCaseAction(status.State), requirementLabel(status)), Percent: 2})
			if err := applyRestoreRequirement(ctx, application, requirement, status.State); err != nil {
				return fmt.Errorf("%s %s: %w", status.State, requirementLabel(status), err)
			}
			if next := inspectRestoreRequirement(ctx, application, requirement); next.State != "ready" {
				return fmt.Errorf("prepare %s: %s", requirementLabel(next), strings.ToLower(next.Message))
			}
		}
	}
	return nil
}

func applyRestoreRequirement(ctx context.Context, application *app.App, requirement backup.RestoreRequirement, action string) error {
	switch requirement.Kind {
	case backup.RequirementDocker:
		if action == "install" {
			if err := application.Docker.Install(ctx); err != nil {
				return err
			}
		}
		return application.Docker.Start(ctx)
	case backup.RequirementMariaDB:
		if action == "install" {
			if err := application.MariaDB.Install(ctx); err != nil {
				return err
			}
		}
		return application.MariaDB.Start(ctx)
	case backup.RequirementNodeJS:
		return application.NodeJS.Install(ctx)
	case backup.RequirementGolang:
		return application.Golang.Install(ctx)
	case backup.RequirementPHP:
		if action == "install" {
			if err := application.PHP.InstallVersion(ctx, requirement.Version); err != nil {
				return err
			}
		}
		return application.PHP.StartVersion(ctx, requirement.Version)
	case backup.RequirementPM2:
		return application.PM2.Install(ctx)
	default:
		return nil
	}
}

func requirementLabel(status restoreRequirementStatus) string {
	if status.Version != "" {
		return status.Name + " " + status.Version
	}
	return status.Name
}

func titleCaseAction(action string) string {
	if action == "start" {
		return "Starting"
	}
	return "Installing"
}
