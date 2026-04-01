package phpmyadmin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestDetectInstallPathUsesOverride(t *testing.T) {
	installPath := filepath.Join(t.TempDir(), "phpmyadmin")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}

	t.Setenv("FLOWPANEL_PHPMYADMIN_PATH", installPath)

	got, ok := detectInstallPath()
	if !ok {
		t.Fatal("detectInstallPath() ok = false, want true")
	}
	if got != installPath {
		t.Fatalf("detectInstallPath() = %q, want %q", got, installPath)
	}
}

func TestStatusReportsInstalledWhenPathExists(t *testing.T) {
	installPath := filepath.Join(t.TempDir(), "phpmyadmin")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}

	t.Setenv("FLOWPANEL_PHPMYADMIN_PATH", installPath)

	status := NewService(zap.NewNop()).Status(context.Background())
	if !status.Installed {
		t.Fatal("Installed = false, want true")
	}
	if status.State != "installed" {
		t.Fatalf("State = %q, want installed", status.State)
	}
	if status.InstallPath != installPath {
		t.Fatalf("InstallPath = %q, want %q", status.InstallPath, installPath)
	}
}

func TestDetectVersionReadsComposerJSON(t *testing.T) {
	installPath := filepath.Join(t.TempDir(), "phpmyadmin")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installPath, "composer.json"), []byte(`{"version":"5.2.3"}`), 0o644); err != nil {
		t.Fatalf("write composer.json: %v", err)
	}

	got := detectVersion(installPath)
	if got != "5.2.3" {
		t.Fatalf("detectVersion() = %q, want 5.2.3", got)
	}
}

func TestAptDebconfSelectionsCommand(t *testing.T) {
	command := aptDebconfSelectionsCommand("/usr/bin/debconf-set-selections")

	for _, expected := range []string{
		"phpmyadmin phpmyadmin/dbconfig-install boolean false",
		"phpmyadmin phpmyadmin/reconfigure-webserver multiselect none",
		"| /usr/bin/debconf-set-selections",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("aptDebconfSelectionsCommand() = %q, missing %q", command, expected)
		}
	}
}

func TestAptInstallCommandsWithoutDebconfBinaryInstallsDebconfUtils(t *testing.T) {
	aptPath := "/usr/bin/apt-get"
	got := aptInstallCommands(aptPath, "")

	want := [][]string{
		{aptPath, "update"},
		{aptPath, "install", "-y", "debconf-utils"},
		{"/bin/sh", "-c", aptDebconfSelectionsCommand("debconf-set-selections")},
		{aptPath, "install", "-y", "phpmyadmin"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptInstallCommands() = %#v, want %#v", got, want)
	}
}

func TestAptInstallCommandsWithDebconfBinarySkipsDebconfUtilsInstall(t *testing.T) {
	aptPath := "/usr/bin/apt-get"
	debconfPath := "/usr/bin/debconf-set-selections"
	got := aptInstallCommands(aptPath, debconfPath)

	want := [][]string{
		{aptPath, "update"},
		{"/bin/sh", "-c", aptDebconfSelectionsCommand(debconfPath)},
		{aptPath, "install", "-y", "phpmyadmin"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptInstallCommands() = %#v, want %#v", got, want)
	}
}
