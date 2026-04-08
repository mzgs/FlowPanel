package phpenv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	dialTimeout          = 500 * time.Millisecond
)

var aptPHPPackages = []string{
	"php-fpm",
	"php-cli",
	"php-common",
	"php-opcache",
	"php-bcmath",
	"php-mysql",
	"php-curl",
	"php-gd",
	"php-intl",
	"php-imagick",
	"php-mbstring",
	"php-xml",
	"php-zip",
}

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	UpdateSettings(context.Context, UpdateSettingsInput) (Status, error)
}

type Status struct {
	Platform          string   `json:"platform"`
	PackageManager    string   `json:"package_manager,omitempty"`
	PHPInstalled      bool     `json:"php_installed"`
	PHPPath           string   `json:"php_path,omitempty"`
	PHPVersion        string   `json:"php_version,omitempty"`
	FPMInstalled      bool     `json:"fpm_installed"`
	FPMPath           string   `json:"fpm_path,omitempty"`
	ListenAddress     string   `json:"listen_address,omitempty"`
	ServiceRunning    bool     `json:"service_running"`
	Ready             bool     `json:"ready"`
	State             string   `json:"state"`
	Message           string   `json:"message"`
	Issues            []string `json:"issues,omitempty"`
	InstallAvailable  bool     `json:"install_available"`
	InstallLabel      string   `json:"install_label,omitempty"`
	StartAvailable    bool     `json:"start_available"`
	StartLabel        string   `json:"start_label,omitempty"`
	StopAvailable     bool     `json:"stop_available"`
	StopLabel         string   `json:"stop_label,omitempty"`
	RestartAvailable  bool     `json:"restart_available"`
	RestartLabel      string   `json:"restart_label,omitempty"`
	LoadedConfigFile  string   `json:"loaded_config_file,omitempty"`
	ScanDir           string   `json:"scan_dir,omitempty"`
	ManagedConfigFile string   `json:"managed_config_file,omitempty"`
	Settings          Settings `json:"settings"`
}

type Settings struct {
	MaxExecutionTime     string `json:"max_execution_time,omitempty"`
	MaxInputTime         string `json:"max_input_time,omitempty"`
	MemoryLimit          string `json:"memory_limit,omitempty"`
	PostMaxSize          string `json:"post_max_size,omitempty"`
	FileUploads          string `json:"file_uploads,omitempty"`
	UploadMaxFilesize    string `json:"upload_max_filesize,omitempty"`
	MaxFileUploads       string `json:"max_file_uploads,omitempty"`
	DefaultSocketTimeout string `json:"default_socket_timeout,omitempty"`
	ErrorReporting       string `json:"error_reporting,omitempty"`
	DisplayErrors        string `json:"display_errors,omitempty"`
}

type UpdateSettingsInput struct {
	MaxExecutionTime     string `json:"max_execution_time"`
	MaxInputTime         string `json:"max_input_time"`
	MemoryLimit          string `json:"memory_limit"`
	PostMaxSize          string `json:"post_max_size"`
	FileUploads          string `json:"file_uploads"`
	UploadMaxFilesize    string `json:"upload_max_filesize"`
	MaxFileUploads       string `json:"max_file_uploads"`
	DefaultSocketTimeout string `json:"default_socket_timeout"`
	ErrorReporting       string `json:"error_reporting"`
	DisplayErrors        string `json:"display_errors"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "php settings validation failed"
}

type Service struct {
	logger *zap.Logger
}

type actionPlan struct {
	packageManager string
	installLabel   string
	startLabel     string
	stopLabel      string
	restartLabel   string
	installCmds    [][]string
	startCmds      [][]string
	stopCmds       [][]string
	restartCmds    [][]string
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		Platform: runtime.GOOS,
	}

	plan := detectActionPlan()
	status.PackageManager = plan.packageManager

	phpPath, phpInstalled := lookupCommand("php")
	if phpInstalled {
		status.PHPInstalled = true
		status.PHPPath = phpPath
		if output, err := runInspectCommand(ctx, phpPath, "-v"); err == nil {
			status.PHPVersion = parsePHPVersion(output)
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
		configInfo, err := inspectPHPConfig(ctx, phpPath)
		if err == nil {
			status.LoadedConfigFile = configInfo.loadedConfigFile
			status.ScanDir = configInfo.scanDir
			status.ManagedConfigFile = configInfo.managedConfigFile
			status.Settings = configInfo.settings
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	fpmPath, fpmInstalled := lookupPHPFPM()
	if fpmInstalled {
		status.FPMInstalled = true
		status.FPMPath = fpmPath

		output, err := runInspectCommand(ctx, fpmPath, "-tt")
		if err != nil {
			status.Issues = append(status.Issues, err.Error())
		}
		status.ListenAddress = parseFPMListenAddress(output)
		if status.ListenAddress == "" {
			status.Issues = append(status.Issues, "FlowPanel could not determine the php-fpm listen address.")
		} else {
			status.ServiceRunning = canDialFastCGI(status.ListenAddress)
		}
	}

	status.InstallAvailable = len(plan.installCmds) > 0 && (!status.PHPInstalled || !status.FPMInstalled)
	status.InstallLabel = plan.installLabel
	status.StartAvailable = len(plan.startCmds) > 0 && status.FPMInstalled && !status.ServiceRunning
	status.StartLabel = plan.startLabel
	status.StopAvailable = len(plan.stopCmds) > 0 && status.FPMInstalled && status.ServiceRunning
	status.StopLabel = plan.stopLabel
	status.RestartAvailable = len(plan.restartCmds) > 0 && status.FPMInstalled && status.ServiceRunning
	status.RestartLabel = plan.restartLabel

	switch {
	case status.PHPInstalled && status.FPMInstalled && status.ListenAddress != "" && status.ServiceRunning:
		status.Ready = true
		status.State = "ready"
		status.Message = fmt.Sprintf("PHP and php-fpm are ready for Caddy at %s.", status.ListenAddress)
	case !status.PHPInstalled && !status.FPMInstalled:
		status.State = "missing"
		if status.InstallAvailable {
			status.Message = "PHP is not installed. Install it here to enable Php site domains."
		} else {
			status.Message = "PHP is not installed on this server."
		}
	case status.PHPInstalled && !status.FPMInstalled:
		status.State = "missing-fpm"
		if status.InstallAvailable {
			status.Message = "PHP CLI is installed, but php-fpm is missing. Reinstall PHP to add FastCGI support."
		} else {
			status.Message = "PHP CLI is installed, but php-fpm is missing."
		}
	case status.FPMInstalled && status.ListenAddress == "":
		status.State = "misconfigured"
		status.Message = "php-fpm is installed, but its listen address could not be determined."
	case status.FPMInstalled && !status.ServiceRunning:
		status.State = "stopped"
		if status.StartAvailable {
			status.Message = fmt.Sprintf("PHP is installed, but php-fpm is not running on %s.", status.ListenAddress)
		} else {
			status.Message = fmt.Sprintf("PHP is installed, but php-fpm is not reachable at %s.", status.ListenAddress)
		}
	default:
		status.State = "unknown"
		status.Message = "FlowPanel could not determine the PHP runtime state."
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic PHP installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing php runtime",
		zap.String("package_manager", plan.packageManager),
	)
	if err := runCommands(ctx, append(plan.installCmds, plan.startCmds...)...); err != nil {
		return err
	}

	return nil
}

func (s *Service) Start(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.startCmds) == 0 {
		return fmt.Errorf("automatic php-fpm startup is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("starting php-fpm service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.startCmds...)
}

func (s *Service) Stop(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.stopCmds) == 0 {
		return fmt.Errorf("automatic php-fpm shutdown is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("stopping php-fpm service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.stopCmds...)
}

func (s *Service) Restart(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.restartCmds) == 0 {
		return fmt.Errorf("automatic php-fpm restart is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("restarting php-fpm service",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.restartCmds...)
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install PHP",
				startLabel:     "Start PHP-FPM",
				stopLabel:      "Stop PHP-FPM",
				restartLabel:   "Restart PHP-FPM",
				installCmds: [][]string{
					{brewPath, "install", "php"},
				},
				startCmds: [][]string{
					{brewPath, "services", "start", "php"},
				},
				stopCmds: [][]string{
					{brewPath, "services", "stop", "php"},
				},
				restartCmds: [][]string{
					{brewPath, "services", "restart", "php"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			if aptPath, ok := lookupCommand("apt-get"); ok {
				installArgs := append([]string{aptPath, "install", "-y"}, aptPHPPackages...)
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install PHP",
					installCmds: [][]string{
						{aptPath, "update"},
						installArgs,
					},
				}
			}
			if dnfPath, ok := lookupCommand("dnf"); ok {
				return actionPlan{
					packageManager: "dnf",
					installLabel:   "Install PHP",
					installCmds: [][]string{
						{dnfPath, "install", "-y", "php", "php-fpm"},
					},
				}
			}
			if yumPath, ok := lookupCommand("yum"); ok {
				return actionPlan{
					packageManager: "yum",
					installLabel:   "Install PHP",
					installCmds: [][]string{
						{yumPath, "install", "-y", "php", "php-fpm"},
					},
				}
			}
			if pacmanPath, ok := lookupCommand("pacman"); ok {
				return actionPlan{
					packageManager: "pacman",
					installLabel:   "Install PHP",
					installCmds: [][]string{
						{pacmanPath, "-Sy", "--noconfirm", "php", "php-fpm"},
					},
				}
			}
		}
	}

	return actionPlan{}
}

func lookupPHPFPM() (string, bool) {
	for _, candidate := range []string{
		"php-fpm",
		"php-fpm8.4",
		"php-fpm8.3",
		"php-fpm8.2",
		"php-fpm8.1",
		"php-fpm8.0",
	} {
		if path, ok := lookupCommand(candidate); ok {
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
	inspectCtx := ctx
	if inspectCtx == nil {
		inspectCtx = context.Background()
	}

	if _, ok := inspectCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		inspectCtx, cancel = context.WithTimeout(inspectCtx, statusCommandTimeout)
		defer cancel()
	}

	return runCommand(inspectCtx, name, args...)
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

func parsePHPVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "PHP ") {
			return line
		}
	}

	return ""
}

func parseFPMListenAddress(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "listen =") {
			continue
		}

		parts := strings.SplitN(line, "listen =", 2)
		if len(parts) != 2 {
			continue
		}

		address := strings.TrimSpace(parts[1])
		address = strings.TrimPrefix(address, "unix:")
		address = strings.Trim(address, `"`)
		if address != "" {
			return address
		}
	}

	return ""
}

func canDialFastCGI(address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}

	network := "tcp"
	if strings.HasPrefix(address, "/") {
		network = "unix"
	}

	conn, err := net.DialTimeout(network, address, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}
