package phpenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseOSReleaseFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "os-release")
	content := "ID=ubuntu\nID_LIKE=\"debian ubuntu\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write os-release: %v", err)
	}

	info := parseOSReleaseFile(path)
	if info.id != "ubuntu" {
		t.Fatalf("expected id ubuntu, got %q", info.id)
	}
	if info.idLike != "debian ubuntu" {
		t.Fatalf("expected id_like %q, got %q", "debian ubuntu", info.idLike)
	}
}

func TestShouldRetryAPTInstallWithOndrej(t *testing.T) {
	t.Parallel()

	err := assertError("apt-get failed: E: Unable to locate package php8.4-fpm")
	if !isMissingAPTPackageError(err) {
		t.Fatal("expected missing apt package error to be detected")
	}

	otherErr := assertError("apt-get failed: temporary failure resolving archive.ubuntu.com")
	if isMissingAPTPackageError(otherErr) {
		t.Fatal("expected unrelated apt error to skip retry")
	}
}

func TestPHPVersionHasSeparateOpcachePackage(t *testing.T) {
	t.Parallel()

	if !phpVersionHasSeparateOpcachePackage("8.4") {
		t.Fatal("expected php 8.4 to keep separate opcache package")
	}
	if phpVersionHasSeparateOpcachePackage("8.5") {
		t.Fatal("expected php 8.5 to skip separate opcache package")
	}
}

func TestAptVersionPackagesSkipsOpcacheForPHP85(t *testing.T) {
	t.Parallel()

	if slices.Contains(aptVersionPackages("8.5"), "php8.5-opcache") {
		t.Fatal("expected php8.5-opcache to be omitted")
	}
	if !slices.Contains(aptVersionPackages("8.4"), "php8.4-opcache") {
		t.Fatal("expected php8.4-opcache to be present")
	}
}

func TestPHPToolCandidatesPreferVersionedBinary(t *testing.T) {
	t.Parallel()

	phpizeCandidates := phpizeBinaryCandidates("8.4", "/usr/bin/php8.4")
	if len(phpizeCandidates) == 0 || phpizeCandidates[0] != "/usr/bin/phpize8.4" {
		t.Fatalf("expected versioned phpize first, got %#v", phpizeCandidates)
	}
	if slices.Index(phpizeCandidates, "/usr/bin/phpize8.4") > slices.Index(phpizeCandidates, "/usr/bin/phpize") {
		t.Fatalf("expected versioned phpize before unversioned phpize, got %#v", phpizeCandidates)
	}

	phpConfigCandidates := phpConfigBinaryCandidates("8.4", "/usr/bin/php8.4")
	if len(phpConfigCandidates) == 0 || phpConfigCandidates[0] != "/usr/bin/php-config8.4" {
		t.Fatalf("expected versioned php-config first, got %#v", phpConfigCandidates)
	}
	if slices.Index(phpConfigCandidates, "/usr/bin/php-config8.4") > slices.Index(phpConfigCandidates, "/usr/bin/php-config") {
		t.Fatalf("expected versioned php-config before unversioned php-config, got %#v", phpConfigCandidates)
	}
}

func TestAMQPExtensionDefinesRabbitMQRequiredDependencies(t *testing.T) {
	t.Parallel()

	definition, ok := findPHPExtensionDefinition("amqp")
	if !ok {
		t.Fatal("expected amqp extension definition")
	}

	if !slices.Contains(definition.requiredDependencies.apt, "librabbitmq-dev") {
		t.Fatalf("expected amqp apt dependencies to include librabbitmq-dev, got %#v", definition.requiredDependencies.apt)
	}
	if !slices.Contains(definition.requiredDependencies.homebrew, "rabbitmq-c") {
		t.Fatalf("expected amqp homebrew dependencies to include rabbitmq-c, got %#v", definition.requiredDependencies.homebrew)
	}
}

func TestPHPExtensionEnableConfigPathsUseScanDir(t *testing.T) {
	t.Parallel()

	definition, ok := findPHPExtensionDefinition("amqp")
	if !ok {
		t.Fatal("expected amqp extension definition")
	}

	runtimeStatus := RuntimeStatus{
		Version:          "8.5",
		ScanDir:          "/etc/php/8.5/cli/conf.d",
		LoadedConfigFile: "/etc/php/8.5/cli/php.ini",
	}
	paths := phpExtensionEnableConfigPaths(runtimeStatus, definition)
	expected := "/etc/php/8.5/cli/conf.d/20-flowpanel-amqp.ini"
	if !slices.Contains(paths, expected) {
		t.Fatalf("expected enable config paths to include %q, got %#v", expected, paths)
	}
}

func TestPHPExtensionEnableConfigPathsIncludeFPMScanDir(t *testing.T) {
	t.Parallel()

	definition, ok := findPHPExtensionDefinition("amqp")
	if !ok {
		t.Fatal("expected amqp extension definition")
	}

	dir := t.TempDir()
	cliScanDir := filepath.Join(dir, "php", "8.5", "cli", "conf.d")
	fpmScanDir := filepath.Join(dir, "php", "8.5", "fpm", "conf.d")
	if err := os.MkdirAll(cliScanDir, 0o755); err != nil {
		t.Fatalf("create cli scan dir: %v", err)
	}
	if err := os.MkdirAll(fpmScanDir, 0o755); err != nil {
		t.Fatalf("create fpm scan dir: %v", err)
	}

	runtimeStatus := RuntimeStatus{
		Version:          "8.5",
		ScanDir:          cliScanDir,
		LoadedConfigFile: filepath.Join(dir, "php", "8.5", "cli", "php.ini"),
		FPMPath:          "/usr/sbin/php-fpm8.5",
	}
	paths := phpExtensionEnableConfigPaths(runtimeStatus, definition)
	expected := filepath.Join(fpmScanDir, "20-flowpanel-amqp.ini")
	if !slices.Contains(paths, expected) {
		t.Fatalf("expected enable config paths to include fpm path %q, got %#v", expected, paths)
	}
}

func TestRenderPHPExtensionEnableConfig(t *testing.T) {
	t.Parallel()

	amqpDefinition, ok := findPHPExtensionDefinition("amqp")
	if !ok {
		t.Fatal("expected amqp extension definition")
	}
	amqpConfig := renderPHPExtensionEnableConfig(amqpDefinition)
	if !strings.Contains(amqpConfig, "extension=amqp.so\n") {
		t.Fatalf("expected amqp config to enable amqp as extension, got %q", amqpConfig)
	}

	xdebugDefinition, ok := findPHPExtensionDefinition("xdebug")
	if !ok {
		t.Fatal("expected xdebug extension definition")
	}
	xdebugConfig := renderPHPExtensionEnableConfig(xdebugDefinition)
	if !strings.Contains(xdebugConfig, "zend_extension=xdebug.so\n") {
		t.Fatalf("expected xdebug config to enable xdebug as zend_extension, got %q", xdebugConfig)
	}
}

type staticError string

func (e staticError) Error() string {
	return string(e)
}

func assertError(message string) error {
	return staticError(message)
}
