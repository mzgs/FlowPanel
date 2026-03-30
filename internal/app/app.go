package app

import (
	"database/sql"

	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	"flowpanel/internal/jobs"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"
)

type App struct {
	Config   config.Config
	Logger   *zap.Logger
	DB       *sql.DB
	Sessions *scs.SessionManager
	Jobs     *jobs.Scheduler
	Caddy    *caddy.Runtime
}

func New(
	cfg config.Config,
	logger *zap.Logger,
	db *sql.DB,
	sessions *scs.SessionManager,
	scheduler *jobs.Scheduler,
	caddyRuntime *caddy.Runtime,
) *App {
	return &App{
		Config:   cfg,
		Logger:   logger,
		DB:       db,
		Sessions: sessions,
		Jobs:     scheduler,
		Caddy:    caddyRuntime,
	}
}
