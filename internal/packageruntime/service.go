package packageruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

const mongoDBAPTSeries = "8.0"

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
	EnableOnInstall bool
	StartOnInstall  bool
	InstallFallback func(context.Context, actionPlan, error) (bool, error)
}

type Service struct {
	logger     *zap.Logger
	definition Definition
}

type actionPlan struct {
	packageManager  string
	installCmds     [][]string
	postInstallCmds [][]string
	removeCmds      [][]string
	startCmds       [][]string
	stopCmds        [][]string
	restartCmds     [][]string
	serviceStatus   func(context.Context) (bool, error)
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
		BinaryNames:     []string{"mongod"},
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
		EnableOnInstall: true,
		StartOnInstall:  true,
		InstallFallback: retryMongoDBInstall,
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
		EnableOnInstall: true,
	})
}

func NewDockerService(logger *zap.Logger) *Service {
	return NewService(logger, Definition{
		Key:            "docker",
		DisplayName:    "Docker",
		BinaryNames:    []string{"docker"},
		VersionArgs:    []string{"--version"},
		InstallLabel:   "Install Docker",
		RemoveLabel:    "Remove Docker",
		StartLabel:     "Start Docker",
		StopLabel:      "Stop Docker",
		RestartLabel:   "Restart Docker",
		APTPackages:    []string{"docker.io"},
		APTService:     "docker",
		DNFPackages:    []string{"moby-engine"},
		DNFService:     "docker",
		YUMPackages:    []string{"moby-engine"},
		YUMService:     "docker",
		PacmanPackages: []string{"docker"},
		PacmanService:  "docker",
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
	if err := runCommands(ctx, plan.installCmds...); err != nil {
		if fallback := s.definition.InstallFallback; fallback != nil {
			if handled, fallbackErr := fallback(ctx, plan, err); handled {
				return fallbackErr
			}
		}
		return err
	}

	return runCommands(ctx, plan.postInstallCmds...)
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
				removeCmds:     packageRemoveCommands(definition, "apt", aptPath),
			}
			if definition.Key != "mongodb" || supportsMongoDBAPTInstall() {
				plan.installCmds = [][]string{append([]string{aptPath, "install", "-y"}, definition.APTPackages...)}
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.APTService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.postInstallCmds = appendSystemdInstallServiceCommands(plan.postInstallCmds, systemctlPath, serviceName, definition)
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
				removeCmds:     packageRemoveCommands(definition, "dnf", dnfPath),
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.DNFService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.postInstallCmds = appendSystemdInstallServiceCommands(plan.postInstallCmds, systemctlPath, serviceName, definition)
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
				removeCmds:     packageRemoveCommands(definition, "yum", yumPath),
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.YUMService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.postInstallCmds = appendSystemdInstallServiceCommands(plan.postInstallCmds, systemctlPath, serviceName, definition)
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
				removeCmds:     packageRemoveCommands(definition, "pacman", pacmanPath),
			}
			if systemctlPath, ok := lookupCommand("systemctl"); ok {
				if serviceName := strings.TrimSpace(definition.PacmanService); serviceName != "" {
					plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
					plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
					plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
					plan.postInstallCmds = appendSystemdInstallServiceCommands(plan.postInstallCmds, systemctlPath, serviceName, definition)
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

func packageRemoveCommands(definition Definition, packageManager string, binaryPath string) [][]string {
	if definition.Key == "mongodb" {
		return mongoDBRemoveCommands(packageManager, binaryPath)
	}

	switch packageManager {
	case "apt":
		return [][]string{append([]string{binaryPath, "remove", "-y"}, definition.APTPackages...)}
	case "dnf":
		return [][]string{append([]string{binaryPath, "remove", "-y"}, definition.DNFPackages...)}
	case "yum":
		return [][]string{append([]string{binaryPath, "remove", "-y"}, definition.YUMPackages...)}
	case "pacman":
		return [][]string{append([]string{binaryPath, "-Rns", "--noconfirm"}, definition.PacmanPackages...)}
	default:
		return nil
	}
}

func mongoDBRemoveCommands(packageManager string, binaryPath string) [][]string {
	switch packageManager {
	case "apt":
		return [][]string{
			{
				"sh",
				"-lc",
				fmt.Sprintf(
					`packages="$(
dpkg-query -W -f='${binary:Package}\n' 'mongodb-org*' 'mongodb-mongosh*' 'mongodb-database-tools*' 2>/dev/null | sort -u
)"
if [ -n "$packages" ]; then
  %s purge -y $packages
fi`,
					shellQuote(binaryPath),
				),
			},
			{binaryPath, "autoremove", "-y"},
		}
	case "dnf", "yum":
		return [][]string{
			{
				"sh",
				"-lc",
				fmt.Sprintf(
					`packages="$(
rpm -qa | grep -E '^(mongodb-org|mongodb-mongosh|mongodb-database-tools)' | sort -u
)"
if [ -n "$packages" ]; then
  %s remove -y $packages
fi`,
					shellQuote(binaryPath),
				),
			},
		}
	default:
		return packageRemoveCommands(
			Definition{PacmanPackages: []string{"mongodb"}},
			packageManager,
			binaryPath,
		)
	}
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type osReleaseInfo struct {
	id              string
	idLike          string
	versionCodename string
	ubuntuCodename  string
}

type mongoDBAPTRepository struct {
	keyURL      string
	keyringPath string
	listPath    string
	listEntry   string
}

func retryMongoDBInstall(ctx context.Context, plan actionPlan, err error) (bool, error) {
	if err == nil || plan.packageManager != "apt" || !isMissingAPTPackageError(err) {
		return false, nil
	}

	repository, repoErr := detectMongoDBAPTRepository()
	if repoErr != nil {
		return true, repoErr
	}
	if repoErr := bootstrapMongoDBAPTRepository(ctx, repository); repoErr != nil {
		return true, repoErr
	}

	if err := runCommands(ctx, plan.installCmds...); err != nil {
		return true, err
	}

	return true, runCommands(ctx, plan.postInstallCmds...)
}

func supportsMongoDBAPTInstall() bool {
	_, err := detectMongoDBAPTRepository()
	return err == nil
}

func isMissingAPTPackageError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unable to locate package") ||
		strings.Contains(message, "has no installation candidate")
}

func detectMongoDBAPTRepository() (mongoDBAPTRepository, error) {
	info := parseOSReleaseFile("/etc/os-release")
	codename := firstNonEmpty(info.versionCodename, info.ubuntuCodename)
	if codename == "" {
		return mongoDBAPTRepository{}, errors.New("automatic mongodb installation requires a Linux release codename in /etc/os-release")
	}

	keyringPath := filepath.Join("/usr/share/keyrings", "mongodb-server-"+mongoDBAPTSeries+".gpg")
	listPath := filepath.Join("/etc/apt/sources.list.d", "mongodb-org-"+mongoDBAPTSeries+".list")

	switch {
	case isUbuntuLikeLinux(info):
		if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
			return mongoDBAPTRepository{}, fmt.Errorf("automatic mongodb installation via apt is only supported on Ubuntu 20.04, 22.04, or 24.04 for amd64 and arm64 systems")
		}
		if codename != "focal" && codename != "jammy" && codename != "noble" {
			return mongoDBAPTRepository{}, fmt.Errorf("automatic mongodb installation via apt is only supported on Ubuntu 20.04, 22.04, or 24.04; detected %q", codename)
		}

		return mongoDBAPTRepository{
			keyURL:      "https://pgp.mongodb.com/server-" + mongoDBAPTSeries + ".asc",
			keyringPath: keyringPath,
			listPath:    listPath,
			listEntry: fmt.Sprintf(
				"deb [ arch=amd64,arm64 signed-by=%s ] https://repo.mongodb.org/apt/ubuntu %s/mongodb-org/%s multiverse",
				keyringPath,
				codename,
				mongoDBAPTSeries,
			),
		}, nil
	case isDebianLikeLinux(info):
		if runtime.GOARCH != "amd64" {
			return mongoDBAPTRepository{}, fmt.Errorf("automatic mongodb installation via apt is only supported on Debian 12 x86_64; detected %s", runtime.GOARCH)
		}
		if codename != "bookworm" {
			return mongoDBAPTRepository{}, fmt.Errorf("automatic mongodb installation via apt is only supported on Debian 12; detected %q", codename)
		}

		return mongoDBAPTRepository{
			keyURL:      "https://pgp.mongodb.com/server-" + mongoDBAPTSeries + ".asc",
			keyringPath: keyringPath,
			listPath:    listPath,
			listEntry: fmt.Sprintf(
				"deb [ signed-by=%s ] https://repo.mongodb.org/apt/debian %s/mongodb-org/%s main",
				keyringPath,
				codename,
				mongoDBAPTSeries,
			),
		}, nil
	default:
		return mongoDBAPTRepository{}, errors.New("automatic mongodb installation via apt is only supported on Ubuntu LTS and Debian 12")
	}
}

func bootstrapMongoDBAPTRepository(ctx context.Context, repository mongoDBAPTRepository) error {
	aptPath, ok := lookupCommand("apt-get")
	if !ok {
		return errors.New("apt-get is not available")
	}

	if err := runCommands(ctx, []string{aptPath, "install", "-y", "gnupg"}); err != nil {
		return err
	}

	publicKeyFile, err := downloadFile(ctx, repository.keyURL, "mongodb-server-*.asc")
	if err != nil {
		return err
	}
	defer os.Remove(publicKeyFile)

	gpgPath, ok := lookupCommand("gpg")
	if !ok {
		return errors.New("gpg is not available after installing gnupg")
	}

	if err := os.MkdirAll(filepath.Dir(repository.keyringPath), 0o755); err != nil {
		return fmt.Errorf("create mongodb keyring directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(repository.listPath), 0o755); err != nil {
		return fmt.Errorf("create mongodb sources directory: %w", err)
	}
	if err := os.RemoveAll(repository.keyringPath); err != nil {
		return fmt.Errorf("reset mongodb keyring: %w", err)
	}
	if err := runCommands(ctx, []string{gpgPath, "--dearmor", "-o", repository.keyringPath, publicKeyFile}); err != nil {
		return err
	}
	if err := os.WriteFile(repository.listPath, []byte(repository.listEntry+"\n"), 0o644); err != nil {
		return fmt.Errorf("write mongodb apt source: %w", err)
	}

	return runCommands(ctx, []string{aptPath, "update"})
}

func downloadFile(ctx context.Context, url string, pattern string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("prepare download request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: unexpected status %s", url, response.Status)
	}

	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}

	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", fmt.Errorf("write temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("close temporary file: %w", err)
	}

	return file.Name(), nil
}

func parseOSReleaseFile(path string) osReleaseInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return osReleaseInfo{}
	}

	var info osReleaseInfo
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"'`))

		switch key {
		case "ID":
			info.id = value
		case "ID_LIKE":
			info.idLike = value
		case "VERSION_CODENAME":
			info.versionCodename = value
		case "UBUNTU_CODENAME":
			info.ubuntuCodename = value
		}
	}

	return info
}

func isUbuntuLikeLinux(info osReleaseInfo) bool {
	return info.id == "ubuntu" || stringListContains(strings.Fields(info.idLike), "ubuntu")
}

func isDebianLikeLinux(info osReleaseInfo) bool {
	return info.id == "debian" || stringListContains(strings.Fields(info.idLike), "debian")
}

func stringListContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func appendSystemdInstallServiceCommands(commands [][]string, systemctlPath string, serviceName string, definition Definition) [][]string {
	if definition.EnableOnInstall {
		commands = append(commands, []string{systemctlPath, "enable", serviceName})
	}
	if definition.StartOnInstall {
		commands = append(commands, []string{systemctlPath, "start", serviceName})
	}

	return commands
}
