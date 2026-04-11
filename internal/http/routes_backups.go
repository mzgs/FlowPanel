package httpx

import (
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"strconv"
	"strings"

	"flowpanel/internal/backup"
	flowcron "flowpanel/internal/cron"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerBackupRoutes(r chi.Router) {
	backupsListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
			return
		}

		records, err := a.app.Backups.List(r.Context())
		if err != nil {
			a.app.Logger.Error("list backups failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to list backups"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"backups": records})
	})
	r.Method(stdhttp.MethodGet, "/backups", backupsListHandler)
	r.Method(stdhttp.MethodHead, "/backups", backupsListHandler)

	backupsCreateHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
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
				writeInvalidRequestBody(w)
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

		record, err := a.app.Backups.Create(r.Context(), input)
		if err != nil {
			var validation backup.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("create backup failed", zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "create", "backup", "backup", "FlowPanel backup", "failed", "Failed to create a backup archive.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		a.mutationEvent(r.Context(), "backups", "create", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Created backup %q.", record.Name))
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"backup": record})
	})
	r.Method(stdhttp.MethodPost, "/backups", backupsCreateHandler)

	backupsScheduleListHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Cron == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "cron scheduler is not configured"})
			return
		}

		snapshot := a.app.Cron.Snapshot()
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
		if a.app.Cron == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "cron scheduler is not configured"})
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
			writeInvalidRequestBody(w)
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
			writeValidationFailed(w, map[string]string{
				"scope": "Select at least one backup source.",
			})
			return
		}

		executablePath, err := os.Executable()
		if err != nil {
			a.app.Logger.Error("resolve executable path failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to resolve flowpanel executable"})
			return
		}

		command, err := backup.BuildScheduledCommand(executablePath, input)
		if err != nil {
			a.app.Logger.Error("build scheduled backup command failed", zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create scheduled backup command"})
			return
		}

		record, err := a.app.Cron.Create(r.Context(), flowcron.CreateInput{
			Name:     payload.Name,
			Schedule: payload.Schedule,
			Command:  command,
		})
		if err != nil {
			var validation flowcron.ValidationErrors
			if errors.As(err, &validation) {
				writeValidationFailed(w, map[string]string(validation))
				return
			}
			a.app.Logger.Error("create scheduled backup failed", zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "schedule", "backup_schedule", "backup_schedule", strings.TrimSpace(payload.Name), "failed", "Failed to create scheduled backup.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to create scheduled backup"})
			return
		}

		a.mutationEvent(r.Context(), "backups", "schedule", "backup_schedule", record.ID, record.Name, "succeeded", fmt.Sprintf("Created scheduled backup %q.", record.Name))
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
		if a.app.Cron == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "cron scheduler is not configured"})
			return
		}

		jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
		if jobID == "" {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "backup schedule id is required"})
			return
		}

		job := flowcron.Record{}
		found := false
		for _, candidate := range a.app.Cron.List() {
			if candidate.ID != jobID {
				continue
			}
			job = candidate
			found = true
			break
		}
		if !found {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup schedule not found"})
			return
		}
		if _, ok := backup.ParseScheduledCommand(job.Command); !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup schedule not found"})
			return
		}

		record, deleted, err := a.app.Cron.Delete(r.Context(), jobID)
		if err != nil {
			a.app.Logger.Error("delete scheduled backup failed", zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "delete_schedule", "backup_schedule", jobID, job.Name, "failed", "Failed to delete scheduled backup.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete scheduled backup"})
			return
		}
		if !deleted {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup schedule not found"})
			return
		}

		a.mutationEvent(r.Context(), "backups", "delete_schedule", "backup_schedule", record.ID, record.Name, "succeeded", fmt.Sprintf("Deleted scheduled backup %q.", record.Name))
		w.WriteHeader(stdhttp.StatusNoContent)
	})
	r.Method(stdhttp.MethodDelete, "/backups/schedules/{jobID}", backupsScheduleDeleteHandler)

	backupsImportHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
			return
		}
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup upload"})
			return
		}

		headers := r.MultipartForm.File["backup"]
		if len(headers) != 1 {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "provide exactly one backup file"})
			return
		}

		header := headers[0]
		file, err := header.Open()
		if err != nil {
			a.app.Logger.Error("open uploaded backup failed", zap.String("backup_name", header.Filename), zap.Error(err))
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to read backup upload"})
			return
		}
		defer file.Close()

		record, err := a.app.Backups.Import(r.Context(), header.Filename, file)
		if err != nil {
			writeBackupError(w, err)
			if errors.Is(err, backup.ErrAlreadyExists) || errors.Is(err, backup.ErrInvalidName) || errors.Is(err, backup.ErrInvalidArchive) {
				return
			}
			a.app.Logger.Error("import backup failed", zap.String("backup_name", header.Filename), zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "import", "backup", header.Filename, header.Filename, "failed", "Failed to import a backup archive.")
			return
		}

		a.mutationEvent(r.Context(), "backups", "import", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Imported backup %q.", record.Name))
		writeJSON(w, stdhttp.StatusCreated, map[string]any{"backup": record})
	})
	r.Method(stdhttp.MethodPost, "/backups/import", backupsImportHandler)

	backupsDeleteHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
			return
		}

		name, err := decodeBackupNameParam(r)
		if err != nil {
			writeBackupError(w, err)
			return
		}
		location := readBackupLocation(r)
		if err := a.app.Backups.Delete(r.Context(), name, location); err != nil {
			switch {
			case errors.Is(err, backup.ErrInvalidName):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup name"})
			case errors.Is(err, backup.ErrInvalidLocation):
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup location"})
			case errors.Is(err, backup.ErrNotFound):
				writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup not found"})
			default:
				a.app.Logger.Error("delete backup failed", zap.String("backup_name", name), zap.Error(err))
				a.mutationEvent(r.Context(), "backups", "delete", "backup", name, name, "failed", "Failed to delete a backup archive.")
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "failed to delete backup"})
			}
			return
		}

		a.mutationEvent(r.Context(), "backups", "delete", "backup", name, name, "succeeded", fmt.Sprintf("Deleted backup %q.", name))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"ok": true})
	})
	r.Method(stdhttp.MethodDelete, "/backups/{backupName}", backupsDeleteHandler)

	backupsDownloadHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
			return
		}

		name, err := decodeBackupNameParam(r)
		if err != nil {
			writeBackupError(w, err)
			return
		}
		location := readBackupLocation(r)
		download, err := a.app.Backups.OpenDownload(r.Context(), name, location)
		if err != nil {
			if errors.Is(err, backup.ErrInvalidLocation) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup location"})
			} else {
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
			a.app.Logger.Error("stream backup download failed", zap.String("backup_name", name), zap.Error(err))
		}
	})
	r.Method(stdhttp.MethodGet, "/backups/{backupName}/download", backupsDownloadHandler)

	backupsRestoreHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.Backups == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup service is not configured"})
			return
		}

		name, err := decodeBackupNameParam(r)
		if err != nil {
			writeBackupError(w, err)
			return
		}
		location := readBackupLocation(r)
		result, err := a.app.Backups.Restore(r.Context(), name, location)
		if err != nil {
			if errors.Is(err, backup.ErrInvalidLocation) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup location"})
			} else {
				writeBackupError(w, err)
			}
			a.app.Logger.Error("restore backup failed", zap.String("backup_name", name), zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "failed", fmt.Sprintf("Failed to restore backup %q: %v", name, err))
			return
		}

		if err := syncBackupRestoreState(r.Context(), a.app, result); err != nil {
			a.app.Logger.Error("sync restored backup state failed", zap.String("backup_name", name), zap.Error(err))
			a.mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "failed", "Restored backup archive but failed to reload runtime state.")
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": "backup restored but runtime sync failed"})
			return
		}

		a.mutationEvent(r.Context(), "backups", "restore", "backup", name, name, "succeeded", fmt.Sprintf("Restored backup %q.", name))
		writeJSON(w, stdhttp.StatusOK, map[string]any{"restore": result})
	})
	r.Method(stdhttp.MethodPost, "/backups/{backupName}/restore", backupsRestoreHandler)
}
