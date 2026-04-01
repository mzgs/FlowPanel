package phpmyadmin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestStatusReportsInstalledWhenPathExists(t *testing.T) {
	installPath := filepath.Join(t.TempDir(), "phpmyadmin")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}

	restore := overrideInstallPath(t, installPath)
	defer restore()

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

func TestDetectVersionReadsMetadataFile(t *testing.T) {
	installPath := filepath.Join(t.TempDir(), "phpmyadmin")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installPath, versionMetadataFile), []byte("5.2.3\n"), 0o644); err != nil {
		t.Fatalf("write version metadata: %v", err)
	}

	got := detectVersion(installPath)
	if got != "5.2.3" {
		t.Fatalf("detectVersion() = %q, want 5.2.3", got)
	}
}

func TestVersionFromArchiveRoot(t *testing.T) {
	got := versionFromArchiveRoot("phpMyAdmin-5.2.3-all-languages")
	if got != "5.2.3" {
		t.Fatalf("versionFromArchiveRoot() = %q, want 5.2.3", got)
	}
}

func TestInstallDownloadsLatestArchiveAndWritesVersionMetadata(t *testing.T) {
	archiveBytes := buildPHPMyAdminArchive(t, "5.2.3")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(archiveBytes); err != nil {
			t.Fatalf("write archive response: %v", err)
		}
	}))
	defer server.Close()

	baseDir := t.TempDir()
	installPath := filepath.Join(baseDir, "phpmyadmin")

	restorePath := overrideInstallPath(t, installPath)
	defer restorePath()

	restoreURL := overrideDownloadURL(t, server.URL+"/phpMyAdmin-latest-all-languages.tar.gz")
	defer restoreURL()

	service := NewService(zap.NewNop())
	if err := service.Install(context.Background()); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	versionData, err := os.ReadFile(filepath.Join(installPath, versionMetadataFile))
	if err != nil {
		t.Fatalf("read version metadata: %v", err)
	}
	if strings.TrimSpace(string(versionData)) != "5.2.3" {
		t.Fatalf("version metadata = %q, want 5.2.3", string(versionData))
	}

	configData, err := os.ReadFile(filepath.Join(installPath, "config.inc.php"))
	if err != nil {
		t.Fatalf("read config.inc.php: %v", err)
	}
	if strings.Contains(string(configData), "$cfg['blowfish_secret'] = '';") {
		t.Fatalf("config.inc.php = %q, want populated blowfish secret", string(configData))
	}

	if info, err := os.Stat(filepath.Join(installPath, "tmp")); err != nil || !info.IsDir() {
		t.Fatalf("tmp dir err = %v, info = %#v", err, info)
	}

	status := service.Status(context.Background())
	if !status.Installed {
		t.Fatal("Installed = false, want true")
	}
	if status.Version != "5.2.3" {
		t.Fatalf("Version = %q, want 5.2.3", status.Version)
	}
}

func overrideInstallPath(t *testing.T, path string) func() {
	t.Helper()

	previous := phpMyAdminInstallPath
	phpMyAdminInstallPath = path
	return func() {
		phpMyAdminInstallPath = previous
	}
}

func overrideDownloadURL(t *testing.T, url string) func() {
	t.Helper()

	previous := phpMyAdminDownloadURL
	phpMyAdminDownloadURL = url
	return func() {
		phpMyAdminDownloadURL = previous
	}
}

func buildPHPMyAdminArchive(t *testing.T, version string) []byte {
	t.Helper()

	root := "phpMyAdmin-" + version + "-all-languages"

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)

	files := map[string]string{
		filepath.ToSlash(filepath.Join(root, "config.sample.inc.php")): "<?php\n$cfg['blowfish_secret'] = '';\n",
		filepath.ToSlash(filepath.Join(root, "composer.json")):         "{\"version\":\"" + version + "\"}\n",
	}

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content %s: %v", name, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return archive.Bytes()
}
