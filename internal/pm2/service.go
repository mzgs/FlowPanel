package pm2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	managedNodeRoot      = "/usr/local/nodejs"
	pm2LogMaxSize        = "100M"
)

var pm2VersionPattern = regexp.MustCompile(`\b(\d+(?:\.\d+)+)\b`)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Remove(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	BinaryPath       string   `json:"binary_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
	RemoveAvailable  bool     `json:"remove_available"`
	RemoveLabel      string   `json:"remove_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{logger: logger}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		Platform:       runtime.GOOS,
		PackageManager: "npm",
		InstallLabel:   "Install PM2",
		RemoveLabel:    "Remove PM2",
	}

	nodePath, nodeInstalled := detectNodeBinary()
	_, npmInstalled := detectNPMBinary()
	pm2Path, pm2Installed := detectPM2Binary()
	if pm2Installed {
		status.Installed = true
		status.BinaryPath = pm2Path
		if output, err := runInspectCommand(ctx, pm2Path, "--version"); err == nil {
			status.Version = parseVersion(output)
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	status.InstallAvailable = nodeInstalled && npmInstalled && !status.Installed
	status.RemoveAvailable = status.Installed && npmInstalled

	switch {
	case status.Installed && status.Version != "" && status.BinaryPath != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 %s is installed at %s.", status.Version, status.BinaryPath)
	case status.Installed && status.Version != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 %s is installed.", status.Version)
	case status.Installed:
		status.State = "installed"
		status.Message = fmt.Sprintf("PM2 is installed at %s.", status.BinaryPath)
	case !nodeInstalled:
		status.State = "missing"
		status.Message = "Install Node.js before installing PM2."
	case !npmInstalled:
		status.State = "missing"
		status.Message = "npm was not detected. Install or repair Node.js before installing PM2."
	case nodePath != "":
		status.State = "missing"
		status.Message = fmt.Sprintf("PM2 is not installed. Node.js is available at %s.", nodePath)
	default:
		status.State = "missing"
		status.Message = "PM2 is not installed."
	}

	if status.Installed && !npmInstalled {
		status.Issues = append(status.Issues, "npm was not detected, so automatic PM2 removal is unavailable.")
	}
	if len(status.Issues) == 0 {
		status.Issues = nil
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	if status := s.Status(ctx); status.Installed {
		return nil
	}

	nodePath, nodeInstalled := detectNodeBinary()
	if !nodeInstalled {
		return errors.New("Node.js must be installed before PM2 can be installed")
	}

	npmPath, npmInstalled := detectNPMBinary()
	if !npmInstalled {
		return errors.New("npm was not detected. Install or repair Node.js before installing PM2")
	}

	s.logger.Info("installing pm2 runtime",
		zap.String("node_path", nodePath),
		zap.String("npm_path", npmPath),
	)
	if err := runCommands(ctx,
		[]string{npmPath, "install", "-g", "pm2"},
	); err != nil {
		return err
	}

	pm2Path, installed := detectPM2Binary()
	if !installed {
		return errors.New("pm2 binary was not found after installation")
	}

	s.logger.Info("configuring pm2 log rotation",
		zap.String("pm2_path", pm2Path),
		zap.String("max_size", pm2LogMaxSize),
	)
	return runCommands(ctx,
		[]string{pm2Path, "install", "pm2-logrotate"},
		[]string{pm2Path, "completion", "install"},
		[]string{pm2Path, "set", "pm2-logrotate:max_size", pm2LogMaxSize},
	)
}

func (s *Service) Remove(ctx context.Context) error {
	if status := s.Status(ctx); !status.Installed {
		return nil
	}

	npmPath, installed := detectNPMBinary()
	if !installed {
		return errors.New("npm was not detected. Install or repair Node.js before removing PM2")
	}

	s.logger.Info("removing pm2 runtime",
		zap.String("npm_path", npmPath),
	)
	return runCommands(ctx,
		[]string{npmPath, "uninstall", "-g", "pm2"},
	)
}

func parseVersion(output string) string {
	match := pm2VersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(match[1])
}

func detectNodeBinary() (string, bool) {
	if path, ok := lookupCommand("node"); ok {
		return path, true
	}

	return lookupCommand("nodejs")
}

func detectNPMBinary() (string, bool) {
	managedPath := filepath.Join(managedNodeRoot, "bin", "npm")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true
	}

	return lookupCommand("npm")
}

func detectPM2Binary() (string, bool) {
	managedPath := filepath.Join(managedNodeRoot, "bin", "pm2")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true
	}

	return lookupCommand("pm2")
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
		filepath.Join(managedNodeRoot, "bin"),
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/usr/sbin",
	} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}

	return "", false
}

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, statusCommandTimeout)
		defer cancel()
	}

	return runCommand(runCtx, name, args...)
}

func runCommands(ctx context.Context, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if _, err := runCommand(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	cmd.Env = commandEnv()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return combinedOutput, fmt.Errorf("%s timed out", name)
	case errors.Is(runCtx.Err(), context.Canceled):
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	case combinedOutput == "":
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	default:
		return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
	}
}

func commandEnv() []string {
	pathEntry := filepath.Join(managedNodeRoot, "bin")
	currentPath := strings.TrimSpace(os.Getenv("PATH"))
	for _, entry := range filepath.SplitList(currentPath) {
		if strings.TrimSpace(entry) == pathEntry {
			return os.Environ()
		}
	}

	if currentPath == "" {
		return append(os.Environ(), "PATH="+pathEntry)
	}

	return append(os.Environ(), "PATH="+pathEntry+string(os.PathListSeparator)+currentPath)
}
