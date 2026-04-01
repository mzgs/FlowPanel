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
	command := aptDebconfSelectionsCommand("/usr/bin/debconf-set-selections", "root-secret", "app-secret")

	for _, expected := range []string{
		"phpmyadmin phpmyadmin/dbconfig-install boolean true",
		"phpmyadmin phpmyadmin/reconfigure-webserver multiselect none",
		"phpmyadmin phpmyadmin/mysql/admin-user string root",
		"phpmyadmin phpmyadmin/mysql/admin-pass password root-secret",
		"phpmyadmin phpmyadmin/mysql/app-pass password app-secret",
		"phpmyadmin phpmyadmin/app-password-confirm password app-secret",
		"| /usr/bin/debconf-set-selections",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("aptDebconfSelectionsCommand() = %q, missing %q", command, expected)
		}
	}
}

func TestAptInstallCommandsWithoutDebconfBinaryInstallsDebconfUtils(t *testing.T) {
	aptPath := "/usr/bin/apt-get"
	got := aptInstallCommands(aptPath, "", "root-secret", "app-secret")

	want := [][]string{
		{aptPath, "update"},
		{aptPath, "install", "-y", "debconf-utils"},
		{"/bin/sh", "-c", aptDebconfSelectionsCommand("debconf-set-selections", "root-secret", "app-secret")},
		{aptPath, "install", "-y", "phpmyadmin"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptInstallCommands() = %#v, want %#v", got, want)
	}
}

func TestAptInstallCommandsWithDebconfBinarySkipsDebconfUtilsInstall(t *testing.T) {
	aptPath := "/usr/bin/apt-get"
	debconfPath := "/usr/bin/debconf-set-selections"
	got := aptInstallCommands(aptPath, debconfPath, "root-secret", "app-secret")

	want := [][]string{
		{aptPath, "update"},
		{"/bin/sh", "-c", aptDebconfSelectionsCommand(debconfPath, "root-secret", "app-secret")},
		{aptPath, "install", "-y", "phpmyadmin"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aptInstallCommands() = %#v, want %#v", got, want)
	}
}

func TestResolveMariaDBRootPasswordUsesEnv(t *testing.T) {
	t.Setenv("FLOWPANEL_MARIADB_PASSWORD", "env-root-secret")

	got, err := resolveMariaDBRootPassword()
	if err != nil {
		t.Fatalf("resolveMariaDBRootPassword() error = %v", err)
	}
	if got != "env-root-secret" {
		t.Fatalf("resolveMariaDBRootPassword() = %q, want env-root-secret", got)
	}
}

func TestResolveMariaDBRootPasswordUsesPasswordFile(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "mariadb-root-password")
	if err := os.WriteFile(passwordFile, []byte("file-root-secret\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	t.Setenv("FLOWPANEL_MARIADB_PASSWORD", "")
	t.Setenv("FLOWPANEL_MARIADB_PASSWORD_FILE", passwordFile)

	got, err := resolveMariaDBRootPassword()
	if err != nil {
		t.Fatalf("resolveMariaDBRootPassword() error = %v", err)
	}
	if got != "file-root-secret" {
		t.Fatalf("resolveMariaDBRootPassword() = %q, want file-root-secret", got)
	}
}

func TestInstallWithAptPlanRunsPreseededInstall(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}

	aptLogPath := filepath.Join(tempDir, "apt.log")
	debconfLogPath := filepath.Join(tempDir, "debconf.log")

	t.Setenv("APT_LOG", aptLogPath)
	t.Setenv("DEBCONF_LOG", debconfLogPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	aptPath := filepath.Join(binDir, "apt-get")
	debconfPath := filepath.Join(binDir, "debconf-set-selections")

	writeExecutable(t, aptPath, `#!/bin/sh
printf '%s\n' "$*" >> "$APT_LOG"
`)
	writeExecutable(t, debconfPath, `#!/bin/sh
cat >> "$DEBCONF_LOG"
`)

	service := NewService(zap.NewNop())
	plan := actionPlan{
		packageManager: "apt",
		installEnv: map[string]string{
			"DEBIAN_FRONTEND": "noninteractive",
		},
		installCmds: aptInstallCommands(aptPath, debconfPath, "root-secret", "app-secret"),
	}

	if err := service.installWithPlan(context.Background(), plan); err != nil {
		t.Fatalf("installWithPlan() error = %v", err)
	}

	aptLog, err := os.ReadFile(aptLogPath)
	if err != nil {
		t.Fatalf("read apt log: %v", err)
	}
	if !strings.Contains(string(aptLog), "install -y phpmyadmin") {
		t.Fatalf("apt log = %q, want phpmyadmin install command", string(aptLog))
	}

	debconfLog, err := os.ReadFile(debconfLogPath)
	if err != nil {
		t.Fatalf("read debconf log: %v", err)
	}
	for _, expected := range []string{
		"phpmyadmin phpmyadmin/dbconfig-install boolean true",
		"phpmyadmin phpmyadmin/mysql/admin-pass password root-secret",
		"phpmyadmin phpmyadmin/mysql/app-pass password app-secret",
	} {
		if !strings.Contains(string(debconfLog), expected) {
			t.Fatalf("debconf log = %q, missing %q", string(debconfLog), expected)
		}
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
