package phpenv

import (
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	composerInstallerURL       = "https://getcomposer.org/installer"
	composerInstallerSignature = "https://composer.github.io/installer.sig"
	composerDownloadLimit      = 4 << 20
	composerInstallDirectory   = "/usr/local/bin"
)

func (s *Service) installLatestComposer(ctx context.Context, version string) error {
	phpPath := strings.TrimSpace(s.StatusForVersion(ctx, version).PHPPath)
	if phpPath == "" {
		return fmt.Errorf("php %s was installed, but its CLI executable is unavailable", version)
	}

	installerPath, cleanup, err := downloadVerifiedComposerInstaller(ctx)
	if err != nil {
		return fmt.Errorf("prepare Composer installer: %w", err)
	}
	defer cleanup()

	if err := os.MkdirAll(composerInstallDirectory, 0o755); err != nil {
		return fmt.Errorf("create Composer install directory: %w", err)
	}
	s.logger.Info("installing latest stable Composer", zap.String("php_version", version))
	composerEnv := []string{"COMPOSER_ALLOW_SUPERUSER=1"}
	if strings.TrimSpace(os.Getenv("HOME")) == "" {
		composerEnv = append(composerEnv, "COMPOSER_HOME="+filepath.Dir(installerPath))
	}
	if _, err := runCommandWithOptions(ctx, "", composerEnv, phpPath,
		"-d", "disable_functions=",
		installerPath,
		"--install-dir="+composerInstallDirectory,
		"--filename=composer",
		"--quiet",
	); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(composerInstallDirectory, "composer")); err != nil {
		return fmt.Errorf("inspect installed Composer executable: %w", err)
	}
	return nil
}

func downloadVerifiedComposerInstaller(ctx context.Context) (string, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 30 * time.Second}
	signature, err := downloadComposerFile(ctx, client, composerInstallerSignature)
	if err != nil {
		return "", nil, err
	}
	expectedHash, err := hex.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || len(expectedHash) != sha512.Size384 {
		return "", nil, fmt.Errorf("invalid Composer installer signature")
	}

	installer, err := downloadComposerFile(ctx, client, composerInstallerURL)
	if err != nil {
		return "", nil, err
	}
	actualHash := sha512.Sum384(installer)
	if subtle.ConstantTimeCompare(expectedHash, actualHash[:]) != 1 {
		return "", nil, fmt.Errorf("Composer installer checksum verification failed")
	}

	tempDir, err := os.MkdirTemp("", "flowpanel-composer-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	installerPath := filepath.Join(tempDir, "composer-setup.php")
	if err := os.WriteFile(installerPath, installer, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return installerPath, cleanup, nil
}

func downloadComposerFile(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s: %s", url, response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, composerDownloadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(content) > composerDownloadLimit {
		return nil, fmt.Errorf("download %s exceeded size limit", url)
	}
	return content, nil
}
