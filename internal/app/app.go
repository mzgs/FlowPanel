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
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"
)

type App struct {
	Config     config.Config
	Logger     *zap.Logger
	DB         *sql.DB
	Domains    *domain.Service
	Sessions   *scs.SessionManager
	Cron       *cron.Scheduler
	Caddy      *caddy.Runtime
	MariaDB    mariadb.Manager
	PHP        phpenv.Manager
	PHPMyAdmin phpmyadmin.Manager
	Files      *files.Service
	Events     *events.Service
	Backups    backup.Manager
}

func New(
	cfg config.Config,
	logger *zap.Logger,
	db *sql.DB,
	domains *domain.Service,
	sessions *scs.SessionManager,
	scheduler *cron.Scheduler,
	caddyRuntime *caddy.Runtime,
	mariadbManager mariadb.Manager,
	phpManager phpenv.Manager,
	phpMyAdminManager phpmyadmin.Manager,
	fileManager *files.Service,
	eventService *events.Service,
	backupManager backup.Manager,
) *App {
	return &App{
		Config:     cfg,
		Logger:     logger,
		DB:         db,
		Domains:    domains,
		Sessions:   sessions,
		Cron:       scheduler,
		Caddy:      caddyRuntime,
		MariaDB:    mariadbManager,
		PHP:        phpManager,
		PHPMyAdmin: phpMyAdminManager,
		Files:      fileManager,
		Events:     eventService,
		Backups:    backupManager,
	}
}
