package httpx

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"

	"flowpanel/internal/pm2"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (a *apiRoutes) registerPM2Routes(r chi.Router) {
	pm2StatusHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{
			"pm2": a.trackPM2Status(a.app.PM2.Status(r.Context())),
		})
	})
	r.Method(stdhttp.MethodGet, "/pm2", pm2StatusHandler)
	r.Method(stdhttp.MethodHead, "/pm2", pm2StatusHandler)
	parseProcessID := func(r *stdhttp.Request) (int, error) {
		processID, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "processID")))
		if err != nil {
			return 0, fmt.Errorf("invalid PM2 process ID")
		}

		return processID, nil
	}

	r.Method(stdhttp.MethodGet, "/pm2/processes", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		processes, err := a.app.PM2.List(r.Context())
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"processes": processes})
	}))
	r.Method(stdhttp.MethodGet, "/pm2/processes/{processID}/logs", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if a.app.PM2 == nil {
			writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
			return
		}

		processID, err := parseProcessID(r)
		if err != nil {
			writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}

		output, err := a.app.PM2.Logs(r.Context(), processID)
		if err != nil {
			writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, stdhttp.StatusOK, map[string]any{"output": output})
	}))
	registerProcessAction := func(action, successMessage string, run func(context.Context, int) ([]pm2.Process, error)) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PM2 == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
				return
			}

			processID, err := parseProcessID(r)
			if err != nil {
				writeJSON(w, stdhttp.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			processes, err := run(actionCtx, processID)
			if err != nil {
				a.app.Logger.Error(action+" pm2 process failed", zap.Int("process_id", processID), zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "pm2_process", strconv.Itoa(processID), fmt.Sprintf("PM2 process %d", processID), "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}

			processLabel := fmt.Sprintf("PM2 process %d", processID)
			for _, process := range processes {
				if process.ID == processID {
					processLabel = process.Name
					break
				}
			}

			a.mutationEvent(actionCtx, "runtime", action, "pm2_process", strconv.Itoa(processID), processLabel, "succeeded", successMessage)
			writeJSON(w, stdhttp.StatusOK, map[string]any{"processes": processes})
		}
	}

	registerRuntimeAction := func(action string, run func(context.Context) error) stdhttp.HandlerFunc {
		return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if a.app.PM2 == nil {
				writeJSON(w, stdhttp.StatusServiceUnavailable, map[string]any{"error": "pm2 runtime is not configured"})
				return
			}

			actionCtx := backgroundRequestContext(r.Context())
			if err := a.runtimeActions.Begin("pm2", action); err != nil {
				writeJSON(w, stdhttp.StatusConflict, map[string]any{"error": err.Error()})
				return
			}
			if err := run(actionCtx); err != nil {
				a.runtimeActions.End("pm2", action)
				a.app.Logger.Error(action+" pm2 failed", zap.Error(err))
				a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "failed", err.Error())
				writeJSON(w, stdhttp.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			a.runtimeActions.End("pm2", action)

			pastTense := map[string]string{
				"install": "Installed PM2.",
				"remove":  "Removed PM2.",
			}[action]
			a.mutationEvent(actionCtx, "runtime", action, "pm2", "pm2", "PM2", "succeeded", pastTense)
			writeJSON(w, stdhttp.StatusOK, map[string]any{
				"pm2": a.trackPM2Status(a.app.PM2.Status(actionCtx)),
			})
		}
	}

	r.Method(stdhttp.MethodPost, "/pm2/install", registerRuntimeAction("install", func(ctx context.Context) error {
		return a.app.PM2.Install(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/remove", registerRuntimeAction("remove", func(ctx context.Context) error {
		return a.app.PM2.Remove(ctx)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/start", registerProcessAction("start", "Started the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.StartProcess(ctx, processID)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/stop", registerProcessAction("stop", "Stopped the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.StopProcess(ctx, processID)
	}))
	r.Method(stdhttp.MethodPost, "/pm2/processes/{processID}/restart", registerProcessAction("restart", "Restarted the PM2 process.", func(ctx context.Context, processID int) ([]pm2.Process, error) {
		return a.app.PM2.RestartProcess(ctx, processID)
	}))
}
