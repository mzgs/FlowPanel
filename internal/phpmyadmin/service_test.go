package phpmyadmin

import (
	"context"
	"os"
	"path/filepath"
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
