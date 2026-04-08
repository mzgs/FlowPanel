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
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	dialTimeout          = 500 * time.Millisecond
)

var supportedPHPVersions = []string{
	"8.4",
	"8.3",
	"8.2",
	"8.1",
	"8.0",
}

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

var versionPattern = regexp.MustCompile(`\b(\d+\.\d+)`)

type Manager interface {
	Status(context.Context) Status
	StatusForVersion(context.Context, string) RuntimeStatus
	Install(context.Context) error
	InstallVersion(context.Context, string) error
	Remove(context.Context) error
	RemoveVersion(context.Context, string) error
	Start(context.Context) error
	StartVersion(context.Context, string) error
	Stop(context.Context) error
	StopVersion(context.Context, string) error
	Restart(context.Context) error
	RestartVersion(context.Context, string) error
	UpdateSettings(context.Context, UpdateSettingsInput) (Status, error)
	UpdateSettingsForVersion(context.Context, string, UpdateSettingsInput) (RuntimeStatus, error)
}

type RuntimeStatus struct {
	Version           string   `json:"version"`
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
	RemoveAvailable   bool     `json:"remove_available"`
	RemoveLabel       string   `json:"remove_label,omitempty"`
	StartAvailable    bool     `json:"start_available"`
	StartLabel        string   `json:"start_label,omitempty"`
	StopAvailable     bool     `json:"stop_available,omitempty"`
	StopLabel         string   `json:"stop_label,omitempty"`
	RestartAvailable  bool     `json:"restart_available,omitempty"`
	RestartLabel      string   `json:"restart_label,omitempty"`
	LoadedConfigFile  string   `json:"loaded_config_file,omitempty"`
	ScanDir           string   `json:"scan_dir,omitempty"`
	ManagedConfigFile string   `json:"managed_config_file,omitempty"`
	Settings          Settings `json:"settings"`
}

type Status struct {
	Platform          string          `json:"platform"`
	PackageManager    string          `json:"package_manager,omitempty"`
	DefaultVersion    string          `json:"default_version,omitempty"`
	AvailableVersions []string        `json:"available_versions,omitempty"`
	Versions          []RuntimeStatus `json:"versions,omitempty"`
	PHPInstalled      bool            `json:"php_installed"`
	PHPPath           string          `json:"php_path,omitempty"`
	PHPVersion        string          `json:"php_version,omitempty"`
	FPMInstalled      bool            `json:"fpm_installed"`
	FPMPath           string          `json:"fpm_path,omitempty"`
	ListenAddress     string          `json:"listen_address,omitempty"`
	ServiceRunning    bool            `json:"service_running"`
	Ready             bool            `json:"ready"`
	State             string          `json:"state"`
	Message           string          `json:"message"`
	Issues            []string        `json:"issues,omitempty"`
	InstallAvailable  bool            `json:"install_available"`
	InstallLabel      string          `json:"install_label,omitempty"`
	RemoveAvailable   bool            `json:"remove_available"`
	RemoveLabel       string          `json:"remove_label,omitempty"`
	StartAvailable    bool            `json:"start_available"`
	StartLabel        string          `json:"start_label,omitempty"`
	StopAvailable     bool            `json:"stop_available,omitempty"`
	StopLabel         string          `json:"stop_label,omitempty"`
	RestartAvailable  bool            `json:"restart_available,omitempty"`
	RestartLabel      string          `json:"restart_label,omitempty"`
	LoadedConfigFile  string          `json:"loaded_config_file,omitempty"`
	ScanDir           string          `json:"scan_dir,omitempty"`
	ManagedConfigFile string          `json:"managed_config_file,omitempty"`
	Settings          Settings        `json:"settings"`
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

type versionActionPlan struct {
	packageManager string
	installLabel   string
	removeLabel    string
	startLabel     string
	stopLabel      string
	restartLabel   string
	installCmds    [][]string
	removeCmds     [][]string
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

func SupportedVersions() []string {
	return append([]string(nil), supportedPHPVersions...)
}

func NormalizeVersion(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "PHP "))
	if value == "" {
		return ""
	}
	match := versionPattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return ""
	}
	for _, version := range supportedPHPVersions {
		if match[1] == version {
			return version
		}
	}
	return ""
}

func (s *Service) Status(ctx context.Context) Status {
	availableVersions := SupportedVersions()
	runtimes := make([]RuntimeStatus, 0, len(availableVersions))
	for _, version := range availableVersions {
		runtimes = append(runtimes, s.inspectVersion(ctx, version))
	}

	defaultVersion := detectDefaultVersion(ctx, runtimes)
	if defaultVersion == "" && len(availableVersions) > 0 {
		defaultVersion = availableVersions[0]
	}

	status := Status{
		Platform:          runtime.GOOS,
		AvailableVersions: availableVersions,
		Versions:          runtimes,
		DefaultVersion:    defaultVersion,
	}

	defaultRuntime := findRuntimeStatus(runtimes, defaultVersion)
	if defaultRuntime.Version == "" && len(runtimes) > 0 {
		defaultRuntime = runtimes[0]
	}
	status.PackageManager = defaultRuntime.PackageManager
	copyRuntimeStatus(&status, defaultRuntime)

	return status
}

func (s *Service) StatusForVersion(ctx context.Context, version string) RuntimeStatus {
	version = normalizeRequestedVersion(version)
	if version == "" {
		status := s.Status(ctx)
		version = status.DefaultVersion
		if version == "" && len(status.AvailableVersions) > 0 {
			version = status.AvailableVersions[0]
		}
	}
	if version == "" {
		return RuntimeStatus{
			Platform: runtime.GOOS,
			State:    "unknown",
			Message:  "FlowPanel could not determine which PHP version to use.",
		}
	}

	return s.inspectVersion(ctx, version)
}

func (s *Service) Install(ctx context.Context) error {
	return s.InstallVersion(ctx, "")
}

func (s *Service) InstallVersion(ctx context.Context, version string) error {
	target := s.resolveActionVersion(ctx, version)
	plan := detectVersionActionPlan(target)
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic PHP %s installation is not supported on %s", target, runtime.GOOS)
	}

	s.logger.Info("installing php runtime",
		zap.String("version", target),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.installCmds...)
}

func (s *Service) Remove(ctx context.Context) error {
	return s.RemoveVersion(ctx, "")
}

func (s *Service) RemoveVersion(ctx context.Context, version string) error {
	target := s.resolveActionVersion(ctx, version)
	plan := detectVersionActionPlan(target)
	if len(plan.removeCmds) == 0 {
		return fmt.Errorf("automatic PHP %s removal is not supported on %s", target, runtime.GOOS)
	}

	s.logger.Info("removing php runtime",
		zap.String("version", target),
		zap.String("package_manager", plan.packageManager),
	)

	runtimeStatus := s.StatusForVersion(ctx, target)
	commands := make([][]string, 0, len(plan.stopCmds)+len(plan.removeCmds))
	if runtimeStatus.ServiceRunning {
		commands = append(commands, plan.stopCmds...)
	}
	commands = append(commands, plan.removeCmds...)
	return runCommands(ctx, commands...)
}

func (s *Service) Start(ctx context.Context) error {
	return s.StartVersion(ctx, "")
}

func (s *Service) StartVersion(ctx context.Context, version string) error {
	target := s.resolveActionVersion(ctx, version)
	plan := detectVersionActionPlan(target)
	if len(plan.startCmds) == 0 {
		return fmt.Errorf("automatic php-fpm startup for PHP %s is not supported on %s", target, runtime.GOOS)
	}

	s.logger.Info("starting php-fpm service",
		zap.String("version", target),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.startCmds...)
}

func (s *Service) Stop(ctx context.Context) error {
	return s.StopVersion(ctx, "")
}

func (s *Service) StopVersion(ctx context.Context, version string) error {
	target := s.resolveActionVersion(ctx, version)
	plan := detectVersionActionPlan(target)
	if len(plan.stopCmds) == 0 {
		return fmt.Errorf("automatic php-fpm shutdown for PHP %s is not supported on %s", target, runtime.GOOS)
	}

	s.logger.Info("stopping php-fpm service",
		zap.String("version", target),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.stopCmds...)
}

func (s *Service) Restart(ctx context.Context) error {
	return s.RestartVersion(ctx, "")
}

func (s *Service) RestartVersion(ctx context.Context, version string) error {
	target := s.resolveActionVersion(ctx, version)
	plan := detectVersionActionPlan(target)
	if len(plan.restartCmds) == 0 {
		return fmt.Errorf("automatic php-fpm restart for PHP %s is not supported on %s", target, runtime.GOOS)
	}

	s.logger.Info("restarting php-fpm service",
		zap.String("version", target),
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.restartCmds...)
}

func (s *Service) inspectVersion(ctx context.Context, version string) RuntimeStatus {
	status := RuntimeStatus{
		Version:        version,
		Platform:       runtime.GOOS,
		PackageManager: detectVersionActionPlan(version).packageManager,
	}

	if NormalizeVersion(version) == "" {
		status.State = "unsupported"
		status.Message = "This PHP version is not supported by FlowPanel."
		return status
	}

	phpPath, phpInstalled := lookupVersionedPHPBinary(ctx, version)
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

	fpmPath, fpmInstalled := lookupVersionedPHPFPM(ctx, version)
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

	plan := detectVersionActionPlan(version)
	status.InstallAvailable = len(plan.installCmds) > 0 && (!status.PHPInstalled || !status.FPMInstalled)
	status.InstallLabel = plan.installLabel
	status.RemoveAvailable = len(plan.removeCmds) > 0 && (status.PHPInstalled || status.FPMInstalled)
	status.RemoveLabel = plan.removeLabel
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
		status.Message = fmt.Sprintf("PHP %s and php-fpm are ready for Caddy at %s.", version, status.ListenAddress)
	case !status.PHPInstalled && !status.FPMInstalled:
		status.State = "missing"
		if status.InstallAvailable {
			status.Message = fmt.Sprintf("PHP %s is not installed. Install it here to enable PHP sites on this runtime.", version)
		} else {
			status.Message = fmt.Sprintf("PHP %s is not installed on this server.", version)
		}
	case status.PHPInstalled && !status.FPMInstalled:
		status.State = "missing-fpm"
		if status.InstallAvailable {
			status.Message = fmt.Sprintf("PHP %s CLI is installed, but php-fpm is missing. Reinstall PHP %s to add FastCGI support.", version, version)
		} else {
			status.Message = fmt.Sprintf("PHP %s CLI is installed, but php-fpm is missing.", version)
		}
	case status.FPMInstalled && status.ListenAddress == "":
		status.State = "misconfigured"
		status.Message = fmt.Sprintf("PHP %s php-fpm is installed, but its listen address could not be determined.", version)
	case status.FPMInstalled && !status.ServiceRunning:
		status.State = "stopped"
		status.Message = fmt.Sprintf("PHP %s is installed, but php-fpm is not reachable at %s.", version, status.ListenAddress)
	default:
		status.State = "unknown"
		status.Message = fmt.Sprintf("FlowPanel could not determine the PHP %s runtime state.", version)
	}

	return status
}

func (s *Service) resolveActionVersion(ctx context.Context, version string) string {
	version = normalizeRequestedVersion(version)
	if version != "" {
		return version
	}
	status := s.Status(ctx)
	if status.DefaultVersion != "" {
		return status.DefaultVersion
	}
	if len(status.AvailableVersions) > 0 {
		return status.AvailableVersions[0]
	}
	return preferredInstallVersion()
}

func detectDefaultVersion(ctx context.Context, runtimes []RuntimeStatus) string {
	if phpPath, ok := lookupCommand("php"); ok {
		if output, err := runInspectCommand(ctx, phpPath, "-v"); err == nil {
			if version := NormalizeVersion(parsePHPVersion(output)); version != "" {
				return version
			}
		}
	}

	for _, runtimeStatus := range runtimes {
		if runtimeStatus.PHPInstalled || runtimeStatus.FPMInstalled {
			return runtimeStatus.Version
		}
	}

	return ""
}

func findRuntimeStatus(runtimes []RuntimeStatus, version string) RuntimeStatus {
	for _, runtimeStatus := range runtimes {
		if runtimeStatus.Version == version {
			return runtimeStatus
		}
	}
	return RuntimeStatus{}
}

func copyRuntimeStatus(target *Status, runtimeStatus RuntimeStatus) {
	if target == nil {
		return
	}
	target.PHPInstalled = runtimeStatus.PHPInstalled
	target.PHPPath = runtimeStatus.PHPPath
	target.PHPVersion = runtimeStatus.PHPVersion
	target.FPMInstalled = runtimeStatus.FPMInstalled
	target.FPMPath = runtimeStatus.FPMPath
	target.ListenAddress = runtimeStatus.ListenAddress
	target.ServiceRunning = runtimeStatus.ServiceRunning
	target.Ready = runtimeStatus.Ready
	target.State = runtimeStatus.State
	target.Message = runtimeStatus.Message
	target.Issues = append([]string(nil), runtimeStatus.Issues...)
	target.InstallAvailable = runtimeStatus.InstallAvailable
	target.InstallLabel = runtimeStatus.InstallLabel
	target.RemoveAvailable = runtimeStatus.RemoveAvailable
	target.RemoveLabel = runtimeStatus.RemoveLabel
	target.StartAvailable = runtimeStatus.StartAvailable
	target.StartLabel = runtimeStatus.StartLabel
	target.StopAvailable = runtimeStatus.StopAvailable
	target.StopLabel = runtimeStatus.StopLabel
	target.RestartAvailable = runtimeStatus.RestartAvailable
	target.RestartLabel = runtimeStatus.RestartLabel
	target.LoadedConfigFile = runtimeStatus.LoadedConfigFile
	target.ScanDir = runtimeStatus.ScanDir
	target.ManagedConfigFile = runtimeStatus.ManagedConfigFile
	target.Settings = runtimeStatus.Settings
}

func normalizeRequestedVersion(version string) string {
	normalized := NormalizeVersion(version)
	if normalized != "" {
		return normalized
	}
	return ""
}

func preferredInstallVersion() string {
	if len(supportedPHPVersions) == 0 {
		return ""
	}
	return supportedPHPVersions[0]
}

func detectVersionActionPlan(version string) versionActionPlan {
	version = normalizeRequestedVersion(version)

	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			formula := brewFormulaForVersion(version)
			return versionActionPlan{
				packageManager: "homebrew",
				installLabel:   fmt.Sprintf("Install PHP %s", version),
				removeLabel:    fmt.Sprintf("Remove PHP %s", version),
				startLabel:     fmt.Sprintf("Start PHP %s FPM", version),
				stopLabel:      fmt.Sprintf("Stop PHP %s FPM", version),
				restartLabel:   fmt.Sprintf("Restart PHP %s FPM", version),
				installCmds: [][]string{
					{brewPath, "install", formula},
				},
				removeCmds: [][]string{
					{brewPath, "uninstall", formula},
				},
				startCmds: [][]string{
					{brewPath, "services", "start", formula},
				},
				stopCmds: [][]string{
					{brewPath, "services", "stop", formula},
				},
				restartCmds: [][]string{
					{brewPath, "services", "restart", formula},
				},
			}
		}
	case "linux":
		if os.Geteuid() != 0 {
			return versionActionPlan{}
		}
		if aptPath, ok := lookupCommand("apt-get"); ok {
			packages := aptVersionPackages(version)
			installArgs := append([]string{aptPath, "install", "-y"}, packages...)
			removeArgs := append([]string{aptPath, "remove", "-y"}, packages...)
			serviceName := "php" + version + "-fpm"
			systemctlPath, hasSystemctl := lookupCommand("systemctl")
			servicePath, hasService := lookupCommand("service")

			plan := versionActionPlan{
				packageManager: "apt",
				installLabel:   fmt.Sprintf("Install PHP %s", version),
				removeLabel:    fmt.Sprintf("Remove PHP %s", version),
				startLabel:     fmt.Sprintf("Start PHP %s FPM", version),
				stopLabel:      fmt.Sprintf("Stop PHP %s FPM", version),
				restartLabel:   fmt.Sprintf("Restart PHP %s FPM", version),
				installCmds: [][]string{
					{aptPath, "update"},
					installArgs,
				},
				removeCmds: [][]string{
					removeArgs,
				},
			}
			if hasSystemctl {
				plan.startCmds = [][]string{{systemctlPath, "start", serviceName}}
				plan.stopCmds = [][]string{{systemctlPath, "stop", serviceName}}
				plan.restartCmds = [][]string{{systemctlPath, "restart", serviceName}}
			} else if hasService {
				plan.startCmds = [][]string{{servicePath, serviceName, "start"}}
				plan.stopCmds = [][]string{{servicePath, serviceName, "stop"}}
				plan.restartCmds = [][]string{{servicePath, serviceName, "restart"}}
			}
			return plan
		}
	}

	return versionActionPlan{}
}

func aptVersionPackages(version string) []string {
	prefix := "php" + version
	return []string{
		prefix + "-fpm",
		prefix + "-cli",
		prefix + "-common",
		prefix + "-opcache",
		prefix + "-bcmath",
		prefix + "-mysql",
		prefix + "-curl",
		prefix + "-gd",
		prefix + "-intl",
		prefix + "-imagick",
		prefix + "-mbstring",
		prefix + "-xml",
		prefix + "-zip",
	}
}

func brewFormulaForVersion(version string) string {
	if version == "" {
		return "php"
	}
	return "php@" + version
}

func lookupVersionedPHPBinary(ctx context.Context, version string) (string, bool) {
	for _, candidate := range phpBinaryCandidates(version) {
		if path, ok := lookupCandidateExecutable(candidate); ok && binaryMatchesVersion(ctx, path, version) {
			return path, true
		}
	}
	return "", false
}

func lookupVersionedPHPFPM(ctx context.Context, version string) (string, bool) {
	for _, candidate := range fpmBinaryCandidates(version) {
		if path, ok := lookupCandidateExecutable(candidate); ok && binaryMatchesVersion(ctx, path, version) {
			return path, true
		}
	}
	return "", false
}

func phpBinaryCandidates(version string) []string {
	candidates := []string{
		"php" + version,
		filepath.Join("/usr/bin", "php"+version),
		filepath.Join("/usr/local/bin", "php"+version),
		filepath.Join("/opt/homebrew/opt", brewFormulaForVersion(version), "bin", "php"),
		filepath.Join("/usr/local/opt", brewFormulaForVersion(version), "bin", "php"),
	}
	candidates = append(candidates,
		"php",
		filepath.Join("/opt/homebrew/bin", "php"),
		filepath.Join("/usr/local/bin", "php"),
	)
	return dedupeStrings(candidates)
}

func fpmBinaryCandidates(version string) []string {
	candidates := []string{
		"php-fpm" + version,
		"php" + version + "-fpm",
		filepath.Join("/usr/sbin", "php-fpm"+version),
		filepath.Join("/usr/sbin", "php"+version+"-fpm"),
		filepath.Join("/usr/local/sbin", "php-fpm"+version),
		filepath.Join("/usr/local/sbin", "php"+version+"-fpm"),
		filepath.Join("/opt/homebrew/opt", brewFormulaForVersion(version), "sbin", "php-fpm"),
		filepath.Join("/usr/local/opt", brewFormulaForVersion(version), "sbin", "php-fpm"),
	}
	candidates = append(candidates,
		"php-fpm",
		filepath.Join("/opt/homebrew/sbin", "php-fpm"),
		filepath.Join("/usr/local/sbin", "php-fpm"),
		filepath.Join("/usr/sbin", "php-fpm"),
	)
	return dedupeStrings(candidates)
}

func dedupeStrings(values []string) []string {
	deduped := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	return deduped
}

func lookupCandidateExecutable(candidate string) (string, bool) {
	if strings.Contains(candidate, string(os.PathSeparator)) {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			return "", false
		}
		return candidate, true
	}
	return lookupCommand(candidate)
}

func binaryMatchesVersion(ctx context.Context, path, version string) bool {
	output, err := runInspectCommand(ctx, path, "-v")
	if err != nil {
		return false
	}
	return NormalizeVersion(parsePHPVersion(output)) == NormalizeVersion(version)
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
