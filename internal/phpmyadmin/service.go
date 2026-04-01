package phpmyadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"go.uber.org/zap"
)

var cellarVersionPattern = regexp.MustCompile(`/phpmyadmin/([^/]+)/`)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	InstallPath      string   `json:"install_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
}

type actionPlan struct {
	packageManager string
	installLabel   string
	installEnv     map[string]string
	installCmds    [][]string
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
	}
}

func (s *Service) Status(context.Context) Status {
	status := Status{
		Platform: runtime.GOOS,
	}

	plan := detectActionPlan()
	status.PackageManager = plan.packageManager
	status.InstallLabel = plan.installLabel

	installPath, installed := detectInstallPath()
	status.Installed = installed
	status.InstallPath = installPath
	status.Version = detectVersion(installPath)
	status.InstallAvailable = len(plan.installCmds) > 0 && !status.Installed

	switch {
	case status.Installed:
		status.State = "installed"
		switch {
		case status.Version != "" && status.InstallPath != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed at %s.", status.Version, status.InstallPath)
		case status.Version != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed.", status.Version)
		case status.InstallPath != "":
			status.Message = fmt.Sprintf("phpMyAdmin is installed at %s.", status.InstallPath)
		default:
			status.Message = "phpMyAdmin is installed."
		}
	case status.InstallAvailable:
		status.State = "missing"
		status.Message = "phpMyAdmin is not installed. Install it here to add a browser-based MariaDB client."
	default:
		status.State = "missing"
		status.Message = "phpMyAdmin is not installed on this server."
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic phpMyAdmin installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing phpmyadmin",
		zap.String("package_manager", plan.packageManager),
	)

	return runCommands(ctx, plan.installEnv, plan.installCmds...)
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install phpMyAdmin",
				installCmds: [][]string{
					{brewPath, "install", "phpmyadmin"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			if aptPath, ok := lookupCommand("apt-get"); ok {
				debconfPath, _ := lookupCommand("debconf-set-selections")
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install phpMyAdmin",
					installEnv: map[string]string{
						"DEBIAN_FRONTEND": "noninteractive",
					},
					installCmds: aptInstallCommands(aptPath, debconfPath),
				}
			}
			if dnfPath, ok := lookupCommand("dnf"); ok {
				return actionPlan{
					packageManager: "dnf",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{dnfPath, "install", "-y", "phpMyAdmin"},
					},
				}
			}
			if yumPath, ok := lookupCommand("yum"); ok {
				return actionPlan{
					packageManager: "yum",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{yumPath, "install", "-y", "phpMyAdmin"},
					},
				}
			}
			if pacmanPath, ok := lookupCommand("pacman"); ok {
				return actionPlan{
					packageManager: "pacman",
					installLabel:   "Install phpMyAdmin",
					installCmds: [][]string{
						{pacmanPath, "-Sy", "--noconfirm", "phpmyadmin"},
					},
				}
			}
		}
	}

	return actionPlan{}
}

func aptInstallCommands(aptPath, debconfPath string) [][]string {
	commands := [][]string{
		{aptPath, "update"},
	}

	debconfPath = strings.TrimSpace(debconfPath)
	if debconfPath == "" {
		commands = append(commands, []string{aptPath, "install", "-y", "debconf-utils"})
		debconfPath = "debconf-set-selections"
	}

	commands = append(commands,
		[]string{"/bin/sh", "-c", aptDebconfSelectionsCommand(debconfPath)},
		[]string{aptPath, "install", "-y", "phpmyadmin"},
	)

	return commands
}

func aptDebconfSelectionsCommand(debconfPath string) string {
	debconfPath = strings.TrimSpace(debconfPath)
	if debconfPath == "" {
		debconfPath = "debconf-set-selections"
	}

	return fmt.Sprintf(
		"printf 'phpmyadmin phpmyadmin/dbconfig-install boolean false\\nphpmyadmin phpmyadmin/reconfigure-webserver multiselect none\\n' | %s",
		debconfPath,
	)
}

func detectInstallPath() (string, bool) {
	for _, candidate := range installPathCandidates() {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}

		return candidate, true
	}

	return "", false
}

func detectVersion(installPath string) string {
	installPath = strings.TrimSpace(installPath)
	if installPath == "" {
		return ""
	}

	if resolvedPath, err := filepath.EvalSymlinks(installPath); err == nil {
		if version := extractVersionFromPath(resolvedPath); version != "" {
			return version
		}
	}

	if version := extractVersionFromPath(installPath); version != "" {
		return version
	}

	for _, name := range []string{"composer.json", "package.json"} {
		version, err := readVersionFromJSON(filepath.Join(installPath, name))
		if err == nil && version != "" {
			return version
		}
	}

	return ""
}

func readVersionFromJSON(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}

	return strings.TrimSpace(payload.Version), nil
}

func extractVersionFromPath(path string) string {
	match := cellarVersionPattern.FindStringSubmatch(filepath.ToSlash(path))
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}

	return ""
}

func installPathCandidates() []string {
	if override := strings.TrimSpace(os.Getenv("FLOWPANEL_PHPMYADMIN_PATH")); override != "" {
		return []string{override}
	}

	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/share/phpmyadmin",
			"/usr/local/share/phpmyadmin",
		}
	default:
		return []string{
			"/usr/share/phpmyadmin",
			"/usr/share/phpMyAdmin",
			"/usr/share/webapps/phpmyadmin",
			"/usr/share/webapps/phpMyAdmin",
		}
	}
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
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

func runCommands(ctx context.Context, env map[string]string, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}

		if _, err := runCommand(ctx, env, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(ctx context.Context, env map[string]string, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return combinedOutput, fmt.Errorf("%s timed out", name)
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	}
	if combinedOutput == "" {
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	}

	return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
}
