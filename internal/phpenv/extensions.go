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
	id           string
	aliases      []string
	piePackage   string
	sharedObject string
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "ioncube", aliases: []string{"oncube", "ioncubeloader"}},
	{id: "fileinfo"},
	{id: "opcache", aliases: []string{"zendopcache"}},
	{id: "memcached", piePackage: "php-memcached/php-memcached"},
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
	{id: "readline"},
	{id: "snmp"},
	{id: "ldap"},
	{id: "enchant"},
	{id: "pspell"},
	{id: "bz2"},
	{id: "sysvshm"},
	{id: "calendar"},
	{id: "gmp"},
	{id: "sysvmsg"},
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

	return nil
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
		filepath.Join(dir, "phpize"),
		filepath.Join(dir, "phpize"+version),
		filepath.Join(dir, "phpize"+versionNoDots),
		"phpize" + version,
		"phpize" + versionNoDots,
		"phpize",
	})
}

func phpConfigBinaryCandidates(version, phpPath string) []string {
	dir := filepath.Dir(strings.TrimSpace(phpPath))
	versionNoDots := strings.ReplaceAll(version, ".", "")
	return dedupeStrings([]string{
		filepath.Join(dir, "php-config"),
		filepath.Join(dir, "php-config"+version),
		filepath.Join(dir, "php-config"+versionNoDots),
		"php-config" + version,
		"php-config" + versionNoDots,
		"php-config",
	})
}
