package phpenv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"flowpanel/internal/config"
)

type phpExtensionDefinition struct {
	id           string
	aliases      []string
	piePackage   string
	sharedObject string
}

type phpBuildIdentity struct {
	version          string
	versionID        string
	extensionDir     string
	phpAPI           string
	zendExtensionAPI string
	phpBinary        string
}

type phpBuildToolchain struct {
	phpPath       string
	phpConfigPath string
	phpizePath    string
}

type phpConfigIdentity struct {
	versionID    string
	extensionDir string
	phpBinary    string
}

type phpizeIdentity struct {
	version          string
	phpAPI           string
	zendModuleAPI    string
	zendExtensionAPI string
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
	{id: "amqp", piePackage: "php-amqp/php-amqp"},
	{id: "parallel", piePackage: "pecl/parallel"},
	{id: "msgpack", piePackage: "msgpack/msgpack-php"},
	{id: "zip", piePackage: "pecl/zip"},
	{id: "uuid", piePackage: "pecl/uuid"},
	{id: "timezonedb", piePackage: "pecl/timezonedb"},
	{id: "imagemagick", aliases: []string{"imagick"}, piePackage: "imagick/imagick", sharedObject: "imagick"},
	{id: "xdebug", piePackage: "xdebug/xdebug"},
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
		toolchain, err := resolvePHPBuildToolchain(ctx, runtimeStatus)
		if err != nil {
			attemptedInstall, installErr := installPHPBuildTools(ctx, runtimeStatus)
			switch {
			case installErr != nil:
				return fmt.Errorf("resolve PHP build tools for php %s: %w; install PHP development tools: %v", runtimeStatus.Version, err, installErr)
			case attemptedInstall:
				toolchain, err = resolvePHPBuildToolchain(ctx, runtimeStatus)
				if err != nil {
					return err
				}
			default:
				return err
			}
		}

		args = append(args,
			"--with-php-config="+toolchain.phpConfigPath,
			"--with-phpize-path="+toolchain.phpizePath,
		)
	}

	args = append(args, definition.piePackage)
	if _, err := runCommand(ctx, piePath, args...); err != nil {
		return fmt.Errorf(
			"install %s with pie (command: %s): %w",
			definition.piePackage,
			formatCommandLine(piePath, args...),
			err,
		)
	}

	return nil
}

func resolvePHPBuildToolchain(ctx context.Context, runtimeStatus RuntimeStatus) (phpBuildToolchain, error) {
	phpPath := strings.TrimSpace(runtimeStatus.PHPPath)
	if phpPath == "" {
		return phpBuildToolchain{}, fmt.Errorf("php %s binary path is unavailable", runtimeStatus.Version)
	}

	identity, err := inspectPHPBuildIdentity(ctx, phpPath)
	if err != nil {
		return phpBuildToolchain{}, fmt.Errorf("inspect php %s build identity from %s: %w", runtimeStatus.Version, phpPath, err)
	}

	phpConfigPath, err := resolvePHPConfigPath(ctx, identity, phpConfigBinaryCandidates(runtimeStatus.Version, phpPath))
	if err != nil {
		return phpBuildToolchain{}, fmt.Errorf("resolve php-config for php %s: %w", runtimeStatus.Version, err)
	}

	phpizePath, err := resolvePHPizePath(ctx, identity, runtimeStatus.Version, phpizeBinaryCandidates(runtimeStatus.Version, phpPath, phpConfigPath))
	if err != nil {
		return phpBuildToolchain{}, fmt.Errorf("resolve phpize for php %s: %w", runtimeStatus.Version, err)
	}

	return phpBuildToolchain{
		phpPath:       phpPath,
		phpConfigPath: phpConfigPath,
		phpizePath:    phpizePath,
	}, nil
}

func inspectPHPBuildIdentity(ctx context.Context, phpPath string) (phpBuildIdentity, error) {
	payloadOutput, err := runInspectCommand(ctx, phpPath, "-r", `echo json_encode([
  "version" => PHP_VERSION,
  "version_id" => (string) PHP_VERSION_ID,
  "extension_dir" => (string) ini_get("extension_dir"),
  "php_binary" => PHP_BINARY,
], JSON_UNESCAPED_SLASHES);`)
	if err != nil {
		return phpBuildIdentity{}, err
	}

	var payload struct {
		Version      string `json:"version"`
		VersionID    string `json:"version_id"`
		ExtensionDir string `json:"extension_dir"`
		PHPBinary    string `json:"php_binary"`
	}
	if err := json.Unmarshal([]byte(payloadOutput), &payload); err != nil {
		return phpBuildIdentity{}, fmt.Errorf("decode php build identity: %w", err)
	}

	infoOutput, err := runInspectCommand(ctx, phpPath, "-i")
	if err != nil {
		return phpBuildIdentity{}, fmt.Errorf("inspect php api values: %w", err)
	}

	return phpBuildIdentity{
		version:          strings.TrimSpace(payload.Version),
		versionID:        strings.TrimSpace(payload.VersionID),
		extensionDir:     strings.TrimSpace(payload.ExtensionDir),
		phpAPI:           parsePHPInfoValue(infoOutput, "PHP API"),
		zendExtensionAPI: parsePHPInfoValue(infoOutput, "Zend Extension"),
		phpBinary:        strings.TrimSpace(payload.PHPBinary),
	}, nil
}

func resolvePHPConfigPath(ctx context.Context, runtime phpBuildIdentity, candidates []string) (string, error) {
	resolvedCandidates := existingExecutableCandidates(candidates)
	if len(resolvedCandidates) == 0 {
		return "", fmt.Errorf("no php-config executable was found in candidates: %s", strings.Join(candidates, ", "))
	}

	for _, candidate := range resolvedCandidates {
		identity, err := inspectPHPConfigIdentity(ctx, candidate)
		if err != nil {
			continue
		}
		if phpConfigMatchesRuntime(runtime, identity) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no php-config candidate matched the selected runtime; checked: %s", strings.Join(resolvedCandidates, ", "))
}

func inspectPHPConfigIdentity(ctx context.Context, phpConfigPath string) (phpConfigIdentity, error) {
	versionID, err := runInspectCommand(ctx, phpConfigPath, "--vernum")
	if err != nil {
		return phpConfigIdentity{}, err
	}

	extensionDir, err := runInspectCommand(ctx, phpConfigPath, "--extension-dir")
	if err != nil {
		return phpConfigIdentity{}, err
	}

	phpBinary := ""
	if output, err := runInspectCommand(ctx, phpConfigPath, "--php-binary"); err == nil {
		phpBinary = output
	}

	return phpConfigIdentity{
		versionID:    strings.TrimSpace(versionID),
		extensionDir: strings.TrimSpace(extensionDir),
		phpBinary:    strings.TrimSpace(phpBinary),
	}, nil
}

func phpConfigMatchesRuntime(runtime phpBuildIdentity, candidate phpConfigIdentity) bool {
	matched := false

	if runtime.versionID != "" && candidate.versionID != "" {
		if runtime.versionID != candidate.versionID {
			return false
		}
		matched = true
	}

	if runtime.extensionDir != "" && candidate.extensionDir != "" {
		if !pathsEqual(runtime.extensionDir, candidate.extensionDir) {
			return false
		}
		matched = true
	}

	if runtime.phpBinary != "" && candidate.phpBinary != "" {
		if !pathsEqual(runtime.phpBinary, candidate.phpBinary) {
			return false
		}
		matched = true
	}

	return matched
}

func resolvePHPizePath(ctx context.Context, runtime phpBuildIdentity, version string, candidates []string) (string, error) {
	resolvedCandidates := existingExecutableCandidates(candidates)
	if len(resolvedCandidates) == 0 {
		return "", fmt.Errorf("no phpize executable was found in candidates: %s", strings.Join(candidates, ", "))
	}

	for _, candidate := range resolvedCandidates {
		identity, err := inspectPHPizeIdentity(ctx, candidate)
		if err != nil {
			continue
		}
		if phpizeMatchesRuntime(runtime, version, identity) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("no phpize candidate matched the selected runtime; checked: %s", strings.Join(resolvedCandidates, ", "))
}

func inspectPHPizeIdentity(ctx context.Context, phpizePath string) (phpizeIdentity, error) {
	output, err := runInspectCommand(ctx, phpizePath, "--version")
	if err != nil {
		return phpizeIdentity{}, err
	}

	return phpizeIdentity{
		version:          parseColonValue(output, "PHP Version"),
		phpAPI:           parseColonValue(output, "PHP Api Version"),
		zendModuleAPI:    parseColonValue(output, "Zend Module Api No"),
		zendExtensionAPI: parseColonValue(output, "Zend Extension Api No"),
	}, nil
}

func phpizeMatchesRuntime(runtime phpBuildIdentity, version string, candidate phpizeIdentity) bool {
	matched := false

	if version != "" && candidate.version != "" {
		if NormalizeVersion(candidate.version) != NormalizeVersion(version) {
			return false
		}
		matched = true
	}

	if runtime.phpAPI != "" && candidate.phpAPI != "" {
		if runtime.phpAPI != candidate.phpAPI {
			return false
		}
		matched = true
	}

	if runtime.phpAPI != "" && candidate.zendModuleAPI != "" {
		if runtime.phpAPI != candidate.zendModuleAPI {
			return false
		}
		matched = true
	}

	if runtime.zendExtensionAPI != "" && candidate.zendExtensionAPI != "" {
		if runtime.zendExtensionAPI != candidate.zendExtensionAPI {
			return false
		}
		matched = true
	}

	return matched
}

func installPHPBuildTools(ctx context.Context, runtimeStatus RuntimeStatus) (bool, error) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		return false, nil
	}

	version := NormalizeVersion(runtimeStatus.Version)
	if version == "" {
		return false, nil
	}

	packageManager := strings.TrimSpace(runtimeStatus.PackageManager)
	if packageManager == "" {
		packageManager = detectVersionActionPlan(version).packageManager
	}

	switch packageManager {
	case "apt":
		aptPath, ok := lookupCommand("apt-get")
		if !ok {
			return false, nil
		}
		_, err := runCommand(ctx, aptPath, "install", "-y", "php"+version+"-dev")
		return true, err
	case "dnf":
		dnfPath, ok := lookupCommand("dnf")
		if !ok {
			return false, nil
		}
		_, err := runCommand(ctx, dnfPath, "install", "-y", remiCollectionForVersion(version)+"-php-devel")
		return true, err
	case "yum":
		yumPath, ok := lookupCommand("yum")
		if !ok {
			return false, nil
		}
		_, err := runCommand(ctx, yumPath, "install", "-y", remiCollectionForVersion(version)+"-php-devel")
		return true, err
	default:
		return false, nil
	}
}

func existingExecutableCandidates(candidates []string) []string {
	resolved := make([]string, 0, len(candidates))
	for _, candidate := range dedupeStrings(candidates) {
		if path, ok := lookupCandidateExecutable(candidate); ok {
			resolved = append(resolved, path)
		}
	}
	return dedupeStrings(resolved)
}

func parsePHPInfoValue(output, key string) string {
	return parseDelimitedValue(output, key, "=>")
}

func parseColonValue(output, key string) string {
	return parseDelimitedValue(output, key, ":")
}

func parseDelimitedValue(output, key, separator string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch separator {
		case "=>":
			prefix := key + " =>"
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if extraIndex := strings.Index(value, "=>"); extraIndex >= 0 {
				value = strings.TrimSpace(value[:extraIndex])
			}
			return value
		case ":":
			prefix := key + ":"
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}

	return ""
}

func pathsEqual(left, right string) bool {
	left = normalizeComparablePath(left)
	right = normalizeComparablePath(right)
	return left != "" && right != "" && left == right
}

func normalizeComparablePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	return filepath.Clean(path)
}

func formatCommandLine(name string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	if strings.TrimSpace(name) != "" {
		parts = append(parts, strconv.Quote(name))
	}
	for _, arg := range args {
		parts = append(parts, strconv.Quote(arg))
	}
	return strings.Join(parts, " ")
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

func phpizeBinaryCandidates(version string, paths ...string) []string {
	versionNoDots := strings.ReplaceAll(version, ".", "")
	candidates := make([]string, 0)
	for _, dir := range candidateBinaryDirs(paths...) {
		candidates = append(candidates,
			filepath.Join(dir, "phpize"+version),
			filepath.Join(dir, "phpize"+versionNoDots),
			filepath.Join(dir, "phpize"),
		)
	}
	candidates = append(candidates,
		"phpize"+version,
		"phpize"+versionNoDots,
		"phpize",
	)
	return dedupeStrings(candidates)
}

func phpConfigBinaryCandidates(version string, paths ...string) []string {
	versionNoDots := strings.ReplaceAll(version, ".", "")
	candidates := make([]string, 0)
	for _, dir := range candidateBinaryDirs(paths...) {
		candidates = append(candidates,
			filepath.Join(dir, "php-config"+version),
			filepath.Join(dir, "php-config"+versionNoDots),
			filepath.Join(dir, "php-config"),
		)
	}
	candidates = append(candidates,
		"php-config"+version,
		"php-config"+versionNoDots,
		"php-config",
	)
	return dedupeStrings(candidates)
}

func candidateBinaryDirs(paths ...string) []string {
	dirs := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}

		dirs = append(dirs, filepath.Dir(path))
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			dirs = append(dirs, filepath.Dir(resolved))
		}
	}

	return dedupeStrings(dirs)
}
