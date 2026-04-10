package phpenv

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

type phpExtensionDefinition struct {
	id           string
	aliases      []string
	aptPackage   string
	dnfPackage   string
	sharedObject string
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "amqp", aptPackage: "amqp", dnfPackage: "pecl-amqp"},
	{id: "apcu", aptPackage: "apcu", dnfPackage: "pecl-apcu"},
	{id: "bcmath", aptPackage: "bcmath", dnfPackage: "bcmath"},
	{id: "bz2", aptPackage: "bz2"},
	{id: "curl", aptPackage: "curl"},
	{id: "dba", aptPackage: "dba", dnfPackage: "dba"},
	{id: "dom", aptPackage: "xml", dnfPackage: "xml"},
	{id: "event", aptPackage: "event", dnfPackage: "pecl-event"},
	{id: "gd", aptPackage: "gd", dnfPackage: "gd"},
	{id: "igbinary", aptPackage: "igbinary", dnfPackage: "pecl-igbinary"},
	{id: "imap", aptPackage: "imap", dnfPackage: "pecl-imap"},
	{id: "imagemagick", aliases: []string{"imagick"}, aptPackage: "imagick", dnfPackage: "pecl-imagick-im7", sharedObject: "imagick"},
	{id: "intl", aptPackage: "intl", dnfPackage: "intl"},
	{id: "ldap", aptPackage: "ldap", dnfPackage: "ldap"},
	{id: "mailparse", aptPackage: "mailparse", dnfPackage: "pecl-mailparse"},
	{id: "mcrypt", aptPackage: "mcrypt", dnfPackage: "pecl-mcrypt"},
	{id: "mbstring", aptPackage: "mbstring", dnfPackage: "mbstring"},
	{id: "memcached", aptPackage: "memcached", dnfPackage: "pecl-memcached"},
	{id: "msgpack", aptPackage: "msgpack", dnfPackage: "pecl-msgpack"},
	{id: "mysqli", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "mysqlnd", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "opcache", aliases: []string{"zendopcache"}, aptPackage: "opcache", dnfPackage: "opcache"},
	{id: "pdo_mysql", aptPackage: "mysql", dnfPackage: "mysqlnd"},
	{id: "pdo_odbc", aptPackage: "odbc", dnfPackage: "odbc"},
	{id: "pdo_pgsql", aliases: []string{"pdopgsql"}, aptPackage: "pgsql", dnfPackage: "pgsql"},
	{id: "pdo_sqlite", aptPackage: "sqlite3"},
	{id: "pgsql", aptPackage: "pgsql", dnfPackage: "pgsql"},
	{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, aptPackage: "mongodb", dnfPackage: "pecl-mongodb", sharedObject: "mongodb"},
	{id: "pcov", aptPackage: "pcov", dnfPackage: "pecl-pcov"},
	{id: "redis", aptPackage: "redis", dnfPackage: "pecl-redis6"},
	{id: "snmp", aptPackage: "snmp", dnfPackage: "snmp"},
	{id: "soap", aptPackage: "soap", dnfPackage: "soap"},
	{id: "sqlite3", aptPackage: "sqlite3"},
	{id: "ssh2", aptPackage: "ssh2", dnfPackage: "pecl-ssh2"},
	{id: "tidy", aptPackage: "tidy", dnfPackage: "tidy"},
	{id: "timezonedb", aptPackage: "timezonedb"},
	{id: "uuid", aptPackage: "uuid", dnfPackage: "pecl-uuid"},
	{id: "xdebug", aptPackage: "xdebug", dnfPackage: "pecl-xdebug3"},
	{id: "xmlreader", aptPackage: "xml", dnfPackage: "xml"},
	{id: "xmlwriter", aptPackage: "xml", dnfPackage: "xml"},
	{id: "xsl", aptPackage: "xsl", dnfPackage: "xml"},
	{id: "yaml", aptPackage: "yaml", dnfPackage: "pecl-yaml"},
	{id: "zip", aptPackage: "zip", dnfPackage: "pecl-zip"},
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
	if !definition.supportsPackageInstall(runtimeStatus.Version, runtimeStatus.PackageManager) {
		return RuntimeStatus{}, fmt.Errorf("php extension %q does not have a configured distro package for php %s", requestedExtension, runtimeStatus.Version)
	}
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return runtimeStatus, nil
	}

	if err := installPHPExtensionPackage(ctx, runtimeStatus, definition); err != nil {
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

func (d phpExtensionDefinition) supportsPackageInstall(version, packageManager string) bool {
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

	return fmt.Errorf("php extension %q was installed but is not loaded for php %s", requestedExtension, runtimeStatus.Version)
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
