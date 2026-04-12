package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const flowPanelDataDirName = "data"

func FlowPanelDataPath() string {
	return filepath.Join(FLOWPANEL_PATH, flowPanelDataDirName)
}

func DefaultDatabasePath() string {
	return filepath.Join(FlowPanelDataPath(), "flowpanel.db")
}

func GoogleDriveOAuthCredentialsPath() string {
	return filepath.Join(FlowPanelDataPath(), "google-drive-oauth-client.json")
}

func BackupsPath() string {
	return filepath.Join(FLOWPANEL_PATH, "backups")
}

func BinPath() string {
	return filepath.Join(FLOWPANEL_PATH, "bin")
}

func CachePath() string {
	return filepath.Join(FLOWPANEL_PATH, "cache")
}

func EnsureFlowPanelDataPath() error {
	path := FlowPanelDataPath()
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create flowpanel data path %q: %w", path, err)
	}

	return nil
}

func EnsureCachePath() error {
	path := CachePath()
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create flowpanel cache path %q: %w", path, err)
	}

	return nil
}

func EnsureBinPath() error {
	path := BinPath()
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create flowpanel bin path %q: %w", path, err)
	}

	return nil
}
