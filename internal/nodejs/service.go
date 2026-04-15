package nodejs

import (
	"bytes"
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

var nodeVersionPattern = regexp.MustCompile(`\bv?(\d+(?:\.\d+)+)\b`)

const (
	nodeReleaseIndexURL = "https://nodejs.org/dist/index.json"
	linuxInstallRoot    = "/usr/local/nodejs"
	linuxProfilePath    = "/etc/profile.d/flowpanel-nodejs.sh"
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
	NPMPath          string   `json:"npm_path,omitempty"`
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

type releaseListEntry struct {
	Version string   `json:"version"`
	Files   []string `json:"files"`
	LTS     any      `json:"lts"`
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
		InstallLabel:   "Install Node.js",
		RemoveLabel:    "Remove Node.js",
	}

	nodePath, installed, managedInstall := detectNodeBinary()
	if installed {
		status.Installed = true
		status.BinaryPath = nodePath
		if output, err := runInspectCommand(ctx, nodePath, "--version"); err == nil {
			status.Version = parseVersion(output)
		} else {
			status.Issues = append(status.Issues, err.Error())
		}
	}

	if npmPath, ok := detectNPMBinary(); ok {
		status.NPMPath = npmPath
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
		status.Message = fmt.Sprintf("Node.js %s is installed at %s.", status.Version, status.BinaryPath)
	case managedInstall && status.Version != "":
		status.State = "installed"
		status.Message = fmt.Sprintf("Node.js %s is installed.", status.Version)
	case managedInstall:
		status.State = "installed"
		status.Message = fmt.Sprintf("Node.js is installed at %s.", status.BinaryPath)
	case status.Installed && status.Version != "" && status.BinaryPath != "" && plan.useOfficialTar:
		status.State = "installed"
		status.Message = fmt.Sprintf(
			"Node.js %s was detected at %s. Install the latest Node.js LTS release to %s to manage it with FlowPanel.",
			status.Version,
			status.BinaryPath,
			linuxInstallRoot,
		)
	case status.Installed && status.BinaryPath != "" && plan.useOfficialTar:
		status.State = "installed"
		status.Message = fmt.Sprintf(
			"Node.js was detected at %s. Install the latest Node.js LTS release to %s to manage it with FlowPanel.",
			status.BinaryPath,
			linuxInstallRoot,
		)
	default:
		status.State = "missing"
		status.Message = "Node.js was not detected on this server."
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	_, installed, managedInstall := detectNodeBinary()
	if installed && !plan.useOfficialTar {
		return nil
	}
	if plan.useOfficialTar {
		if !installed || !managedInstall {
			if err := s.installLatestLinuxRelease(ctx); err != nil {
				return err
			}
		}
		return ensureManagedLinuxPath()
	}
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic Node.js installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing nodejs runtime",
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
		return fmt.Errorf("automatic Node.js removal is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("removing nodejs runtime",
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
				installLabel:   "Install Node.js",
				removeLabel:    "Remove Node.js",
				installCmds: [][]string{
					{brewPath, "install", "node"},
				},
				removeCmds: [][]string{
					{brewPath, "uninstall", "node"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			return actionPlan{
				packageManager: "official tarball",
				installLabel:   "Install Node.js",
				removeLabel:    "Remove Node.js",
				useOfficialTar: true,
			}
		}
	}

	return actionPlan{}
}

func parseVersion(output string) string {
	match := nodeVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
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
		filepath.Join(linuxInstallRoot, "bin"),
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

func detectNodeBinary() (string, bool, bool) {
	managedPath := filepath.Join(linuxInstallRoot, "bin", "node")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true, true
	}

	if path, ok := lookupCommand("node"); ok {
		return path, true, false
	}
	path, ok := lookupCommand("nodejs")
	return path, ok, false
}

func detectNPMBinary() (string, bool) {
	managedPath := filepath.Join(linuxInstallRoot, "bin", "npm")
	if info, err := os.Stat(managedPath); err == nil && !info.IsDir() {
		return managedPath, true
	}

	return lookupCommand("npm")
}

func fallback(value, next string) string {
	if strings.TrimSpace(next) == "" {
		return value
	}

	return next
}

func (s *Service) installLatestLinuxRelease(ctx context.Context) error {
	archiveURL, archiveRoot, version, err := latestLinuxArchiveURL(ctx)
	if err != nil {
		return err
	}

	basePath := filepath.Dir(linuxInstallRoot)
	workDir, err := os.MkdirTemp(basePath, ".flowpanel-nodejs-install-")
	if err != nil {
		return fmt.Errorf("create nodejs install workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, "nodejs.tar.xz")
	extractDir := filepath.Join(workDir, "extract")

	s.logger.Info("installing nodejs runtime",
		zap.String("download_url", archiveURL),
		zap.String("install_path", linuxInstallRoot),
		zap.String("version", version),
	)

	if err := downloadArchive(ctx, archiveURL, archivePath); err != nil {
		return err
	}

	extractedPath, err := extractArchive(ctx, archivePath, extractDir, archiveRoot)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(linuxInstallRoot); err != nil {
		return fmt.Errorf("remove existing nodejs path: %w", err)
	}
	if err := os.Rename(extractedPath, linuxInstallRoot); err != nil {
		return fmt.Errorf("move nodejs into place: %w", err)
	}

	return nil
}

func latestLinuxArchiveURL(ctx context.Context) (string, string, string, error) {
	arch, ok := linuxReleaseArch(runtime.GOARCH)
	if !ok {
		return "", "", "", fmt.Errorf("automatic Node.js installation is not supported for linux/%s", runtime.GOARCH)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeReleaseIndexURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("build nodejs download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("download nodejs release metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("download nodejs release metadata: unexpected status %s", resp.Status)
	}

	var releases []releaseListEntry
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", "", fmt.Errorf("decode nodejs release metadata: %w", err)
	}

	targetFile := "linux-" + arch
	for _, release := range releases {
		if !isLTSRelease(release.LTS) {
			continue
		}
		if !containsString(release.Files, targetFile) {
			continue
		}

		version := strings.TrimSpace(release.Version)
		if version == "" {
			continue
		}

		filename := fmt.Sprintf("node-%s-linux-%s.tar.xz", version, arch)
		return "https://nodejs.org/dist/" + version + "/" + filename, strings.TrimSuffix(filename, ".tar.xz"), strings.TrimPrefix(version, "v"), nil
	}

	return "", "", "", fmt.Errorf("no Node.js LTS release archive found for linux/%s", arch)
}

func linuxReleaseArch(goarch string) (string, bool) {
	switch strings.TrimSpace(goarch) {
	case "amd64":
		return "x64", true
	case "arm64":
		return "arm64", true
	case "arm":
		return "armv7l", true
	case "ppc64le":
		return "ppc64le", true
	case "s390x":
		return "s390x", true
	default:
		return "", false
	}
}

func isLTSRelease(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}

	return false
}

func removeManagedLinuxInstall() error {
	info, err := os.Stat(linuxInstallRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return removeManagedLinuxPath()
	case err != nil:
		return fmt.Errorf("inspect nodejs path: %w", err)
	case !info.IsDir():
		return fmt.Errorf("%s exists but is not a directory", linuxInstallRoot)
	}

	if err := os.RemoveAll(linuxInstallRoot); err != nil {
		return fmt.Errorf("remove nodejs path: %w", err)
	}
	if err := removeManagedLinuxPath(); err != nil {
		return err
	}

	return nil
}

func ensureManagedLinuxPath() error {
	pathEntry := filepath.Join(linuxInstallRoot, "bin")
	content := fmt.Sprintf(
		"# Added by FlowPanel for Node.js\ncase \":$PATH:\" in\n  *:%[1]s:*) ;;\n  *) export PATH=\"$PATH:%[1]s\" ;;\nesac\n",
		pathEntry,
	)

	if err := os.WriteFile(linuxProfilePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write nodejs profile: %w", err)
	}

	return nil
}

func removeManagedLinuxPath() error {
	if err := os.Remove(linuxProfilePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove nodejs profile: %w", err)
	}

	return nil
}

func downloadArchive(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build nodejs download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download nodejs archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download nodejs archive: unexpected status %s", resp.Status)
	}

	file, err := os.Create(destination)
	if err != nil {
		return fmt.Errorf("create nodejs archive file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write nodejs archive: %w", err)
	}

	return nil
}

func extractArchive(ctx context.Context, archivePath, destination, archiveRoot string) (string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", fmt.Errorf("create nodejs extraction directory: %w", err)
	}

	tarPath, ok := lookupCommand("tar")
	if !ok {
		return "", errors.New("tar command is required for Node.js installation")
	}
	if _, err := runCommand(ctx, tarPath, "-xJf", archivePath, "-C", destination); err != nil {
		return "", fmt.Errorf("extract nodejs archive: %w", err)
	}

	extractedPath := filepath.Join(destination, archiveRoot)
	info, err := os.Stat(extractedPath)
	if err != nil {
		return "", fmt.Errorf("inspect extracted nodejs archive: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("extracted nodejs archive root %s is not a directory", extractedPath)
	}

	return extractedPath, nil
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
