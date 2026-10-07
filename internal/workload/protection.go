package workload

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"flowpanel/internal/executil"
)

const Slice = "flowpanel-workloads.slice"
const PHPSlice = "flowpanel-workloads-php.slice"
const AppsSlice = "flowpanel-workloads-apps.slice"
const TasksMax = 512

var protectionMu sync.Mutex
var ready bool

// Installed Linux servers are protected by default; development hosts are opt-in.
func Enabled() bool {
	value := strings.TrimSpace(os.Getenv("FLOWPANEL_WORKLOAD_PROTECTION"))
	if value != "" {
		parsed, err := strconv.ParseBool(value)
		return err != nil || parsed
	}
	return runtime.GOOS == "linux" && os.Getenv("FLOWPANEL_ENV") == "production"
}

func Ensure(ctx context.Context) error {
	if !Enabled() {
		return nil
	}
	protectionMu.Lock()
	defer protectionMu.Unlock()
	if ready {
		return nil
	}
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_WORKLOAD_PROTECTION")); value != "" {
		if _, err := strconv.ParseBool(value); err != nil {
			return errors.New("FLOWPANEL_WORKLOAD_PROTECTION must be a boolean")
		}
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("workload protection requires Linux, root, systemd and cgroup v2")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("workload protection requires systemd")
	}
	controllers, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers")
	if err != nil {
		return errors.New("workload protection requires cgroup v2")
	}
	for _, controller := range []string{"cpu", "memory", "pids"} {
		if !strings.Contains(" "+strings.TrimSpace(string(controllers))+" ", " "+controller+" ") {
			return fmt.Errorf("workload protection requires the %s cgroup controller", controller)
		}
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return err
	}
	cpu, err := percentage("FLOWPANEL_WORKLOAD_CPU_PERCENT", 75)
	if err != nil {
		return err
	}
	memory, err := percentage("FLOWPANEL_WORKLOAD_MEMORY_PERCENT", 70)
	if err != nil {
		return err
	}
	phpCPU, err := percentage("FLOWPANEL_PHP_CPU_SHARE_PERCENT", 20)
	if err != nil {
		return err
	}
	phpMemory, err := percentage("FLOWPANEL_PHP_MEMORY_RESERVE_PERCENT", 10)
	if err != nil {
		return err
	}
	if phpMemory >= memory {
		return errors.New("FLOWPANEL_PHP_MEMORY_RESERVE_PERCENT must be below FLOWPANEL_WORKLOAD_MEMORY_PERCENT")
	}
	low := fmt.Sprintf("MemoryLow=%d%%", phpMemory)
	// Reclaim protection must be carried through every ancestor of the PHP slice.
	if _, err := writeUnit("/etc/systemd/system/flowpanel.slice.d/flowpanel-php.conf", "[Slice]\n"+low+"\n"); err != nil {
		return err
	}
	units := []struct {
		name       string
		properties []string
	}{
		{Slice, []string{
			fmt.Sprintf("CPUQuota=%d%%", runtime.NumCPU()*cpu),
			fmt.Sprintf("MemoryHigh=%d%%", memory*6/7),
			fmt.Sprintf("MemoryMax=%d%%", memory), low, "MemorySwapMax=0", "TasksMax=4096",
		}},
		{PHPSlice, []string{fmt.Sprintf("CPUWeight=%d", phpCPU), low}},
		{AppsSlice, []string{
			fmt.Sprintf("CPUWeight=%d", 100-phpCPU),
			fmt.Sprintf("MemoryHigh=%d%%", max(1, (memory-phpMemory)*6/7)),
			fmt.Sprintf("MemoryMax=%d%%", memory-phpMemory), "TasksMax=3584",
		}},
	}
	for _, unit := range units {
		content := "[Unit]\nDescription=FlowPanel managed workloads\n\n[Slice]\nCPUAccounting=yes\nMemoryAccounting=yes\nTasksAccounting=yes\n" + strings.Join(unit.properties, "\n") + "\n"
		if _, err := writeUnit(filepath.Join("/etc/systemd/system", unit.name), content); err != nil {
			return err
		}
	}
	if _, err := systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	for _, unit := range units {
		if _, err := systemctl(ctx, "start", unit.name); err != nil {
			return err
		}
		if _, err := systemctl(ctx, append([]string{"set-property", "--runtime", unit.name}, unit.properties...)...); err != nil {
			return err
		}
	}
	if _, err := systemctl(ctx, "set-property", "--runtime", "flowpanel.slice", low); err != nil {
		return err
	}
	// Read the kernel files, so an unsupported/ignored unit setting cannot look protected.
	root := "/sys/fs/cgroup/flowpanel.slice"
	checks := map[string]map[string]string{
		root:                                  {"memory.low": ""},
		filepath.Join(root, Slice):            {"cpu.max": "", "memory.max": "", "memory.high": "", "memory.low": "", "memory.swap.max": "0", "pids.max": "4096"},
		filepath.Join(root, Slice, PHPSlice):  {"cpu.weight": strconv.Itoa(phpCPU), "memory.low": ""},
		filepath.Join(root, Slice, AppsSlice): {"cpu.weight": strconv.Itoa(100 - phpCPU), "memory.max": "", "memory.high": "", "pids.max": "3584"},
	}
	for group, limits := range checks {
		if err := verifyLimits(group, limits); err != nil {
			return err
		}
	}
	ready = true
	return nil
}

// An empty expected value requires an effective, positive kernel limit.
func verifyLimits(group string, limits map[string]string) error {
	for file, expected := range limits {
		data, err := os.ReadFile(filepath.Join(group, file))
		if err != nil {
			return fmt.Errorf("read workload protection %s/%s: %w", group, file, err)
		}
		value := strings.TrimSpace(string(data))
		valid := value == expected
		if expected == "" {
			first, _, _ := strings.Cut(value, " ")
			number, parseErr := strconv.ParseUint(first, 10, 64)
			valid = parseErr == nil && number > 0
		}
		if !valid {
			return fmt.Errorf("workload protection could not enforce %s/%s", group, file)
		}
	}
	return nil
}

func percentage(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 90 {
		return 0, fmt.Errorf("%s must be between 1 and 90", key)
	}
	return parsed, nil
}

// ProtectService enrolls PHP-FPM before installation/start, and restarts an
// existing service once when its processes need to move to the workload slice.
func ProtectService(ctx context.Context, name string) error {
	if !Enabled() {
		return nil
	}
	if err := Ensure(ctx); err != nil {
		return err
	}
	if strings.ContainsAny(name, "/\\\x00\r\n") || !strings.HasSuffix(name, ".service") {
		return errors.New("invalid workload service name")
	}
	content := fmt.Sprintf(`[Unit]
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Slice=%s
MemoryLow=infinity
TasksMax=%d
OOMPolicy=kill
Restart=on-failure
RestartSec=5
`, PHPSlice, TasksMax)
	changed, err := writeUnit(filepath.Join("/etc/systemd/system", name+".d", "flowpanel-workload.conf"), content)
	if err != nil {
		return err
	}
	if changed {
		if _, err := systemctl(ctx, "daemon-reload"); err != nil {
			return err
		}
	}
	settings, err := systemctl(ctx, "show", name, "--property=LoadState", "--property=Slice")
	if err != nil {
		return err
	}
	if strings.Contains("\n"+settings+"\n", "\nLoadState=not-found\n") {
		return nil // The drop-in is ready before the package creates its service.
	}
	if !strings.Contains("\n"+settings+"\n", "\nSlice="+PHPSlice+"\n") {
		return fmt.Errorf("%s does not use the protected workload slice", name)
	}
	state, err := systemctl(ctx, "show", name, "--property=ActiveState", "--value")
	if err != nil {
		return err
	}
	if state = strings.TrimSpace(state); state == "active" || state == "reloading" || state == "activating" {
		group, err := systemctl(ctx, "show", name, "--property=ControlGroup", "--value")
		if err != nil {
			return err
		}
		if !strings.Contains(group, "/"+PHPSlice+"/") {
			if _, err := systemctl(ctx, "restart", name); err != nil {
				return err
			}
			group, err = systemctl(ctx, "show", name, "--property=ControlGroup", "--value")
			if err != nil {
				return err
			}
			if !strings.Contains(group, "/"+PHPSlice+"/") {
				return fmt.Errorf("%s did not enter the protected workload slice", name)
			}
		}
		if err := verifyLimits(filepath.Join("/sys/fs/cgroup", strings.TrimSpace(group)), map[string]string{"memory.low": "max"}); err != nil {
			return err
		}
	}
	return nil
}

func Run(ctx context.Context, cmd *exec.Cmd) error { return run(ctx, cmd, false) }

func ReconcilePHP(ctx context.Context) error {
	if !Enabled() {
		return nil
	}
	if err := Ensure(ctx); err != nil {
		return err
	}
	units, err := systemctl(ctx, "list-unit-files", "--type=service", "--no-legend", "php*-fpm.service", "php*-php-fpm.service")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(units, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			if err := ProtectService(ctx, fields[0]); err != nil {
				return err
			}
		}
	}
	return nil
}

// PM2's daemon must outlive the CLI that starts it, inside the protected scope.
func RunPersistent(ctx context.Context, cmd *exec.Cmd) error { return run(ctx, cmd, true) }

func run(ctx context.Context, cmd *exec.Cmd, persistent bool) error {
	if !Enabled() {
		return cmd.Run()
	}
	if err := Ensure(ctx); err != nil {
		return err
	}
	unit := "flowpanel-job-" + rand.Text() + ".scope"
	args := []string{"systemd-run", "--scope", "--quiet", "--collect", "--expand-environment=no", "--unit=" + unit, "--slice=" + AppsSlice, fmt.Sprintf("--property=TasksMax=%d", TasksMax), "--property=TimeoutStopSec=5s"}
	identity, err := scopeIdentity(cmd)
	if err != nil {
		return err
	}
	args = append(args, "--")
	args = append(args, identity...)
	args = append(args, cmd.Path)
	args = append(args, cmd.Args[1:]...)
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return err
	}
	cmd.Path, cmd.Args = path, args
	// Scopes inherit cwd, env and streams without re-encoding them.
	cmd.WaitDelay = 5 * time.Second
	if cmd.Cancel != nil {
		cmd.Cancel = func() error {
			stopScope(unit)
			return cmd.Process.Kill()
		}
	}
	err = cmd.Run()
	if !persistent || err != nil || ctx.Err() != nil {
		stopScope(unit)
	}
	return err
}

func stopScope(unit string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = systemctl(ctx, "stop", unit)
}

func systemctl(ctx context.Context, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, _, err := executil.RunCombined(exec.CommandContext(commandCtx, "systemctl", args...), executil.DefaultOutputLimit)
	if err != nil {
		return "", fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func writeUnit(path, content string) (bool, error) {
	old, err := os.ReadFile(path)
	if err == nil && string(old) == content {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}
