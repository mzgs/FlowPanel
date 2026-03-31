package main

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/files"
	httpx "flowpanel/internal/http"
	"flowpanel/internal/jobs"
	"flowpanel/internal/logging"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"

	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "flowpanel: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
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

	domainService := domain.NewService(domainStore)
	if err := domainService.Load(startupCtx); err != nil {
		return fmt.Errorf("load persisted domains: %w", err)
	}

	sessionManager := auth.NewSessionManager(cfg)
	scheduler := jobs.NewScheduler(logger.Named("jobs"), cfg.Cron.Enabled)
	mariadbManager := mariadb.NewService(logger.Named("mariadb"), mariaDBStore)
	phpManager := phpenv.NewService(logger.Named("php"))
	caddyRuntime := caddy.NewRuntime(logger.Named("caddy"), cfg.PublicHTTPAddr, cfg.PublicHTTPSAddr, phpManager)
	fileManager, err := files.NewService(domainService.BasePath())
	if err != nil {
		return fmt.Errorf("initialize file manager: %w", err)
	}

	appContainer := app.New(cfg, logger, dbConn, domainService, sessionManager, scheduler, caddyRuntime, mariadbManager, phpManager, fileManager)

	router, err := httpx.NewRouter(appContainer)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	if err := caddyRuntime.Start(context.Background()); err != nil {
		return fmt.Errorf("start embedded caddy runtime: %w", err)
	}
	if err := caddyRuntime.Sync(context.Background(), domainService.List()); err != nil {
		return fmt.Errorf("sync embedded caddy runtime: %w", err)
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

	logger.Info("flowpanel stopped")

	return nil
}
