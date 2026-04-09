package phpenv

import (
	"reflect"
	"strings"
	"testing"
)

func TestFindPHPExtensionDefinition(t *testing.T) {
	tests := []struct {
		input         string
		wantID        string
		wantPECL      string
		wantSupported bool
	}{
		{input: "redis", wantID: "redis", wantPECL: "redis", wantSupported: true},
		{input: "imagick", wantID: "imagemagick", wantPECL: "imagick", wantSupported: true},
		{input: "pdo_sqlsrv", wantID: "pdosqlsrv", wantPECL: "pdo_sqlsrv", wantSupported: true},
		{input: "fileinfo", wantID: "fileinfo", wantPECL: "", wantSupported: false},
	}

	for _, test := range tests {
		definition, ok := findPHPExtensionDefinition(test.input)
		if !ok {
			t.Fatalf("findPHPExtensionDefinition(%q) ok = false, want true", test.input)
		}
		if definition.id != test.wantID {
			t.Fatalf("findPHPExtensionDefinition(%q) id = %q, want %q", test.input, definition.id, test.wantID)
		}
		if definition.peclPackage != test.wantPECL {
			t.Fatalf("findPHPExtensionDefinition(%q) peclPackage = %q, want %q", test.input, definition.peclPackage, test.wantPECL)
		}
		if definition.supportsPECLInstall() != test.wantSupported {
			t.Fatalf("findPHPExtensionDefinition(%q) supportsPECLInstall = %t, want %t", test.input, definition.supportsPECLInstall(), test.wantSupported)
		}
	}
}

func TestExtensionLoadedMatchesAliasesAndSharedObject(t *testing.T) {
	tests := []struct {
		name       string
		definition phpExtensionDefinition
		installed  []string
		want       bool
	}{
		{
			name:       "alias match",
			definition: phpExtensionDefinition{id: "imagemagick", aliases: []string{"imagick"}, peclPackage: "imagick", sharedObject: "imagick"},
			installed:  []string{"imagick"},
			want:       true,
		},
		{
			name:       "shared object match",
			definition: phpExtensionDefinition{id: "phpmongodb", aliases: []string{"php_mongodb", "mongodb"}, peclPackage: "mongodb", sharedObject: "mongodb"},
			installed:  []string{"mongodb"},
			want:       true,
		},
		{
			name:       "no match",
			definition: phpExtensionDefinition{id: "redis", peclPackage: "redis"},
			installed:  []string{"xdebug"},
			want:       false,
		},
	}

	for _, test := range tests {
		if got := extensionLoaded(test.installed, test.definition); got != test.want {
			t.Fatalf("extensionLoaded(%q) = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestRenderManagedPHPExtensionConfig(t *testing.T) {
	tests := []struct {
		name       string
		definition phpExtensionDefinition
		wantLine   string
	}{
		{
			name:       "extension",
			definition: phpExtensionDefinition{id: "redis", peclPackage: "redis"},
			wantLine:   "extension=redis.so",
		},
		{
			name:       "zend extension",
			definition: phpExtensionDefinition{id: "xdebug", peclPackage: "xdebug", enableMode: phpExtensionEnableModeZendExtension},
			wantLine:   "zend_extension=xdebug.so",
		},
	}

	for _, test := range tests {
		config := renderManagedPHPExtensionConfig(test.definition)
		if !strings.Contains(config, test.wantLine) {
			t.Fatalf("renderManagedPHPExtensionConfig(%q) = %q, want line %q", test.name, config, test.wantLine)
		}
	}
}

func TestDetermineManagedPHPExtensionConfigFile(t *testing.T) {
	definition := phpExtensionDefinition{id: "redis", peclPackage: "redis"}

	got := determineManagedPHPExtensionConfigFile("/etc/php/8.3/cli/php.ini", "/etc/php/8.3/cli/conf.d", definition)
	want := "/etc/php/8.3/cli/conf.d/flowpanel-redis.ini"
	if got != want {
		t.Fatalf("determineManagedPHPExtensionConfigFile() = %q, want %q", got, want)
	}
}

func TestPECLBinaryCandidatesPreferVersionSpecificPaths(t *testing.T) {
	got := peclBinaryCandidates("8.3", "/usr/bin/php8.3")
	wantPrefix := []string{"/usr/bin/pecl", "/usr/bin/pecl8.3", "/usr/bin/pecl83", "pecl8.3", "pecl83", "pecl"}
	if !reflect.DeepEqual(got, wantPrefix) {
		t.Fatalf("peclBinaryCandidates() = %#v, want %#v", got, wantPrefix)
	}
}

func TestValidateInstalledExtension(t *testing.T) {
	definition := phpExtensionDefinition{
		id:           "ioncube",
		aliases:      []string{"ioncubeloader"},
		sharedObject: "ioncube",
	}

	if err := validateInstalledExtension(
		RuntimeStatus{Version: "8.3", Extensions: []string{"ionCube Loader"}},
		"ioncube",
		definition,
	); err != nil {
		t.Fatalf("validateInstalledExtension() error = %v, want nil", err)
	}

	err := validateInstalledExtension(
		RuntimeStatus{Version: "8.3", Extensions: []string{"redis"}},
		"ioncube",
		definition,
	)
	if err == nil {
		t.Fatal("validateInstalledExtension() error = nil, want non-nil")
	}
	if got := err.Error(); got != `php extension "ioncube" was installed but is not loaded for php 8.3` {
		t.Fatalf("validateInstalledExtension() error = %q, want expected message", got)
	}
}
