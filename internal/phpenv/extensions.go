package phpenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type phpExtensionDefinition struct {
	id            string
	aliases       []string
	peclPackage   string
	sharedObject  string
	enableMode    string
	configureArgs []string
}

const (
	phpExtensionEnableModeExtension     = "extension"
	phpExtensionEnableModeZendExtension = "zend_extension"
)

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "ioncube", aliases: []string{"oncube", "ioncubeloader"}},
	{id: "fileinfo"},
	{id: "opcache", aliases: []string{"zendopcache"}},
	{id: "memcached", peclPackage: "memcached"},
	{id: "redis", peclPackage: "redis"},
	{id: "mcrypt", peclPackage: "mcrypt"},
	{id: "apcu", peclPackage: "apcu"},
	{id: "imagemagick", aliases: []string{"imagick"}, peclPackage: "imagick", sharedObject: "imagick"},
	{id: "xdebug", peclPackage: "xdebug", enableMode: phpExtensionEnableModeZendExtension},
	{id: "imap"},
	{id: "exif"},
	{id: "intl"},
	{id: "xsl"},
	{id: "swoole4", aliases: []string{"swoole"}, peclPackage: "swoole", sharedObject: "swoole"},
	{id: "swoole5", aliases: []string{"swoole"}, peclPackage: "swoole", sharedObject: "swoole"},
	{id: "swoole6", aliases: []string{"swoole"}, peclPackage: "swoole", sharedObject: "swoole"},
	{id: "xlswriter", peclPackage: "xlswriter"},
	{id: "oci8"},
	{id: "pdooci", aliases: []string{"pdo_oci"}},
	{id: "swow", peclPackage: "swow"},
	{id: "pdosqlsrv", aliases: []string{"pdo_sqlsrv"}, peclPackage: "pdo_sqlsrv", sharedObject: "pdo_sqlsrv"},
	{id: "sqlsrv", peclPackage: "sqlsrv"},
	{id: "rdkafka", aliases: []string{"rdkakfa"}, peclPackage: "rdkafka"},
	{id: "yaf", peclPackage: "yaf"},
	{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, peclPackage: "mongodb", sharedObject: "mongodb"},
	{id: "yac", peclPackage: "yac"},
	{id: "sg11", aliases: []string{"sourceguardian11"}},
	{id: "sg14", aliases: []string{"sourceguardian14"}},
	{id: "sg15", aliases: []string{"sourceguardian15"}},
	{id: "sg16", aliases: []string{"sourceguardian16"}},
	{id: "xload"},
	{id: "pgsql"},
	{id: "ssh2", peclPackage: "ssh2"},
	{id: "grpc", peclPackage: "grpc"},
	{id: "xhprof", peclPackage: "xhprof"},
	{id: "protobuf", peclPackage: "protobuf"},
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
	{id: "igbinary", peclPackage: "igbinary"},
	{id: "zmq", peclPackage: "zmq"},
	{id: "zstd", peclPackage: "zstd"},
	{id: "smbclient", peclPackage: "smbclient"},
	{id: "event", peclPackage: "event"},
	{id: "mailparse", peclPackage: "mailparse"},
	{id: "yaml", peclPackage: "yaml"},
}

type phpPECLToolchain struct {
	phpPath       string
	peclPath      string
	phpizePath    string
	phpConfigPath string
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
	definition, ok := findPHPExtensionDefinition(extension)
	if !ok {
		return RuntimeStatus{}, fmt.Errorf("php extension %q is not supported", requestedExtension)
	}
	if !definition.supportsPECLInstall() {
		return RuntimeStatus{}, fmt.Errorf("php extension %q does not support automatic PECL installation", requestedExtension)
	}
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return runtimeStatus, nil
	}

	if err := installPHPExtensionWithPECL(ctx, runtimeStatus, definition); err != nil {
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

func (d phpExtensionDefinition) supportsPECLInstall() bool {
	return strings.TrimSpace(d.peclPackage) != ""
}

func (d phpExtensionDefinition) enableDirective() string {
	if d.enableMode == phpExtensionEnableModeZendExtension {
		return phpExtensionEnableModeZendExtension
	}
	return phpExtensionEnableModeExtension
}

func (d phpExtensionDefinition) moduleName() string {
	return "flowpanel-" + normalizePHPExtensionKey(d.id)
}

func (d phpExtensionDefinition) sharedObjectName() string {
	name := strings.TrimSpace(d.sharedObject)
	if name == "" {
		name = strings.TrimSpace(d.peclPackage)
	}
	if name == "" {
		name = strings.TrimSpace(d.id)
	}
	return strings.TrimSuffix(name, ".so")
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

func installPHPExtensionWithPECL(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	toolchain, err := ensurePECLToolchain(ctx, runtimeStatus.Version, runtimeStatus.PHPPath)
	if err != nil {
		return err
	}

	buildDir, err := os.MkdirTemp("", "flowpanel-pecl-*")
	if err != nil {
		return fmt.Errorf("create pecl build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	archivePath, err := downloadPECLPackage(ctx, buildDir, toolchain, definition.peclPackage)
	if err != nil {
		return err
	}

	sourceDir, err := extractPECLPackage(ctx, buildDir, archivePath)
	if err != nil {
		return err
	}

	if _, err := runCommandInDir(ctx, sourceDir, toolchain.phpizePath); err != nil {
		return fmt.Errorf("prepare %s with phpize: %w", definition.peclPackage, err)
	}

	configureArgs := []string{"--with-php-config=" + toolchain.phpConfigPath}
	configureArgs = append(configureArgs, definition.configureArgs...)
	if _, err := runCommandInDir(ctx, sourceDir, "./configure", configureArgs...); err != nil {
		return fmt.Errorf("configure %s: %w", definition.peclPackage, err)
	}

	makeJobs := fmt.Sprintf("-j%d", maxInt(1, runtime.NumCPU()))
	if _, err := runCommandInDir(ctx, sourceDir, "make", makeJobs); err != nil {
		return fmt.Errorf("build %s: %w", definition.peclPackage, err)
	}
	if _, err := runCommandInDir(ctx, sourceDir, "make", "install"); err != nil {
		return fmt.Errorf("install %s: %w", definition.peclPackage, err)
	}

	if err := enablePHPExtension(ctx, runtimeStatus, definition); err != nil {
		return err
	}

	return nil
}

func ensurePECLToolchain(ctx context.Context, version, phpPath string) (phpPECLToolchain, error) {
	if toolchain, ok := lookupPECLToolchain(version, phpPath); ok {
		return toolchain, nil
	}

	commands := buildPECLToolchainInstallCommands(version)
	if len(commands) == 0 {
		return phpPECLToolchain{}, fmt.Errorf("automatic PECL bootstrap for PHP %s is not supported on %s", version, runtime.GOOS)
	}
	if err := runCommands(ctx, commands...); err != nil {
		return phpPECLToolchain{}, fmt.Errorf("install PECL toolchain for PHP %s: %w", version, err)
	}

	toolchain, ok := lookupPECLToolchain(version, phpPath)
	if !ok {
		return phpPECLToolchain{}, fmt.Errorf("the PECL toolchain for PHP %s is not available after installation", version)
	}
	return toolchain, nil
}

func lookupPECLToolchain(version, phpPath string) (phpPECLToolchain, bool) {
	toolchain := phpPECLToolchain{
		phpPath: strings.TrimSpace(phpPath),
	}
	if toolchain.phpPath == "" {
		return phpPECLToolchain{}, false
	}

	toolchain.peclPath, _ = lookupExecutableCandidates(peclBinaryCandidates(version, phpPath))
	toolchain.phpizePath, _ = lookupExecutableCandidates(phpizeBinaryCandidates(version, phpPath))
	toolchain.phpConfigPath, _ = lookupExecutableCandidates(phpConfigBinaryCandidates(version, phpPath))

	if toolchain.peclPath == "" || toolchain.phpizePath == "" || toolchain.phpConfigPath == "" {
		return toolchain, false
	}

	return toolchain, true
}

func lookupExecutableCandidates(candidates []string) (string, bool) {
	for _, candidate := range dedupeStrings(candidates) {
		if path, ok := lookupCandidateExecutable(candidate); ok {
			return path, true
		}
	}
	return "", false
}

func peclBinaryCandidates(version, phpPath string) []string {
	dir := filepath.Dir(strings.TrimSpace(phpPath))
	versionNoDots := strings.ReplaceAll(version, ".", "")
	return dedupeStrings([]string{
		filepath.Join(dir, "pecl"),
		filepath.Join(dir, "pecl"+version),
		filepath.Join(dir, "pecl"+versionNoDots),
		"pecl" + version,
		"pecl" + versionNoDots,
		"pecl",
	})
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

func buildPECLToolchainInstallCommands(version string) [][]string {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil
	}

	if aptGetPath, ok := lookupCommand("apt-get"); ok {
		packages := dedupeStrings([]string{
			"php-pear",
			"php" + version + "-dev",
			"build-essential",
			"pkg-config",
		})
		return [][]string{
			{aptGetPath, "update"},
			append([]string{aptGetPath, "install", "-y"}, packages...),
		}
	}
	if dnfPath, ok := lookupCommand("dnf"); ok {
		packages := dedupeStrings([]string{
			remiCollectionForVersion(version) + "-php-pear",
			remiCollectionForVersion(version) + "-php-devel",
			"gcc",
			"make",
			"autoconf",
		})
		return [][]string{
			append([]string{dnfPath, "install", "-y"}, packages...),
		}
	}
	if yumPath, ok := lookupCommand("yum"); ok {
		packages := dedupeStrings([]string{
			remiCollectionForVersion(version) + "-php-pear",
			remiCollectionForVersion(version) + "-php-devel",
			"gcc",
			"make",
			"autoconf",
		})
		return [][]string{
			append([]string{yumPath, "install", "-y"}, packages...),
		}
	}

	return nil
}

func downloadPECLPackage(ctx context.Context, buildDir string, toolchain phpPECLToolchain, packageName string) (string, error) {
	if err := runPECLCommand(ctx, buildDir, toolchain, "download", packageName); err != nil {
		return "", fmt.Errorf("download PECL package %q: %w", packageName, err)
	}

	matches, err := filepath.Glob(filepath.Join(buildDir, packageName+"-*.tgz"))
	if err != nil {
		return "", fmt.Errorf("locate downloaded PECL archive for %q: %w", packageName, err)
	}
	if len(matches) == 0 {
		matches, err = filepath.Glob(filepath.Join(buildDir, "*.tgz"))
		if err != nil {
			return "", fmt.Errorf("locate downloaded PECL archive for %q: %w", packageName, err)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("download PECL package %q: no archive was created", packageName)
	}

	return matches[0], nil
}

func runPECLCommand(ctx context.Context, buildDir string, toolchain phpPECLToolchain, args ...string) error {
	attempts := []struct {
		name string
		args []string
	}{
		{name: toolchain.phpPath, args: append([]string{toolchain.peclPath}, args...)},
	}
	if toolchain.peclPath != "" {
		attempts = append(attempts, struct {
			name string
			args []string
		}{name: toolchain.peclPath, args: args})
	}

	var failures []string
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.name) == "" {
			continue
		}
		if _, err := runCommandInDir(ctx, buildDir, attempt.name, attempt.args...); err == nil {
			return nil
		} else {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) == 0 {
		return fmt.Errorf("no PECL command candidates were generated")
	}
	if len(failures) == 1 {
		return fmt.Errorf("%s", failures[0])
	}
	return fmt.Errorf("%s", strings.Join(failures, " | "))
}

func extractPECLPackage(ctx context.Context, buildDir, archivePath string) (string, error) {
	if _, err := runCommand(ctx, "tar", "-xf", archivePath, "-C", buildDir); err != nil {
		return "", fmt.Errorf("extract PECL archive %q: %w", filepath.Base(archivePath), err)
	}

	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return "", fmt.Errorf("list extracted PECL sources: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sourceDir := filepath.Join(buildDir, entry.Name())
		if _, err := os.Stat(filepath.Join(sourceDir, "package.xml")); err == nil {
			return sourceDir, nil
		}
	}

	return "", fmt.Errorf("could not determine extracted PECL source directory for %q", filepath.Base(archivePath))
}

func enablePHPExtension(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	content := renderManagedPHPExtensionConfig(definition)

	if phpenmodPath, ok := lookupCommand("phpenmod"); ok && runtime.GOOS == "linux" {
		moduleName := definition.moduleName()
		configPath := filepath.Join("/etc/php", runtimeStatus.Version, "mods-available", moduleName+".ini")
		if err := writeManagedPHPExtensionConfig(configPath, content); err == nil {
			if _, err := runCommand(ctx, phpenmodPath, "-v", runtimeStatus.Version, moduleName); err == nil {
				return nil
			}
		}
	}

	configPath := determineManagedPHPExtensionConfigFile(runtimeStatus.LoadedConfigFile, runtimeStatus.ScanDir, definition)
	if configPath == "" {
		return fmt.Errorf("flowpanel could not determine where to enable PHP extension %q", definition.id)
	}
	if err := writeManagedPHPExtensionConfig(configPath, content); err != nil {
		return err
	}
	return nil
}

func renderManagedPHPExtensionConfig(definition phpExtensionDefinition) string {
	return fmt.Sprintf(
		"; Managed by FlowPanel.\n; Manual edits may be overwritten.\n%s=%s.so\n",
		definition.enableDirective(),
		definition.sharedObjectName(),
	)
}

func writeManagedPHPExtensionConfig(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("php extension config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create php extension config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write php extension config: %w", err)
	}
	return nil
}

func determineManagedPHPExtensionConfigFile(loadedConfigFile, scanDir string, definition phpExtensionDefinition) string {
	filename := definition.moduleName() + ".ini"
	if scanDir != "" {
		return filepath.Join(scanDir, filename)
	}
	if loadedConfigFile != "" {
		return filepath.Join(filepath.Dir(loadedConfigFile), filename)
	}
	return ""
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
