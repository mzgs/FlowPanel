package pm2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/executil"
	"flowpanel/internal/workload"
)

// ponytail: serialize PM2 commands for safe daemon migration; per-PM2_HOME locks if throughput matters.
var daemonMu sync.Mutex

func runProtectedPM2(ctx context.Context, cmd *exec.Cmd) error {
	if !workload.Enabled() {
		return cmd.Run()
	}
	daemonMu.Lock()
	defer daemonMu.Unlock()
	if err := workload.Ensure(ctx); err != nil {
		return err
	}
	environment := cmd.Environ()
	home, pm2Home := "", ""
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		switch key {
		case "HOME":
			home = value
		case "PM2_HOME":
			pm2Home = value
		}
	}
	if pm2Home == "" {
		if home == "" {
			return errors.New("HOME is required for protected PM2 startup")
		}
		pm2Home = filepath.Join(home, ".pm2")
	}
	pidData, err := os.ReadFile(filepath.Join(pm2Home, "pm2.pid"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil || pid <= 0 {
			return errors.New("PM2 daemon PID is invalid")
		}
		group, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !strings.Contains(string(group), "/"+workload.AppsSlice+"/") {
			if err := migrateDaemon(ctx, cmd.Path, environment, pm2Home); err != nil {
				return err
			}
		}
	}
	return workload.RunPersistent(ctx, cmd)
}

func migrateDaemon(ctx context.Context, binary string, environment []string, home string) error {
	// Finish restoring the saved apps even if the inspection that triggered
	// migration times out or its HTTP client disconnects.
	run := func(args ...string) error {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(runCtx, binary, args...)
		cmd.Env = environment
		output := executil.NewTailBuffer(executil.DefaultOutputLimit)
		cmd.Stdout, cmd.Stderr = output, output
		if err := workload.RunPersistent(runCtx, cmd); err != nil {
			return fmt.Errorf("protect PM2 daemon: %w: %s", err, strings.TrimSpace(output.String()))
		}
		return nil
	}
	if err := run("save", "--force"); err != nil {
		return err
	}
	path := filepath.Join(home, "dump.pm2")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var processes []map[string]any
	if err := json.Unmarshal(data, &processes); err != nil {
		return fmt.Errorf("read PM2 snapshot before migration: %w", err)
	}
	for _, process := range processes {
		process["exp_backoff_restart_delay"] = 1000
		process["max_restarts"] = 10
		process["min_uptime"] = 30000
	}
	data, err = json.Marshal(processes)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(home, ".flowpanel-pm2-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	_, writeErr := staged.Write(data)
	if err := errors.Join(writeErr, staged.Close()); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), path); err != nil {
		return err
	}
	if err := run("kill"); err != nil {
		return err
	}
	return run("resurrect")
}
