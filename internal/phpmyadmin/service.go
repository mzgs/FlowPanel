package phpmyadmin

import (
	"archive/tar"
	"archive/zip"
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
	runtimeDirPerm      = 0o1777
)

var (
	archiveVersionPattern = regexp.MustCompile(`^phpMyAdmin-(.+)-all-languages$`)
	blowfishSecretPattern = regexp.MustCompile(`\$cfg\['blowfish_secret'\]\s*=\s*'';?`)

	phpMyAdminInstallPath = filepath.Join(config.FLOWPANEL_PATH, installDirName)
	phpMyAdminDownloadURL = downloadURL

	ErrThemeImportRequiresInstall = errors.New("phpmyadmin must be installed before importing a theme")
	ErrInvalidThemeArchive        = errors.New("invalid phpmyadmin theme archive")
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Remove(context.Context) error
	ImportTheme(context.Context, io.Reader) (Status, error)
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
	RemoveAvailable  bool     `json:"remove_available"`
	RemoveLabel      string   `json:"remove_label,omitempty"`
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
		RemoveLabel:      "Remove phpMyAdmin",
	}

	info, err := os.Stat(installPath)
	switch {
	case err == nil && info.IsDir():
		status.Installed = true
		status.InstallPath = installPath
		status.Version = detectVersion(installPath)
		status.InstallAvailable = false
		status.RemoveAvailable = true
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

	installPath := installPath()
	basePath := filepath.Dir(installPath)
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
		zap.String("install_path", installPath),
	)

	if err := downloadArchive(ctx, phpMyAdminDownloadURL, archivePath); err != nil {
		return err
	}

	extractedPath, version, err := extractArchive(archivePath, extractDir)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(installPath); err != nil {
		return fmt.Errorf("remove existing phpmyadmin path: %w", err)
	}
	if err := os.Rename(extractedPath, installPath); err != nil {
		return fmt.Errorf("move phpmyadmin into place: %w", err)
	}

	if err := writeRuntimeConfig(installPath); err != nil {
		return err
	}
	if err := ensureRuntimeDirectories(installPath); err != nil {
		return err
	}
	if err := writeVersionMetadata(installPath, version); err != nil {
		return err
	}

	return nil
}

func (s *Service) Remove(context.Context) error {
	path := installPath()
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect phpmyadmin path: %w", err)
	case !info.IsDir():
		return fmt.Errorf("%s exists but is not a directory", path)
	}

	s.logger.Info("removing phpmyadmin",
		zap.String("install_path", path),
	)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove phpmyadmin path: %w", err)
	}

	return nil
}

func (s *Service) ImportTheme(ctx context.Context, archive io.Reader) (Status, error) {
	status := s.Status(ctx)
	if !status.Installed || strings.TrimSpace(status.InstallPath) == "" {
		return status, ErrThemeImportRequiresInstall
	}
	if archive == nil {
		return status, fmt.Errorf("%w: theme archive is required", ErrInvalidThemeArchive)
	}

	themesPath := filepath.Join(status.InstallPath, "themes")
	if err := os.MkdirAll(themesPath, 0o755); err != nil {
		return status, fmt.Errorf("create phpmyadmin themes directory: %w", err)
	}

	workDir, err := os.MkdirTemp(status.InstallPath, "theme-import-")
	if err != nil {
		return status, fmt.Errorf("create phpmyadmin theme import workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, "theme.zip")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return status, fmt.Errorf("create phpmyadmin theme archive: %w", err)
	}
	if _, err := io.Copy(archiveFile, archive); err != nil {
		archiveFile.Close()
		return status, fmt.Errorf("write phpmyadmin theme archive: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return status, fmt.Errorf("close phpmyadmin theme archive: %w", err)
	}

	extractDir := filepath.Join(workDir, "extract")
	entries, err := extractZipArchive(archivePath, extractDir)
	if err != nil {
		return status, err
	}
	if len(entries) == 0 {
		return status, fmt.Errorf("%w: theme archive did not contain any files", ErrInvalidThemeArchive)
	}

	for _, name := range entries {
		sourcePath := filepath.Join(extractDir, name)
		destinationPath := filepath.Join(themesPath, name)
		if err := ensureWithinBase(themesPath, destinationPath); err != nil {
			return status, err
		}
		if err := os.RemoveAll(destinationPath); err != nil {
			return status, fmt.Errorf("remove existing phpmyadmin theme path: %w", err)
		}
		if err := movePath(sourcePath, destinationPath); err != nil {
			return status, err
		}
	}

	return s.Status(ctx), nil
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

	updated = strings.TrimRight(updated, "\n") + "\n\n" +
		fmt.Sprintf("$cfg['TempDir'] = '%s';\n", phpConfigPath(filepath.Join(installPath, "tmp")))

	if err := os.WriteFile(filepath.Join(installPath, "config.inc.php"), []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write phpmyadmin config: %w", err)
	}

	return nil
}

func ensureRuntimeDirectories(installPath string) error {
	tmpDir := filepath.Join(installPath, "tmp")
	if err := os.MkdirAll(tmpDir, runtimeDirPerm); err != nil {
		return fmt.Errorf("create phpmyadmin tmp directory: %w", err)
	}
	if err := os.Chmod(tmpDir, runtimeDirPerm); err != nil {
		return fmt.Errorf("set phpmyadmin tmp directory permissions: %w", err)
	}

	return nil
}

func phpConfigPath(path string) string {
	normalized := filepath.ToSlash(path)
	normalized = strings.ReplaceAll(normalized, `'`, `\'`)
	return strings.TrimRight(normalized, "/") + "/"
}

func generatePassword() (string, error) {
	randomBytes := make([]byte, passwordBytesLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func extractZipArchive(archivePath, destination string) ([]string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return nil, fmt.Errorf("create phpmyadmin theme extraction directory: %w", err)
	}

	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open phpmyadmin theme archive: %w", err)
	}
	defer reader.Close()

	entries := make(map[string]struct{})
	for _, file := range reader.File {
		if file == nil || strings.TrimSpace(file.Name) == "" {
			continue
		}

		cleanName := filepath.Clean(filepath.FromSlash(file.Name))
		if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%w: path %q is invalid", ErrInvalidThemeArchive, file.Name)
		}

		topEntry := strings.Split(cleanName, string(filepath.Separator))[0]
		if topEntry == "__MACOSX" {
			continue
		}
		entries[topEntry] = struct{}{}

		targetPath := filepath.Join(destination, cleanName)
		if err := ensureWithinBase(destination, targetPath); err != nil {
			return nil, err
		}

		fileMode := file.Mode()
		if fileMode&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: entry %q is unsupported", ErrInvalidThemeArchive, file.Name)
		}

		if file.FileInfo().IsDir() {
			dirPerm := fileMode.Perm()
			if dirPerm == 0 {
				dirPerm = 0o755
			}
			if err := os.MkdirAll(targetPath, dirPerm); err != nil {
				return nil, fmt.Errorf("create phpmyadmin theme directory: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return nil, fmt.Errorf("create phpmyadmin theme parent directory: %w", err)
		}

		source, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open phpmyadmin theme archive entry: %w", err)
		}

		filePerm := fileMode.Perm()
		if filePerm == 0 {
			filePerm = 0o644
		}

		destinationFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
		if err != nil {
			source.Close()
			return nil, fmt.Errorf("create phpmyadmin theme file: %w", err)
		}

		if _, err := io.Copy(destinationFile, source); err != nil {
			destinationFile.Close()
			source.Close()
			return nil, fmt.Errorf("extract phpmyadmin theme file: %w", err)
		}
		if err := destinationFile.Close(); err != nil {
			source.Close()
			return nil, fmt.Errorf("close phpmyadmin theme file: %w", err)
		}
		if err := source.Close(); err != nil {
			return nil, fmt.Errorf("close phpmyadmin theme archive entry: %w", err)
		}
	}

	result := make([]string, 0, len(entries))
	for name := range entries {
		result = append(result, name)
	}

	return result, nil
}

func movePath(sourcePath, destinationPath string) error {
	if err := os.Rename(sourcePath, destinationPath); err == nil {
		return nil
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("inspect phpmyadmin theme path: %w", err)
	}

	if info.IsDir() {
		if err := copyDirectory(sourcePath, destinationPath); err != nil {
			return err
		}
	} else {
		if err := copyFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(sourcePath); err != nil {
		return fmt.Errorf("remove temporary phpmyadmin theme path: %w", err)
	}

	return nil
}

func copyDirectory(sourceDir, destinationDir string) error {
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return fmt.Errorf("create phpmyadmin theme destination directory: %w", err)
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("read phpmyadmin theme directory: %w", err)
	}

	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDir, entry.Name())
		destinationPath := filepath.Join(destinationDir, entry.Name())

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect phpmyadmin theme entry: %w", err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("phpmyadmin theme contains unsupported symlink %q", sourcePath)
		}

		if entry.IsDir() {
			if err := copyDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}

		if err := copyFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(sourcePath, destinationPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open phpmyadmin theme file: %w", err)
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create phpmyadmin theme file parent: %w", err)
	}

	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create phpmyadmin theme destination file: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy phpmyadmin theme file: %w", err)
	}

	return nil
}
