package phpmyadmin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
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

const (
	defaultPasswordFile = "mariadb-root-password"
	passwordBytesLength = 24
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
	status.InstallAvailable = plan.packageManager != "" && !status.Installed

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
	if plan.packageManager == "apt" {
		aptPath, ok := lookupCommand("apt-get")
		if !ok {
			return errors.New("apt-get is not installed")
		}

		rootPassword, err := resolveMariaDBRootPassword()
		if err != nil {
			return fmt.Errorf("resolve mariadb root password: %w", err)
		}

		appPassword, err := generatePassword()
		if err != nil {
			return fmt.Errorf("generate phpMyAdmin application password: %w", err)
		}

		debconfPath, _ := lookupCommand("debconf-set-selections")
		plan.installEnv = map[string]string{
			"DEBIAN_FRONTEND": "noninteractive",
		}
		plan.installCmds = aptInstallCommands(aptPath, debconfPath, rootPassword, appPassword)
	}

	return s.installWithPlan(ctx, plan)
}

func (s *Service) installWithPlan(ctx context.Context, plan actionPlan) error {
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
			if _, ok := lookupCommand("apt-get"); ok {
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install phpMyAdmin",
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

func aptInstallCommands(aptPath, debconfPath, adminPassword, appPassword string) [][]string {
	commands := [][]string{
		{aptPath, "update"},
	}

	debconfPath = strings.TrimSpace(debconfPath)
	if debconfPath == "" {
		commands = append(commands, []string{aptPath, "install", "-y", "debconf-utils"})
		debconfPath = "debconf-set-selections"
	}

	commands = append(commands,
		[]string{"/bin/sh", "-c", aptDebconfSelectionsCommand(debconfPath, adminPassword, appPassword)},
		[]string{aptPath, "install", "-y", "phpmyadmin"},
	)

	return commands
}

func aptDebconfSelectionsCommand(debconfPath, adminPassword, appPassword string) string {
	debconfPath = strings.TrimSpace(debconfPath)
	if debconfPath == "" {
		debconfPath = "debconf-set-selections"
	}

	lines := []string{
		"phpmyadmin phpmyadmin/dbconfig-install boolean true",
		"phpmyadmin phpmyadmin/reconfigure-webserver multiselect none",
		"phpmyadmin phpmyadmin/mysql/admin-user string root",
		fmt.Sprintf("phpmyadmin phpmyadmin/mysql/admin-pass password %s", adminPassword),
		fmt.Sprintf("phpmyadmin phpmyadmin/mysql/app-pass password %s", appPassword),
		fmt.Sprintf("phpmyadmin phpmyadmin/app-password-confirm password %s", appPassword),
	}

	args := make([]string, 0, len(lines)+2)
	args = append(args, "printf", "%s\\n")
	for _, line := range lines {
		args = append(args, shellQuote(line))
	}

	return strings.Join(args, " ") + " | " + debconfPath
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func resolveMariaDBRootPassword() (string, error) {
	if password, ok := rootPasswordFromEnv(); ok {
		return password, nil
	}

	password, configured, err := readPasswordFile(resolvePasswordFilePath())
	if err != nil {
		return "", fmt.Errorf("read mariadb root password file: %w", err)
	}
	if !configured {
		return "", errors.New("mariadb root password is not configured")
	}

	return password, nil
}

func resolvePasswordFilePath() string {
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_MARIADB_PASSWORD_FILE")); value != "" {
		return value
	}

	if dbPath := strings.TrimSpace(os.Getenv("FLOWPANEL_DB_PATH")); dbPath != "" && dbPath != ":memory:" {
		return filepath.Join(filepath.Dir(dbPath), defaultPasswordFile)
	}

	return filepath.Join(".", "data", defaultPasswordFile)
}

func rootPasswordFromEnv() (string, bool) {
	password, configured := os.LookupEnv("FLOWPANEL_MARIADB_PASSWORD")
	if !configured {
		return "", false
	}

	password = strings.TrimSpace(password)
	if password == "" {
		return "", false
	}

	return password, true
}

func readPasswordFile(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}

	password := strings.TrimSpace(string(content))
	if password == "" {
		return "", false, nil
	}

	return password, true, nil
}

func generatePassword() (string, error) {
	randomBytes := make([]byte, passwordBytesLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
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
