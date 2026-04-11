package phpenv

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type phpExtensionDefinition struct {
	id            string
	catalogID     string
	label         string
	aliases       []string
	installID     string
	aptPackage    string
	dnfPackage    string
	sharedObject  string
	zendExtension bool
}

type PHPExtensionCatalogEntry struct {
	ID                     string   `json:"id"`
	Label                  string   `json:"label"`
	Aliases                []string `json:"aliases,omitempty"`
	InstallID              string   `json:"install_id,omitempty"`
	InstallPackageManagers []string `json:"install_package_managers,omitempty"`
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "amqp", aptPackage: "amqp", dnfPackage: "pecl-amqp"},
	{id: "apcu", aptPackage: "apcu", dnfPackage: "pecl-apcu"},
	{id: "bcmath", aptPackage: "bcmath", dnfPackage: "bcmath"},
	{id: "bz2", aptPackage: "bz2"},
	{id: "calendar"},
	{id: "curl", aptPackage: "curl"},
	{id: "dba", aptPackage: "dba", dnfPackage: "dba"},
	{id: "dom", aptPackage: "xml", dnfPackage: "xml"},
	{id: "ds", aptPackage: "ds", dnfPackage: "pecl-ds"},
	{id: "exif"},
	{id: "fileinfo"},
	{id: "gd", aptPackage: "gd", dnfPackage: "gd"},
	{id: "igbinary", aptPackage: "igbinary", dnfPackage: "pecl-igbinary"},
	{id: "imagemagick", aliases: []string{"imagick"}, aptPackage: "imagick", dnfPackage: "pecl-imagick-im7", sharedObject: "imagick"},
	{id: "imap", aptPackage: "imap", dnfPackage: "pecl-imap"},
	{id: "ioncube", label: "ionCube Loader", aliases: []string{"ioncube loader", "ioncubeloader"}, dnfPackage: "ioncube-loader", zendExtension: true},
	{id: "intl", aptPackage: "intl", dnfPackage: "intl"},
	{id: "ldap", aptPackage: "ldap", dnfPackage: "ldap"},
	{id: "mailparse", aptPackage: "mailparse", dnfPackage: "pecl-mailparse"},
	{id: "mcrypt", aptPackage: "mcrypt", dnfPackage: "pecl-mcrypt"},
	{id: "mbstring", aptPackage: "mbstring", dnfPackage: "mbstring"},
	{id: "memcached", aptPackage: "memcached", dnfPackage: "pecl-memcached"},
	{id: "msgpack", aptPackage: "msgpack", dnfPackage: "pecl-msgpack"},
	{id: "mysqli", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "mysqlnd", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "oci8"},
	{id: "opcache", aliases: []string{"zend opcache", "zendopcache"}, aptPackage: "opcache", dnfPackage: "opcache", zendExtension: true},
	{id: "openswoole", aptPackage: "openswoole"},
	{id: "parallel", dnfPackage: "pecl-parallel"},
	{id: "pcov", aptPackage: "pcov", dnfPackage: "pecl-pcov"},
	{id: "pdo", dnfPackage: "pdo"},
	{id: "pdo_mysql", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "pdo_oci", aliases: []string{"pdooci"}},
	{id: "pdo_odbc", aptPackage: "odbc", dnfPackage: "odbc"},
	{id: "pdo_pgsql", aliases: []string{"pdopgsql"}, aptPackage: "pgsql", dnfPackage: "pgsql"},
	{id: "pdo_sqlite", aptPackage: "sqlite3"},
	{id: "pdo_sqlsrv", aliases: []string{"pdosqlsrv"}},
	{id: "pgsql", aptPackage: "pgsql", dnfPackage: "pgsql"},
	{id: "phpmongodb", catalogID: "php_mongodb", label: "php_mongodb", aliases: []string{"php_mongodb", "mongodb"}, installID: "mongodb", aptPackage: "mongodb", dnfPackage: "pecl-mongodb", sharedObject: "mongodb"},
	{id: "protobuf", aptPackage: "protobuf", dnfPackage: "pecl-protobuf"},
	{id: "rdkafka", aliases: []string{"rdkakfa"}, aptPackage: "rdkafka", dnfPackage: "pecl-rdkafka6"},
	{id: "redis", aptPackage: "redis", dnfPackage: "pecl-redis6"},
	{id: "snmp", aptPackage: "snmp", dnfPackage: "snmp"},
	{id: "soap", aptPackage: "soap", dnfPackage: "soap"},
	{id: "sqlite3", aptPackage: "sqlite3"},
	{id: "ssh2", aptPackage: "ssh2", dnfPackage: "pecl-ssh2"},
	{id: "swoole", aliases: []string{"swoole6"}, aptPackage: "swoole"},
	{id: "tidy", aptPackage: "tidy", dnfPackage: "tidy"},
	{id: "uuid", aptPackage: "uuid", dnfPackage: "pecl-uuid"},
	{id: "xdebug", aptPackage: "xdebug", dnfPackage: "pecl-xdebug3", zendExtension: true},
	{id: "xlswriter", aptPackage: "xlswriter", dnfPackage: "pecl-xlswriter"},
	{id: "xmlreader", aptPackage: "xml", dnfPackage: "xml"},
	{id: "xmlwriter", aptPackage: "xml", dnfPackage: "xml"},
	{id: "xsl", aptPackage: "xsl", dnfPackage: "xml"},
	{id: "yaml", aptPackage: "yaml", dnfPackage: "pecl-yaml"},
	{id: "zip", aptPackage: "zip", dnfPackage: "pecl-zip"},
	{id: "zstd", aptPackage: "zstd", dnfPackage: "zstd"},
}

const (
	ionCubeLinuxAMD64URL = "https://downloads.ioncube.com/loader_downloads/ioncube_loaders_lin_x86-64.tar.gz"
	ionCubeLinuxARM64URL = "https://downloads.ioncube.com/loader_downloads/ioncube_loaders_lin_aarch64.tar.gz"
)

func PHPExtensionCatalog() []PHPExtensionCatalogEntry {
	catalog := make([]PHPExtensionCatalogEntry, 0, len(phpExtensionDefinitions))
	for _, definition := range phpExtensionDefinitions {
		catalog = append(catalog, definition.catalogEntry())
	}
	return catalog
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
	initialStatus := s.StatusForVersion(ctx, version)
	if !initialStatus.PHPInstalled || initialStatus.PHPPath == "" {
		return RuntimeStatus{}, fmt.Errorf("php %s is not installed", initialStatus.Version)
	}

	requestedExtension := strings.TrimSpace(extension)
	definition, ok := findPHPExtensionDefinition(requestedExtension)
	if !ok {
		return RuntimeStatus{}, fmt.Errorf("php extension %q is not supported", requestedExtension)
	}
	if !definition.supportsPackageInstall(initialStatus.Version, initialStatus.PackageManager) {
		return RuntimeStatus{}, fmt.Errorf("php extension %q does not have a configured automatic install path for php %s", requestedExtension, initialStatus.Version)
	}
	if extensionLoaded(initialStatus.Extensions, definition) {
		return initialStatus, nil
	}
	packageWasInstalled := phpExtensionPackageInstalled(ctx, initialStatus, definition)

	if err := installPHPExtensionPackage(ctx, initialStatus, definition); err != nil {
		return RuntimeStatus{}, err
	}

	rollbackInstallFailure := func(cause error) (RuntimeStatus, error) {
		status, rollbackErr := s.rollbackFailedExtensionInstall(ctx, initialStatus, definition, packageWasInstalled)
		if rollbackErr != nil {
			return status, fmt.Errorf("%w; FlowPanel could not fully disable %q again: %v", cause, requestedExtension, rollbackErr)
		}
		return status, fmt.Errorf("%w; FlowPanel disabled %q again to keep PHP running", cause, requestedExtension)
	}

	runtimeStatus := s.StatusForVersion(ctx, initialStatus.Version)
	if !extensionLoaded(runtimeStatus.Extensions, definition) {
		if _, err := enablePHPExtension(ctx, runtimeStatus, definition); err != nil {
			return rollbackInstallFailure(err)
		}
		runtimeStatus = s.StatusForVersion(ctx, runtimeStatus.Version)
	}
	if initialStatus.ServiceRunning {
		if err := s.RestartVersion(ctx, runtimeStatus.Version); err != nil {
			fpmPath := runtimeStatus.FPMPath
			if fpmPath == "" {
				fpmPath = initialStatus.FPMPath
			}
			if fpmPath == "" {
				return rollbackInstallFailure(fmt.Errorf("php extension installed but failed to restart php-fpm: %w", err))
			}
			if fallbackErr := restartPHPFPM(ctx, fpmPath); fallbackErr != nil {
				return rollbackInstallFailure(fmt.Errorf("php extension installed but failed to restart php-fpm: %w", err))
			}
		}
	}

	runtimeStatus = s.StatusForVersion(ctx, runtimeStatus.Version)
	if err := validateInstalledExtension(ctx, runtimeStatus, requestedExtension, definition); err != nil {
		return rollbackInstallFailure(err)
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

func (d phpExtensionDefinition) isIONCubeLoader() bool {
	return normalizePHPExtensionKey(d.id) == "ioncube"
}

func (d phpExtensionDefinition) supportsPackageInstall(version, packageManager string) bool {
	if d.isIONCubeLoader() {
		switch strings.TrimSpace(packageManager) {
		case "apt", "dnf", "yum":
			return runtime.GOOS == "linux"
		default:
			return false
		}
	}

	switch strings.TrimSpace(packageManager) {
	case "apt":
		if strings.TrimSpace(d.aptPackage) == "" {
			return false
		}
	case "dnf":
		if strings.TrimSpace(d.dnfPackage) == "" {
			return false
		}
	default:
		return false
	}
	if normalizePHPExtensionKey(d.id) == "opcache" && !phpVersionHasSeparateOpcachePackage(version) {
		return false
	}
	return true
}

func (d phpExtensionDefinition) packageName(version, packageManager string) string {
	switch strings.TrimSpace(packageManager) {
	case "apt":
		return "php" + NormalizeVersion(version) + "-" + d.aptPackage
	case "dnf":
		return remiCollectionForVersion(version) + "-php-" + d.dnfPackage
	default:
		return ""
	}
}

func (d phpExtensionDefinition) catalogEntry() PHPExtensionCatalogEntry {
	return PHPExtensionCatalogEntry{
		ID:                     d.catalogKey(),
		Label:                  d.catalogLabel(),
		Aliases:                append([]string(nil), d.aliases...),
		InstallID:              d.catalogInstallID(),
		InstallPackageManagers: d.catalogInstallPackageManagers(),
	}
}

func (d phpExtensionDefinition) catalogKey() string {
	if value := strings.TrimSpace(d.catalogID); value != "" {
		return value
	}
	return strings.TrimSpace(d.id)
}

func (d phpExtensionDefinition) catalogLabel() string {
	if value := strings.TrimSpace(d.label); value != "" {
		return value
	}
	return d.catalogKey()
}

func (d phpExtensionDefinition) catalogInstallID() string {
	if value := strings.TrimSpace(d.installID); value != "" {
		return value
	}
	if !d.hasManagedInstall() {
		return ""
	}
	return strings.TrimSpace(d.id)
}

func (d phpExtensionDefinition) catalogInstallPackageManagers() []string {
	if d.isIONCubeLoader() {
		if runtime.GOOS != "linux" {
			return nil
		}
		return []string{"apt", "dnf", "yum"}
	}
	if normalizePHPExtensionKey(d.id) == "opcache" {
		return nil
	}

	managers := make([]string, 0, 2)
	if strings.TrimSpace(d.aptPackage) != "" {
		managers = append(managers, "apt")
	}
	if strings.TrimSpace(d.dnfPackage) != "" {
		managers = append(managers, "dnf")
	}
	return managers
}

func (d phpExtensionDefinition) hasManagedInstall() bool {
	if d.isIONCubeLoader() {
		return runtime.GOOS == "linux"
	}
	return strings.TrimSpace(d.aptPackage) != "" || strings.TrimSpace(d.dnfPackage) != ""
}

func (d phpExtensionDefinition) sharedObjectName() string {
	if name := strings.TrimSpace(d.sharedObject); name != "" {
		return strings.TrimSuffix(name, ".so")
	}
	if name := strings.TrimSpace(d.installID); name != "" {
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

func validateInstalledExtension(ctx context.Context, runtimeStatus RuntimeStatus, requestedExtension string, definition phpExtensionDefinition) error {
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return nil
	}

	message := fmt.Sprintf("php extension %q was installed but is not loaded for php %s", requestedExtension, runtimeStatus.Version)
	if diagnostics := inspectPHPExtensionLoadDiagnostics(ctx, runtimeStatus, definition); diagnostics != "" {
		message += ": " + diagnostics
	}
	return fmt.Errorf("%s", message)
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

func installPHPExtensionPackage(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	if definition.isIONCubeLoader() {
		return installIONCubeLoader(ctx, runtimeStatus)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("automatic PHP extension installation is only supported on Linux runtimes")
	}

	packageManager := strings.TrimSpace(runtimeStatus.PackageManager)
	packageName := definition.packageName(runtimeStatus.Version, packageManager)
	if packageName == "" {
		return fmt.Errorf("automatic PHP extension installation is not supported for %s-managed runtimes", packageManager)
	}

	commandName := packageManager
	if packageManager == "apt" {
		commandName = "apt-get"
	}

	packageManagerPath, ok := lookupCommand(commandName)
	if !ok {
		return fmt.Errorf("%s is not available", commandName)
	}

	installArgs := []string{packageManagerPath, "install", "-y", packageName}
	if err := runCommands(ctx, installArgs); err != nil {
		if packageManager == "apt" && shouldRetryAPTInstallWithOndrej(versionActionPlan{packageManager: runtimeStatus.PackageManager}, err) {
			if bootstrapErr := bootstrapOndrejPHPRepository(ctx); bootstrapErr != nil {
				return fmt.Errorf("bootstrap ondrej/php repository: %w", bootstrapErr)
			}
			if retryErr := runCommands(ctx, installArgs); retryErr == nil {
				return nil
			} else {
				err = retryErr
			}
		}
		return fmt.Errorf("install %s via %s: %w", packageName, packageManager, err)
	}

	return nil
}

func removePHPExtensionPackage(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	if definition.isIONCubeLoader() {
		return removeIONCubeLoader(runtimeStatus)
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("automatic PHP extension removal is only supported on Linux runtimes")
	}

	packageManager := strings.TrimSpace(runtimeStatus.PackageManager)
	packageName := definition.packageName(runtimeStatus.Version, packageManager)
	if packageName == "" {
		return fmt.Errorf("automatic PHP extension removal is not supported for %s-managed runtimes", packageManager)
	}

	commandName := packageManager
	if packageManager == "apt" {
		commandName = "apt-get"
	}

	packageManagerPath, ok := lookupCommand(commandName)
	if !ok {
		return fmt.Errorf("%s is not available", commandName)
	}

	removeArgs := []string{packageManagerPath, "remove", "-y", packageName}
	if err := runCommands(ctx, removeArgs); err != nil {
		return fmt.Errorf("remove %s via %s: %w", packageName, packageManager, err)
	}

	return nil
}

func phpExtensionPackageInstalled(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) bool {
	if definition.isIONCubeLoader() {
		return ionCubeLoaderInstalled(runtimeStatus)
	}
	packageName := definition.packageName(runtimeStatus.Version, runtimeStatus.PackageManager)
	if packageName == "" {
		return false
	}

	switch strings.TrimSpace(runtimeStatus.PackageManager) {
	case "apt":
		dpkgQueryPath, ok := lookupCommand("dpkg-query")
		if !ok {
			return false
		}
		output, err := runCommand(ctx, dpkgQueryPath, "-W", "-f=${Status}", packageName)
		return err == nil && strings.Contains(strings.ToLower(output), "install ok installed")
	case "dnf", "yum":
		rpmPath, ok := lookupCommand("rpm")
		if !ok {
			return false
		}
		_, err := runCommand(ctx, rpmPath, "-q", packageName)
		return err == nil
	default:
		return false
	}
}

func enablePHPExtension(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) (bool, error) {
	if enabled, err := enablePHPExtensionWithTool(ctx, runtimeStatus, definition); enabled || err != nil {
		return enabled, err
	}
	return writeManagedPHPExtensionConfig(runtimeStatus, definition)
}

func disablePHPExtension(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	var errs []string
	if _, err := disablePHPExtensionWithTool(ctx, runtimeStatus, definition); err != nil {
		errs = append(errs, err.Error())
	}
	if _, err := removeManagedPHPExtensionConfig(runtimeStatus, definition); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func enablePHPExtensionWithTool(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) (bool, error) {
	if strings.TrimSpace(runtimeStatus.PackageManager) != "apt" {
		return false, nil
	}

	phpenmodPath, ok := lookupCommand("phpenmod")
	if !ok {
		return false, nil
	}

	args := []string{phpenmodPath}
	if version := NormalizeVersion(runtimeStatus.Version); version != "" {
		args = append(args, "-v", version)
	}
	if runtimeStatus.FPMInstalled {
		args = append(args, "-s", "cli,fpm")
	} else {
		args = append(args, "-s", "cli")
	}
	args = append(args, definition.sharedObjectName())

	if _, err := runCommand(ctx, args[0], args[1:]...); err != nil {
		return false, nil
	}

	return true, nil
}

func disablePHPExtensionWithTool(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) (bool, error) {
	if strings.TrimSpace(runtimeStatus.PackageManager) != "apt" {
		return false, nil
	}

	phpdismodPath, ok := lookupCommand("phpdismod")
	if !ok {
		return false, nil
	}

	args := []string{phpdismodPath}
	if version := NormalizeVersion(runtimeStatus.Version); version != "" {
		args = append(args, "-v", version)
	}
	if runtimeStatus.FPMInstalled {
		args = append(args, "-s", "cli,fpm")
	} else {
		args = append(args, "-s", "cli")
	}
	args = append(args, definition.sharedObjectName())

	if _, err := runCommand(ctx, args[0], args[1:]...); err != nil {
		return false, nil
	}

	return true, nil
}

func writeManagedPHPExtensionConfig(runtimeStatus RuntimeStatus, definition phpExtensionDefinition) (bool, error) {
	paths := managedPHPExtensionConfigPaths(runtimeStatus, definition)
	if len(paths) == 0 {
		return false, nil
	}

	content := renderManagedPHPExtensionConfig(runtimeStatus, definition)
	wroteConfig := false
	for _, path := range paths {
		exists, err := phpExtensionConfigExists(path, definition)
		if err != nil {
			return wroteConfig, err
		}
		if exists {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return wroteConfig, fmt.Errorf("create php extension config directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return wroteConfig, fmt.Errorf("write php extension config: %w", err)
		}
		wroteConfig = true
	}

	return wroteConfig, nil
}

func removeManagedPHPExtensionConfig(runtimeStatus RuntimeStatus, definition phpExtensionDefinition) (bool, error) {
	paths := managedPHPExtensionConfigPaths(runtimeStatus, definition)
	removedConfig := false
	for _, path := range paths {
		if err := os.Remove(path); err == nil {
			removedConfig = true
			continue
		} else if os.IsNotExist(err) {
			continue
		} else {
			return removedConfig, fmt.Errorf("remove php extension config: %w", err)
		}
	}

	return removedConfig, nil
}

func managedPHPExtensionConfigPaths(runtimeStatus RuntimeStatus, definition phpExtensionDefinition) []string {
	moduleName := definition.sharedObjectName()
	if moduleName == "" {
		return nil
	}

	dirs := []string{}
	if scanDir := strings.TrimSpace(runtimeStatus.ScanDir); scanDir != "" {
		dirs = append(dirs, scanDir)
	}

	version := NormalizeVersion(runtimeStatus.Version)
	switch strings.TrimSpace(runtimeStatus.PackageManager) {
	case "apt":
		if version != "" {
			dirs = append(dirs, filepath.Join("/etc/php", version, "cli", "conf.d"))
			if runtimeStatus.FPMInstalled {
				dirs = append(dirs, filepath.Join("/etc/php", version, "fpm", "conf.d"))
			}
		}
	case "dnf", "yum":
		if version != "" {
			dirs = append(dirs, filepath.Join("/etc/opt/remi", remiCollectionForVersion(version), "php.d"))
		}
		dirs = append(dirs, "/etc/php.d")
	}

	paths := make([]string, 0, len(dirs))
	for _, dir := range dedupeStrings(dirs) {
		paths = append(paths, filepath.Join(dir, managedPHPExtensionConfigFilename(definition)))
	}
	return dedupeStrings(paths)
}

func managedPHPExtensionConfigFilename(definition phpExtensionDefinition) string {
	prefix := "99"
	if definition.isIONCubeLoader() {
		prefix = "00"
	}
	return prefix + "-flowpanel-" + definition.sharedObjectName() + ".ini"
}

func renderManagedPHPExtensionConfig(runtimeStatus RuntimeStatus, definition phpExtensionDefinition) string {
	directive := "extension"
	value := definition.sharedObjectName() + ".so"
	if definition.zendExtension {
		directive = "zend_extension"
	}
	if definition.isIONCubeLoader() {
		if loaderPath := ionCubeLoaderPath(runtimeStatus); loaderPath != "" {
			value = loaderPath
		}
	}
	return fmt.Sprintf("; Managed by FlowPanel.\n%s=%s\n", directive, value)
}

func installIONCubeLoader(ctx context.Context, runtimeStatus RuntimeStatus) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("automatic ionCube Loader installation is only supported on Linux runtimes")
	}

	targetPath := ionCubeLoaderPath(runtimeStatus)
	if targetPath == "" {
		return fmt.Errorf("flowpanel could not determine the ionCube Loader target path for php %s", runtimeStatus.Version)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create php extension directory: %w", err)
	}

	archiveURL, err := ionCubeLoaderArchiveURL()
	if err != nil {
		return err
	}
	if err := downloadIONCubeLoader(ctx, archiveURL, filepath.Base(targetPath), targetPath); err != nil {
		return err
	}
	return nil
}

func removeIONCubeLoader(runtimeStatus RuntimeStatus) error {
	targetPath := ionCubeLoaderPath(runtimeStatus)
	if targetPath == "" {
		return fmt.Errorf("flowpanel could not determine the ionCube Loader target path for php %s", runtimeStatus.Version)
	}
	if err := os.Remove(targetPath); err == nil || os.IsNotExist(err) {
		return nil
	} else {
		return fmt.Errorf("remove ionCube Loader: %w", err)
	}
}

func ionCubeLoaderInstalled(runtimeStatus RuntimeStatus) bool {
	targetPath := ionCubeLoaderPath(runtimeStatus)
	if targetPath == "" {
		return false
	}
	info, err := os.Stat(targetPath)
	return err == nil && !info.IsDir()
}

func ionCubeLoaderPath(runtimeStatus RuntimeStatus) string {
	if runtimeStatus.extensionDir == "" {
		return ""
	}
	loaderName := ionCubeLoaderFilename(runtimeStatus.Version)
	if loaderName == "" {
		return ""
	}
	return filepath.Join(runtimeStatus.extensionDir, loaderName)
}

func ionCubeLoaderFilename(version string) string {
	version = NormalizeVersion(version)
	if version == "" {
		return ""
	}
	return "ioncube_loader_lin_" + version + ".so"
}

func ionCubeLoaderArchiveURL() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return ionCubeLinuxAMD64URL, nil
	case "arm64":
		return ionCubeLinuxARM64URL, nil
	default:
		return "", fmt.Errorf("automatic ionCube Loader installation is not supported on linux/%s", runtime.GOARCH)
	}
}

func downloadIONCubeLoader(ctx context.Context, archiveURL, loaderName, targetPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return fmt.Errorf("prepare ionCube download: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download ionCube Loader: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download ionCube Loader: unexpected HTTP status %s", response.Status)
	}

	gzipReader, err := gzip.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("read ionCube archive: %w", err)
	}
	defer gzipReader.Close()

	archivePath := "ioncube/" + loaderName
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		switch {
		case err == io.EOF:
			return fmt.Errorf("download ionCube Loader: %s was not found in the archive", loaderName)
		case err != nil:
			return fmt.Errorf("read ionCube archive: %w", err)
		case header == nil:
			continue
		case header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA:
			continue
		}

		name := filepath.ToSlash(strings.TrimPrefix(header.Name, "./"))
		if name != archivePath {
			continue
		}

		tempFile, err := os.CreateTemp(filepath.Dir(targetPath), "ioncube-*.so")
		if err != nil {
			return fmt.Errorf("create temporary ionCube Loader file: %w", err)
		}
		tempPath := tempFile.Name()
		defer os.Remove(tempPath)

		if _, err := io.Copy(tempFile, reader); err != nil {
			tempFile.Close()
			return fmt.Errorf("write ionCube Loader: %w", err)
		}
		if err := tempFile.Close(); err != nil {
			return fmt.Errorf("close ionCube Loader: %w", err)
		}
		if err := os.Chmod(tempPath, 0o644); err != nil {
			return fmt.Errorf("chmod ionCube Loader: %w", err)
		}
		if err := os.Rename(tempPath, targetPath); err != nil {
			return fmt.Errorf("install ionCube Loader: %w", err)
		}
		return nil
	}
}

func phpExtensionConfigExists(path string, definition phpExtensionDefinition) (bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return phpExtensionConfigReferences(string(data), definition), nil
	}
	if !os.IsNotExist(err) {
		return false, fmt.Errorf("read php extension config: %w", err)
	}

	dir := filepath.Dir(path)
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return false, nil
		}
		return false, fmt.Errorf("read php extension config directory: %w", readErr)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ini") {
			continue
		}
		content, entryErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if entryErr != nil {
			return false, fmt.Errorf("read php extension config entry: %w", entryErr)
		}
		if phpExtensionConfigReferences(string(content), definition) {
			return true, nil
		}
	}

	return false, nil
}

func phpExtensionConfigReferences(content string, definition phpExtensionDefinition) bool {
	contentKey := normalizePHPExtensionKey(content)
	for _, candidate := range extensionLoadCandidates(definition) {
		if candidate != "" && strings.Contains(contentKey, candidate) {
			return true
		}
	}
	return false
}

func inspectPHPExtensionLoadDiagnostics(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) string {
	if runtimeStatus.PHPPath == "" {
		return ""
	}

	output, err := runInspectCommand(ctx, runtimeStatus.PHPPath, "-d", "display_startup_errors=1", "-m")
	if err != nil && strings.TrimSpace(output) == "" {
		return ""
	}

	lines := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		lineKey := normalizePHPExtensionKey(line)
		isRelevant := strings.Contains(strings.ToLower(line), "unable to load") ||
			strings.Contains(strings.ToLower(line), "startup") ||
			strings.Contains(strings.ToLower(line), "warning") ||
			strings.Contains(strings.ToLower(line), "error")
		if !isRelevant {
			continue
		}
		for _, candidate := range extensionLoadCandidates(definition) {
			if candidate != "" && strings.Contains(lineKey, candidate) {
				if _, ok := seen[line]; ok {
					break
				}
				seen[line] = struct{}{}
				lines = append(lines, line)
				break
			}
		}
		if len(lines) == 2 {
			break
		}
	}

	return strings.Join(lines, "; ")
}

func (s *Service) rollbackFailedExtensionInstall(
	ctx context.Context,
	initialStatus RuntimeStatus,
	definition phpExtensionDefinition,
	packageWasInstalled bool,
) (RuntimeStatus, error) {
	runtimeStatus := s.StatusForVersion(ctx, initialStatus.Version)
	var errs []string

	if err := disablePHPExtension(ctx, runtimeStatus, definition); err != nil {
		errs = append(errs, fmt.Sprintf("disable extension: %v", err))
	}

	runtimeStatus = s.StatusForVersion(ctx, initialStatus.Version)
	diagnostics := inspectPHPExtensionLoadDiagnostics(ctx, runtimeStatus, definition)
	shouldRemovePackage := !packageWasInstalled || extensionLoaded(runtimeStatus.Extensions, definition) || diagnostics != ""
	if shouldRemovePackage {
		if err := removePHPExtensionPackage(ctx, runtimeStatus, definition); err != nil {
			errs = append(errs, fmt.Sprintf("remove package: %v", err))
		}
		runtimeStatus = s.StatusForVersion(ctx, initialStatus.Version)
	}

	if initialStatus.ServiceRunning {
		if err := s.RestartVersion(ctx, initialStatus.Version); err != nil {
			fpmPath := runtimeStatus.FPMPath
			if fpmPath == "" {
				fpmPath = initialStatus.FPMPath
			}
			if fpmPath == "" {
				errs = append(errs, fmt.Sprintf("restart php-fpm: %v", err))
			} else if fallbackErr := restartPHPFPM(ctx, fpmPath); fallbackErr != nil {
				errs = append(errs, fmt.Sprintf("restart php-fpm: %v", err))
			}
		}
		runtimeStatus = s.StatusForVersion(ctx, initialStatus.Version)
	}

	if len(errs) > 0 {
		return runtimeStatus, fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	return runtimeStatus, nil
}

func extensionLoadCandidates(definition phpExtensionDefinition) []string {
	candidates := append([]string{definition.id}, definition.aliases...)
	if sharedObject := definition.sharedObjectName(); sharedObject != "" {
		candidates = append(candidates, sharedObject)
	}

	keys := make([]string, 0, len(candidates))
	for _, candidate := range dedupeStrings(candidates) {
		if key := normalizePHPExtensionKey(candidate); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
