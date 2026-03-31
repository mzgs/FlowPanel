package mariadb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	statusCommandTimeout = 3 * time.Second
	dialTimeout          = 500 * time.Millisecond
)

var (
	serverBinaryCandidates = []string{
		"mariadbd",
		"mysqld",
	}
	clientBinaryCandidates = []string{
		"mariadb",
		"mysql",
	}
	socketCandidates = []string{
		"/run/mysqld/mysqld.sock",
		"/var/run/mysqld/mysqld.sock",
		"/run/mysql/mysql.sock",
		"/var/run/mysql/mysql.sock",
		"/run/mariadb/mariadb.sock",
		"/var/run/mariadb/mariadb.sock",
		"/tmp/mysql.sock",
		"/tmp/mysqld.sock",
	}
)

type Manager interface {
	Status(context.Context) Status
	Install(context.Context) error
}

type Status struct {
	Platform         string   `json:"platform"`
	PackageManager   string   `json:"package_manager,omitempty"`
	Product          string   `json:"product,omitempty"`
	ServerInstalled  bool     `json:"server_installed"`
	ServerPath       string   `json:"server_path,omitempty"`
	ClientInstalled  bool     `json:"client_installed"`
	ClientPath       string   `json:"client_path,omitempty"`
	Version          string   `json:"version,omitempty"`
	ListenAddress    string   `json:"listen_address,omitempty"`
	ServiceRunning   bool     `json:"service_running"`
	Ready            bool     `json:"ready"`
	State            string   `json:"state"`
	Message          string   `json:"message"`
	Issues           []string `json:"issues,omitempty"`
	InstallAvailable bool     `json:"install_available"`
	InstallLabel     string   `json:"install_label,omitempty"`
}

type Service struct {
	logger *zap.Logger
}

type actionPlan struct {
	packageManager string
	installLabel   string
	installCmds    [][]string
}

func NewService(logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
	}
}

func (s *Service) Status(ctx context.Context) Status {
	status := Status{
		Platform: runtime.GOOS,
		Product:  "MariaDB",
	}
	plan := detectActionPlan()
	status.PackageManager = plan.packageManager

	serverPath, serverInstalled := lookupFirstCommand(serverBinaryCandidates...)
	if serverInstalled {
		status.ServerInstalled = true
		status.ServerPath = serverPath
	}

	clientPath, clientInstalled := lookupFirstCommand(clientBinaryCandidates...)
	if clientInstalled {
		status.ClientInstalled = true
		status.ClientPath = clientPath
	}

	if output, err := inspectVersion(ctx, clientPath, clientInstalled, serverPath, serverInstalled); err == nil {
		status.Product, status.Version = parseVersion(output)
		if status.Product == "" {
			status.Product = "MariaDB"
		}
	} else if err != nil {
		status.Issues = append(status.Issues, err.Error())
	}

	status.ListenAddress, status.ServiceRunning = detectReachableAddress()
	status.InstallAvailable = len(plan.installCmds) > 0 && !status.ServerInstalled
	status.InstallLabel = plan.installLabel

	switch {
	case status.ServiceRunning:
		status.Ready = true
		status.State = "ready"
		if status.ListenAddress != "" {
			status.Message = fmt.Sprintf("%s is accepting local connections on %s.", status.Product, status.ListenAddress)
		} else {
			status.Message = fmt.Sprintf("%s is accepting local connections.", status.Product)
		}
	case status.ServerInstalled:
		status.State = "stopped"
		status.Message = fmt.Sprintf("%s appears installed, but no local socket or TCP listener responded.", status.Product)
	case status.ClientInstalled:
		status.State = "client-only"
		status.Message = fmt.Sprintf("%s client tools are installed, but no local server binary was found.", status.Product)
	default:
		status.State = "missing"
		status.Message = fmt.Sprintf("%s was not detected on this server.", status.Product)
	}

	return status
}

func (s *Service) Install(ctx context.Context) error {
	plan := detectActionPlan()
	if len(plan.installCmds) == 0 {
		return fmt.Errorf("automatic MariaDB installation is not supported on %s", runtime.GOOS)
	}

	s.logger.Info("installing mariadb runtime",
		zap.String("package_manager", plan.packageManager),
	)
	return runCommands(ctx, plan.installCmds...)
}

func detectActionPlan() actionPlan {
	switch runtime.GOOS {
	case "darwin":
		if brewPath, ok := lookupCommand("brew"); ok {
			return actionPlan{
				packageManager: "homebrew",
				installLabel:   "Install MariaDB",
				installCmds: [][]string{
					{brewPath, "install", "mariadb"},
				},
			}
		}
	case "linux":
		if os.Geteuid() == 0 {
			if aptPath, ok := lookupCommand("apt-get"); ok {
				return actionPlan{
					packageManager: "apt",
					installLabel:   "Install MariaDB",
					installCmds: [][]string{
						{aptPath, "update"},
						{aptPath, "install", "-y", "mariadb-server", "mariadb-client"},
					},
				}
			}
			if dnfPath, ok := lookupCommand("dnf"); ok {
				return actionPlan{
					packageManager: "dnf",
					installLabel:   "Install MariaDB",
					installCmds: [][]string{
						{dnfPath, "install", "-y", "mariadb-server", "mariadb"},
					},
				}
			}
			if yumPath, ok := lookupCommand("yum"); ok {
				return actionPlan{
					packageManager: "yum",
					installLabel:   "Install MariaDB",
					installCmds: [][]string{
						{yumPath, "install", "-y", "mariadb-server", "mariadb"},
					},
				}
			}
			if pacmanPath, ok := lookupCommand("pacman"); ok {
				return actionPlan{
					packageManager: "pacman",
					installLabel:   "Install MariaDB",
					installCmds: [][]string{
						{pacmanPath, "-Sy", "--noconfirm", "mariadb"},
					},
				}
			}
		}
	}

	return actionPlan{}
}

func inspectVersion(ctx context.Context, clientPath string, clientInstalled bool, serverPath string, serverInstalled bool) (string, error) {
	switch {
	case clientInstalled:
		return runInspectCommand(ctx, clientPath, "--version")
	case serverInstalled:
		return runInspectCommand(ctx, serverPath, "--version")
	default:
		return "", nil
	}
}

func lookupFirstCommand(candidates ...string) (string, bool) {
	for _, candidate := range candidates {
		if path, ok := lookupCommand(candidate); ok {
			return path, true
		}
	}

	return "", false
}

func lookupCommand(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range []string{
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

func runInspectCommand(ctx context.Context, name string, args ...string) (string, error) {
	inspectCtx := ctx
	if inspectCtx == nil {
		inspectCtx = context.Background()
	}

	if _, ok := inspectCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		inspectCtx, cancel = context.WithTimeout(inspectCtx, statusCommandTimeout)
		defer cancel()
	}

	return runCommand(inspectCtx, name, args...)
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

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return combinedOutput, fmt.Errorf("%s timed out", name)
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return combinedOutput, fmt.Errorf("%s was canceled", name)
	}

	if combinedOutput == "" {
		return combinedOutput, fmt.Errorf("%s failed: %w", name, err)
	}

	return combinedOutput, fmt.Errorf("%s failed: %s", name, combinedOutput)
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

func detectReachableAddress() (string, bool) {
	for _, socketPath := range socketCandidates {
		if !pathExists(socketPath) {
			continue
		}

		if canDial("unix", socketPath) {
			return socketPath, true
		}
	}

	if canDial("tcp", "127.0.0.1:3306") {
		return "127.0.0.1:3306", true
	}

	return "", false
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func canDial(network, address string) bool {
	conn, err := net.DialTimeout(network, address, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}

func parseVersion(output string) (string, string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		lowerLine := strings.ToLower(line)
		switch {
		case strings.Contains(lowerLine, "mariadb"):
			return "MariaDB", line
		case strings.Contains(lowerLine, "mysql"):
			return "MySQL", line
		default:
			return "", line
		}
	}

	return "", ""
}
