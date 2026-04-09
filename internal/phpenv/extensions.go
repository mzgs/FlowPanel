package phpenv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"flowpanel/internal/config"
)

type phpExtensionDefinition struct {
	id                   string
	aliases              []string
	piePackage           string
	sharedObject         string
	iniDirective         string
	requiredDependencies phpExtensionRequiredDependencies
}

type phpExtensionRequiredDependencies struct {
	apt      []string
	dnf      []string
	homebrew []string
	yum      []string
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "bcmath"},
	{id: "curl"},
	{id: "dba"},
	{id: "dom"},
	{id: "ioncube", aliases: []string{"oncube", "ioncubeloader"}},
	{id: "fileinfo"},
	{id: "gd"},
	{id: "mbstring"},
	{id: "mysqli"},
	{id: "mysqlnd"},
	{id: "odbc"},
	{id: "opcache", aliases: []string{"zendopcache"}},
	{id: "pdo"},
	{id: "memcached", piePackage: "php-memcached/php-memcached"},
	{id: "pdo_mysql"},
	{id: "pdo_odbc"},
	{id: "redis", piePackage: "phpredis/phpredis"},
	{id: "mcrypt", piePackage: "pecl/mcrypt"},
	{id: "apcu", piePackage: "apcu/apcu"},
	{id: "pcov", piePackage: "pecl/pcov"},
	{id: "ds", piePackage: "php-ds/ext-ds"},
	{id: "amqp", piePackage: "php-amqp/php-amqp", requiredDependencies: phpExtensionRequiredDependencies{
		apt:      []string{"librabbitmq-dev"},
		dnf:      []string{"librabbitmq-devel"},
		homebrew: []string{"rabbitmq-c"},
		yum:      []string{"librabbitmq-devel"},
	}},
	{id: "parallel", piePackage: "pecl/parallel"},
	{id: "msgpack", piePackage: "msgpack/msgpack-php"},
	{id: "zip", piePackage: "pecl/zip"},
	{id: "uuid", piePackage: "pecl/uuid"},
	{id: "timezonedb", piePackage: "pecl/timezonedb"},
	{id: "imagemagick", aliases: []string{"imagick"}, piePackage: "imagick/imagick", sharedObject: "imagick"},
	{id: "xdebug", piePackage: "xdebug/xdebug", iniDirective: "zend_extension"},
	{id: "imap"},
	{id: "exif"},
	{id: "intl"},
	{id: "xsl"},
	{id: "swoole4", aliases: []string{"swoole"}, piePackage: "swoole/swoole", sharedObject: "swoole"},
	{id: "swoole5", aliases: []string{"swoole"}, piePackage: "swoole/swoole", sharedObject: "swoole"},
	{id: "swoole6", aliases: []string{"swoole"}, piePackage: "swoole/swoole", sharedObject: "swoole"},
	{id: "openswoole", piePackage: "openswoole/ext-openswoole"},
	{id: "xlswriter", piePackage: "viest/xlswriter"},
	{id: "oci8", piePackage: "oci8/oci8"},
	{id: "pdooci", aliases: []string{"pdo_oci"}, piePackage: "pecl/pdo_oci"},
	{id: "swow", piePackage: "swow/swow-extension", sharedObject: "swow"},
	{id: "pdosqlsrv", aliases: []string{"pdo_sqlsrv"}},
	{id: "sqlsrv"},
	{id: "rdkafka", aliases: []string{"rdkakfa"}, piePackage: "rdkafka/rdkafka"},
	{id: "yaf"},
	{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, piePackage: "mongodb/mongodb-extension", sharedObject: "mongodb"},
	{id: "yac"},
	{id: "sg11", aliases: []string{"sourceguardian11"}},
	{id: "sg14", aliases: []string{"sourceguardian14"}},
	{id: "sg15", aliases: []string{"sourceguardian15"}},
	{id: "sg16", aliases: []string{"sourceguardian16"}},
	{id: "xload"},
	{id: "pgsql"},
	{id: "ssh2"},
	{id: "grpc", piePackage: "pie-extensions/grpc"},
	{id: "xhprof"},
	{id: "protobuf", piePackage: "pie-extensions/protobuf"},
	{id: "pdopgsql", aliases: []string{"pdo_pgsql"}},
	{id: "pdo_sqlite"},
	{id: "phar"},
	{id: "posix"},
	{id: "readline"},
	{id: "snmp"},
	{id: "soap"},
	{id: "sodium"},
	{id: "sqlite3"},
	{id: "ldap"},
	{id: "enchant"},
	{id: "pspell"},
	{id: "bz2"},
	{id: "sysvshm"},
	{id: "sysvsem"},
	{id: "calendar"},
	{id: "gmp"},
	{id: "sysvmsg"},
	{id: "tidy"},
	{id: "xmlreader"},
	{id: "xmlwriter"},
	{id: "igbinary", piePackage: "igbinary/igbinary"},
	{id: "zmq"},
	{id: "zstd", piePackage: "kjdev/zstd"},
	{id: "smbclient"},
	{id: "event", piePackage: "osmanov/pecl-event"},
	{id: "mailparse", piePackage: "pecl/mailparse"},
	{id: "yaml", piePackage: "pecl/yaml"},
}

func (s *Service) InstallExtension(ctx context.Context, extension string) (Status, error) {
	runtimeStatus, err := s.InstallExtensionForVersion(ctx, "", extension)
	if err != nil {
		return Status{}, err
	}

	status := s.Status(ctx)
	if status.DefaultVersion == "" {
		status.DefaultVersion = runtimeStatus.Version
	}
	return status, nil
}

func (s *Service) InstallExtensionForVersion(ctx context.Context, version, extension string) (RuntimeStatus, error) {
	runtimeStatus := s.StatusForVersion(ctx, version)
	if !runtimeStatus.PHPInstalled || runtimeStatus.PHPPath == "" {
		return RuntimeStatus{}, fmt.Errorf("php %s is not installed", runtimeStatus.Version)
	}

	requestedExtension := strings.TrimSpace(extension)
	definition, ok := findPHPExtensionDefinition(requestedExtension)
	if !ok {
		return RuntimeStatus{}, fmt.Errorf("php extension %q is not supported", requestedExtension)
	}
	if !definition.supportsPIEInstall() {
		return RuntimeStatus{}, fmt.Errorf("php extension %q does not have a configured PIE package", requestedExtension)
	}
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return runtimeStatus, nil
	}

	if err := installPHPExtensionWithPIE(ctx, runtimeStatus, definition); err != nil {
		return RuntimeStatus{}, err
	}

	runtimeStatus = s.StatusForVersion(ctx, runtimeStatus.Version)
	if runtimeStatus.ServiceRunning {
		if err := s.RestartVersion(ctx, runtimeStatus.Version); err != nil {
			if runtimeStatus.FPMPath == "" {
				return RuntimeStatus{}, fmt.Errorf("php extension installed but failed to restart php-fpm: %w", err)
			}
			if fallbackErr := restartPHPFPM(ctx, runtimeStatus.FPMPath); fallbackErr != nil {
				return RuntimeStatus{}, fmt.Errorf("php extension installed but failed to restart php-fpm: %w", err)
			}
		}
	}

	runtimeStatus = s.StatusForVersion(ctx, runtimeStatus.Version)
	if err := validateInstalledExtension(runtimeStatus, requestedExtension, definition); err != nil {
		return RuntimeStatus{}, err
	}

	return runtimeStatus, nil
}

func findPHPExtensionDefinition(value string) (phpExtensionDefinition, bool) {
	normalized := normalizePHPExtensionKey(value)
	if normalized == "" {
		return phpExtensionDefinition{}, false
	}

	for _, definition := range phpExtensionDefinitions {
		if normalizePHPExtensionKey(definition.id) == normalized {
			return definition, true
		}
		for _, alias := range definition.aliases {
			if normalizePHPExtensionKey(alias) == normalized {
				return definition, true
			}
		}
	}

	return phpExtensionDefinition{}, false
}

func (d phpExtensionDefinition) supportsPIEInstall() bool {
	return strings.TrimSpace(d.piePackage) != ""
}

func (d phpExtensionDefinition) sharedObjectName() string {
	if name := strings.TrimSpace(d.sharedObject); name != "" {
		return strings.TrimSuffix(name, ".so")
	}
	if name := strings.TrimSpace(d.id); name != "" {
		return strings.TrimSuffix(name, ".so")
	}
	return ""
}

func (d phpExtensionDefinition) extensionINIDirective() string {
	if directive := strings.TrimSpace(d.iniDirective); directive != "" {
		return directive
	}
	return "extension"
}

func extensionLoaded(installed []string, definition phpExtensionDefinition) bool {
	loaded := make(map[string]struct{}, len(installed))
	for _, item := range installed {
		key := normalizePHPExtensionKey(item)
		if key == "" {
			continue
		}
		loaded[key] = struct{}{}
	}

	candidates := append([]string{definition.id}, definition.aliases...)
	if sharedObject := definition.sharedObjectName(); sharedObject != "" {
		candidates = append(candidates, sharedObject)
	}
	for _, candidate := range candidates {
		if _, ok := loaded[normalizePHPExtensionKey(candidate)]; ok {
			return true
		}
	}

	return false
}

func validateInstalledExtension(runtimeStatus RuntimeStatus, requestedExtension string, definition phpExtensionDefinition) error {
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return nil
	}

	return fmt.Errorf(
		"php extension %q was installed but is not loaded for php %s",
		requestedExtension,
		runtimeStatus.Version,
	)
}

func normalizePHPExtensionKey(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func installPHPExtensionWithPIE(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	if phpExtensionSharedObjectInstalled(ctx, runtimeStatus, definition) {
		return ensurePHPExtensionEnabled(ctx, runtimeStatus, definition)
	}

	if err := installPHPExtensionRequiredDependencies(ctx, definition); err != nil {
		return err
	}

	piePath, err := ensurePIEBinary(ctx)
	if err != nil {
		return err
	}

	args := []string{
		"install",
		"--auto-install-build-tools",
		"--auto-install-system-dependencies",
	}

	if runtime.GOOS == "windows" {
		if runtimeStatus.PHPPath != "" {
			args = append(args, "--with-php-path="+runtimeStatus.PHPPath)
		}
	} else {
		if runtimeStatus.PHPPath != "" {
			args = append(args, "--with-php-path="+runtimeStatus.PHPPath)
		}
		if phpConfigPath, ok := lookupExecutableCandidates(phpConfigBinaryCandidates(runtimeStatus.Version, runtimeStatus.PHPPath)); ok {
			args = append(args, "--with-php-config="+phpConfigPath)
		}
		if phpizePath, ok := lookupExecutableCandidates(phpizeBinaryCandidates(runtimeStatus.Version, runtimeStatus.PHPPath)); ok {
			args = append(args, "--with-phpize-path="+phpizePath)
		}
	}

	args = append(args, definition.piePackage)
	if _, err := runCommand(ctx, piePath, args...); err != nil {
		return fmt.Errorf("install %s with pie: %w", definition.piePackage, err)
	}

	if err := ensurePHPExtensionEnabled(ctx, runtimeStatus, definition); err != nil {
		return err
	}

	return nil
}

func ensurePHPExtensionEnabled(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	if runtimeStatus.PHPPath == "" {
		return nil
	}

	if extensionLoadedForPHP(ctx, runtimeStatus.PHPPath, definition) {
		return nil
	}
	if !phpExtensionSharedObjectInstalled(ctx, runtimeStatus, definition) {
		return fmt.Errorf("enable php extension %q: shared object %s.so is not installed for php %s", definition.id, definition.sharedObjectName(), runtimeStatus.Version)
	}

	configPaths := phpExtensionEnableConfigPaths(runtimeStatus, definition)
	if len(configPaths) == 0 {
		return fmt.Errorf("enable php extension %q: php %s has no writable ini scan directory", definition.id, runtimeStatus.Version)
	}

	content := []byte(renderPHPExtensionEnableConfig(definition))
	for _, configPath := range configPaths {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			return fmt.Errorf("create php extension config directory: %w", err)
		}
		if err := os.WriteFile(configPath, content, 0o644); err != nil {
			return fmt.Errorf("write php extension enable config: %w", err)
		}
	}

	if !extensionLoadedForPHP(ctx, runtimeStatus.PHPPath, definition) {
		return fmt.Errorf("php extension %q enable config was written but is still not loaded for php %s", definition.id, runtimeStatus.Version)
	}

	return nil
}

func extensionLoadedForPHP(ctx context.Context, phpPath string, definition phpExtensionDefinition) bool {
	output, err := runInspectCommand(ctx, phpPath, "-m")
	if err != nil {
		return false
	}
	return extensionLoaded(parsePHPExtensions(output), definition)
}

func phpExtensionSharedObjectInstalled(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) bool {
	path := phpExtensionSharedObjectPath(ctx, runtimeStatus, definition)
	if path == "" {
		return false
	}

	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func phpExtensionSharedObjectPath(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) string {
	if runtimeStatus.PHPPath == "" {
		return ""
	}

	extensionDir, err := runInspectCommand(ctx, runtimeStatus.PHPPath, "-r", `echo ini_get("extension_dir");`)
	if err != nil {
		return ""
	}
	extensionDir = strings.TrimSpace(extensionDir)
	if extensionDir == "" || !filepath.IsAbs(extensionDir) {
		return ""
	}

	sharedObject := definition.sharedObjectName()
	if sharedObject == "" {
		return ""
	}

	return filepath.Join(extensionDir, strings.TrimSuffix(sharedObject, ".so")+".so")
}

func phpExtensionEnableConfigPaths(runtimeStatus RuntimeStatus, definition phpExtensionDefinition) []string {
	baseDir := strings.TrimSpace(runtimeStatus.ScanDir)
	if baseDir == "" && runtimeStatus.LoadedConfigFile != "" {
		baseDir = filepath.Dir(runtimeStatus.LoadedConfigFile)
	}
	if baseDir == "" {
		return nil
	}

	name := normalizePHPExtensionKey(definition.sharedObjectName())
	if name == "" {
		name = normalizePHPExtensionKey(definition.id)
	}
	if name == "" {
		return nil
	}

	paths := []string{filepath.Join(baseDir, "20-flowpanel-"+name+".ini")}
	for _, candidate := range phpFPMExtensionConfigDirs(runtimeStatus, baseDir) {
		paths = append(paths, filepath.Join(candidate, "20-flowpanel-"+name+".ini"))
	}

	return dedupeStrings(paths)
}

func phpFPMExtensionConfigDirs(runtimeStatus RuntimeStatus, cliConfigDir string) []string {
	candidates := make([]string, 0, 3)
	for _, value := range []string{cliConfigDir, runtimeStatus.LoadedConfigFile} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		fpmPath := strings.Replace(filepath.ToSlash(value), "/cli/", "/fpm/", 1)
		if fpmPath == filepath.ToSlash(value) {
			continue
		}
		if strings.HasSuffix(fpmPath, ".ini") {
			fpmPath = filepath.Dir(fpmPath)
		}
		candidates = append(candidates, filepath.FromSlash(fpmPath))
	}

	if version := NormalizeVersion(runtimeStatus.Version); version != "" && runtimeStatus.FPMPath != "" {
		candidates = append(candidates, filepath.Join("/etc/php", version, "fpm", "conf.d"))
	}

	dirs := make([]string, 0, len(candidates))
	for _, candidate := range dedupeStrings(candidates) {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			dirs = append(dirs, candidate)
		}
	}
	return dirs
}

func renderPHPExtensionEnableConfig(definition phpExtensionDefinition) string {
	sharedObject := definition.sharedObjectName()
	if sharedObject == "" {
		sharedObject = definition.id
	}
	sharedObject = strings.TrimSuffix(sharedObject, ".so") + ".so"

	var builder strings.Builder
	builder.WriteString("; Managed by FlowPanel.\n")
	builder.WriteString("; Enables a PHP extension installed through FlowPanel.\n")
	builder.WriteString(fmt.Sprintf("%s=%s\n", definition.extensionINIDirective(), sharedObject))
	return builder.String()
}

func installPHPExtensionRequiredDependencies(ctx context.Context, definition phpExtensionDefinition) error {
	packageManager, packages, ok, err := matchingPHPExtensionRequiredDependency(definition.requiredDependencies)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	commands, err := dependencyInstallCommands(packageManager, packages)
	if err != nil {
		return err
	}
	if err := runCommands(ctx, commands...); err != nil {
		return fmt.Errorf(
			"install system dependencies for php extension %q with %s: %w",
			definition.id,
			packageManager,
			err,
		)
	}

	return nil
}

func matchingPHPExtensionRequiredDependency(dependencies phpExtensionRequiredDependencies) (string, []string, bool, error) {
	if !dependencies.hasPackages() {
		return "", nil, false, nil
	}

	packageManager := phpExtensionDependencyPackageManager()
	if packageManager == "" {
		return "", nil, false, fmt.Errorf("install system dependencies: unsupported package manager on %s", runtime.GOOS)
	}

	packages := dependencies.packagesFor(packageManager)
	if len(packages) == 0 {
		return "", nil, false, fmt.Errorf("install system dependencies: no packages configured for %s", packageManager)
	}

	return packageManager, packages, true, nil
}

func (d phpExtensionRequiredDependencies) hasPackages() bool {
	return len(d.apt) > 0 || len(d.dnf) > 0 || len(d.homebrew) > 0 || len(d.yum) > 0
}

func (d phpExtensionRequiredDependencies) packagesFor(packageManager string) []string {
	switch strings.TrimSpace(packageManager) {
	case "apt":
		return d.apt
	case "dnf":
		return d.dnf
	case "homebrew":
		return d.homebrew
	case "yum":
		return d.yum
	default:
		return nil
	}
}

func phpExtensionDependencyPackageManager() string {
	switch runtime.GOOS {
	case "darwin":
		if _, ok := lookupCommand("brew"); ok {
			return "homebrew"
		}
	case "linux":
		if _, ok := lookupCommand("apt-get"); ok {
			return "apt"
		}
		if _, ok := lookupCommand("dnf"); ok {
			return "dnf"
		}
		if _, ok := lookupCommand("yum"); ok {
			return "yum"
		}
	}

	return ""
}

func dependencyInstallCommands(packageManager string, packages []string) ([][]string, error) {
	packages = dedupeStrings(packages)
	if len(packages) == 0 {
		return nil, nil
	}

	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		return nil, fmt.Errorf("install system dependencies: root privileges are required on linux")
	}

	switch packageManager {
	case "apt":
		aptPath, ok := lookupCommand("apt-get")
		if !ok {
			return nil, fmt.Errorf("install system dependencies: apt-get is not available")
		}
		installArgs := append([]string{aptPath, "install", "-y"}, packages...)
		return [][]string{
			{aptPath, "update"},
			installArgs,
		}, nil
	case "dnf":
		dnfPath, ok := lookupCommand("dnf")
		if !ok {
			return nil, fmt.Errorf("install system dependencies: dnf is not available")
		}
		installArgs := append([]string{dnfPath, "install", "-y"}, packages...)
		return [][]string{installArgs}, nil
	case "homebrew":
		brewPath, ok := lookupCommand("brew")
		if !ok {
			return nil, fmt.Errorf("install system dependencies: brew is not available")
		}
		installArgs := append([]string{brewPath, "install"}, packages...)
		return [][]string{installArgs}, nil
	case "yum":
		yumPath, ok := lookupCommand("yum")
		if !ok {
			return nil, fmt.Errorf("install system dependencies: yum is not available")
		}
		installArgs := append([]string{yumPath, "install", "-y"}, packages...)
		return [][]string{installArgs}, nil
	default:
		return nil, fmt.Errorf("install system dependencies: unsupported package manager %s", packageManager)
	}
}

func ensurePIEBinary(ctx context.Context) (string, error) {
	if path, ok := lookupCommand("pie"); ok {
		return path, nil
	}

	managedPath := managedPIEBinaryPath()
	if path, ok := lookupCandidateExecutable(managedPath); ok {
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		return "", fmt.Errorf("create managed pie directory: %w", err)
	}

	downloadURL, err := latestPIEBinaryDownloadURL()
	if err != nil {
		return "", err
	}
	if err := downloadPIEBinary(ctx, downloadURL, managedPath); err != nil {
		return "", err
	}
	if _, err := runInspectCommand(ctx, managedPath, "-V"); err != nil {
		return "", fmt.Errorf("verify downloaded pie binary: %w", err)
	}
	return managedPath, nil
}

func managedPIEBinaryPath() string {
	name := "pie"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(config.FlowPanelDataPath(), "bin", name)
}

func latestPIEBinaryDownloadURL() (string, error) {
	var platform string
	switch runtime.GOOS {
	case "linux":
		platform = "Linux"
	case "darwin":
		platform = "macOS"
	case "windows":
		platform = "Windows"
	default:
		return "", fmt.Errorf("automatic pie bootstrap is not supported on %s", runtime.GOOS)
	}

	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "X64"
	case "arm64":
		arch = "ARM64"
	default:
		return "", fmt.Errorf("automatic pie bootstrap is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if platform == "Windows" && arch != "X64" {
		return "", fmt.Errorf("automatic pie bootstrap is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	suffix := ""
	if platform == "Windows" {
		suffix = ".exe"
	}
	return fmt.Sprintf("https://github.com/php/pie/releases/latest/download/pie-%s-%s%s", platform, arch, suffix), nil
}

func downloadPIEBinary(ctx context.Context, url, destinationPath string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create pie download request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download pie binary: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download pie binary: unexpected status %s", response.Status)
	}

	tempPath := destinationPath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create temporary pie binary: %w", err)
	}

	copyErr := error(nil)
	if _, err := io.Copy(file, response.Body); err != nil {
		copyErr = fmt.Errorf("write pie binary: %w", err)
	}
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close temporary pie binary: %w", closeErr)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempPath, 0o755); err != nil {
			_ = os.Remove(tempPath)
			return fmt.Errorf("mark pie binary executable: %w", err)
		}
	}
	if err := os.Rename(tempPath, destinationPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("activate pie binary: %w", err)
	}
	return nil
}

func lookupExecutableCandidates(candidates []string) (string, bool) {
	for _, candidate := range dedupeStrings(candidates) {
		if path, ok := lookupCandidateExecutable(candidate); ok {
			return path, true
		}
	}
	return "", false
}

func phpizeBinaryCandidates(version, phpPath string) []string {
	dir := filepath.Dir(strings.TrimSpace(phpPath))
	versionNoDots := strings.ReplaceAll(version, ".", "")
	return dedupeStrings([]string{
		filepath.Join(dir, "phpize"+version),
		filepath.Join(dir, "phpize"+versionNoDots),
		"phpize" + version,
		"phpize" + versionNoDots,
		filepath.Join(dir, "phpize"),
		"phpize",
	})
}

func phpConfigBinaryCandidates(version, phpPath string) []string {
	dir := filepath.Dir(strings.TrimSpace(phpPath))
	versionNoDots := strings.ReplaceAll(version, ".", "")
	return dedupeStrings([]string{
		filepath.Join(dir, "php-config"+version),
		filepath.Join(dir, "php-config"+versionNoDots),
		"php-config" + version,
		"php-config" + versionNoDots,
		filepath.Join(dir, "php-config"),
		"php-config",
	})
}
