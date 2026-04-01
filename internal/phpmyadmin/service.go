package phpmyadmin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"flowpanel/internal/config"

	"go.uber.org/zap"
)

const (
	downloadURL         = "https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz"
	installDirName      = "phpmyadmin"
	versionMetadataFile = ".flowpanel-version"
	passwordBytesLength = 24
)

var (
	archiveVersionPattern = regexp.MustCompile(`^phpMyAdmin-(.+)-all-languages$`)
	blowfishSecretPattern = regexp.MustCompile(`\$cfg\['blowfish_secret'\]\s*=\s*'';?`)

	phpMyAdminInstallPath = filepath.Join(config.FLOWPANEL_PATH, installDirName)
	phpMyAdminDownloadURL = downloadURL
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	InstallPath      string   `json:"install_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
	}
}

func (s *Service) Status(context.Context) Status {
	installPath := installPath()
	status := Status{
		Platform:         runtime.GOOS,
		PackageManager:   "manual",
		InstallLabel:     "Install phpMyAdmin",
		InstallAvailable: true,
	}

	info, err := os.Stat(installPath)
	switch {
	case err == nil && info.IsDir():
		status.Installed = true
		status.InstallPath = installPath
		status.Version = detectVersion(installPath)
		status.InstallAvailable = false
		status.State = "installed"
		switch {
		case status.Version != "" && status.InstallPath != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed at %s.", status.Version, status.InstallPath)
		case status.Version != "":
			status.Message = fmt.Sprintf("phpMyAdmin %s is installed.", status.Version)
		default:
			status.Message = fmt.Sprintf("phpMyAdmin is installed at %s.", status.InstallPath)
		}
	case err == nil:
		status.State = "missing"
		status.Message = fmt.Sprintf("%s exists but is not a directory.", installPath)
	case errors.Is(err, os.ErrNotExist):
		status.State = "missing"
		status.Message = "phpMyAdmin is not installed. Install it here to add a browser-based MariaDB client."
	default:
		status.State = "missing"
		status.Message = "FlowPanel could not inspect the phpMyAdmin installation path."
		status.Issues = append(status.Issues, err.Error())
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	if status := s.Status(ctx); status.Installed {
		return nil
	}

	basePath := filepath.Dir(installPath())
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return fmt.Errorf("create flowpanel path: %w", err)
	}

	workDir, err := os.MkdirTemp(basePath, "phpmyadmin-install-")
	if err != nil {
		return fmt.Errorf("create phpmyadmin workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, "phpmyadmin.tar.gz")
	extractDir := filepath.Join(workDir, "extract")

	s.logger.Info("installing phpmyadmin",
		zap.String("download_url", phpMyAdminDownloadURL),
		zap.String("install_path", installPath()),
	)

	if err := downloadArchive(ctx, phpMyAdminDownloadURL, archivePath); err != nil {
		return err
	}

	extractedPath, version, err := extractArchive(archivePath, extractDir)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(installPath()); err != nil {
		return fmt.Errorf("remove existing phpmyadmin path: %w", err)
	}
	if err := os.Rename(extractedPath, installPath()); err != nil {
		return fmt.Errorf("move phpmyadmin into place: %w", err)
	}

	if err := writeRuntimeConfig(installPath()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(installPath(), "tmp"), 0o770); err != nil {
		return fmt.Errorf("create phpmyadmin tmp directory: %w", err)
	}
	if err := writeVersionMetadata(installPath(), version); err != nil {
		return err
	}

	return nil
}

func installPath() string {
	return phpMyAdminInstallPath
}

func detectVersion(installPath string) string {
	installPath = strings.TrimSpace(installPath)
	if installPath == "" {
		return ""
	}

	if version, err := readVersionMetadata(installPath); err == nil && version != "" {
		return version
	}

	for _, name := range []string{"composer.json", "package.json"} {
		version, err := readVersionFromJSON(filepath.Join(installPath, name))
		if err == nil && version != "" {
			return version
		}
	}

	return ""
}

func readVersionMetadata(installPath string) (string, error) {
	content, err := os.ReadFile(filepath.Join(installPath, versionMetadataFile))
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}

func writeVersionMetadata(installPath, version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}

	if err := os.WriteFile(filepath.Join(installPath, versionMetadataFile), []byte(version+"\n"), 0o644); err != nil {
		return fmt.Errorf("write phpmyadmin version metadata: %w", err)
	}

	return nil
}

func readVersionFromJSON(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(content, &payload); err != nil {
		return "", err
	}

	return strings.TrimSpace(payload.Version), nil
}

func downloadArchive(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build phpmyadmin download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download phpmyadmin archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download phpmyadmin archive: unexpected status %s", resp.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create phpmyadmin archive file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write phpmyadmin archive: %w", err)
	}

	return nil
}

func extractArchive(archivePath, destination string) (string, string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", "", fmt.Errorf("create phpmyadmin extraction directory: %w", err)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("open phpmyadmin archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", "", fmt.Errorf("open phpmyadmin archive gzip stream: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	var rootDir string
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", "", fmt.Errorf("read phpmyadmin archive: %w", err)
		}

		if header == nil || header.Name == "" {
			continue
		}

		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		cleanName := filepath.Clean(header.Name)
		if filepath.IsAbs(cleanName) || cleanName == "." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return "", "", fmt.Errorf("phpmyadmin archive contains invalid path %q", header.Name)
		}

		parts := strings.Split(cleanName, string(filepath.Separator))
		if len(parts) > 0 && rootDir == "" {
			rootDir = parts[0]
		}

		targetPath := filepath.Join(destination, cleanName)
		if err := ensureWithinBase(destination, targetPath); err != nil {
			return "", "", err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return "", "", fmt.Errorf("create phpmyadmin directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return "", "", fmt.Errorf("create phpmyadmin file parent: %w", err)
			}

			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return "", "", fmt.Errorf("create phpmyadmin file: %w", err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return "", "", fmt.Errorf("extract phpmyadmin file: %w", err)
			}
			if err := file.Close(); err != nil {
				return "", "", fmt.Errorf("close phpmyadmin file: %w", err)
			}
		default:
			continue
		}
	}

	if strings.TrimSpace(rootDir) == "" {
		return "", "", errors.New("phpmyadmin archive did not contain a root directory")
	}

	rootPath := filepath.Join(destination, rootDir)
	version := versionFromArchiveRoot(rootDir)
	return rootPath, version, nil
}

func ensureWithinBase(basePath, targetPath string) error {
	basePath, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("resolve archive base path: %w", err)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve archive target path: %w", err)
	}

	prefix := basePath + string(filepath.Separator)
	if targetPath != basePath && !strings.HasPrefix(targetPath, prefix) {
		return fmt.Errorf("phpmyadmin archive path escapes extraction directory: %s", targetPath)
	}

	return nil
}

func versionFromArchiveRoot(rootDir string) string {
	match := archiveVersionPattern.FindStringSubmatch(strings.TrimSpace(rootDir))
	if len(match) != 2 {
		return ""
	}

	return strings.TrimSpace(match[1])
}

func writeRuntimeConfig(installPath string) error {
	samplePath := filepath.Join(installPath, "config.sample.inc.php")
	content, err := os.ReadFile(samplePath)
	if err != nil {
		return fmt.Errorf("read phpmyadmin sample config: %w", err)
	}

	secret, err := generatePassword()
	if err != nil {
		return fmt.Errorf("generate phpmyadmin blowfish secret: %w", err)
	}

	updated := blowfishSecretPattern.ReplaceAllLiteralString(string(content), fmt.Sprintf("$cfg['blowfish_secret'] = '%s';", secret))
	if updated == string(content) {
		return errors.New("phpmyadmin sample config did not contain an empty blowfish secret")
	}

	if err := os.WriteFile(filepath.Join(installPath, "config.inc.php"), []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write phpmyadmin config: %w", err)
	}

	return nil
}

func generatePassword() (string, error) {
	randomBytes := make([]byte, passwordBytesLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}
