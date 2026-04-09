package phpenv

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

type phpExtensionDefinition struct {
	id          string
	aliases     []string
	aptPackages []string
	rpmPackages []string
}

var phpExtensionDefinitions = []phpExtensionDefinition{
	{id: "oncube", aliases: []string{"ioncube", "ioncubeloader"}},
	{id: "fileinfo", aptPackages: []string{"php{version}-common", "php-common"}, rpmPackages: []string{"{remi}-php-common"}},
	{id: "opcache", aliases: []string{"zendopcache"}, aptPackages: []string{"php{version}-opcache", "php-opcache"}, rpmPackages: []string{"{remi}-php-opcache"}},
	{id: "memcached", aptPackages: []string{"php{version}-memcached", "php-memcached"}, rpmPackages: []string{"{remi}-php-pecl-memcached"}},
	{id: "redis", aptPackages: []string{"php{version}-redis", "php-redis"}, rpmPackages: []string{"{remi}-php-pecl-redis"}},
	{id: "mcrypt", aptPackages: []string{"php{version}-mcrypt", "php-mcrypt"}, rpmPackages: []string{"{remi}-php-pecl-mcrypt"}},
	{id: "apcu", aptPackages: []string{"php{version}-apcu", "php-apcu"}, rpmPackages: []string{"{remi}-php-pecl-apcu"}},
	{id: "imagemagick", aliases: []string{"imagick"}, aptPackages: []string{"php{version}-imagick", "php-imagick"}, rpmPackages: []string{"{remi}-php-pecl-imagick"}},
	{id: "xdebug", aptPackages: []string{"php{version}-xdebug", "php-xdebug"}, rpmPackages: []string{"{remi}-php-pecl-xdebug"}},
	{id: "imap", aptPackages: []string{"php{version}-imap", "php-imap"}, rpmPackages: []string{"{remi}-php-imap"}},
	{id: "exif", aptPackages: []string{"php{version}-common", "php-common"}, rpmPackages: []string{"{remi}-php-common"}},
	{id: "intl", aptPackages: []string{"php{version}-intl", "php-intl"}, rpmPackages: []string{"{remi}-php-intl"}},
	{id: "xsl", aptPackages: []string{"php{version}-xsl", "php-xsl", "php{version}-xml", "php-xml"}, rpmPackages: []string{"{remi}-php-xml"}},
	{id: "swoole4", aliases: []string{"swoole"}, aptPackages: []string{"php{version}-swoole", "php-swoole"}, rpmPackages: []string{"{remi}-php-pecl-swoole"}},
	{id: "swoole5", aliases: []string{"swoole"}, aptPackages: []string{"php{version}-swoole", "php-swoole"}, rpmPackages: []string{"{remi}-php-pecl-swoole"}},
	{id: "swoole6", aliases: []string{"swoole"}, aptPackages: []string{"php{version}-swoole", "php-swoole"}, rpmPackages: []string{"{remi}-php-pecl-swoole"}},
	{id: "xlswriter", aptPackages: []string{"php{version}-xlswriter", "php-xlswriter"}, rpmPackages: []string{"{remi}-php-pecl-xlswriter"}},
	{id: "oci8", aptPackages: []string{"php{version}-oci8", "php-oci8"}, rpmPackages: []string{"{remi}-php-oci8"}},
	{id: "pdooci", aliases: []string{"pdo_oci"}, aptPackages: []string{"php{version}-oci8", "php-oci8"}, rpmPackages: []string{"{remi}-php-oci8"}},
	{id: "swow", aptPackages: []string{"php{version}-swow", "php-swow"}, rpmPackages: []string{"{remi}-php-pecl-swow"}},
	{id: "pdosqlsrv", aliases: []string{"pdo_sqlsrv"}, aptPackages: []string{"php{version}-pdo-sqlsrv", "php-pdo-sqlsrv", "php{version}-sqlsrv", "php-sqlsrv"}, rpmPackages: []string{"{remi}-php-pecl-pdo_sqlsrv"}},
	{id: "sqlsrv", aptPackages: []string{"php{version}-sqlsrv", "php-sqlsrv"}, rpmPackages: []string{"{remi}-php-pecl-sqlsrv"}},
	{id: "rdkafka", aliases: []string{"rdkakfa"}, aptPackages: []string{"php{version}-rdkafka", "php-rdkafka"}, rpmPackages: []string{"{remi}-php-pecl-rdkafka"}},
	{id: "yaf", aptPackages: []string{"php{version}-yaf", "php-yaf"}, rpmPackages: []string{"{remi}-php-pecl-yaf"}},
	{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, aptPackages: []string{"php{version}-mongodb", "php-mongodb"}, rpmPackages: []string{"{remi}-php-pecl-mongodb"}},
	{id: "yac", aptPackages: []string{"php{version}-yac", "php-yac"}, rpmPackages: []string{"{remi}-php-pecl-yac"}},
	{id: "sg11", aliases: []string{"sourceguardian11"}},
	{id: "sg14", aliases: []string{"sourceguardian14"}},
	{id: "sg15", aliases: []string{"sourceguardian15"}},
	{id: "sg16", aliases: []string{"sourceguardian16"}},
	{id: "xload"},
	{id: "pgsql", aptPackages: []string{"php{version}-pgsql", "php-pgsql"}, rpmPackages: []string{"{remi}-php-pgsql"}},
	{id: "ssh2", aptPackages: []string{"php{version}-ssh2", "php-ssh2"}, rpmPackages: []string{"{remi}-php-pecl-ssh2"}},
	{id: "grpc", aptPackages: []string{"php{version}-grpc", "php-grpc"}, rpmPackages: []string{"{remi}-php-pecl-grpc"}},
	{id: "xhprof", aptPackages: []string{"php{version}-xhprof", "php-xhprof"}, rpmPackages: []string{"{remi}-php-pecl-xhprof"}},
	{id: "protobuf", aptPackages: []string{"php{version}-protobuf", "php-protobuf"}, rpmPackages: []string{"{remi}-php-pecl-protobuf"}},
	{id: "pdopgsql", aliases: []string{"pdo_pgsql"}, aptPackages: []string{"php{version}-pgsql", "php-pgsql"}, rpmPackages: []string{"{remi}-php-pgsql"}},
	{id: "readline", aptPackages: []string{"php{version}-readline", "php-readline"}, rpmPackages: []string{"{remi}-php-process"}},
	{id: "snmp", aptPackages: []string{"php{version}-snmp", "php-snmp"}, rpmPackages: []string{"{remi}-php-snmp"}},
	{id: "ldap", aptPackages: []string{"php{version}-ldap", "php-ldap"}, rpmPackages: []string{"{remi}-php-ldap"}},
	{id: "enchant", aptPackages: []string{"php{version}-enchant", "php-enchant"}, rpmPackages: []string{"{remi}-php-enchant"}},
	{id: "pspell", aptPackages: []string{"php{version}-pspell", "php-pspell"}, rpmPackages: []string{"{remi}-php-pspell"}},
	{id: "bz2", aptPackages: []string{"php{version}-bz2", "php-bz2"}, rpmPackages: []string{"{remi}-php-bz2"}},
	{id: "sysvshm", aptPackages: []string{"php{version}-common", "php-common"}, rpmPackages: []string{"{remi}-php-common"}},
	{id: "calendar", aptPackages: []string{"php{version}-common", "php-common"}, rpmPackages: []string{"{remi}-php-common"}},
	{id: "gmp", aptPackages: []string{"php{version}-gmp", "php-gmp"}, rpmPackages: []string{"{remi}-php-gmp"}},
	{id: "sysvmsg", aptPackages: []string{"php{version}-common", "php-common"}, rpmPackages: []string{"{remi}-php-common"}},
	{id: "igbinary", aptPackages: []string{"php{version}-igbinary", "php-igbinary"}, rpmPackages: []string{"{remi}-php-pecl-igbinary"}},
	{id: "zmq", aptPackages: []string{"php{version}-zmq", "php-zmq"}, rpmPackages: []string{"{remi}-php-pecl-zmq"}},
	{id: "zstd", aptPackages: []string{"php{version}-zstd", "php-zstd"}, rpmPackages: []string{"{remi}-php-pecl-zstd"}},
	{id: "smbclient", aptPackages: []string{"php{version}-smbclient", "php-smbclient"}, rpmPackages: []string{"{remi}-php-pecl-smbclient"}},
	{id: "event", aptPackages: []string{"php{version}-event", "php-event"}, rpmPackages: []string{"{remi}-php-pecl-event"}},
	{id: "mailparse", aptPackages: []string{"php{version}-mailparse", "php-mailparse"}, rpmPackages: []string{"{remi}-php-pecl-mailparse"}},
	{id: "yaml", aptPackages: []string{"php{version}-yaml", "php-yaml"}, rpmPackages: []string{"{remi}-php-pecl-yaml"}},
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

	definition, ok := findPHPExtensionDefinition(extension)
	if !ok {
		return RuntimeStatus{}, fmt.Errorf("php extension %q is not supported", strings.TrimSpace(extension))
	}
	if extensionLoaded(runtimeStatus.Extensions, definition) {
		return runtimeStatus, nil
	}

	commands := buildPHPExtensionInstallCommands(runtimeStatus.Version, definition)
	if len(commands) == 0 {
		return RuntimeStatus{}, fmt.Errorf(
			"automatic installation for PHP extension %q is not supported on %s",
			strings.TrimSpace(extension),
			runtime.GOOS,
		)
	}
	if err := runAlternativeCommands(ctx, commands); err != nil {
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

	return s.StatusForVersion(ctx, runtimeStatus.Version), nil
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
	for _, candidate := range candidates {
		if _, ok := loaded[normalizePHPExtensionKey(candidate)]; ok {
			return true
		}
	}

	return false
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

func buildPHPExtensionInstallCommands(version string, definition phpExtensionDefinition) [][]string {
	if aptGetPath, ok := lookupCommand("apt-get"); ok {
		return buildPackageInstallCommands(aptGetPath, "install", "-y", version, definition.aptPackages)
	}
	if dnfPath, ok := lookupCommand("dnf"); ok {
		return buildPackageInstallCommands(dnfPath, "install", "-y", version, definition.rpmPackages)
	}
	if yumPath, ok := lookupCommand("yum"); ok {
		return buildPackageInstallCommands(yumPath, "install", "-y", version, definition.rpmPackages)
	}
	return nil
}

func buildPackageInstallCommands(path string, action string, confirmation string, version string, templates []string) [][]string {
	commands := make([][]string, 0, len(templates))
	for _, template := range templates {
		pkg := renderExtensionPackageTemplate(template, version)
		if strings.TrimSpace(pkg) == "" {
			continue
		}
		commands = append(commands, []string{path, action, confirmation, pkg})
	}
	return dedupeCommandSets(commands)
}

func renderExtensionPackageTemplate(template string, version string) string {
	replacer := strings.NewReplacer(
		"{version}", version,
		"{remi}", remiCollectionForVersion(version),
	)
	return replacer.Replace(strings.TrimSpace(template))
}

func dedupeCommandSets(commands [][]string) [][]string {
	deduped := make([][]string, 0, len(commands))
	seen := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		key := strings.Join(command, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, command)
	}
	return deduped
}

func runAlternativeCommands(ctx context.Context, commands [][]string) error {
	var failures []string
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if _, err := runCommand(ctx, command[0], command[1:]...); err == nil {
			return nil
		} else {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) == 0 {
		return fmt.Errorf("no installation command candidates were generated")
	}
	if len(failures) == 1 {
		return fmt.Errorf("php extension install failed: %s", failures[0])
	}

	return fmt.Errorf("php extension install failed: %s", strings.Join(failures, " | "))
}
