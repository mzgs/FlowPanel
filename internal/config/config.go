package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDevelopmentSessionSecret = "development-session-secret-change-me-123456"

type Config struct {
	Env             string
	AdminListenAddr string
	PublicHTTPAddr  string
	PublicHTTPSAddr string
	PHPMyAdminAddr  string
	ShutdownTimeout time.Duration
	Database        DatabaseConfig
	Session         SessionConfig
	Cron            CronConfig
	GoogleDrive     GoogleDriveConfig
}

type DatabaseConfig struct {
	Path string
}

type SessionConfig struct {
	Secret     string
	CookieName string
	Lifetime   time.Duration
}

type CronConfig struct {
	Enabled bool
}

type GoogleDriveConfig struct {
	ClientID        string
	ClientSecret    string
	CredentialsPath string
}

func Load() (Config, error) {
	shutdownTimeout, err := getDuration("FLOWPANEL_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	sessionLifetime, err := getDuration("FLOWPANEL_SESSION_LIFETIME", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}

	cronEnabled, err := getBool("FLOWPANEL_CRON_ENABLED", true)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Env:             getEnv("FLOWPANEL_ENV", "development"),
		AdminListenAddr: getEnv("FLOWPANEL_ADMIN_LISTEN_ADDR", ":8080"),
		PublicHTTPAddr:  getEnv("FLOWPANEL_PUBLIC_HTTP_ADDR", ":80"),
		PublicHTTPSAddr: getEnv("FLOWPANEL_PUBLIC_HTTPS_ADDR", ":443"),
		PHPMyAdminAddr:  getEnv("FLOWPANEL_PHPMYADMIN_ADDR", ":32109"),
		ShutdownTimeout: shutdownTimeout,
		Database: DatabaseConfig{
			Path: getEnv("FLOWPANEL_DB_PATH", DefaultDatabasePath()),
		},
		Session: SessionConfig{
			Secret:     getEnv("FLOWPANEL_SESSION_SECRET", defaultDevelopmentSessionSecret),
			CookieName: getEnv("FLOWPANEL_SESSION_COOKIE_NAME", "flowpanel_session"),
			Lifetime:   sessionLifetime,
		},
		Cron: CronConfig{
			Enabled: cronEnabled,
		},
		GoogleDrive: GoogleDriveConfig{
			ClientID:        getEnv("FLOWPANEL_GOOGLE_DRIVE_CLIENT_ID", ""),
			ClientSecret:    getEnv("FLOWPANEL_GOOGLE_DRIVE_CLIENT_SECRET", ""),
			CredentialsPath: getEnv("FLOWPANEL_GOOGLE_DRIVE_CREDENTIALS_PATH", GoogleDriveOAuthCredentialsPath()),
		},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) IsProduction() bool {
	return c.Env == "production"
}

func (c Config) validate() error {
	var problems []string

	switch c.Env {
	case "development", "production", "test":
	default:
		problems = append(problems, "FLOWPANEL_ENV must be one of development, production, test")
	}

	if strings.TrimSpace(c.AdminListenAddr) == "" {
		problems = append(problems, "FLOWPANEL_ADMIN_LISTEN_ADDR must not be empty")
	}
	if strings.TrimSpace(c.PublicHTTPAddr) == "" {
		problems = append(problems, "FLOWPANEL_PUBLIC_HTTP_ADDR must not be empty")
	}
	if strings.TrimSpace(c.PublicHTTPSAddr) == "" {
		problems = append(problems, "FLOWPANEL_PUBLIC_HTTPS_ADDR must not be empty")
	}
	if strings.TrimSpace(c.PHPMyAdminAddr) == "" {
		problems = append(problems, "FLOWPANEL_PHPMYADMIN_ADDR must not be empty")
	}
	if strings.TrimSpace(c.Database.Path) == "" {
		problems = append(problems, "FLOWPANEL_DB_PATH must not be empty")
	}
	if strings.TrimSpace(c.Session.CookieName) == "" {
		problems = append(problems, "FLOWPANEL_SESSION_COOKIE_NAME must not be empty")
	}
	if c.Session.Lifetime <= 0 {
		problems = append(problems, "FLOWPANEL_SESSION_LIFETIME must be greater than zero")
	}
	if c.ShutdownTimeout <= 0 {
		problems = append(problems, "FLOWPANEL_SHUTDOWN_TIMEOUT must be greater than zero")
	}
	if len(c.Session.Secret) < 32 {
		problems = append(problems, "FLOWPANEL_SESSION_SECRET must be at least 32 characters")
	}
	if c.IsProduction() && c.Session.Secret == defaultDevelopmentSessionSecret {
		problems = append(problems, "FLOWPANEL_SESSION_SECRET must be set explicitly in production")
	}
	if (strings.TrimSpace(c.GoogleDrive.ClientID) == "") != (strings.TrimSpace(c.GoogleDrive.ClientSecret) == "") {
		problems = append(problems, "FLOWPANEL_GOOGLE_DRIVE_CLIENT_ID and FLOWPANEL_GOOGLE_DRIVE_CLIENT_SECRET must be set together")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}

	return nil
}

func getEnv(key, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}

func getBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a valid boolean: %w", key, err)
	}

	return parsed, nil
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", key, err)
	}

	return parsed, nil
}

func (c Config) String() string {
	return fmt.Sprintf("env=%s admin=%s public_http=%s public_https=%s phpmyadmin=%s db=%s cron_enabled=%t",
		c.Env,
		c.AdminListenAddr,
		c.PublicHTTPAddr,
		c.PublicHTTPSAddr,
		c.PHPMyAdminAddr,
		c.Database.Path,
		c.Cron.Enabled,
	)
}
