package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
)

type storageEnsurer interface {
	Ensure(context.Context) error
}

type panelStores struct {
	Auth          *auth.Store
	Domain        *domain.Store
	MariaDB       *mariadb.Store
	PM2           *pm2.Store
	Cron          *flowcron.Store
	Events        *events.Store
	Settings      *settings.Store
	FTP           *ftp.Store
	SystemMonitor *systemmonitor.Store
}

type namedStore struct {
	name  string
	store storageEnsurer
}

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
		Domain:        domain.NewStore(dbConn),
		MariaDB:       mariadb.NewStore(dbConn),
		PM2:           pm2.NewStore(dbConn),
		Cron:          flowcron.NewStore(dbConn),
		Events:        events.NewStore(dbConn),
		Settings:      settings.NewStore(dbConn),
		FTP:           ftp.NewStore(dbConn),
		SystemMonitor: systemmonitor.NewStore(dbConn),
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
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer()
		},
	}

	cmd.AddCommand(newServeCommand(), newStatusCommand(), newBackupCommand())

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

func newBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups",
	}

	cmd.AddCommand(newBackupCreateCommand())

	return cmd
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
	cmd.Flags().BoolVar(&input.IncludeSites, "sites", false, "include managed site files")
	cmd.Flags().BoolVar(&input.IncludeDatabases, "databases", false, "include MariaDB dumps")
	cmd.Flags().StringVar(&input.Location, "location", backup.LocationLocal, "backup destination: local or google_drive")

	return cmd
}

type panelStatus struct {
	Running bool
	Error   bool
	WebURL  string
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func runMenu() error {
	reader := bufio.NewReader(os.Stdin)

	for {
		status, err := loadPanelStatus(context.Background())
		if err != nil {
			return err
		}

		printPanelStatus(os.Stdout, status)
		_, _ = fmt.Fprintln(os.Stdout)
		_, _ = fmt.Fprintln(os.Stdout, "1. Start panel")
		_, _ = fmt.Fprintln(os.Stdout, "2. Refresh status")
		_, _ = fmt.Fprintln(os.Stdout, "3. Show help")
		_, _ = fmt.Fprintln(os.Stdout, "0. Exit")
		_, _ = fmt.Fprint(os.Stdout, "Select: ")

		choice, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if errors.Is(err, io.EOF) && strings.TrimSpace(choice) == "" {
			return nil
		}

		switch strings.TrimSpace(choice) {
		case "1":
			if status.Running {
				_, _ = fmt.Fprintf(os.Stdout, "Panel is already running at %s\n\n", status.WebURL)
				continue
			}
			return runServer()
		case "2", "":
			_, _ = fmt.Fprintln(os.Stdout)
			continue
		case "3":
			if err := newRootCommand().Help(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stdout)
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

func loadPanelStatus(ctx context.Context) (panelStatus, error) {
	cfg, err := config.Load()
	if err != nil {
		return panelStatus{}, err
	}

	status := panelStatus{WebURL: adminWebURL(cfg.AdminListenAddr)}
	checkCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()

	req, err := stdhttp.NewRequestWithContext(checkCtx, stdhttp.MethodGet, status.WebURL+"/healthz", nil)
	if err != nil {
		return panelStatus{}, err
	}

	resp, err := stdhttp.DefaultClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		status.Running = resp.StatusCode == stdhttp.StatusOK
		status.Error = resp.StatusCode >= stdhttp.StatusInternalServerError
	}

	return status, nil
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
	_, _ = fmt.Fprintf(w, "%s FlowPanel: %s\n", icon, state)
	_, _ = fmt.Fprintf(w, "Web UI: %s\n", status.WebURL)
}

func adminWebURL(listenAddr string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://" + listenAddr
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port)
}

func runBackupCreateCommand(input backup.CreateInput) error {
	if !input.IncludePanelData && !input.IncludeSites && !input.IncludeDatabases {
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

	logger, err := logging.New(cfg.Env)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	logger.Info("flowpanel starting",
		zap.String("env", cfg.Env),
		zap.String("admin_listen_addr", cfg.AdminListenAddr),
		zap.String("public_http_addr", cfg.PublicHTTPAddr),
		zap.String("public_https_addr", cfg.PublicHTTPSAddr),
		zap.String("database_path", cfg.Database.Path),
		zap.Bool("cron_enabled", cfg.Cron.Enabled),
	)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

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
		namedStore{name: "mariadb", store: stores.MariaDB},
		namedStore{name: "pm2", store: stores.PM2},
		namedStore{name: "cron", store: stores.Cron},
		namedStore{name: "event", store: stores.Events},
		namedStore{name: "settings", store: stores.Settings},
		namedStore{name: "ftp", store: stores.FTP},
		namedStore{name: "system monitor", store: stores.SystemMonitor},
	); err != nil {
		return err
	}

	domainService := domain.NewService(stores.Domain)
	if err := domainService.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}

	sessionManager := auth.NewSessionManager(cfg)
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
	nodeJSManager := nodejs.NewService(logger.Named("nodejs"))
	pm2Manager := pm2.NewService(logger.Named("pm2"), stores.PM2)
	redisManager := packageruntime.NewRedisService(logger.Named("redis"))
	dockerManager := packageruntime.NewDockerService(logger.Named("docker"))
	ffmpegManager := packageruntime.NewFFmpegService(logger.Named("ffmpeg"))
	mongoDBManager := packageruntime.NewMongoDBService(logger.Named("mongodb"))
	postgresqlManager := packageruntime.NewPostgreSQLService(logger.Named("postgresql"))
	phpManager := phpenv.NewService(logger.Named("php"))
	phpMyAdminManager := phpmyadmin.NewService(logger.Named("phpmyadmin"))
	eventService := events.NewService(logger.Named("events"), stores.Events)
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
	systemMonitorService := systemmonitor.NewService(logger.Named("system-monitor"), stores.SystemMonitor)
	taskManagerService := taskmanager.NewService(logger.Named("task-manager"), scheduler)
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
	if _, err := pm2Manager.Sync(startupCtx); err != nil {
		logger.Error("sync pm2 processes failed", zap.Error(err))
	}
	caddyRuntime := caddy.NewRuntime(
		logger.Named("caddy"),
		cfg.AdminListenAddr,
		cfg.PublicHTTPAddr,
		cfg.PublicHTTPSAddr,
		phpManager,
		phpMyAdminManager,
		cfg.PHPMyAdminAddr,
	)
	fileManager, err := files.NewService(domainService.BasePath())
	if err != nil {
		return fmt.Errorf("initialize file manager: %w", err)
	}

	appContainer := app.New(
		cfg,
		logger,
		dbConn,
		domainService,
		authService,
		sessionManager,
		scheduler,
		caddyRuntime,
		golangManager,
		nodeJSManager,
		pm2Manager,
		mariadbManager,
		dockerManager,
		ffmpegManager,
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
	)

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
	systemMonitorService.Start()
	scheduler.Start()

	server := &stdhttp.Server{
		Addr:              cfg.AdminListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("admin server listening", zap.String("addr", cfg.AdminListenAddr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
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

	logger.Info("flowpanel stopped")

	return nil
}
