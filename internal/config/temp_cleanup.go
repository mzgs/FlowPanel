package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const staleTemporaryPathAge = 24 * time.Hour

var temporaryPathPrefixes = []string{
	"flowpanel-backup-",
	"flowpanel-chrome-",
	"flowpanel-composer-",
	"flowpanel-download-",
	"flowpanel-drive-backup-",
	"flowpanel-drive-restore-",
	"flowpanel-git-auth-",
	"flowpanel-phpinfo-",
	"flowpanel-restore-",
	"flowpanel-template-",
}

func CleanupStaleTemporaryPaths() (int, error) {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-staleTemporaryPathAge)
	removed := 0
	var cleanupErr error
	for _, entry := range entries {
		if !hasTemporaryPathPrefix(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(os.TempDir(), entry.Name())
		if err := os.RemoveAll(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove stale temporary path %q: %w", path, err))
			continue
		}
		removed++
	}
	return removed, cleanupErr
}

func hasTemporaryPathPrefix(name string) bool {
	for _, prefix := range temporaryPathPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
