package packageruntime

import (
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

const statusCommandTimeout = 3 * time.Second

var versionPattern = regexp.MustCompile(`\b(\d+(?:\.\d+)+)\b`)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Remove(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
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
	ServiceRunning   bool     `json:"service_running"`
	StartAvailable   bool     `json:"start_available"`
	StartLabel       string   `json:"start_label,omitempty"`
	StopAvailable    bool     `json:"stop_available"`
	StopLabel        string   `json:"stop_label,omitempty"`
	RestartAvailable bool     `json:"restart_available"`
	RestartLabel     string   `json:"restart_label,omitempty"`
}

type Definition struct {
	Key             string
	DisplayName     string
	BinaryNames     []string
	VersionArgs     []string
	InstallLabel    string
	RemoveLabel     string
	StartLabel      string
	StopLabel       string
	RestartLabel    string
	HomebrewFormula string
	HomebrewTap     string
	HomebrewService string
	APTPackages     []string
	APTService      string
	DNFPackages     []string
	DNFService      string
	YUMPackages     []string
	YUMService      string
	PacmanPackages  []string
	PacmanService   string
}

type Service struct {
	logger     *zap.Logger
	definition Definition
}

type actionPlan struct {
	packageManager string
	installCmds    [][]string
	removeCmds     [][]string
	startCmds      [][]string
	stopCmds       [][]string
	restartCmds    [][]string
	serviceStatus  func(context.Context) (bool, error)
}

func NewService(logger *zap.Logger, definition Definition) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger:     logger,
		definition: definition,
	}
}

func NewRedisService(logger *zap.Logger) *Service {
	return NewService(logger, Definition{
		Key:             "redis",
		DisplayName:     "Redis",
		BinaryNames:     []string{"redis-server"},
		VersionArgs:     []string{"--version"},
		InstallLabel:    "Install Redis",
		RemoveLabel:     "Remove Redis",
		StartLabel:      "Start Redis",
		StopLabel:       "Stop Redis",
		RestartLabel:    "Restart Redis",
		HomebrewFormula: "redis",
		HomebrewService: "redis",
		APTPackages:     []string{"redis-server"},
		APTService:      "redis-server",
		DNFPackages:     []string{"redis"},
		DNFService:      "redis",
		YUMPackages:     []string{"redis"},
		YUMService:      "redis",
		PacmanPackages:  []string{"redis"},
		PacmanService:   "redis",
	})
}

func NewMongoDBService(logger *zap.Logger) *Service {
	return NewService(logger, Definition{
		Key:             "mongodb",
		DisplayName:     "MongoDB",
		BinaryNames:     []string{"mongod", "mongosh"},
		VersionArgs:     []string{"--version"},
		InstallLabel:    "Install MongoDB",
		RemoveLabel:     "Remove MongoDB",
		StartLabel:      "Start MongoDB",
		StopLabel:       "Stop MongoDB",
		RestartLabel:    "Restart MongoDB",
		HomebrewFormula: "mongodb-community",
		HomebrewTap:     "mongodb/brew",
		HomebrewService: "mongodb-community",
		APTPackages:     []string{"mongodb-org"},
		APTService:      "mongod",
		DNFPackages:     []string{"mongodb-org"},
		DNFService:      "mongod",
		YUMPackages:     []string{"mongodb-org"},
		YUMService:      "mongod",
		PacmanPackages:  []string{"mongodb"},
		PacmanService:   "mongodb",
	})
}

func NewPostgreSQLService(logger *zap.Logger) *Service {
	return NewService(logger, Definition{
		Key:             "postgresql",
		DisplayName:     "PostgreSQL",
		BinaryNames:     []string{"postgres", "psql", "pg_config"},
		VersionArgs:     []string{"--version"},
		InstallLabel:    "Install PostgreSQL",
		RemoveLabel:     "Remove PostgreSQL",
		StartLabel:      "Start PostgreSQL",
		StopLabel:       "Stop PostgreSQL",
		RestartLabel:    "Restart PostgreSQL",
		HomebrewFormula: "postgresql",
		HomebrewService: "postgresql",
		APTPackages:     []string{"postgresql"},
		APTService:      "postgresql",
		DNFPackages:     []string{"postgresql-server", "postgresql"},
		DNFService:      "postgresql",
		YUMPackages:     []string{"postgresql-server", "postgresql"},
		YUMService:      "postgresql",
		PacmanPackages:  []string{"postgresql"},
		PacmanService:   "postgresql",
	})
}

func (s *Service) Status(ctx context.Context) Status {
	plan := detectActionPlan(s.definition)
	status := Status{
		Platform:       runtime.GOOS,
		PackageManager: plan.packageManager,
		InstallLabel:   s.definition.InstallLabel,
		RemoveLabel:    s.definition.RemoveLabel,
		StartLabel:     s.definition.StartLabel,
		StopLabel:      s.definition.StopLabel,
		RestartLabel:   s.definition.RestartLabel,
	}

	if binaryPath, installed := lookupFirstCommand(s.definition.BinaryNames...); installed {
		status.Installed = true
		status.BinaryPath = binaryPath
		if version, err := inspectVersion(ctx, binaryPath, s.definition.VersionArgs...); err == nil {
			status.Version = version
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	status.InstallAvailable = len(plan.installCmds) > 0 && !status.Installed
	status.RemoveAvailable = len(plan.removeCmds) > 0 && status.Installed
	if plan.serviceStatus != nil && status.Installed {
		running, err := plan.serviceStatus(ctx)
		if err == nil {
			status.ServiceRunning = running
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}
	status.StartAvailable = len(plan.startCmds) > 0 && status.Installed && !status.ServiceRunning
	status.StopAvailable = len(plan.stopCmds) > 0 && status.Installed && status.ServiceRunning
	status.RestartAvailable = len(plan.restartCmds) > 0 && status.Installed && status.ServiceRunning

	switch {
	case status.ServiceRunning && status.Version != "" && status.BinaryPath != "":
		status.State = "running"
		status.Message = fmt.Sprintf("%s %s is running at %s.", s.definition.DisplayName, status.Version, status.BinaryPath)
	case status.ServiceRunning && status.BinaryPath != "":
		status.State = "running"
		status.Message = fmt.Sprintf("%s is running at %s.", s.definition.DisplayName, status.BinaryPath)
	case status.Installed && status.Version != "" && status.BinaryPath != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("%s %s is installed at %s.", s.definition.DisplayName, status.Version, status.BinaryPath)
	case status.Installed && status.Version != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("%s %s is installed.", s.definition.DisplayName, status.Version)
	case status.Installed:
		status.State = "installed"
		status.Message = fmt.Sprintf("%s is installed at %s.", s.definition.DisplayName, status.BinaryPath)
	default:
		status.State = "missing"
		status.Message = fmt.Sprintf("%s was not detected on this server.", s.definition.DisplayName)
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	if status := s.Status(ctx); status.Installed {
		return nil
	}

	plan := detectActionPlan(s.definition)
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic %s installation is not supported on %s", strings.ToLower(s.definition.DisplayName), runtime.GOOS)
	}

	s.logger.Info("installing package runtime",
		zap.String("runtime", s.definition.Key),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.installCmds...)
}

func (s *Service) Remove(ctx context.Context) error {
	if status := s.Status(ctx); !status.Installed {
		return nil
	}

	plan := detectActionPlan(s.definition)
	if len(plan.removeCmds) == 0 {
		return fmt.Errorf("automatic %s removal is not supported on %s", strings.ToLower(s.definition.DisplayName), runtime.GOOS)
	}

	s.logger.Info("removing package runtime",
		zap.String("runtime", s.definition.Key),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.removeCmds...)
}

func (s *Service) Start(ctx context.Context) error {
	status := s.Status(ctx)
	if !status.Installed || status.ServiceRunning {
		return nil
	}

	plan := detectActionPlan(s.definition)
	if len(plan.startCmds) == 0 {
		return fmt.Errorf("automatic %s start is not supported on %s", strings.ToLower(s.definition.DisplayName), runtime.GOOS)
	}

	s.logger.Info("starting package runtime service",
		zap.String("runtime", s.definition.Key),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.startCmds...)
}

func (s *Service) Stop(ctx context.Context) error {
	status := s.Status(ctx)
	if !status.Installed || !status.ServiceRunning {
		return nil
	}

	plan := detectActionPlan(s.definition)
	if len(plan.stopCmds) == 0 {
		return fmt.Errorf("automatic %s stop is not supported on %s", strings.ToLower(s.definition.DisplayName), runtime.GOOS)
	}

	s.logger.Info("stopping package runtime service",
		zap.String("runtime", s.definition.Key),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.stopCmds...)
}

func (s *Service) Restart(ctx context.Context) error {
	status := s.Status(ctx)
	if !status.Installed {
		return nil
	}

	plan := detectActionPlan(s.definition)
	if len(plan.restartCmds) == 0 {
		return fmt.Errorf("automatic %s restart is not supported on %s", strings.ToLower(s.definition.DisplayName), runtime.GOOS)
	}

	s.logger.Info("restarting package runtime service",
		zap.String("runtime", s.definition.Key),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.restartCmds...)
}

func detectActionPlan(definition Definition) actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			installCmds := make([][]string, 0, 2)
			if strings.TrimSpace(definition.HomebrewTap) != "" {
				installCmds = append(installCmds, []string{brewPath, "tap", definition.HomebrewTap})
			}
			if strings.TrimSpace(definition.HomebrewFormula) != "" {
				installCmds = append(installCmds, []string{brewPath, "install", definition.HomebrewFormula})
				plan := actionPlan{
					packageManager: "homebrew",
					installCmds:    installCmds,
					removeCmds:     [][]string{{brewPath, "uninstall", definition.HomebrewFormula}},
				}
				if serviceName := strings.TrimSpace(definition.HomebrewService); serviceName != "" {
					plan.startCmds = [][]string{{brewPath, "services", "start", serviceName}}
					plan.stopCmds = [][]string{{brewPath, "services", "stop", serviceName}}
					plan.restartCmds = [][]string{{brewPath, "services", "restart", serviceName}}
					plan.serviceStatus = func(ctx context.Context) (bool, error) {
						return inspectHomebrewService(ctx, brewPath, serviceName)
					}
				}
				return plan
			}
		}
	case "linux":
		if os.Geteuid() != 0 {
			return actionPlan{}
		}
		if aptPath, ok := lookupCommand("apt-get"); ok && len(definition.APTPackages) > 0 {
			plan := actionPlan{
				packageManager: "apt",
				installCmds:    [][]string{append([]string{aptPath, "install", "-y"}, definition.APTPackages...)},
				removeCmds:     [][]string{append([]string{aptPath, "remove", "-y"}, definition.APTPackages...)},
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.APTService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.serviceStatus = func(ctx context.Context) (bool, error) {
						return inspectSystemdService(ctx, systemctlPath, serviceName)
					}
				}
			}
			return plan
		}
		if dnfPath, ok := lookupCommand("dnf"); ok && len(definition.DNFPackages) > 0 {
			plan := actionPlan{
				packageManager: "dnf",
				installCmds:    [][]string{append([]string{dnfPath, "install", "-y"}, definition.DNFPackages...)},
				removeCmds:     [][]string{append([]string{dnfPath, "remove", "-y"}, definition.DNFPackages...)},
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.DNFService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.serviceStatus = func(ctx context.Context) (bool, error) {
						return inspectSystemdService(ctx, systemctlPath, serviceName)
					}
				}
			}
			return plan
		}
		if yumPath, ok := lookupCommand("yum"); ok && len(definition.YUMPackages) > 0 {
			plan := actionPlan{
				packageManager: "yum",
				installCmds:    [][]string{append([]string{yumPath, "install", "-y"}, definition.YUMPackages...)},
				removeCmds:     [][]string{append([]string{yumPath, "remove", "-y"}, definition.YUMPackages...)},
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.YUMService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.serviceStatus = func(ctx context.Context) (bool, error) {
						return inspectSystemdService(ctx, systemctlPath, serviceName)
					}
				}
			}
			return plan
		}
		if pacmanPath, ok := lookupCommand("pacman"); ok && len(definition.PacmanPackages) > 0 {
			plan := actionPlan{
				packageManager: "pacman",
				installCmds:    [][]string{append([]string{pacmanPath, "-Sy", "--noconfirm"}, definition.PacmanPackages...)},
				removeCmds:     [][]string{append([]string{pacmanPath, "-Rns", "--noconfirm"}, definition.PacmanPackages...)},
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.PacmanService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.serviceStatus = func(ctx context.Context) (bool, error) {
						return inspectSystemdService(ctx, systemctlPath, serviceName)
					}
				}
			}
			return plan
		}
	}

	return actionPlan{}
}

func lookupFirstCommand(names ...string) (string, bool) {
	for _, name := range names {
		if path, ok := lookupCommand(name); ok {
			return path, true
		}
	}

	return "", false
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
		"/snap/bin",
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

func inspectVersion(ctx context.Context, binaryPath string, args ...string) (string, error) {
	output, err := runInspectCommand(ctx, binaryPath, args...)
	if err != nil {
		return "", err
	}

	match := versionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) < 2 {
		return "", errors.New("version could not be determined")
	}

	return strings.TrimSpace(match[1]), nil
}

func inspectHomebrewService(ctx context.Context, brewPath, serviceName string) (bool, error) {
	output, err := runInspectCommand(ctx, brewPath, "services", "info", serviceName)
	if err != nil {
		return false, err
	}

	lowerOutput := strings.ToLower(output)
	return strings.Contains(lowerOutput, "running: true") || strings.Contains(lowerOutput, "status: started"), nil
}

func inspectSystemdService(ctx context.Context, systemctlPath, serviceName string) (bool, error) {
	output, err := runInspectCommand(ctx, systemctlPath, "is-active", serviceName)
	if err != nil {
		if strings.Contains(err.Error(), "inactive") || strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "not-found") {
			return false, nil
		}
		return false, err
	}

	return strings.EqualFold(strings.TrimSpace(output), "active"), nil
}

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, statusCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, name, args...)
	output, err := cmd.CombinedOutput()
	if commandCtx.Err() != nil {
		return "", commandCtx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), message)
	}

	return string(output), nil
}

func runCommands(ctx context.Context, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}

		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf("%s: %s", strings.Join(command, " "), message)
		}
	}

	return nil
}
