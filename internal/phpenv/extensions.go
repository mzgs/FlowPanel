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
	sharedObject string
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "amqp", aptPackage: "amqp"},
	{id: "apcu", aptPackage: "apcu"},
	{id: "bcmath", aptPackage: "bcmath"},
	{id: "bz2", aptPackage: "bz2"},
	{id: "curl", aptPackage: "curl"},
	{id: "dba", aptPackage: "dba"},
	{id: "dom", aptPackage: "xml"},
	{id: "event", aptPackage: "event"},
	{id: "gd", aptPackage: "gd"},
	{id: "igbinary", aptPackage: "igbinary"},
	{id: "imap", aptPackage: "imap"},
	{id: "imagemagick", aliases: []string{"imagick"}, aptPackage: "imagick", sharedObject: "imagick"},
	{id: "intl", aptPackage: "intl"},
	{id: "ldap", aptPackage: "ldap"},
	{id: "mailparse", aptPackage: "mailparse"},
	{id: "mcrypt", aptPackage: "mcrypt"},
	{id: "mbstring", aptPackage: "mbstring"},
	{id: "memcached", aptPackage: "memcached"},
	{id: "msgpack", aptPackage: "msgpack"},
	{id: "mysqli", aptPackage: "mysql"},
	{id: "mysqlnd", aptPackage: "mysql"},
	{id: "opcache", aliases: []string{"zendopcache"}, aptPackage: "opcache"},
	{id: "pdo_mysql", aptPackage: "mysql"},
	{id: "pdo_odbc", aptPackage: "odbc"},
	{id: "pdo_pgsql", aliases: []string{"pdopgsql"}, aptPackage: "pgsql"},
	{id: "pdo_sqlite", aptPackage: "sqlite3"},
	{id: "pgsql", aptPackage: "pgsql"},
	{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, aptPackage: "mongodb", sharedObject: "mongodb"},
	{id: "pcov", aptPackage: "pcov"},
	{id: "redis", aptPackage: "redis"},
	{id: "snmp", aptPackage: "snmp"},
	{id: "soap", aptPackage: "soap"},
	{id: "sqlite3", aptPackage: "sqlite3"},
	{id: "ssh2", aptPackage: "ssh2"},
	{id: "tidy", aptPackage: "tidy"},
	{id: "timezonedb", aptPackage: "timezonedb"},
	{id: "uuid", aptPackage: "uuid"},
	{id: "xdebug", aptPackage: "xdebug"},
	{id: "xmlreader", aptPackage: "xml"},
	{id: "xmlwriter", aptPackage: "xml"},
	{id: "xsl", aptPackage: "xsl"},
	{id: "yaml", aptPackage: "yaml"},
	{id: "zip", aptPackage: "zip"},
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
	if !definition.supportsAPTInstall(runtimeStatus.Version, runtimeStatus.PackageManager) {
		return RuntimeStatus{}, fmt.Errorf("php extension %q does not have a configured distro package for php %s", requestedExtension, runtimeStatus.Version)
	}
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return runtimeStatus, nil
	}

	if err := installPHPExtensionWithAPT(ctx, runtimeStatus, definition); err != nil {
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

func (d phpExtensionDefinition) supportsAPTInstall(version, packageManager string) bool {
	if strings.TrimSpace(d.aptPackage) == "" || strings.TrimSpace(packageManager) != "apt" {
		return false
	}
	if normalizePHPExtensionKey(d.id) == "opcache" && !phpVersionHasSeparateOpcachePackage(version) {
		return false
	}
	return true
}

func (d phpExtensionDefinition) aptPackageName(version string) string {
	return "php" + NormalizeVersion(version) + "-" + d.aptPackage
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

func installPHPExtensionWithAPT(ctx context.Context, runtimeStatus RuntimeStatus, definition phpExtensionDefinition) error {
	if runtime.GOOS != "linux" || strings.TrimSpace(runtimeStatus.PackageManager) != "apt" {
		return fmt.Errorf("automatic PHP extension installation is only supported with apt-managed Linux runtimes")
	}

	aptPath, ok := lookupCommand("apt-get")
	if !ok {
		return fmt.Errorf("apt-get is not available")
	}

	packageName := definition.aptPackageName(runtimeStatus.Version)
	installArgs := []string{aptPath, "install", "-y", packageName}
	if err := runCommands(ctx, installArgs); err != nil {
		if shouldRetryAPTInstallWithOndrej(versionActionPlan{packageManager: runtimeStatus.PackageManager}, err) {
			if bootstrapErr := bootstrapOndrejPHPRepository(ctx); bootstrapErr != nil {
				return fmt.Errorf("bootstrap ondrej/php repository: %w", bootstrapErr)
			}
			if retryErr := runCommands(ctx, installArgs); retryErr == nil {
				return nil
			} else {
				err = retryErr
			}
		}
		return fmt.Errorf("install %s via apt: %w", packageName, err)
	}

	return nil
}
