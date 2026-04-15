package golang

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

const statusCommandTimeout = 3 * time.Second

var goVersionPattern = regexp.MustCompile(`\bgo([0-9][^\s]*)\b`)

const (
	goDownloadListURL = "https://go.dev/dl/?mode=json"
	linuxInstallRoot  = "/usr/local/go"
	linuxProfilePath  = "/etc/profile.d/flowpanel-go.sh"
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
	Remove(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Installed        bool     `json:"installed"`
	BinaryPath       string   `json:"binary_path,omitempty"`
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

type actionPlan struct {
	packageManager string
	installLabel   string
	removeLabel    string
	installCmds    [][]string
	removeCmds     [][]string
	useOfficialTar bool
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{logger: logger}
}

func (s *Service) Status(ctx context.Context) Status {
	plan := detectActionPlan()
	status := Status{
		Platform:       runtime.GOOS,
		PackageManager: plan.packageManager,
		InstallLabel:   "Install Go",
		RemoveLabel:    "Remove Go",
	}

	goPath, installed, managedInstall := detectGoBinary()
	if installed {
		status.Installed = true
		status.BinaryPath = goPath
		if output, err := runInspectCommand(ctx, goPath, "version"); err == nil {
			status.Version = parseVersion(output)
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	status.InstallLabel = fallback(status.InstallLabel, plan.installLabel)
	status.RemoveLabel = fallback(status.RemoveLabel, plan.removeLabel)
	switch {
	case plan.useOfficialTar:
		status.InstallAvailable = !managedInstall
		status.RemoveAvailable = managedInstall
	case len(plan.installCmds) > 0:
		status.InstallAvailable = !status.Installed
		status.RemoveAvailable = len(plan.removeCmds) > 0 && status.Installed
	default:
		status.RemoveAvailable = len(plan.removeCmds) > 0 && status.Installed
	}

	switch {
	case managedInstall && status.Version != "" && status.BinaryPath != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("Go %s is installed at %s.", status.Version, status.BinaryPath)
	case managedInstall && status.Version != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("Go %s is installed.", status.Version)
	case managedInstall:
		status.State = "installed"
		status.Message = fmt.Sprintf("Go is installed at %s.", status.BinaryPath)
	case status.Installed && status.Version != "" && status.BinaryPath != "" && plan.useOfficialTar:
		status.State = "installed"
		status.Message = fmt.Sprintf(
			"Go %s was detected at %s. Install the latest official Go release to %s to upgrade.",
			status.Version,
			status.BinaryPath,
			linuxInstallRoot,
		)
	case status.Installed && status.BinaryPath != "" && plan.useOfficialTar:
		status.State = "installed"
		status.Message = fmt.Sprintf("Go was detected at %s. Install the latest official Go release to %s to upgrade.", status.BinaryPath, linuxInstallRoot)
	default:
		status.State = "missing"
		status.Message = "Go was not detected on this server."
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if status := s.Status(ctx); status.Installed {
		if plan.useOfficialTar {
			return ensureManagedLinuxPath()
		}
		return nil
	}
	if plan.useOfficialTar {
		if err := s.installLatestLinuxRelease(ctx); err != nil {
			return err
		}
		return ensureManagedLinuxPath()
	}
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic Go installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing go runtime",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.installCmds...)
}

func (s *Service) Remove(ctx context.Context) error {
	if status := s.Status(ctx); !status.Installed {
		return nil
	}

	plan := detectActionPlan()
	if plan.useOfficialTar {
		return removeManagedLinuxInstall()
	}
	if len(plan.removeCmds) == 0 {
		return fmt.Errorf("automatic Go removal is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("removing go runtime",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.removeCmds...)
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install Go",
				removeLabel:    "Remove Go",
				installCmds: [][]string{
					{brewPath, "install", "go"},
				},
				removeCmds: [][]string{
					{brewPath, "uninstall", "go"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			return actionPlan{
				packageManager: "official tarball",
				installLabel:   "Install Go",
				removeLabel:    "Remove Go",
				useOfficialTar: true,
			}
		}
	}

	return actionPlan{}
}

func parseVersion(output string) string {
	match := goVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) < 2 {
		return ""
	}

	return strings.TrimSpace(match[1])
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
		"/usr/local/go/bin",
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/usr/sbin",
	} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}

	return "", false
}

func detectGoBinary() (string, bool, bool) {
	managedPath := filepath.Join(linuxInstallRoot, "bin", "go")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true, true
	}

	path, ok := lookupCommand("go")
	return path, ok, false
}

func fallback(value, next string) string {
	if strings.TrimSpace(next) == "" {
		return value
	}

	return next
}

type releaseListEntry struct {
	Version string            `json:"version"`
	Stable  bool              `json:"stable"`
	Files   []releaseListFile `json:"files"`
}

type releaseListFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kind     string `json:"kind"`
}

func (s *Service) installLatestLinuxRelease(ctx context.Context) error {
	archiveURL, _, err := latestLinuxArchiveURL(ctx)
	if err != nil {
		return err
	}

	basePath := filepath.Dir(linuxInstallRoot)
	workDir, err := os.MkdirTemp(basePath, ".flowpanel-go-install-")
	if err != nil {
		return fmt.Errorf("create go install workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, "go.tar.gz")
	extractDir := filepath.Join(workDir, "extract")

	s.logger.Info("installing go runtime",
		zap.String("download_url", archiveURL),
		zap.String("install_path", linuxInstallRoot),
	)

	if err := downloadArchive(ctx, archiveURL, archivePath); err != nil {
		return err
	}

	extractedPath, err := extractArchive(archivePath, extractDir)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(linuxInstallRoot); err != nil {
		return fmt.Errorf("remove existing go path: %w", err)
	}
	if err := os.Rename(extractedPath, linuxInstallRoot); err != nil {
		return fmt.Errorf("move go into place: %w", err)
	}

	return nil
}

func latestLinuxArchiveURL(ctx context.Context) (string, string, error) {
	arch, ok := linuxReleaseArch(runtime.GOARCH)
	if !ok {
		return "", "", fmt.Errorf("automatic Go installation is not supported for linux/%s", runtime.GOARCH)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, goDownloadListURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("build go download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download go release metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download go release metadata: unexpected status %s", resp.Status)
	}

	var releases []releaseListEntry
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", fmt.Errorf("decode go release metadata: %w", err)
	}

	for _, release := range releases {
		if !release.Stable {
			continue
		}
		for _, file := range release.Files {
			if file.OS == "linux" && file.Arch == arch && file.Kind == "archive" && strings.HasSuffix(file.Filename, ".tar.gz") {
				return "https://go.dev/dl/" + file.Filename, strings.TrimPrefix(strings.TrimSpace(release.Version), "go"), nil
			}
		}
	}

	return "", "", fmt.Errorf("no official Go release archive found for linux/%s", arch)
}

func linuxReleaseArch(goarch string) (string, bool) {
	switch strings.TrimSpace(goarch) {
	case "386", "amd64", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x":
		return goarch, true
	case "arm":
		return "armv6l", true
	default:
		return "", false
	}
}

func removeManagedLinuxInstall() error {
	info, err := os.Stat(linuxInstallRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect go path: %w", err)
	case !info.IsDir():
		return fmt.Errorf("%s exists but is not a directory", linuxInstallRoot)
	}

	if err := os.RemoveAll(linuxInstallRoot); err != nil {
		return fmt.Errorf("remove go path: %w", err)
	}
	if err := removeManagedLinuxPath(); err != nil {
		return err
	}

	return nil
}

func ensureManagedLinuxPath() error {
	pathEntry := filepath.Join(linuxInstallRoot, "bin")
	content := fmt.Sprintf(
		"# Added by FlowPanel for Go\ncase \":$PATH:\" in\n  *:%[1]s:*) ;;\n  *) export PATH=\"$PATH:%[1]s\" ;;\nesac\n",
		pathEntry,
	)

	if err := os.WriteFile(linuxProfilePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write go profile: %w", err)
	}

	return nil
}

func removeManagedLinuxPath() error {
	if err := os.Remove(linuxProfilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove go profile: %w", err)
	}

	return nil
}

func downloadArchive(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build go download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download go archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download go archive: unexpected status %s", resp.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create go archive file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write go archive: %w", err)
	}

	return nil
}

func extractArchive(archivePath, destination string) (string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", fmt.Errorf("create go extraction directory: %w", err)
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open go archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("open go archive gzip stream: %w", err)
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
			return "", fmt.Errorf("read go archive: %w", err)
		}

		if header == nil || header.Name == "" || header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		cleanName := filepath.Clean(header.Name)
		if filepath.IsAbs(cleanName) || cleanName == "." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("go archive contains invalid path %q", header.Name)
		}

		parts := strings.Split(cleanName, string(filepath.Separator))
		if len(parts) > 0 && rootDir == "" {
			rootDir = parts[0]
		}

		targetPath := filepath.Join(destination, cleanName)
		if err := ensureWithinBase(destination, targetPath); err != nil {
			return "", err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return "", fmt.Errorf("create go directory: %w", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return "", fmt.Errorf("create go file parent: %w", err)
			}

			targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return "", fmt.Errorf("create go file: %w", err)
			}
			if _, err := io.Copy(targetFile, tarReader); err != nil {
				targetFile.Close()
				return "", fmt.Errorf("extract go file: %w", err)
			}
			if err := targetFile.Close(); err != nil {
				return "", fmt.Errorf("close go file: %w", err)
			}
		default:
			continue
		}
	}

	if strings.TrimSpace(rootDir) == "" {
		return "", errors.New("go archive did not contain a root directory")
	}

	return filepath.Join(destination, rootDir), nil
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
		return fmt.Errorf("go archive path escapes extraction directory: %s", targetPath)
	}

	return nil
}

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, statusCommandTimeout)
		defer cancel()
	}

	return runCommand(runCtx, name, args...)
}

func runCommands(ctx context.Context, commands ...[]string) error {
	for _, command := range commands {
		if len(command) == 0 {
			continue
		}
		if _, err := runCommand(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return combinedOutput, fmt.Errorf("%s timed out", name)
	case errors.Is(runCtx.Err(), context.Canceled):
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	case combinedOutput == "":
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	default:
		return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
	}
}
