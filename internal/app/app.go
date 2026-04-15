package app

import (
	"database/sql"

	"flowpanel/internal/backup"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	"flowpanel/internal/cron"
	"flowpanel/internal/domain"
	"flowpanel/internal/events"
	"flowpanel/internal/files"
	"flowpanel/internal/ftp"
	"flowpanel/internal/golang"
	"flowpanel/internal/googledrive"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"
	"flowpanel/internal/settings"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"
)

type App struct {
	Config      config.Config
	Logger      *zap.Logger
	DB          *sql.DB
	Domains     *domain.Service
	Sessions    *scs.SessionManager
	Cron        *cron.Scheduler
	Caddy       *caddy.Runtime
	Golang      golang.Manager
	MariaDB     mariadb.Manager
	PHP         phpenv.Manager
	PHPMyAdmin  phpmyadmin.Manager
	Files       *files.Service
	FTP         *ftp.Runtime
	FTPAccounts *ftp.Service
	Events      *events.Service
	Backups     backup.Manager
	Settings    *settings.Service
	GoogleDrive *googledrive.Service
}

func New(
	cfg config.Config,
	logger *zap.Logger,
	db *sql.DB,
	domains *domain.Service,
	sessions *scs.SessionManager,
	scheduler *cron.Scheduler,
	caddyRuntime *caddy.Runtime,
	golangManager golang.Manager,
	mariadbManager mariadb.Manager,
	phpManager phpenv.Manager,
	phpMyAdminManager phpmyadmin.Manager,
	fileManager *files.Service,
	ftpRuntime *ftp.Runtime,
	ftpAccounts *ftp.Service,
	eventService *events.Service,
	backupManager backup.Manager,
	settingsService *settings.Service,
	googleDriveService *googledrive.Service,
) *App {
	return &App{
		Config:      cfg,
		Logger:      logger,
		DB:          db,
		Domains:     domains,
		Sessions:    sessions,
		Cron:        scheduler,
		Caddy:       caddyRuntime,
		Golang:      golangManager,
		MariaDB:     mariadbManager,
		PHP:         phpManager,
		PHPMyAdmin:  phpMyAdminManager,
		Files:       fileManager,
		FTP:         ftpRuntime,
		FTPAccounts: ftpAccounts,
		Events:      eventService,
		Backups:     backupManager,
		Settings:    settingsService,
		GoogleDrive: googleDriveService,
	}
}
