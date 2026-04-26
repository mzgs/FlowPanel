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

	linuxToolsHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
				"error": "task manager is unavailable",
			})
			return
		}

		writeLinuxToolsSnapshot(w, a, r.Context())
	})

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
	r.Method(stdhttp.MethodGet, "/task-manager/linux-tools", linuxToolsHandler)
	r.Method(stdhttp.MethodHead, "/task-manager/linux-tools", linuxToolsHandler)

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

	r.Method(stdhttp.MethodPost, "/task-manager/linux-tools/timezone", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "task manager is unavailable"})
			return
		}
		var input struct {
			Timezone string `json:"timezone"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		if err := a.app.TaskManager.SetTimezone(r.Context(), input.Timezone); err != nil {
			a.app.Logger.Error("set timezone failed", zap.Error(err))
			a.mutationEvent(r.Context(), "task_manager", "set_timezone", "linux_tools", "timezone", input.Timezone, "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		a.mutationEvent(r.Context(), "task_manager", "set_timezone", "linux_tools", "timezone", input.Timezone, "succeeded", "Updated timezone.")
		writeLinuxToolsSnapshot(w, a, r.Context())
	}))

	r.Method(stdhttp.MethodPost, "/task-manager/linux-tools/hostname", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "task manager is unavailable"})
			return
		}
		var input struct {
			Hostname string `json:"hostname"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		if err := a.app.TaskManager.SetHostname(r.Context(), input.Hostname); err != nil {
			a.app.Logger.Error("set hostname failed", zap.Error(err))
			a.mutationEvent(r.Context(), "task_manager", "set_hostname", "linux_tools", "hostname", input.Hostname, "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		a.mutationEvent(r.Context(), "task_manager", "set_hostname", "linux_tools", "hostname", input.Hostname, "succeeded", "Updated hostname.")
		writeLinuxToolsSnapshot(w, a, r.Context())
	}))

	r.Method(stdhttp.MethodPost, "/task-manager/linux-tools/dns", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "task manager is unavailable"})
			return
		}
		var input struct {
			Servers []string `json:"servers"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		if err := a.app.TaskManager.SetDNS(r.Context(), input.Servers); err != nil {
			a.app.Logger.Error("set dns failed", zap.Error(err))
			a.mutationEvent(r.Context(), "task_manager", "set_dns", "linux_tools", "dns", "DNS", "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		a.mutationEvent(r.Context(), "task_manager", "set_dns", "linux_tools", "dns", "DNS", "succeeded", "Updated DNS servers.")
		writeLinuxToolsSnapshot(w, a, r.Context())
	}))

	r.Method(stdhttp.MethodPost, "/task-manager/linux-tools/swap", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app == nil || a.app.TaskManager == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "task manager is unavailable"})
			return
		}
		var input struct {
			SizeMB int `json:"size_mb"`
		}
		if err := decodeJSON(r, &input); err != nil {
			writeInvalidRequestBody(w)
			return
		}
		if err := a.app.TaskManager.ResizeSwap(r.Context(), input.SizeMB); err != nil {
			a.app.Logger.Error("resize swap failed", zap.Int("size_mb", input.SizeMB), zap.Error(err))
			a.mutationEvent(r.Context(), "task_manager", "resize_swap", "linux_tools", "swap", "Swap", "failed", err.Error())
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		a.mutationEvent(r.Context(), "task_manager", "resize_swap", "linux_tools", "swap", "Swap", "succeeded", "Updated swap size.")
		writeLinuxToolsSnapshot(w, a, r.Context())
	}))
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

func writeLinuxToolsSnapshot(w stdhttp.ResponseWriter, a *apiRoutes, ctx context.Context) {
	if a == nil || a.app == nil || a.app.TaskManager == nil {
		writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{
			"error": "task manager is unavailable",
		})
		return
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"linux_tools": a.app.TaskManager.LinuxTools(ctx),
	})
}
