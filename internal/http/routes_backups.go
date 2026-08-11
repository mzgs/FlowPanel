package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/alerts"
	"flowpanel/internal/backup"
	flowcron "flowpanel/internal/cron"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type backupCreateJob struct {
	ID        string                `json:"id"`
	Done      bool                  `json:"done"`
	Backup    *backup.Record        `json:"backup,omitempty"`
	Error     string                `json:"error,omitempty"`
	Progress  backup.CreateProgress `json:"progress"`
	createdAt time.Time
}

func (a *apiRoutes) beginBackupCreate() (backupCreateJob, bool) {
	a.backupCreateMu.Lock()
	defer a.backupCreateMu.Unlock()
	if a.backupCreateRunning {
		return backupCreateJob{}, false
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for id, job := range a.backupCreateJobs {
		if job.Done && job.createdAt.Before(cutoff) {
			delete(a.backupCreateJobs, id)
		}
	}
	job := backupCreateJob{
		ID:        strconv.FormatInt(time.Now().UnixNano(), 36),
		Progress:  backup.CreateProgress{Label: "Starting backup…", Percent: 0},
		createdAt: time.Now(),
	}
	a.backupCreateJobs[job.ID] = job
	a.backupCreateRunning = true
	return job, true
}

func (a *apiRoutes) updateBackupCreate(id string, progress backup.CreateProgress) {
	a.backupCreateMu.Lock()
	job := a.backupCreateJobs[id]
	job.Progress = progress
	a.backupCreateJobs[id] = job
	a.backupCreateMu.Unlock()
}

func (a *apiRoutes) finishBackupCreate(id string, record backup.Record, err error) {
	a.backupCreateMu.Lock()
	job := a.backupCreateJobs[id]
	job.Done = true
	if err != nil {
		job.Error = err.Error()
	} else {
		job.Backup = &record
		job.Progress = backup.CreateProgress{Label: "Backup complete", Percent: 100}
	}
	a.backupCreateJobs[id] = job
	a.backupCreateRunning = false
	a.backupCreateMu.Unlock()
}

func (a *apiRoutes) backupCreateJob(id string) (backupCreateJob, bool) {
	a.backupCreateMu.Lock()
	defer a.backupCreateMu.Unlock()
	job, ok := a.backupCreateJobs[id]
	return job, ok
}

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

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"backups":   records,
			"directory": a.app.Backups.Directory(),
		})
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
			IncludePanelData  *bool    `json:"include_panel_data"`
			IncludeDockerData *bool    `json:"include_docker_data"`
			IncludeSites      *bool    `json:"include_sites"`
			IncludeDatabases  *bool    `json:"include_databases"`
			SiteHostnames     []string `json:"site_hostnames"`
			DatabaseNames     []string `json:"database_names"`
			Location          string   `json:"location"`
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
		if payload.IncludeDockerData != nil {
			input.IncludeDockerData = *payload.IncludeDockerData
		} else {
			input.IncludeDockerData = input.IncludePanelData
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

		job, started := a.beginBackupCreate()
		if !started {
			writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "a backup is already being created"})
			return
		}
		a.mutationEvent(r.Context(), "backups", "create", "backup", job.ID, "FlowPanel backup", "started", "Started creating a backup archive.")
		go func() {
			jobCtx := context.Background()
			var record backup.Record
			var err error
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("backup creation panicked: %v", recovered)
					a.app.Logger.Error("create backup panicked", zap.Any("panic", recovered), zap.ByteString("stack", debug.Stack()))
					a.mutationEvent(jobCtx, "backups", "create", "backup", "backup", "FlowPanel backup", "failed", fmt.Sprintf("Failed to create a backup archive: %v", err))
					a.triggerAlert(jobCtx, alerts.TriggerInput{Key: "backup:manual", Severity: "critical", Title: "Backup failed", Message: err.Error()})
					a.finishBackupCreate(job.ID, backup.Record{}, err)
				}
			}()
			if manager, ok := a.app.Backups.(backup.CreateProgressManager); ok {
				record, err = manager.CreateWithProgress(jobCtx, input, func(progress backup.CreateProgress) {
					a.updateBackupCreate(job.ID, progress)
				})
			} else {
				record, err = a.app.Backups.Create(jobCtx, input)
			}
			if err != nil {
				a.app.Logger.Error("create backup failed", zap.Error(err))
				a.mutationEvent(jobCtx, "backups", "create", "backup", "backup", "FlowPanel backup", "failed", fmt.Sprintf("Failed to create a backup archive: %v", err))
				a.triggerAlert(jobCtx, alerts.TriggerInput{Key: "backup:manual", Severity: "critical", Title: "Backup failed", Message: err.Error()})
			} else {
				a.mutationEvent(jobCtx, "backups", "create", "backup", record.Name, record.Name, "succeeded", fmt.Sprintf("Created backup %q.", record.Name))
				a.resolveAlert(jobCtx, "backup:manual")
			}
			a.finishBackupCreate(job.ID, record, err)
		}()
		writeJSON(w, stdhttp.StatusAccepted, map[string]any{"job": job})
	})
	r.Method(stdhttp.MethodPost, "/backups", backupsCreateHandler)

	backupsCreateJobHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		job, ok := a.backupCreateJob(strings.TrimSpace(chi.URLParam(r, "jobID")))
		if !ok {
			writeJSON(w, stdhttp.StatusNotFound, map[string]any{"error": "backup creation job not found"})
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"job": job})
	})
	r.Method(stdhttp.MethodGet, "/backups/create-jobs/{jobID}", backupsCreateJobHandler)

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
				"id":                  job.ID,
				"name":                job.Name,
				"schedule":            job.Schedule,
				"created_at":          job.CreatedAt,
				"include_panel_data":  scope.IncludePanelData,
				"include_docker_data": scope.IncludeDockerData,
				"include_sites":       scope.IncludeSites,
				"include_databases":   scope.IncludeDatabases,
				"location":            scope.Location,
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
			Name              string `json:"name"`
			Schedule          string `json:"schedule"`
			IncludePanelData  *bool  `json:"include_panel_data"`
			IncludeDockerData *bool  `json:"include_docker_data"`
			IncludeSites      *bool  `json:"include_sites"`
			IncludeDatabases  *bool  `json:"include_databases"`
			Location          string `json:"location"`
		}
		if err := decodeJSON(r, &payload); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		if payload.IncludePanelData != nil {
			input.IncludePanelData = *payload.IncludePanelData
		}
		if payload.IncludeDockerData != nil {
			input.IncludeDockerData = *payload.IncludeDockerData
		} else {
			input.IncludeDockerData = input.IncludePanelData
		}
		if payload.IncludeSites != nil {
			input.IncludeSites = *payload.IncludeSites
		}
		if payload.IncludeDatabases != nil {
			input.IncludeDatabases = *payload.IncludeDatabases
		}
		input.Location = payload.Location

		if !input.IncludePanelData && !input.IncludeDockerData && !input.IncludeSites && !input.IncludeDatabases {
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
				"id":                  record.ID,
				"name":                record.Name,
				"schedule":            record.Schedule,
				"created_at":          record.CreatedAt,
				"include_panel_data":  input.IncludePanelData,
				"include_docker_data": input.IncludeDockerData,
				"include_sites":       input.IncludeSites,
				"include_databases":   input.IncludeDatabases,
				"location":            input.Location,
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
		r.Body = stdhttp.MaxBytesReader(w, r.Body, maxFileUploadBytes)
		parseErr := r.ParseMultipartForm(multipartFormMemoryMax)
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if parseErr != nil {
			var maxBytesError *stdhttp.MaxBytesError
			if errors.As(parseErr, &maxBytesError) {
				writeJSON(w, stdhttp.StatusRequestEntityTooLarge, map[string]any{"error": "upload exceeds the 8 GB limit"})
				return
			}
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

	backupsRestorePreflightHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		manager, ok := a.app.Backups.(backup.PreflightManager)
		if !ok {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup restore preflight is not available"})
			return
		}
		name, err := decodeBackupNameParam(r)
		if err != nil {
			writeBackupError(w, err)
			return
		}
		preflight, err := manager.Preflight(r.Context(), name, readBackupLocation(r))
		if err != nil {
			writeBackupError(w, err)
			return
		}
		writeJSON(w, stdhttp.StatusOK, map[string]any{"preflight": inspectRestorePreflight(r.Context(), a.app, preflight)})
	})
	r.Method(stdhttp.MethodGet, "/backups/{backupName}/restore-preflight", backupsRestorePreflightHandler)

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
		if !a.beginBackupRestore() {
			writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "a backup restore is already running"})
			return
		}
		defer a.endBackupRestore()

		restoreCtx := backgroundRequestContext(r.Context())
		if manager, ok := a.app.Backups.(backup.PreflightManager); ok {
			preflight, preflightErr := manager.Preflight(restoreCtx, name, location)
			if preflightErr != nil {
				a.recordBackupRestoreFailure(restoreCtx, name, preflightErr)
				writeBackupError(w, preflightErr)
				return
			}
			status := inspectRestorePreflight(restoreCtx, a.app, preflight)
			if status.ChangesRequired && r.URL.Query().Get("install_missing") != "true" {
				a.recordBackupRestoreFailure(restoreCtx, name, errors.New("restore prerequisites require approval"))
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "restore prerequisites require approval", "preflight": status})
				return
			}
			if err := prepareRestoreRequirements(restoreCtx, a.app, preflight, func(backup.RestoreProgress) {}); err != nil {
				a.recordBackupRestoreFailure(restoreCtx, name, err)
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
		}
		result, err := a.app.Backups.Restore(restoreCtx, name, location)
		if err != nil {
			if errors.Is(err, backup.ErrInvalidLocation) {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": "invalid backup location"})
			} else {
				writeBackupError(w, err)
			}
			a.recordBackupRestoreFailure(restoreCtx, name, err)
			return
		}

		result = a.finalizeBackupRestore(restoreCtx, name, result)
		writeJSON(w, stdhttp.StatusOK, map[string]any{"restore": result})
	})
	r.Method(stdhttp.MethodPost, "/backups/{backupName}/restore", backupsRestoreHandler)

	backupsRestoreProgressHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		manager, ok := a.app.Backups.(backup.ProgressManager)
		if !ok {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "backup restore progress is not available"})
			return
		}
		name, err := decodeBackupNameParam(r)
		if err != nil {
			writeBackupError(w, err)
			return
		}
		if !a.beginBackupRestore() {
			writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": "a backup restore is already running"})
			return
		}
		location := readBackupLocation(r)
		installMissing := r.URL.Query().Get("install_missing") == "true"

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		encoder := json.NewEncoder(w)
		flusher, _ := w.(stdhttp.Flusher)
		emit := func(value any) {
			_ = encoder.Encode(value)
			if flusher != nil {
				flusher.Flush()
			}
		}
		type restoreOutcome struct {
			result backup.RestoreResult
			err    error
		}
		progress := make(chan backup.RestoreProgress, 16)
		outcome := make(chan restoreOutcome, 1)
		go func() {
			defer a.endBackupRestore()
			restoreCtx := context.Background()
			fail := func(err error) {
				a.recordBackupRestoreFailure(restoreCtx, name, err)
				outcome <- restoreOutcome{err: err}
			}
			if preflightManager, ok := a.app.Backups.(backup.PreflightManager); ok {
				preflight, err := preflightManager.Preflight(restoreCtx, name, location)
				if err != nil {
					fail(err)
					return
				}
				status := inspectRestorePreflight(restoreCtx, a.app, preflight)
				if status.ChangesRequired && !installMissing {
					fail(errors.New("restore prerequisites require approval"))
					return
				}
				if err := prepareRestoreRequirements(restoreCtx, a.app, preflight, func(update backup.RestoreProgress) {
					select {
					case progress <- update:
					default:
					}
				}); err != nil {
					fail(err)
					return
				}
			}
			result, err := manager.RestoreWithProgress(restoreCtx, name, location, func(update backup.RestoreProgress) {
				select {
				case progress <- update:
				default:
				}
			})
			if err != nil {
				fail(err)
				return
			}
			select {
			case progress <- backup.RestoreProgress{Label: "Finalizing restore…", Percent: 95}:
			default:
			}
			outcome <- restoreOutcome{result: a.finalizeBackupRestore(restoreCtx, name, result)}
		}()

		for {
			select {
			case update := <-progress:
				emit(map[string]any{"progress": update})
			case completed := <-outcome:
				if completed.err != nil {
					emit(map[string]any{"error": completed.err.Error()})
					return
				}
				emit(map[string]any{
					"progress": backup.RestoreProgress{Label: "Restore complete", Percent: 100},
					"restore":  completed.result,
				})
				return
			case <-r.Context().Done():
				return
			}
		}
	})
	r.Method(stdhttp.MethodPost, "/backups/{backupName}/restore-progress", backupsRestoreProgressHandler)
}

func (a *apiRoutes) recordBackupRestoreFailure(ctx context.Context, name string, err error) {
	a.app.Logger.Error("restore backup failed", zap.String("backup_name", name), zap.Error(err))
	a.mutationEvent(ctx, "backups", "restore", "backup", name, name, "failed", fmt.Sprintf("Failed to restore backup %q: %v", name, err))
}

func (a *apiRoutes) finalizeBackupRestore(ctx context.Context, name string, result backup.RestoreResult) backup.RestoreResult {
	if err := syncBackupRestoreState(ctx, a.app, result); err != nil {
		a.app.Logger.Error("sync restored backup state failed", zap.String("backup_name", name), zap.Error(err))
		result.Warnings = append(result.Warnings, fmt.Sprintf("Backup data was restored, but runtime state could not be fully reloaded: %v", err))
	}
	if result.RestoredPanelFiles || len(result.RestoredContainers) > 0 {
		if err := a.reconcileFirewall(ctx); err != nil {
			a.app.Logger.Error("reconcile firewall after backup restore failed", zap.String("backup_name", name), zap.Error(err))
			result.Warnings = append(result.Warnings, fmt.Sprintf("Backup data was restored, but firewall ports could not be reconciled: %v", err))
		}
	}
	detail := fmt.Sprintf("Restored backup %q.", name)
	if len(result.Warnings) > 0 {
		detail = fmt.Sprintf("Restored backup %q with %d warning(s):\n- %s", name, len(result.Warnings), strings.Join(result.Warnings, "\n- "))
	}
	a.mutationEvent(ctx, "backups", "restore", "backup", name, name, "succeeded", detail)
	return result
}
