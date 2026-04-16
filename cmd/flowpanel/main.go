package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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

	"go.uber.org/zap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "flowpanel: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 {
		return runCommand(args)
	}

	return runServer()
}

func runCommand(args []string) error {
	if len(args) >= 2 && args[0] == "backup" && args[1] == "create" {
		return runBackupCreateCommand(args[2:])
	}

	return fmt.Errorf("unknown command: %s", strings.Join(args, " "))
}

func runBackupCreateCommand(args []string) error {
	flagSet := flag.NewFlagSet("backup create", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	includePanelData := flagSet.Bool("panel-data", false, "include FlowPanel data files and SQLite database")
	includeSites := flagSet.Bool("sites", false, "include managed site files")
	includeDatabases := flagSet.Bool("databases", false, "include MariaDB dumps")
	location := flagSet.String("location", backup.LocationLocal, "backup destination: local or google_drive")
	if err := flagSet.Parse(args); err != nil {
		return err
	}
	if flagSet.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flagSet.Args(), " "))
	}

	input := backup.CreateInput{
		IncludePanelData: *includePanelData,
		IncludeSites:     *includeSites,
		IncludeDatabases: *includeDatabases,
		Location:         *location,
	}
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

	domainStore := domain.NewStore(dbConn)
	if err := domainStore.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure domain storage: %w", err)
	}
	mariaDBStore := mariadb.NewStore(dbConn)
	if err := mariaDBStore.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure mariadb storage: %w", err)
	}

	domainService := domain.NewService(domainStore)
	if err := domainService.Load(ctx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}
	mariadbManager := mariadb.NewService(logger.Named("mariadb"), mariaDBStore)
	settingsStore := settings.NewStore(dbConn)
	if err := settingsStore.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure settings storage: %w", err)
	}
	settingsService := settings.NewService(settingsStore)
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

	domainStore := domain.NewStore(dbConn)
	if err := domainStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure domain storage: %w", err)
	}
	mariaDBStore := mariadb.NewStore(dbConn)
	if err := mariaDBStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure mariadb storage: %w", err)
	}
	cronStore := flowcron.NewStore(dbConn)
	if err := cronStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure cron storage: %w", err)
	}
	eventsStore := events.NewStore(dbConn)
	if err := eventsStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure event storage: %w", err)
	}
	settingsStore := settings.NewStore(dbConn)
	if err := settingsStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure settings storage: %w", err)
	}
	ftpStore := ftp.NewStore(dbConn)
	if err := ftpStore.Ensure(startupCtx); err != nil {
		return fmt.Errorf("ensure ftp storage: %w", err)
	}

	domainService := domain.NewService(domainStore)
	if err := domainService.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}

	sessionManager := auth.NewSessionManager(cfg)
	scheduler := flowcron.NewScheduler(logger.Named("cron"), cfg.Cron.Enabled, cronStore)
	if err := scheduler.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted cron jobs: %w", err)
	}
	mariadbManager := mariadb.NewService(logger.Named("mariadb"), mariaDBStore)
	golangManager := golang.NewService(logger.Named("golang"))
	nodeJSManager := nodejs.NewService(logger.Named("nodejs"))
	pm2Manager := pm2.NewService(logger.Named("pm2"))
	redisManager := packageruntime.NewRedisService(logger.Named("redis"))
	dockerManager := packageruntime.NewDockerService(logger.Named("docker"))
	mongoDBManager := packageruntime.NewMongoDBService(logger.Named("mongodb"))
	postgresqlManager := packageruntime.NewPostgreSQLService(logger.Named("postgresql"))
	phpManager := phpenv.NewService(logger.Named("php"))
	phpMyAdminManager := phpmyadmin.NewService(logger.Named("phpmyadmin"))
	eventService := events.NewService(logger.Named("events"), eventsStore)
	settingsService := settings.NewService(settingsStore)
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
	ftpService := ftp.NewService(ftpStore, domainService)
	ftpRuntime := ftp.NewRuntime(logger.Named("ftp"), ftpService)
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
	)
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
		sessionManager,
		scheduler,
		caddyRuntime,
		golangManager,
		nodeJSManager,
		pm2Manager,
		mariadbManager,
		dockerManager,
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

	logger.Info("flowpanel stopped")

	return nil
}
