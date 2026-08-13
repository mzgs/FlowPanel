package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"flowpanel/internal/alerts"
	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/backup"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	flowcron "flowpanel/internal/cron"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/events"
	"flowpanel/internal/files"
	"flowpanel/internal/firewall"
	"flowpanel/internal/ftp"
	"flowpanel/internal/golang"
	"flowpanel/internal/googledrive"
	httpx "flowpanel/internal/http"
	"flowpanel/internal/logging"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/nodejs"
	"flowpanel/internal/packageruntime"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/pm2"
	"flowpanel/internal/settings"
	"flowpanel/internal/systemmonitor"
	"flowpanel/internal/taskmanager"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/term"
)

type storageEnsurer interface {
	Ensure(context.Context) error
}

type panelStores struct {
	Auth          *auth.Store
	Sessions      *auth.SessionStore
	Domain        *domain.Store
	MariaDB       *mariadb.Store
	PM2           *pm2.Store
	Cron          *flowcron.Store
	Events        *events.Store
	Settings      *settings.Store
	FTP           *ftp.Store
	SystemMonitor *systemmonitor.Store
	Alerts        *alerts.Store
}

type namedStore struct {
	name  string
	store storageEnsurer
}

const (
	installedBinaryPath = "/usr/local/bin/flowpanel"
	installerURL        = "https://raw.githubusercontent.com/mzgs/FlowPanel/main/install.sh"
	macosLaunchdLabel   = "com.mzgs.flowpanel"
	pm2StartupAttempts  = 3
	pm2StartupTimeout   = 30 * time.Second
)

var version = "0.0.0"

var (
	publicHostIPCache      string
	publicHostIPCacheUntil time.Time
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "flowpanel: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 && isTerminal(os.Stdin) {
		return runMenu()
	}

	cmd := newRootCommand()
	cmd.SetArgs(args)
	return cmd.Execute()
}

func newPanelStores(dbConn *sql.DB) panelStores {
	return panelStores{
		Auth:          auth.NewStore(dbConn),
		Sessions:      auth.NewSessionStore(dbConn),
		Domain:        domain.NewStore(dbConn),
		MariaDB:       mariadb.NewStore(dbConn),
		PM2:           pm2.NewStore(dbConn),
		Cron:          flowcron.NewStore(dbConn),
		Events:        events.NewStore(dbConn),
		Settings:      settings.NewStore(dbConn),
		FTP:           ftp.NewStore(dbConn),
		SystemMonitor: systemmonitor.NewStore(dbConn),
		Alerts:        alerts.NewStore(dbConn),
	}
}

func ensureStores(ctx context.Context, stores ...namedStore) error {
	for _, store := range stores {
		if err := store.store.Ensure(ctx); err != nil {
			return fmt.Errorf("ensure %s storage: %w", store.name, err)
		}
	}

	return nil
}

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "flowpanel",
		Short:         "FlowPanel server control panel",
		Version:       panelVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer()
		},
	}

	cmd.AddCommand(newServeCommand(), newStopCommand(), newRestartCommand(), newRepairCommand(), newUpdateCommand(), newStatusCommand(), newVersionCommand(), newCredentialsCommand(), newBackupCommand())

	return cmd
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the FlowPanel server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer()
		},
	}
}

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show FlowPanel status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			status, err := loadPanelStatus(cmd.Context())
			if err != nil {
				return err
			}
			printPanelStatus(cmd.OutOrStdout(), status)
			return nil
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show FlowPanel version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), panelVersion())
			return nil
		},
	}
}

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the FlowPanel server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStopCommand(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newRestartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the FlowPanel server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestartCommand(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newRepairCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "repair",
		Short: "Repair panel storage and restart FlowPanel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepairPanelCommand(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update FlowPanel to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateCommand(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

func newBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups",
	}

	cmd.AddCommand(newBackupCreateCommand())

	return cmd
}

func newCredentialsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credentials",
		Short: "Manage panel credentials",
	}

	cmd.AddCommand(newCredentialsUsernameCommand(), newCredentialsPasswordCommand())
	return cmd
}

func newCredentialsUsernameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "username <username>",
		Short: "Change the panel username",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChangePanelUsername(cmd.Context(), cmd.OutOrStdout(), args[0])
		},
	}
}

func newCredentialsPasswordCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "password [password]",
		Short: "Change the panel password",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			password := ""
			if len(args) > 0 {
				password = args[0]
			} else {
				var err error
				password, err = promptConfirmedPassword(cmd.OutOrStdout())
				if err != nil {
					return err
				}
			}
			return runChangePanelPassword(cmd.Context(), cmd.OutOrStdout(), password)
		},
	}
}

func newBackupCreateCommand() *cobra.Command {
	input := backup.CreateInput{Location: backup.LocationLocal}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackupCreateCommand(input)
		},
	}

	cmd.Flags().BoolVar(&input.IncludePanelData, "panel-data", false, "include FlowPanel data files and SQLite database")
	cmd.Flags().BoolVar(&input.IncludeDockerData, "docker-data", false, "include FlowPanel-managed Docker data and container definitions")
	cmd.Flags().BoolVar(&input.IncludeSites, "sites", false, "include managed site files")
	cmd.Flags().BoolVar(&input.IncludeDatabases, "databases", false, "include MariaDB dumps")
	cmd.Flags().StringVar(&input.Location, "location", backup.LocationLocal, "backup destination: local or google_drive")

	return cmd
}

type panelStatus struct {
	Running       bool
	Error         bool
	WebURL        string
	HealthURL     string
	WebUIUsername string
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func runMenu() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		clearMenuScreen(os.Stdout)

		status, err := loadPanelStatus(context.Background())
		if err != nil {
			return err
		}

		printPanelStatus(os.Stdout, status)
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "(1) Start panel")
		_, _ = fmt.Fprintln(os.Stdout, "(2) Stop panel")
		_, _ = fmt.Fprintln(os.Stdout, "(3) Restart panel")
		_, _ = fmt.Fprintln(os.Stdout, "(4) Repair panel")
		_, _ = fmt.Fprintln(os.Stdout, "(5) Change panel username")
		_, _ = fmt.Fprintln(os.Stdout, "(6) Change panel password")
		_, _ = fmt.Fprintln(os.Stdout, "(7) Create backup")
		_, _ = fmt.Fprintln(os.Stdout, "(8) Update FlowPanel")
		_, _ = fmt.Fprintln(os.Stdout, "(9) Uninstall FlowPanel")
		_, _ = fmt.Fprintln(os.Stdout, "(10) Show help")
		_, _ = fmt.Fprintln(os.Stdout, "(0) Exit")
		_, _ = fmt.Fprint(os.Stdout, "Select: ")

		choice, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if errors.Is(err, io.EOF) && strings.TrimSpace(choice) == "" {
			return nil
		}

		selection := strings.TrimSpace(choice)
		switch selection {
		case "1", "3":
			restart := selection == "3"
			if !restart && status.Running {
				continue
			}
			if !restart && status.Error {
				continue
			}
			if restart && (status.Running || status.Error) {
				if err := runStopCommand(context.Background(), io.Discard); err != nil {
					return err
				}
			}
			if err := startPanelDetached(context.Background(), io.Discard, status.WebURL, status.HealthURL); err != nil {
				return err
			}
		case "2":
			if err := runStopCommand(context.Background(), io.Discard); err != nil {
				return err
			}
		case "4":
			if err := runRepairPanelCommand(context.Background(), os.Stdout); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "5":
			username, err := promptMenuLine(reader, os.Stdout, "New panel username: ")
			if err != nil {
				return err
			}
			if err := runChangePanelUsername(context.Background(), os.Stdout, username); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "6":
			password, err := promptConfirmedPassword(os.Stdout)
			if err != nil {
				return err
			}
			if err := runChangePanelPassword(context.Background(), os.Stdout, password); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "7":
			input, err := promptBackupCreateInput(reader, os.Stdout)
			if err != nil {
				return err
			}
			if err := runBackupCreateCommand(input); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "8":
			if err := runUpdateCommand(context.Background(), os.Stdout); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "9":
			if err := promptUninstall(reader, os.Stdout); err != nil {
				_, _ = fmt.Fprintf(os.Stdout, "Error: %v\n", err)
			}
			pauseMenu(reader, os.Stdout)
		case "10":
			if err := newRootCommand().Help(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout)
			pauseMenu(reader, os.Stdout)
		case "":
			_, _ = fmt.Fprintln(os.Stdout)
			continue
		case "0", "q", "quit", "exit":
			return nil
		default:
			_, _ = fmt.Fprintln(os.Stdout, "Unknown selection")
			_, _ = fmt.Fprintln(os.Stdout)
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func clearMenuScreen(w io.Writer) {
	_, _ = fmt.Fprint(w, "\033[H\033[2J\033[3J")
}

func promptMenuLine(reader *bufio.Reader, w io.Writer, label string) (string, error) {
	_, _ = fmt.Fprint(w, label)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	return strings.TrimSpace(value), nil
}

func promptBackupCreateInput(reader *bufio.Reader, w io.Writer) (backup.CreateInput, error) {
	location, err := promptBackupLocation(reader, w)
	if err != nil {
		return backup.CreateInput{}, err
	}

	input := backup.CreateInput{Location: location}
	prompts := []struct {
		label string
		set   *bool
	}{
		{label: "Include FlowPanel data? [Y/n]: ", set: &input.IncludePanelData},
		{label: "Include Docker data and containers? [Y/n]: ", set: &input.IncludeDockerData},
		{label: "Include site files? [Y/n]: ", set: &input.IncludeSites},
		{label: "Include database dumps? [Y/n]: ", set: &input.IncludeDatabases},
	}
	for _, prompt := range prompts {
		enabled, err := promptMenuBool(reader, w, prompt.label, true)
		if err != nil {
			return backup.CreateInput{}, err
		}
		*prompt.set = enabled
	}

	return input, nil
}

func promptBackupLocation(reader *bufio.Reader, w io.Writer) (string, error) {
	for {
		value, err := promptMenuLine(reader, w, "Backup location local/google_drive [local]: ")
		if err != nil {
			return "", err
		}

		switch strings.ReplaceAll(strings.ToLower(value), " ", "_") {
		case "", "local", "l", "1":
			return backup.LocationLocal, nil
		case "google_drive", "googledrive", "drive", "g", "2":
			return backup.LocationGoogleDrive, nil
		default:
			_, _ = fmt.Fprintln(w, "Enter local or google_drive.")
		}
	}
}

func promptMenuBool(reader *bufio.Reader, w io.Writer, label string, defaultValue bool) (bool, error) {
	for {
		value, err := promptMenuLine(reader, w, label)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "":
			return defaultValue, nil
		case "y", "yes", "1", "true":
			return true, nil
		case "n", "no", "0", "false":
			return false, nil
		default:
			_, _ = fmt.Fprintln(w, "Enter yes or no.")
		}
	}
}

func promptConfirmedPassword(w io.Writer) (string, error) {
	if !isTerminal(os.Stdin) {
		return "", errors.New("password argument is required when stdin is not a terminal")
	}

	password, err := promptHiddenPassword(w, "New panel password: ")
	if err != nil {
		return "", err
	}
	confirmation, err := promptHiddenPassword(w, "Confirm panel password: ")
	if err != nil {
		return "", err
	}
	if password != confirmation {
		return "", errors.New("passwords do not match")
	}

	return password, nil
}

func promptHiddenPassword(w io.Writer, label string) (string, error) {
	_, _ = fmt.Fprint(w, label)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(w)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	return strings.TrimSpace(string(password)), nil
}

func pauseMenu(reader *bufio.Reader, w io.Writer) {
	_, _ = fmt.Fprint(w, "Press Enter to continue...")
	_, _ = reader.ReadString('\n')
}

func promptUninstall(reader *bufio.Reader, w io.Writer) error {
	removeData, err := promptMenuBool(reader, w, "Remove FlowPanel config and data too? [y/N]: ", false)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "This will stop FlowPanel and remove its service files and installed binary.")
	if removeData {
		_, _ = fmt.Fprintln(w, "FlowPanel config and data directories will also be removed.")
	} else {
		_, _ = fmt.Fprintln(w, "FlowPanel config and data directories will be preserved.")
	}
	confirmation, err := promptMenuLine(reader, w, `Type "uninstall" to continue: `)
	if err != nil {
		return err
	}
	if strings.ToLower(confirmation) != "uninstall" {
		_, _ = fmt.Fprintln(w, "Uninstall canceled")
		return nil
	}

	return runUninstallCommand(context.Background(), w, removeData)
}

func runUpdateCommand(ctx context.Context, w io.Writer) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("update is not supported on %s", runtime.GOOS)
	}

	request, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, installerURL, nil)
	if err != nil {
		return fmt.Errorf("create installer request: %w", err)
	}
	response, err := stdhttp.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download installer: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != stdhttp.StatusOK {
		return fmt.Errorf("download installer: unexpected HTTP status %s", response.Status)
	}

	installer, err := os.CreateTemp("", "flowpanel-update-*.sh")
	if err != nil {
		return fmt.Errorf("create installer file: %w", err)
	}
	installerPath := installer.Name()
	defer func() {
		_ = os.Remove(installerPath)
	}()
	if _, err := io.Copy(installer, response.Body); err != nil {
		_ = installer.Close()
		return fmt.Errorf("save installer: %w", err)
	}
	if err := installer.Close(); err != nil {
		return fmt.Errorf("close installer file: %w", err)
	}

	_, _ = fmt.Fprintln(w, "Updating FlowPanel to the latest release...")
	command := exec.CommandContext(ctx, "/bin/sh", installerPath)
	command.Stdin = os.Stdin
	command.Stdout = w
	command.Stderr = w
	if err := command.Run(); err != nil {
		return fmt.Errorf("run installer: %w", err)
	}
	return nil
}

func runUninstallCommand(ctx context.Context, w io.Writer, removeData bool) error {
	_, _ = loadInstallerEnvFile()

	if err := stopInstalledService(ctx, w); err != nil {
		return err
	}
	status, statusErr := loadPanelStatus(ctx)
	if statusErr == nil && (status.Running || status.Error) {
		if err := runStopCommand(ctx, w); err != nil {
			_, _ = fmt.Fprintf(w, "Warning: %v\n", err)
		}
	}
	if err := removeInstalledFiles(ctx, w, removeData); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "FlowPanel uninstalled")
	if !removeData {
		_, _ = fmt.Fprintln(w, "Config and data were preserved.")
	}
	return nil
}

func stopInstalledService(ctx context.Context, w io.Writer) error {
	switch runtime.GOOS {
	case "linux":
		if !pathExists("/etc/systemd/system/flowpanel.service") {
			return nil
		}
		if err := runRootCommand(ctx, w, true, "systemctl", "stop", "flowpanel"); err != nil {
			return err
		}
		return runRootCommand(ctx, w, true, "systemctl", "disable", "flowpanel")
	case "darwin":
		plist := "/Library/LaunchDaemons/" + macosLaunchdLabel + ".plist"
		if !pathExists(plist) {
			return nil
		}
		if err := runRootCommand(ctx, w, true, "launchctl", "bootout", "system", plist); err != nil {
			return err
		}
		return runRootCommand(ctx, w, true, "launchctl", "disable", "system/"+macosLaunchdLabel)
	default:
		return fmt.Errorf("uninstall is not supported on %s", runtime.GOOS)
	}
}

func removeInstalledFiles(ctx context.Context, w io.Writer, removeData bool) error {
	for _, path := range uninstallFilePaths() {
		if err := runRootCommand(ctx, w, false, "rm", "-f", path); err != nil {
			return err
		}
	}
	if runtime.GOOS == "linux" {
		if err := runRootCommand(ctx, w, true, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if !removeData {
		return nil
	}

	for _, path := range uninstallDataPaths() {
		if err := runRootCommand(ctx, w, false, "rm", "-rf", path); err != nil {
			return err
		}
	}
	return nil
}

func uninstallFilePaths() []string {
	paths := []string{installedBinaryPath}
	switch runtime.GOOS {
	case "linux":
		paths = append(paths, "/etc/systemd/system/flowpanel.service")
	case "darwin":
		paths = append(paths, "/Library/LaunchDaemons/"+macosLaunchdLabel+".plist")
	}
	return paths
}

func uninstallDataPaths() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"/etc/flowpanel", "/var/lib/flowpanel", config.FLOWPANEL_PATH}
	case "darwin":
		return []string{"/usr/local/etc/flowpanel", "/Library/Application Support/FlowPanel", "/Library/Logs/FlowPanel", config.FLOWPANEL_PATH}
	default:
		return nil
	}
}

func runRootCommand(ctx context.Context, w io.Writer, optional bool, name string, args ...string) error {
	commandName, commandArgs := rootCommand(name, args...)
	cmd := exec.CommandContext(ctx, commandName, commandArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil && !optional {
		return fmt.Errorf("%s failed: %w", strings.Join(append([]string{commandName}, commandArgs...), " "), err)
	}
	return nil
}

func rootCommand(name string, args ...string) (string, []string) {
	if os.Geteuid() == 0 {
		return name, args
	}
	return "sudo", append([]string{name}, args...)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runStopCommand(ctx context.Context, w io.Writer) error {
	status, err := loadPanelStatus(ctx)
	if err != nil {
		return err
	}
	if !status.Running && !status.Error {
		_, _ = fmt.Fprintln(w, "FlowPanel is not running")
		return nil
	}

	pid, err := readPanelPID()
	if err != nil {
		return fmt.Errorf("read FlowPanel pid: %w", err)
	}
	if pid == os.Getpid() {
		return errors.New("refusing to stop the current CLI process")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find FlowPanel process %d: %w", pid, err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop FlowPanel process %d: %w", pid, err)
	}
	if err := waitForPanelStop(ctx, status.HealthURL, 15*time.Second); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(w, "FlowPanel stopped")
	return nil
}

func runRestartCommand(ctx context.Context, w io.Writer) error {
	status, err := loadPanelStatus(ctx)
	if err != nil {
		return err
	}
	if status.Running || status.Error {
		if err := runStopCommand(ctx, w); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(w, "Starting FlowPanel...")
	return runServer()
}

func runRepairPanelCommand(ctx context.Context, w io.Writer) error {
	if _, err := loadInstallerEnvFile(); err != nil {
		return err
	}
	if err := repairPanelStorage(ctx, w); err != nil {
		return err
	}

	status, err := loadPanelStatus(ctx)
	if err != nil {
		return err
	}
	if status.Running || status.Error {
		if err := runStopCommand(ctx, w); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintln(w, "Starting FlowPanel...")
	return startPanelDetached(ctx, w, status.WebURL, status.HealthURL)
}

func repairPanelStorage(ctx context.Context, w io.Writer) error {
	if err := config.EnsureFlowPanelDataPath(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dbConn, err := db.Open(ctx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = dbConn.Close()
	}()

	stores := newPanelStores(dbConn)
	if err := ensureStores(
		ctx,
		namedStore{name: "domain", store: stores.Domain},
		namedStore{name: "auth", store: stores.Auth},
		namedStore{name: "sessions", store: stores.Sessions},
		namedStore{name: "mariadb", store: stores.MariaDB},
		namedStore{name: "pm2", store: stores.PM2},
		namedStore{name: "cron", store: stores.Cron},
		namedStore{name: "event", store: stores.Events},
		namedStore{name: "settings", store: stores.Settings},
		namedStore{name: "ftp", store: stores.FTP},
		namedStore{name: "system monitor", store: stores.SystemMonitor},
		namedStore{name: "alerts", store: stores.Alerts},
	); err != nil {
		return err
	}

	authService := auth.NewService(stores.Auth)
	if _, ok, err := authService.CurrentAdmin(ctx); err != nil {
		return fmt.Errorf("load panel admin user: %w", err)
	} else if ok {
		_, _ = fmt.Fprintln(w, "Panel storage repaired")
		return nil
	}

	username, password, generated, err := repairPanelCredentials(cfg)
	if err != nil {
		return err
	}
	if _, err := authService.CreateInitialAdmin(ctx, auth.CreateInitialAdminInput{
		Username: username,
		Password: password,
	}); err != nil {
		var validation auth.ValidationErrors
		if errors.As(err, &validation) {
			return fmt.Errorf("invalid initial admin credentials: %v", map[string]string(validation))
		}
		return fmt.Errorf("ensure initial admin user: %w", err)
	}
	if err := syncPanelCredentialsEnvFile(username, password); err != nil {
		return fmt.Errorf("panel admin created in database but env file update failed: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Panel admin created: %s\n", username)
	if generated {
		_, _ = fmt.Fprintf(w, "Generated panel password: %s\n", password)
	}
	return nil
}

func repairPanelCredentials(cfg config.Config) (string, string, bool, error) {
	username := strings.TrimSpace(cfg.InitialAdmin.Username)
	password := strings.TrimSpace(cfg.InitialAdmin.Password)
	if username != "" && password != "" {
		return username, password, false, nil
	}

	usernameSuffix, err := randomHex(4)
	if err != nil {
		return "", "", false, err
	}
	generatedPassword, err := randomHex(18)
	if err != nil {
		return "", "", false, err
	}

	return "admin-" + usernameSuffix, "fp-" + generatedPassword, true, nil
}

func randomHex(byteCount int) (string, error) {
	data := make([]byte, byteCount)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate random credentials: %w", err)
	}

	return hex.EncodeToString(data), nil
}

func startPanelDetached(ctx context.Context, w io.Writer, webURL, healthURL string) error {
	if err := config.EnsureFlowPanelDataPath(); err != nil {
		return err
	}

	logPath := panelLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open FlowPanel log file: %w", err)
	}
	defer func() {
		_ = logFile.Close()
	}()

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open null device: %w", err)
	}
	defer func() {
		_ = devNull.Close()
	}()

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve FlowPanel executable: %w", err)
	}

	cmd := exec.Command(executable, "serve")
	cmd.Env = os.Environ()
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start FlowPanel: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release FlowPanel process: %w", err)
	}

	if err := waitForPanelStart(ctx, healthURL, 15*time.Second); err != nil {
		return fmt.Errorf("%w; check logs at %s", err, logPath)
	}

	_, _ = fmt.Fprintf(w, "FlowPanel started at %s\n", webURL)
	return nil
}

func loadPanelStatus(ctx context.Context) (panelStatus, error) {
	_, _ = loadInstallerEnvFile()

	cfg, err := config.Load()
	if err != nil {
		return panelStatus{}, err
	}

	status := panelStatus{
		WebURL:        adminWebURL(cfg.AdminListenAddr, adminTLSEnabled(cfg)),
		HealthURL:     adminHealthURL(cfg.AdminListenAddr, adminTLSEnabled(cfg)),
		WebUIUsername: strings.TrimSpace(cfg.InitialAdmin.Username),
	}
	if username := loadPanelStatusUsername(ctx, cfg.Database.Path); username != "" {
		status.WebUIUsername = username
	}

	checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	req, err := stdhttp.NewRequestWithContext(checkCtx, stdhttp.MethodGet, status.HealthURL+"/healthz", nil)
	if err != nil {
		return panelStatus{}, err
	}

	resp, err := adminHTTPClient().Do(req)
	if err == nil {
		defer resp.Body.Close()
		status.Running = resp.StatusCode == stdhttp.StatusOK
		status.Error = resp.StatusCode >= stdhttp.StatusInternalServerError
	}

	return status, nil
}

func loadPanelStatusUsername(ctx context.Context, dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" || (dbPath != ":memory:" && !pathExists(dbPath)) {
		return ""
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	dbConn, err := db.Open(lookupCtx, dbPath)
	if err != nil {
		return ""
	}
	defer func() {
		_ = dbConn.Close()
	}()

	user, ok, err := auth.NewService(auth.NewStore(dbConn)).CurrentAdmin(lookupCtx)
	if err != nil || !ok {
		return ""
	}
	return user.Username
}

func printPanelStatus(w io.Writer, status panelStatus) {
	icon := "\033[31m●\033[0m"
	state := "stopped"
	switch {
	case status.Running:
		icon = "\033[32m●\033[0m"
		state = "running"
	case status.Error:
		icon = "\033[33m●\033[0m"
		state = "error"
	}
	_, _ = fmt.Fprintf(w, "%s FlowPanel %s: %s\n", icon, panelVersion(), state)
	_, _ = fmt.Fprintf(w, "Web UI: %s\n", status.WebURL)
	if status.WebUIUsername != "" {
		_, _ = fmt.Fprintf(w, "Web UI username: %s\n", status.WebUIUsername)
	}
}

func panelVersion() string {
	value := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if value == "" {
		return "0.0.0"
	}
	return value
}

func adminWebURL(listenAddr string, tlsEnabled bool) string {
	scheme := adminURLScheme(tlsEnabled)
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return scheme + "://" + listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = publicHostIP()
		if host == "" {
			host = primaryHostIP()
		}
	}

	return scheme + "://" + net.JoinHostPort(host, port)
}

func adminHealthURL(listenAddr string, tlsEnabled bool) string {
	scheme := adminURLScheme(tlsEnabled)
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return scheme + "://" + listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func adminURLScheme(tlsEnabled bool) string {
	if tlsEnabled {
		return "https"
	}
	return "http"
}

func publicHostIP() string {
	now := time.Now()
	if now.Before(publicHostIPCacheUntil) {
		return publicHostIPCache
	}

	publicHostIPCache = lookupPublicHostIP()
	publicHostIPCacheUntil = now.Add(5 * time.Minute)
	return publicHostIPCache
}

func lookupPublicHostIP() string {
	client := stdhttp.Client{Timeout: 1200 * time.Millisecond}
	for _, url := range []string{"https://api.ipify.org", "https://ipv4.icanhazip.com", "https://v4.ident.me"} {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != stdhttp.StatusOK {
			continue
		}
		if host := usablePublicIPv4(strings.TrimSpace(string(body))); host != "" {
			return host
		}
	}
	return ""
}

func primaryHostIP() string {
	if conn, err := net.Dial("udp", "1.1.1.1:80"); err == nil {
		defer func() {
			_ = conn.Close()
		}()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			if host := usableHostIP(addr.IP); host != "" {
				return host
			}
		}
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	ipv6Host := ""
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := addrIP(addr)
			host := usableHostIP(ip)
			if host == "" {
				continue
			}
			if ip.To4() != nil {
				return host
			}
			if ipv6Host == "" {
				ipv6Host = host
			}
		}
	}
	if ipv6Host != "" {
		return ipv6Host
	}
	return "127.0.0.1"
}

func addrIP(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}

func usableHostIP(ip net.IP) string {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.String()
}

func usablePublicIPv4(value string) string {
	ip := net.ParseIP(value)
	if ip4 := ip.To4(); ip4 != nil && ip4.IsGlobalUnicast() && !ip4.IsPrivate() && !ip4.IsLoopback() {
		return ip4.String()
	}
	return ""
}

func panelPIDPath() string {
	return filepath.Join(config.FlowPanelDataPath(), "flowpanel.pid")
}

func panelLogPath() string {
	return filepath.Join(config.FlowPanelDataPath(), "flowpanel.log")
}

func writePanelPID() error {
	return os.WriteFile(panelPIDPath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func readPanelPID() (int, error) {
	data, err := os.ReadFile(panelPIDPath())
	if err != nil {
		return 0, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file %q", panelPIDPath())
	}

	return pid, nil
}

func removePanelPID(pid int) {
	currentPID, err := readPanelPID()
	if err == nil && currentPID == pid {
		_ = os.Remove(panelPIDPath())
	}
}

func waitForPanelStop(ctx context.Context, webURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if !panelHealthOK(ctx, webURL) {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for FlowPanel to stop")
		case <-ticker.C:
		}
	}
}

func waitForPanelStart(ctx context.Context, webURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if panelHealthOK(ctx, webURL) {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for FlowPanel to start")
		case <-ticker.C:
		}
	}
}

func panelHealthOK(ctx context.Context, webURL string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	req, err := stdhttp.NewRequestWithContext(checkCtx, stdhttp.MethodGet, webURL+"/healthz", nil)
	if err != nil {
		return false
	}

	resp, err := adminHTTPClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == stdhttp.StatusOK
}

func runChangePanelUsername(ctx context.Context, w io.Writer, username string) error {
	return withAuthService(ctx, func(ctx context.Context, authService *auth.Service) error {
		user, created, err := updateOrCreatePanelCredentials(ctx, authService, username, "")
		if err != nil {
			return credentialCommandError(err)
		}
		if err := syncPanelCredentialsEnvFile(user.Username, ""); err != nil {
			return fmt.Errorf("panel username changed in database but env file update failed: %w", err)
		}

		action := "changed"
		if created {
			action = "created"
		}
		_, _ = fmt.Fprintf(w, "Panel username %s to %s\n", action, user.Username)
		return nil
	})
}

func runChangePanelPassword(ctx context.Context, w io.Writer, password string) error {
	return withAuthService(ctx, func(ctx context.Context, authService *auth.Service) error {
		_, created, err := updateOrCreatePanelCredentials(ctx, authService, "", password)
		if err != nil {
			return credentialCommandError(err)
		}
		if err := syncPanelCredentialsEnvFile("", password); err != nil {
			return fmt.Errorf("panel password changed in database but env file update failed: %w", err)
		}

		action := "changed"
		if created {
			action = "created"
		}
		_, _ = fmt.Fprintf(w, "Panel password %s\n", action)
		return nil
	})
}

func updateOrCreatePanelCredentials(ctx context.Context, authService *auth.Service, username string, password string) (auth.PublicUser, bool, error) {
	if _, ok, err := authService.CurrentAdmin(ctx); err != nil {
		return auth.PublicUser{}, false, err
	} else if ok {
		user, err := authService.UpdateFirstAdminCredentials(ctx, username, password)
		return user, false, err
	}

	initialUsername := strings.TrimSpace(username)
	if initialUsername == "" {
		initialUsername = strings.TrimSpace(os.Getenv("FLOWPANEL_ADMIN_USERNAME"))
	}
	initialPassword := strings.TrimSpace(password)
	if initialPassword == "" {
		initialPassword = strings.TrimSpace(os.Getenv("FLOWPANEL_ADMIN_PASSWORD"))
	}

	user, err := authService.CreateInitialAdmin(ctx, auth.CreateInitialAdminInput{
		Username: initialUsername,
		Password: initialPassword,
	})
	if err != nil {
		return auth.PublicUser{}, false, err
	}

	return user, true, nil
}

func withAuthService(ctx context.Context, fn func(context.Context, *auth.Service) error) error {
	if _, err := loadInstallerEnvFile(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dbConn, err := db.Open(ctx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = dbConn.Close()
	}()

	store := auth.NewStore(dbConn)
	if err := store.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure auth storage: %w", err)
	}

	return fn(ctx, auth.NewService(store))
}

func loadInstallerEnvFile() (string, error) {
	envFile := installerEnvFilePath()
	if envFile == "" {
		return "", nil
	}
	if _, err := os.Stat(envFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("inspect FlowPanel env file: %w", err)
	}

	values, err := readEnvFile(envFile)
	if err != nil {
		return "", err
	}
	for key, value := range values {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			_ = os.Setenv(key, value)
		}
	}
	if strings.TrimSpace(os.Getenv("FLOWPANEL_ENV_FILE")) == "" {
		_ = os.Setenv("FLOWPANEL_ENV_FILE", envFile)
	}

	return envFile, nil
}

func syncPanelCredentialsEnvFile(username string, password string) error {
	envFile := installerEnvFilePath()
	if envFile == "" {
		return nil
	}
	if _, err := os.Stat(envFile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect FlowPanel env file: %w", err)
	}

	values := map[string]string{}
	if strings.TrimSpace(username) != "" {
		values["FLOWPANEL_ADMIN_USERNAME"] = strings.TrimSpace(username)
	}
	if strings.TrimSpace(password) != "" {
		values["FLOWPANEL_ADMIN_PASSWORD"] = strings.TrimSpace(password)
	}

	if err := updateEnvFile(envFile, values); err != nil {
		return err
	}
	for key, value := range values {
		_ = os.Setenv(key, value)
	}

	return nil
}

func installerEnvFilePath() string {
	if envFile := strings.TrimSpace(os.Getenv("FLOWPANEL_ENV_FILE")); envFile != "" {
		return trimEnvQuotes(envFile)
	}

	switch runtime.GOOS {
	case "linux":
		return "/etc/flowpanel/flowpanel.env"
	case "darwin":
		return "/usr/local/etc/flowpanel/flowpanel.env"
	default:
		return ""
	}
}

func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read FlowPanel env file: %w", err)
	}

	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := parseEnvLine(line)
		if ok {
			values[key] = value
		}
	}

	return values, nil
}

func updateEnvFile(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read FlowPanel env file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	seen := map[string]bool{}
	for i, line := range lines {
		key, _, ok := parseEnvLine(line)
		value, update := values[key]
		if !ok || !update {
			continue
		}

		lines[i] = envAssignmentLine(line, key, value)
		seen[key] = true
	}
	for key, value := range values {
		if !seen[key] {
			lines = append(lines, key+"="+quoteEnvValue(value))
		}
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return fmt.Errorf("write FlowPanel env file: %w", err)
	}

	return nil
}

func parseEnvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "export ")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	key, value, ok := strings.Cut(trimmed, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return "", "", false
	}

	return key, unquoteEnvValue(strings.TrimSpace(value)), true
}

func envAssignmentLine(existing string, key string, value string) string {
	prefix := ""
	if strings.HasPrefix(strings.TrimSpace(existing), "export ") {
		prefix = "export "
	}

	return prefix + key + "=" + quoteEnvValue(value)
}

func quoteEnvValue(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("._/@:%+=,-", r)
	}) == -1 {
		return value
	}

	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + replacer.Replace(value) + `"`
}

func unquoteEnvValue(value string) string {
	value = trimEnvQuotes(value)
	replacer := strings.NewReplacer(`\$`, "$", "\\`", "`", `\"`, `"`, `\\`, `\`)
	return replacer.Replace(value)
}

func trimEnvQuotes(value string) string {
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
			return value[1 : len(value)-1]
		}
	}

	return value
}

func credentialCommandError(err error) error {
	var validation auth.ValidationErrors
	if !errors.As(err, &validation) {
		return err
	}

	for _, field := range []string{"username", "password", "credentials"} {
		if message := strings.TrimSpace(validation[field]); message != "" {
			return errors.New(message)
		}
	}

	return errors.New("panel credentials are invalid")
}

func runBackupCreateCommand(input backup.CreateInput) error {
	if os.Getenv("FLOWPANEL_SCHEDULED_BACKUP") == "1" && os.Getenv("FLOWPANEL_BACKUP_SCOPE_V2") != "1" && input.IncludePanelData {
		input.IncludeDockerData = true
	}
	if !input.IncludePanelData && !input.IncludeDockerData && !input.IncludeSites && !input.IncludeDatabases {
		return fmt.Errorf("select at least one backup scope")
	}

	if err := config.EnsureFlowPanelDataPath(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := logging.New(cfg.Env)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dbConn, err := db.Open(ctx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = dbConn.Close()
	}()

	stores := newPanelStores(dbConn)
	if err := ensureStores(
		ctx,
		namedStore{name: "domain", store: stores.Domain},
		namedStore{name: "auth", store: stores.Auth},
		namedStore{name: "mariadb", store: stores.MariaDB},
		namedStore{name: "pm2", store: stores.PM2},
		namedStore{name: "settings", store: stores.Settings},
	); err != nil {
		return err
	}

	domainService := domain.NewService(stores.Domain)
	if err := domainService.Load(ctx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}
	mariadbManager := mariadb.NewService(logger.Named("mariadb"), stores.MariaDB)
	pm2Manager := pm2.NewService(logger.Named("pm2"), stores.PM2)
	settingsService := settings.NewService(stores.Settings)
	googleDriveService := googledrive.NewService(cfg.GoogleDrive)
	backupService := backup.NewService(
		logger.Named("backup"),
		config.FlowPanelDataPath(),
		config.BackupsPath(),
		cfg.Database.Path,
		dbConn,
		domainService,
		mariadbManager,
		settingsService,
		googleDriveService,
		pm2Manager,
	)
	record, err := backupService.Create(ctx, input)
	if err != nil {
		var validation backup.ValidationErrors
		if errors.As(err, &validation) {
			return fmt.Errorf("backup validation failed: %v", map[string]string(validation))
		}
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, record.Name)

	return nil
}

func runServer() error {
	if err := config.EnsureFlowPanelDataPath(); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := ensureAdminTLSCertificate(cfg); err != nil {
		return err
	}

	logger, logWriter, err := logging.NewRotating(cfg.Env, panelLogPath())
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() {
		_ = logger.Sync()
		_ = logWriter.Close()
	}()
	if removed, cleanupErr := config.CleanupStaleTemporaryPaths(); cleanupErr != nil {
		logger.Warn("clean stale temporary paths failed", zap.Error(cleanupErr))
	} else if removed > 0 {
		logger.Info("removed stale temporary paths", zap.Int("count", removed))
	}

	pid := os.Getpid()
	if err := writePanelPID(); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer removePanelPID(pid)

	logger.Info("flowpanel starting",
		zap.String("env", cfg.Env),
		zap.String("admin_listen_addr", cfg.AdminListenAddr),
		zap.String("public_http_addr", cfg.PublicHTTPAddr),
		zap.String("public_https_addr", cfg.PublicHTTPSAddr),
		zap.String("database_path", cfg.Database.Path),
		zap.Bool("cron_enabled", cfg.Cron.Enabled),
	)

	startupCtx := context.Background()

	dbConn, err := db.Open(startupCtx, cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		_ = dbConn.Close()
	}()

	stores := newPanelStores(dbConn)
	if err := ensureStores(
		startupCtx,
		namedStore{name: "domain", store: stores.Domain},
		namedStore{name: "auth", store: stores.Auth},
		namedStore{name: "sessions", store: stores.Sessions},
		namedStore{name: "mariadb", store: stores.MariaDB},
		namedStore{name: "pm2", store: stores.PM2},
		namedStore{name: "cron", store: stores.Cron},
		namedStore{name: "event", store: stores.Events},
		namedStore{name: "settings", store: stores.Settings},
		namedStore{name: "ftp", store: stores.FTP},
		namedStore{name: "system monitor", store: stores.SystemMonitor},
		namedStore{name: "alerts", store: stores.Alerts},
	); err != nil {
		return err
	}

	domainService := domain.NewService(stores.Domain)
	if err := domainService.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}

	sessionManager := auth.NewSessionManager(cfg, stores.Sessions)
	authService := auth.NewService(stores.Auth)
	if strings.TrimSpace(cfg.InitialAdmin.Username) != "" {
		user, created, err := authService.EnsureInitialAdmin(startupCtx, auth.CreateInitialAdminInput{
			Username: cfg.InitialAdmin.Username,
			Password: cfg.InitialAdmin.Password,
		})
		if err != nil {
			var validation auth.ValidationErrors
			if errors.As(err, &validation) {
				return fmt.Errorf("invalid initial admin credentials: %v", map[string]string(validation))
			}
			return fmt.Errorf("ensure initial admin user: %w", err)
		}
		if created {
			logger.Info("created initial panel admin", zap.String("username", user.Username))
		}
	}
	scheduler := flowcron.NewScheduler(logger.Named("cron"), cfg.Cron.Enabled, stores.Cron)
	if err := scheduler.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted cron jobs: %w", err)
	}
	mariadbManager := mariadb.NewService(logger.Named("mariadb"), stores.MariaDB)
	golangManager := golang.NewService(logger.Named("golang"))
	rustManager := packageruntime.NewRustService(logger.Named("rust"))
	nodeJSManager := nodejs.NewService(logger.Named("nodejs"))
	pm2Manager := pm2.NewService(logger.Named("pm2"), stores.PM2)
	redisManager := packageruntime.NewRedisService(logger.Named("redis"))
	dockerManager := packageruntime.NewDockerService(logger.Named("docker"))
	ffmpegManager := packageruntime.NewFFmpegService(logger.Named("ffmpeg"))
	imageMagickManager := packageruntime.NewImageMagickService(logger.Named("imagemagick"))
	ytdlpManager := packageruntime.NewYTDLPService(logger.Named("ytdlp"))
	mongoDBManager := packageruntime.NewMongoDBService(logger.Named("mongodb"))
	postgresqlManager := packageruntime.NewPostgreSQLService(logger.Named("postgresql"))
	phpManager := phpenv.NewService(logger.Named("php"))
	phpMyAdminManager := phpmyadmin.NewService(logger.Named("phpmyadmin"))
	eventService := events.NewService(logger.Named("events"), stores.Events)
	retentionCtx, stopEventRetention := context.WithCancel(context.Background())
	defer stopEventRetention()
	eventService.StartRetention(retentionCtx)
	logging.SetSecurityEventHandler(func(event logging.SecurityEvent) {
		if _, err := eventService.RecordSecurity(context.Background(), events.SecurityInput{
			Action:        event.Action,
			Hostname:      event.Hostname,
			URI:           event.URI,
			ClientIP:      event.ClientIP,
			TransactionID: event.TransactionID,
			ExpiresAt:     event.ExpiresAt,
		}); err != nil {
			logger.Error("record WAF security event failed", zap.String("hostname", event.Hostname), zap.Error(err))
		}
	})
	settingsService := settings.NewService(stores.Settings)
	phpManager.SetDefaultVersionResolver(func(ctx context.Context, status phpenv.Status) string {
		record, err := settingsService.Get(ctx)
		if err != nil {
			return status.DefaultVersion
		}
		if strings.TrimSpace(record.DefaultPHPVersion) == "" {
			return status.DefaultVersion
		}
		return record.DefaultPHPVersion
	})
	ftpService := ftp.NewService(stores.FTP, domainService)
	ftpRuntime := ftp.NewRuntime(logger.Named("ftp"), ftpService)
	alertService := alerts.NewService(logger.Named("alerts"), stores.Alerts)
	systemMonitorService := systemmonitor.NewService(logger.Named("system-monitor"), stores.SystemMonitor, alertService)
	taskManagerService := taskmanager.NewService(logger.Named("task-manager"), scheduler)
	firewallService := firewall.NewService(
		logger.Named("firewall"),
		filepath.Join(config.FlowPanelDataPath(), "firewall.json"),
		cfg.Firewall.Enabled,
	)
	googleDriveService := googledrive.NewService(cfg.GoogleDrive)
	backupService := backup.NewService(
		logger.Named("backup"),
		config.FlowPanelDataPath(),
		config.BackupsPath(),
		cfg.Database.Path,
		dbConn,
		domainService,
		mariadbManager,
		settingsService,
		googleDriveService,
		pm2Manager,
	)
	caddyRuntime := caddy.NewRuntime(
		logger.Named("caddy"),
		cfg.AdminListenAddr,
		cfg.AdminTLSCertFile,
		cfg.PublicHTTPAddr,
		cfg.PublicHTTPSAddr,
		phpManager,
		phpMyAdminManager,
		cfg.PHPMyAdminAddr,
	)
	fileManagerRoot := filepath.VolumeName(domainService.BasePath()) + string(filepath.Separator)
	fileManager, err := files.NewService(fileManagerRoot)
	if err != nil {
		return fmt.Errorf("initialize file manager: %w", err)
	}

	appContainer := app.New(
		cfg,
		panelVersion(),
		logger,
		dbConn,
		domainService,
		authService,
		sessionManager,
		scheduler,
		caddyRuntime,
		golangManager,
		rustManager,
		nodeJSManager,
		pm2Manager,
		mariadbManager,
		dockerManager,
		ffmpegManager,
		imageMagickManager,
		ytdlpManager,
		redisManager,
		mongoDBManager,
		postgresqlManager,
		phpManager,
		phpMyAdminManager,
		fileManager,
		ftpRuntime,
		ftpService,
		eventService,
		backupService,
		settingsService,
		googleDriveService,
		systemMonitorService,
		taskManagerService,
		firewallService,
		alertService,
	)
	alertService.SetCertificateSources(func() []string {
		records := domainService.List()
		hostnames := make([]string, 0, len(records))
		for _, record := range records {
			hostnames = append(hostnames, record.Hostname)
		}
		return hostnames
	}, cfg.AdminTLSCertFile)
	scheduler.SetExecutionObserver(func(job flowcron.Record, execution flowcron.ExecutionLog) {
		if _, ok := backup.ParseScheduledCommand(job.Command); !ok {
			return
		}
		key := "backup:scheduled:" + job.ID
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if execution.Status == "success" {
			if err := alertService.Resolve(ctx, key); err != nil {
				logger.Error("resolve scheduled backup alert failed", zap.Error(err))
			}
			return
		}
		message := strings.TrimSpace(execution.Error)
		if message == "" {
			message = "The scheduled backup command failed."
		}
		if err := alertService.Trigger(ctx, alerts.TriggerInput{Key: key, Severity: "critical", Title: "Scheduled backup failed", Message: fmt.Sprintf("%s: %s", job.Name, message)}); err != nil {
			logger.Error("trigger scheduled backup alert failed", zap.Error(err))
		}
	})

	router, err := httpx.NewRouter(appContainer)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	if err := caddyRuntime.Start(context.Background()); err != nil {
		return fmt.Errorf("start embedded caddy runtime: %w", err)
	}
	settingsRecord, err := settingsService.Get(startupCtx)
	if err != nil {
		return fmt.Errorf("load persisted settings: %w", err)
	}
	syncPM2AtStartup(pm2Manager, logger)
	if err := caddyRuntime.Sync(context.Background(), domainService.List(), settingsRecord.PanelURL); err != nil {
		return fmt.Errorf("sync embedded caddy runtime: %w", err)
	}
	if err := ftpRuntime.Apply(context.Background(), ftp.Config{
		Enabled:      settingsRecord.FTPEnabled,
		Host:         settingsRecord.FTPHost,
		Port:         settingsRecord.FTPPort,
		PublicIP:     settingsRecord.FTPPublicIP,
		PassivePorts: settingsRecord.FTPPassivePorts,
	}); err != nil {
		return fmt.Errorf("start ftp runtime: %w", err)
	}
	if err := firewallService.Reconcile(context.Background(), firewall.Config{
		AdminAddr:       cfg.AdminListenAddr,
		HTTPAddr:        cfg.PublicHTTPAddr,
		HTTPSAddr:       cfg.PublicHTTPSAddr,
		FTPEnabled:      settingsRecord.FTPEnabled,
		FTPPort:         settingsRecord.FTPPort,
		FTPPassivePorts: settingsRecord.FTPPassivePorts,
		DomainPorts:     domain.TargetPorts(domainService.List()),
	}); err != nil {
		logger.Error("reconcile managed firewall at startup failed", zap.Error(err))
	}
	systemMonitorService.Start()
	scheduler.Start()
	alertService.Start()

	server := &stdhttp.Server{
		Addr:              cfg.AdminListenAddr,
		Handler:           router,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if adminTLSEnabled(cfg) {
		server.TLSConfig = adminTLSConfig(cfg)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("admin server listening", zap.String("addr", cfg.AdminListenAddr), zap.Bool("tls", adminTLSEnabled(cfg)))
		serve := server.ListenAndServe
		if adminTLSEnabled(cfg) {
			serve = func() error {
				return server.ListenAndServeTLS("", "")
			}
		}
		if err := serve(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case err := <-serverErrCh:
		return fmt.Errorf("admin server failed: %w", err)
	case sig := <-signalCh:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("admin server shutdown failed", zap.Error(err))
	}

	if err := scheduler.Stop(shutdownCtx); err != nil {
		logger.Error("cron scheduler shutdown failed", zap.Error(err))
	}

	if err := caddyRuntime.Stop(shutdownCtx); err != nil {
		logger.Error("embedded caddy runtime shutdown failed", zap.Error(err))
	}

	if err := ftpRuntime.Stop(); err != nil {
		logger.Error("ftp runtime shutdown failed", zap.Error(err))
	}

	if err := systemMonitorService.Stop(shutdownCtx); err != nil {
		logger.Error("system monitor shutdown failed", zap.Error(err))
	}
	if err := alertService.Stop(shutdownCtx); err != nil {
		logger.Error("alert service shutdown failed", zap.Error(err))
	}

	logger.Info("flowpanel stopped")

	return nil
}

func syncPM2AtStartup(manager pm2.Manager, logger *zap.Logger) {
	for attempt := 1; attempt <= pm2StartupAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), pm2StartupTimeout)
		_, err := manager.Sync(ctx)
		cancel()
		if err == nil {
			if attempt > 1 {
				logger.Info("restored pm2 processes after retry", zap.Int("attempt", attempt))
			}
			return
		}

		if attempt == pm2StartupAttempts {
			logger.Error("restore pm2 processes at startup failed", zap.Int("attempts", attempt), zap.Error(err))
			return
		}
		logger.Warn("restore pm2 processes at startup failed; retrying", zap.Int("attempt", attempt), zap.Error(err))
		time.Sleep(time.Duration(attempt) * time.Second)
	}
}
