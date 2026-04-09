package phpenv

import (
	"os"
	"path/filepath"
	"slices"
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

type staticError string

func (e staticError) Error() string {
	return string(e)
}

func assertError(message string) error {
	return staticError(message)
}
