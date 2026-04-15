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
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	dialTimeout          = 500 * time.Millisecond
)

var supportedPHPVersions = []string{
	"8.5",
	"8.4",
	"8.3",
	"8.2",
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
	WorkerIdentity(context.Context, string) (WorkerIdentity, error)
	Install(context.Context) error
	InstallVersion(context.Context, string) error
	InstallExtension(context.Context, string) (Status, error)
	InstallExtensionForVersion(context.Context, string, string) (RuntimeStatus, error)
	Remove(context.Context) error
	RemoveVersion(context.Context, string) error
	Start(context.Context) error
	StartVersion(context.Context, string) error
	Stop(context.Context) error
	StopVersion(context.Context, string) error
	Restart(context.Context) error
	RestartVersion(context.Context, string) error
	ReadManagedConfigForVersion(context.Context, string) (ManagedConfig, error)
	UpdateManagedConfigForVersion(context.Context, string, string) (RuntimeStatus, error)
	UpdateSettings(context.Context, UpdateSettingsInput) (Status, error)
	UpdateSettingsForVersion(context.Context, string, UpdateSettingsInput) (RuntimeStatus, error)
}

type ManagedConfig struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WorkerIdentity struct {
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
}

type RuntimeStatus struct {
	Version           string   `json:"version"`
	Platform          string   `json:"platform"`
	PackageManager    string   `json:"package_manager,omitempty"`
	PHPInstalled      bool     `json:"php_installed"`
	PHPPath           string   `json:"php_path,omitempty"`
	PHPVersion        string   `json:"php_version,omitempty"`
	Extensions        []string `json:"extensions,omitempty"`
	extensionDir      string
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
	Platform          string                     `json:"platform"`
	PackageManager    string                     `json:"package_manager,omitempty"`
	DefaultVersion    string                     `json:"default_version,omitempty"`
	AvailableVersions []string                   `json:"available_versions,omitempty"`
	Versions          []RuntimeStatus            `json:"versions,omitempty"`
	ExtensionCatalog  []PHPExtensionCatalogEntry `json:"extension_catalog,omitempty"`
	PHPInstalled      bool                       `json:"php_installed"`
	PHPPath           string                     `json:"php_path,omitempty"`
	PHPVersion        string                     `json:"php_version,omitempty"`
	Extensions        []string                   `json:"extensions,omitempty"`
	FPMInstalled      bool                       `json:"fpm_installed"`
	FPMPath           string                     `json:"fpm_path,omitempty"`
	ListenAddress     string                     `json:"listen_address,omitempty"`
	ServiceRunning    bool                       `json:"service_running"`
	Ready             bool                       `json:"ready"`
	State             string                     `json:"state"`
	Message           string                     `json:"message"`
	Issues            []string                   `json:"issues,omitempty"`
	InstallAvailable  bool                       `json:"install_available"`
	InstallLabel      string                     `json:"install_label,omitempty"`
	RemoveAvailable   bool                       `json:"remove_available"`
	RemoveLabel       string                     `json:"remove_label,omitempty"`
	StartAvailable    bool                       `json:"start_available"`
	StartLabel        string                     `json:"start_label,omitempty"`
	StopAvailable     bool                       `json:"stop_available,omitempty"`
	StopLabel         string                     `json:"stop_label,omitempty"`
	RestartAvailable  bool                       `json:"restart_available,omitempty"`
	RestartLabel      string                     `json:"restart_label,omitempty"`
	LoadedConfigFile  string                     `json:"loaded_config_file,omitempty"`
	ScanDir           string                     `json:"scan_dir,omitempty"`
	ManagedConfigFile string                     `json:"managed_config_file,omitempty"`
	Settings          Settings                   `json:"settings"`
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
	DisableFunctions     string `json:"disable_functions,omitempty"`
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
	DisableFunctions     string `json:"disable_functions"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "php settings validation failed"
}

type Service struct {
	logger                 *zap.Logger
	defaultVersionResolver func(context.Context, Status) string
}

type versionActionPlan struct {
	packageManager string
	installLabel   string
	removeLabel    string
	startLabel     string
	stopLabel      string
	restartLabel   string
	installCmds    [][]string
	composerCmds   [][]string
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

func (s *Service) SetDefaultVersionResolver(fn func(context.Context, Status) string) {
	if s == nil {
		return
	}

	s.defaultVersionResolver = fn
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
		ExtensionCatalog:  PHPExtensionCatalog(),
	}
	if resolved := s.resolvePreferredDefaultVersion(ctx, status); resolved != "" {
		status.DefaultVersion = resolved
	}

	defaultRuntime := findRuntimeStatus(runtimes, status.DefaultVersion)
	if defaultRuntime.Version == "" && len(runtimes) > 0 {
		defaultRuntime = runtimes[0]
	}
	status.PackageManager = defaultRuntime.PackageManager
	copyRuntimeStatus(&status, defaultRuntime)

	return status
}

func (s *Service) resolvePreferredDefaultVersion(ctx context.Context, status Status) string {
	if s == nil || s.defaultVersionResolver == nil {
		return ""
	}

	candidate := NormalizeVersion(s.defaultVersionResolver(ctx, status))
	if candidate == "" {
		return ""
	}
	for _, version := range status.AvailableVersions {
		if version == candidate {
			return candidate
		}
	}

	return ""
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

func (s *Service) WorkerIdentity(ctx context.Context, version string) (WorkerIdentity, error) {
	runtimeStatus := s.StatusForVersion(ctx, version)
	if !runtimeStatus.FPMInstalled || strings.TrimSpace(runtimeStatus.FPMPath) == "" {
		return WorkerIdentity{}, fmt.Errorf("php-fpm is not configured for PHP %s", s.resolveActionVersion(ctx, version))
	}

	output, err := runInspectCommand(ctx, runtimeStatus.FPMPath, "-tt")
	if err != nil {
		return WorkerIdentity{}, fmt.Errorf("inspect php-fpm worker identity: %w", err)
	}

	identity := parseFPMWorkerIdentity(output)
	if identity.User == "" {
		return WorkerIdentity{}, fmt.Errorf("php-fpm worker user is not configured for PHP %s", runtimeStatus.Version)
	}
	if identity.Group == "" {
		identity.Group = identity.User
	}

	return identity, nil
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
	if err := runCommands(ctx, plan.installCmds...); err != nil {
		if shouldRetryAPTInstallWithOndrej(plan, err) {
			s.logger.Info("retrying php install after bootstrapping ondrej/php repository",
				zap.String("version", target),
			)
			if bootstrapErr := bootstrapOndrejPHPRepository(ctx); bootstrapErr != nil {
				return fmt.Errorf("bootstrap ondrej/php repository: %w", bootstrapErr)
			}
			if err := runCommands(ctx, plan.installCmds...); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return s.installComposerIfMissing(ctx, target, plan)
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
			status.extensionDir = configInfo.extensionDir
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
		if output, err := runInspectCommand(ctx, phpPath, "-m"); err == nil {
			status.Extensions = parsePHPExtensions(output)
		} else {
			status.Issues = append(status.Issues, fmt.Sprintf("inspect php extensions: %v", err))
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
	target.Extensions = append([]string(nil), runtimeStatus.Extensions...)
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

func shouldRetryAPTInstallWithOndrej(plan versionActionPlan, err error) bool {
	if err == nil || runtime.GOOS != "linux" || plan.packageManager != "apt" {
		return false
	}
	if !isUbuntuLikeLinux() {
		return false
	}

	return isMissingAPTPackageError(err)
}

func isMissingAPTPackageError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unable to locate package") ||
		strings.Contains(message, "has no installation candidate")
}

func bootstrapOndrejPHPRepository(ctx context.Context) error {
	aptPath, ok := lookupCommand("apt-get")
	if !ok {
		return errors.New("apt-get is not available")
	}

	return runCommands(
		ctx,
		[]string{aptPath, "update"},
		[]string{aptPath, "install", "-y", "software-properties-common"},
		[]string{"add-apt-repository", "-y", "ppa:ondrej/php"},
		[]string{aptPath, "update"},
	)
}

func isUbuntuLikeLinux() bool {
	info := parseOSReleaseFile("/etc/os-release")
	if info.id == "ubuntu" {
		return true
	}

	for _, item := range strings.Fields(info.idLike) {
		if item == "ubuntu" {
			return true
		}
	}

	return false
}

type osReleaseInfo struct {
	id     string
	idLike string
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
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		switch key {
		case "ID":
			info.id = strings.ToLower(value)
		case "ID_LIKE":
			info.idLike = strings.ToLower(value)
		}
	}

	return info
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
				composerCmds: [][]string{
					{brewPath, "install", "composer"},
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
				composerCmds: [][]string{
					{aptPath, "install", "-y", "composer"},
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
		if dnfPath, ok := lookupCommand("dnf"); ok {
			packages := rpmVersionPackages(version)
			installArgs := append([]string{dnfPath, "install", "-y"}, packages...)
			removeArgs := append([]string{dnfPath, "remove", "-y"}, packages...)
			serviceName := remiFPMServiceName(version)
			systemctlPath, hasSystemctl := lookupCommand("systemctl")
			servicePath, hasService := lookupCommand("service")

			plan := versionActionPlan{
				packageManager: "dnf",
				installLabel:   fmt.Sprintf("Install PHP %s", version),
				removeLabel:    fmt.Sprintf("Remove PHP %s", version),
				startLabel:     fmt.Sprintf("Start PHP %s FPM", version),
				stopLabel:      fmt.Sprintf("Stop PHP %s FPM", version),
				restartLabel:   fmt.Sprintf("Restart PHP %s FPM", version),
				installCmds: [][]string{
					installArgs,
				},
				composerCmds: [][]string{
					{dnfPath, "install", "-y", "composer"},
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
		if yumPath, ok := lookupCommand("yum"); ok {
			packages := rpmVersionPackages(version)
			installArgs := append([]string{yumPath, "install", "-y"}, packages...)
			removeArgs := append([]string{yumPath, "remove", "-y"}, packages...)
			serviceName := remiFPMServiceName(version)
			systemctlPath, hasSystemctl := lookupCommand("systemctl")
			servicePath, hasService := lookupCommand("service")

			plan := versionActionPlan{
				packageManager: "yum",
				installLabel:   fmt.Sprintf("Install PHP %s", version),
				removeLabel:    fmt.Sprintf("Remove PHP %s", version),
				startLabel:     fmt.Sprintf("Start PHP %s FPM", version),
				stopLabel:      fmt.Sprintf("Stop PHP %s FPM", version),
				restartLabel:   fmt.Sprintf("Restart PHP %s FPM", version),
				installCmds: [][]string{
					installArgs,
				},
				composerCmds: [][]string{
					{yumPath, "install", "-y", "composer"},
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

func (s *Service) installComposerIfMissing(ctx context.Context, version string, plan versionActionPlan) error {
	if _, ok := lookupCommand("composer"); ok {
		return nil
	}
	if len(plan.composerCmds) == 0 {
		return fmt.Errorf("php %s was installed, but automatic Composer installation is not supported on %s", version, runtime.GOOS)
	}

	s.logger.Info("installing composer",
		zap.String("version", version),
		zap.String("package_manager", plan.packageManager),
	)
	if err := runCommands(ctx, plan.composerCmds...); err != nil {
		return err
	}
	if _, ok := lookupCommand("composer"); ok {
		return nil
	}

	return fmt.Errorf("php %s was installed, but composer is still unavailable", version)
}

func aptVersionPackages(version string) []string {
	prefix := "php" + version
	packages := []string{
		prefix + "-fpm",
		prefix + "-cli",
		prefix + "-common",
		prefix + "-bcmath",
		prefix + "-mysql",
		prefix + "-sqlite3",
		prefix + "-curl",
		prefix + "-gd",
		prefix + "-intl",
		prefix + "-imagick",
		prefix + "-mbstring",
		prefix + "-xml",
		prefix + "-zip",
	}

	if phpVersionHasSeparateOpcachePackage(version) {
		packages = append(packages[:3], append([]string{prefix + "-opcache"}, packages[3:]...)...)
	}

	return packages
}

func rpmVersionPackages(version string) []string {
	prefix := remiCollectionForVersion(version) + "-php"
	packages := []string{
		prefix + "-fpm",
		prefix + "-cli",
		prefix + "-common",
		prefix + "-bcmath",
		prefix + "-mysqlnd",
		prefix + "-sqlite3",
		prefix + "-curl",
		prefix + "-gd",
		prefix + "-intl",
		prefix + "-mbstring",
		prefix + "-xml",
		prefix + "-process",
	}

	if phpVersionHasSeparateOpcachePackage(version) {
		packages = append(packages[:3], append([]string{prefix + "-opcache"}, packages[3:]...)...)
	}

	return packages
}

func phpVersionHasSeparateOpcachePackage(version string) bool {
	parts := strings.Split(NormalizeVersion(version), ".")
	if len(parts) != 2 {
		return true
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return true
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return true
	}

	if major != 8 {
		return true
	}

	return minor < 5
}

func brewFormulaForVersion(version string) string {
	if version == "" {
		return "php"
	}
	return "php@" + version
}

func remiCollectionForVersion(version string) string {
	return "php" + strings.ReplaceAll(version, ".", "")
}

func remiFPMServiceName(version string) string {
	return remiCollectionForVersion(version) + "-php-fpm"
}

func lookupVersionedPHPBinary(ctx context.Context, version string) (string, bool) {
	for _, candidate := range phpBinaryCandidates(version) {
		if path, ok := lookupCandidateExecutable(candidate); ok {
			if binaryMatchesVersion(ctx, path, version) || executablePathImpliesVersion(path, version) {
				return path, true
			}
		}
	}
	return "", false
}

func lookupVersionedPHPFPM(ctx context.Context, version string) (string, bool) {
	for _, candidate := range fpmBinaryCandidates(version) {
		if path, ok := lookupCandidateExecutable(candidate); ok {
			if binaryMatchesVersion(ctx, path, version) || executablePathImpliesVersion(path, version) {
				return path, true
			}
		}
	}
	return "", false
}

func executablePathImpliesVersion(path, version string) bool {
	version = NormalizeVersion(version)
	if version == "" {
		return false
	}

	normalizedPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if normalizedPath == "" {
		return false
	}

	versionDigits := strings.ReplaceAll(version, ".", "")
	return strings.Contains(normalizedPath, "php"+version) ||
		strings.Contains(normalizedPath, "php@"+version) ||
		strings.Contains(normalizedPath, "php"+versionDigits) ||
		strings.Contains(normalizedPath, "php-fpm"+version) ||
		strings.Contains(normalizedPath, "php-fpm"+versionDigits)
}

func phpBinaryCandidates(version string) []string {
	candidates := []string{
		"php" + version,
		filepath.Join("/usr/bin", "php"+version),
		filepath.Join("/usr/local/bin", "php"+version),
		filepath.Join("/opt/remi", remiCollectionForVersion(version), "root", "usr", "bin", "php"),
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
		filepath.Join("/opt/remi", remiCollectionForVersion(version), "root", "usr", "sbin", "php-fpm"),
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
	return runCommandWithOptions(ctx, "", nil, name, args...)
}

func runCommandInDir(ctx context.Context, dir string, name string, args ...string) (string, error) {
	return runCommandWithOptions(ctx, dir, nil, name, args...)
}

func runCommandWithOptions(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	if strings.TrimSpace(dir) != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
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

func parsePHPExtensions(output string) []string {
	extensions := make([]string, 0)
	seen := make(map[string]struct{})

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		extensions = append(extensions, line)
	}

	return extensions
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

func parseFPMWorkerIdentity(output string) WorkerIdentity {
	identity := WorkerIdentity{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case identity.User == "" && strings.Contains(line, "user ="):
			identity.User = parseFPMDirectiveValue(line, "user =")
		case identity.Group == "" && strings.Contains(line, "group ="):
			identity.Group = parseFPMDirectiveValue(line, "group =")
		}
		if identity.User != "" && identity.Group != "" {
			break
		}
	}

	return identity
}

func parseFPMDirectiveValue(line, marker string) string {
	parts := strings.SplitN(line, marker, 2)
	if len(parts) != 2 {
		return ""
	}

	value := strings.TrimSpace(parts[1])
	value = strings.Trim(value, `"`)
	if cut := strings.Index(value, ";"); cut >= 0 {
		value = strings.TrimSpace(value[:cut])
	}

	return value
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
