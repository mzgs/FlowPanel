package phpmyadmin

import (
	"context"
	"os"
	"path/filepath"
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

func TestReadPHPMyAdminStorageTableConfig(t *testing.T) {
	sqlPath := filepath.Join(t.TempDir(), "create_tables.sql")
	sql := strings.Join([]string{
		"CREATE TABLE IF NOT EXISTS `pma__bookmark` (id int);",
		"CREATE TABLE IF NOT EXISTS `pma__history` (id int);",
		"CREATE TABLE IF NOT EXISTS `unrelated_table` (id int);",
	}, "\n")
	if err := os.WriteFile(sqlPath, []byte(sql), 0o644); err != nil {
		t.Fatalf("write create_tables.sql: %v", err)
	}

	got, err := readPHPMyAdminStorageTableConfig(sqlPath)
	if err != nil {
		t.Fatalf("readPHPMyAdminStorageTableConfig(): %v", err)
	}
	if got["bookmarktable"] != "pma__bookmark" {
		t.Fatalf("bookmarktable = %q, want pma__bookmark", got["bookmarktable"])
	}
	if got["history"] != "pma__history" {
		t.Fatalf("history = %q, want pma__history", got["history"])
	}
	if len(got) != 2 {
		t.Fatalf("len(config) = %d, want 2", len(got))
	}
}

func TestDetectLinuxSetup(t *testing.T) {
	aptSetup, ok := detectLinuxSetup("apt")
	if !ok {
		t.Fatal("detectLinuxSetup(apt) ok = false, want true")
	}
	if aptSetup.configPath != debianConfigUserPath {
		t.Fatalf("apt configPath = %q, want %q", aptSetup.configPath, debianConfigUserPath)
	}
	if aptSetup.schemaPath != debianCreateTablesSQLPath {
		t.Fatalf("apt schemaPath = %q, want %q", aptSetup.schemaPath, debianCreateTablesSQLPath)
	}

	rpmSetup, ok := detectLinuxSetup("dnf")
	if !ok {
		t.Fatal("detectLinuxSetup(dnf) ok = false, want true")
	}
	if rpmSetup.configPath != rpmConfigUserPath {
		t.Fatalf("dnf configPath = %q, want %q", rpmSetup.configPath, rpmConfigUserPath)
	}
	if rpmSetup.schemaPath != rpmCreateTablesSQLPath {
		t.Fatalf("dnf schemaPath = %q, want %q", rpmSetup.schemaPath, rpmCreateTablesSQLPath)
	}

	if _, ok := detectLinuxSetup("pacman"); ok {
		t.Fatal("detectLinuxSetup(pacman) ok = true, want false")
	}
}

func TestRenderManagedPHPMyAdminConfig(t *testing.T) {
	got := renderManagedPHPMyAdminConfig("secret'pw", map[string]string{
		"bookmarktable": "pma__bookmark",
		"history":       "pma__history",
	})

	for _, want := range []string{
		phpMyAdminConfigStartMarker,
		"$cfg['Servers'][$i]['pmadb'] = 'phpmyadmin';",
		"$cfg['Servers'][$i]['controluser'] = 'phpmyadmin';",
		"$cfg['Servers'][$i]['controlpass'] = 'secret\\'pw';",
		"$cfg['Servers'][$i]['bookmarktable'] = 'pma__bookmark';",
		"$cfg['Servers'][$i]['history'] = 'pma__history';",
		phpMyAdminConfigEndMarker,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderManagedPHPMyAdminConfig() missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<?php") {
		t.Fatalf("renderManagedPHPMyAdminConfig() unexpectedly included php open tag: %q", got)
	}
}

func TestUpsertManagedPHPMyAdminConfigAppendsAndReplacesBlock(t *testing.T) {
	managed := renderManagedPHPMyAdminConfig("secret", map[string]string{
		"history": "pma__history",
	})

	appended := upsertManagedPHPMyAdminConfig("<?php\n$cfg['blowfish_secret'] = 'abc';\n", managed)
	if !strings.Contains(appended, "$cfg['blowfish_secret'] = 'abc';") {
		t.Fatalf("appended config lost existing content: %q", appended)
	}
	if strings.Count(appended, phpMyAdminConfigStartMarker) != 1 {
		t.Fatalf("appended config markers = %d, want 1", strings.Count(appended, phpMyAdminConfigStartMarker))
	}

	replaced := upsertManagedPHPMyAdminConfig(appended, renderManagedPHPMyAdminConfig("next-secret", map[string]string{
		"bookmarktable": "pma__bookmark",
	}))
	if strings.Contains(replaced, "$cfg['Servers'][$i]['history'] = 'pma__history';") {
		t.Fatalf("replaced config kept old managed block: %q", replaced)
	}
	if !strings.Contains(replaced, "$cfg['Servers'][$i]['bookmarktable'] = 'pma__bookmark';") {
		t.Fatalf("replaced config missing new block: %q", replaced)
	}
	if strings.Count(replaced, phpMyAdminConfigStartMarker) != 1 {
		t.Fatalf("replaced config markers = %d, want 1", strings.Count(replaced, phpMyAdminConfigStartMarker))
	}
}

func TestWriteManagedPHPMyAdminConfigCreatesPHPFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.user.inc.php")
	managed := renderManagedPHPMyAdminConfig("secret", map[string]string{
		"history": "pma__history",
	})

	if err := writeManagedPHPMyAdminConfig(configPath, managed); err != nil {
		t.Fatalf("writeManagedPHPMyAdminConfig(): %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(content)
	if !strings.HasPrefix(text, "<?php\n") {
		t.Fatalf("config missing php open tag: %q", text)
	}
	if !strings.Contains(text, "$cfg['Servers'][$i]['history'] = 'pma__history';") {
		t.Fatalf("config missing managed content: %q", text)
	}
}
