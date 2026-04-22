package httpx

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerTaskManagerRoutes(r chi.Router) {
	if r == nil {
		return
	}

	snapshotHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
				"error": "task manager is unavailable",
			})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"snapshot": a.app.TaskManager.Snapshot(r.Context()),
		})
	})

	r.Method(stdhttp.MethodGet, "/task-manager", snapshotHandler)
	r.Method(stdhttp.MethodHead, "/task-manager", snapshotHandler)

	r.Method(stdhttp.MethodPost, "/task-manager/processes/{pid}/terminate", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
				"error": "task manager is unavailable",
			})
			return
		}

		pid, err := strconv.Atoi(chi.URLParam(r, "pid"))
		if err != nil || pid <= 0 {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
				"error": "process id must be a valid integer",
			})
			return
		}

		if err := a.app.TaskManager.TerminateProcess(r.Context(), pid); err != nil {
			a.app.Logger.Error("terminate process failed", zap.Int("pid", pid), zap.Error(err))
			a.mutationEvent(r.Context(), "task_manager", "terminate_process", "process", strconv.Itoa(pid), fmt.Sprintf("PID %d", pid), "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
				"error": err.Error(),
			})
			return
		}

		a.mutationEvent(r.Context(), "task_manager", "terminate_process", "process", strconv.Itoa(pid), fmt.Sprintf("PID %d", pid), "succeeded", fmt.Sprintf("Terminated process %d.", pid))
		writeTaskManagerSnapshot(w, a, r.Context())
	}))

	registerServiceAction := func(action string, run func(context.Context, string) error, successMessage string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app == nil || a.app.TaskManager == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "task manager is unavailable",
				})
				return
			}

			serviceID := chi.URLParam(r, "serviceID")
			if serviceID == "" {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "service id is required",
				})
				return
			}

			if err := run(r.Context(), serviceID); err != nil {
				a.app.Logger.Error(action+" service failed", zap.String("service_id", serviceID), zap.Error(err))
				a.mutationEvent(r.Context(), "task_manager", action+"_service", "service", serviceID, serviceID, "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			a.mutationEvent(r.Context(), "task_manager", action+"_service", "service", serviceID, serviceID, "succeeded", successMessage)
			writeTaskManagerSnapshot(w, a, r.Context())
		}
	}

	registerStartupAction := func(action string, run func(context.Context, string) error, successMessage string) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app == nil || a.app.TaskManager == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
					"error": "task manager is unavailable",
				})
				return
			}

			startupID := chi.URLParam(r, "startupID")
			if startupID == "" {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{
					"error": "startup item id is required",
				})
				return
			}

			if err := run(r.Context(), startupID); err != nil {
				a.app.Logger.Error(action+" startup item failed", zap.String("startup_id", startupID), zap.Error(err))
				a.mutationEvent(r.Context(), "task_manager", action+"_startup_item", "startup_item", startupID, startupID, "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{
					"error": err.Error(),
				})
				return
			}

			a.mutationEvent(r.Context(), "task_manager", action+"_startup_item", "startup_item", startupID, startupID, "succeeded", successMessage)
			writeTaskManagerSnapshot(w, a, r.Context())
		}
	}

	r.Method(stdhttp.MethodPost, "/task-manager/services/{serviceID}/start", registerServiceAction("start", func(ctx context.Context, id string) error {
		return a.app.TaskManager.StartService(ctx, id)
	}, "Started the service."))
	r.Method(stdhttp.MethodPost, "/task-manager/services/{serviceID}/stop", registerServiceAction("stop", func(ctx context.Context, id string) error {
		return a.app.TaskManager.StopService(ctx, id)
	}, "Stopped the service."))
	r.Method(stdhttp.MethodPost, "/task-manager/services/{serviceID}/restart", registerServiceAction("restart", func(ctx context.Context, id string) error {
		return a.app.TaskManager.RestartService(ctx, id)
	}, "Restarted the service."))
	r.Method(stdhttp.MethodPost, "/task-manager/startup-items/{startupID}/enable", registerStartupAction("enable", func(ctx context.Context, id string) error {
		return a.app.TaskManager.EnableStartupItem(ctx, id)
	}, "Enabled the startup item."))
	r.Method(stdhttp.MethodPost, "/task-manager/startup-items/{startupID}/disable", registerStartupAction("disable", func(ctx context.Context, id string) error {
		return a.app.TaskManager.DisableStartupItem(ctx, id)
	}, "Disabled the startup item."))
}

func writeTaskManagerSnapshot(w stdhttp.ResponseWriter, a *apiRoutes, ctx context.Context) {
	if a == nil || a.app == nil || a.app.TaskManager == nil {
		writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
			"error": "task manager is unavailable",
		})
		return
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"snapshot": a.app.TaskManager.Snapshot(ctx),
	})
}
