package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, fmt.Errorf("create database directory: %w", err)
			}
		}
	}

	dsn := path
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite connection: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite connection: %w", err)
	}
	if path != ":memory:" {
		if err := secureFiles(path, path+"-journal", path+"-wal", path+"-shm"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, nil
}

func secureFiles(paths ...string) error {
	for _, path := range paths {
		if err := os.Chmod(path, 0o600); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("secure database file %q: %w", path, err)
		}
	}
	return nil
}
