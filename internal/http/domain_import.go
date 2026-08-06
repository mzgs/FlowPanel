package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/domain"
	filesvc "flowpanel/internal/files"

	ftpclient "github.com/jlaffaye/ftp"
)

const (
	websiteImportTimeout  = 30 * time.Minute
	websiteImportMaxFiles = 100000
	websiteImportMaxBytes = int64(20 << 30)
)

type websiteImportInput struct {
	Provider           string                      `json:"provider"`
	UsePanelBackup     bool                        `json:"use_panel_backup"`
	Panel              *panelConnectionInput       `json:"panel,omitempty"`
	SiteID             string                      `json:"site_id,omitempty"`
	SubscriptionID     string                      `json:"subscription_id,omitempty"`
	SiteHostname       string                      `json:"site_hostname,omitempty"`
	Host               string                      `json:"host"`
	Port               int                         `json:"port"`
	Username           string                      `json:"username"`
	Password           string                      `json:"password"`
	SourcePath         string                      `json:"source_path"`
	Secure             bool                        `json:"secure"`
	ReplaceTargetFiles bool                        `json:"replace_target_files"`
	Database           *websiteImportDatabaseInput `json:"database,omitempty"`
}

type websiteImportResult struct {
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
	Database string `json:"database,omitempty"`
}

func (input *websiteImportInput) normalizeAndValidate() domain.ValidationErrors {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.SiteID = strings.TrimSpace(input.SiteID)
	input.SubscriptionID = strings.TrimSpace(input.SubscriptionID)
	input.SiteHostname = strings.ToLower(strings.TrimSpace(input.SiteHostname))
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.SourcePath = strings.TrimSpace(input.SourcePath)
	validation := domain.ValidationErrors{}

	if input.Provider != "cpanel" && input.Provider != "plesk" {
		validation["provider"] = "Select cPanel or Plesk."
	}
	if input.Provider == "plesk" && input.UsePanelBackup {
		if input.Panel == nil {
			validation["panel"] = "Reconnect to Plesk before importing."
		} else {
			for field, message := range input.Panel.normalizeAndValidate() {
				validation[field] = message
			}
			if input.Panel.Provider != "plesk" {
				validation["provider"] = "The panel connection must use Plesk."
			}
		}
		if input.SiteID == "" || input.SubscriptionID == "" || input.SiteHostname == "" {
			validation["site_id"] = "Select a Plesk website."
		}
	} else {
		if input.Host == "" || strings.ContainsAny(input.Host, "/?#@") {
			validation["host"] = "Enter a valid FTP hostname or IP address."
		}
		if input.Port < 1 || input.Port > 65535 {
			validation["port"] = "Port must be between 1 and 65535."
		}
		if input.Username == "" {
			validation["username"] = "Enter the FTP username."
		}
		if input.Password == "" {
			validation["password"] = "Enter the FTP password."
		}
		if input.SourcePath == "" {
			validation["source_path"] = "Enter the source document root."
		}
	}
	if input.Database != nil {
		input.Database.normalize()
		databaseValidation := input.Database.validate()
		if input.Provider == "plesk" && input.UsePanelBackup {
			databaseValidation = input.Database.validateDestination()
		}
		for field, message := range databaseValidation {
			validation[field] = message
		}
	}

	return validation
}

func importWebsiteFiles(ctx context.Context, record domain.Record, basePath string, input websiteImportInput) (websiteImportResult, error) {
	targetPath, err := resolveDomainDocumentRoot(record, basePath)
	if err != nil {
		return websiteImportResult{}, err
	}
	parentPath := filepath.Dir(targetPath)
	if err := os.MkdirAll(parentPath, 0o755); err != nil {
		return websiteImportResult{}, fmt.Errorf("ensure document root parent: %w", err)
	}

	stagePath, err := os.MkdirTemp(parentPath, ".flowpanel-import-")
	if err != nil {
		return websiteImportResult{}, fmt.Errorf("create import staging directory: %w", err)
	}
	defer os.RemoveAll(stagePath)

	importCtx, cancel := context.WithTimeout(ctx, websiteImportTimeout)
	defer cancel()
	result, err := downloadFTPDirectory(importCtx, input, stagePath)
	if err != nil {
		return websiteImportResult{}, err
	}
	if result.Files == 0 {
		return websiteImportResult{}, errors.New("the source directory does not contain any files")
	}

	if err := publishImportedWebsite(stagePath, targetPath, input.ReplaceTargetFiles); err != nil {
		return websiteImportResult{}, err
	}

	return result, nil
}

func publishImportedWebsite(stagePath, targetPath string, replace bool) error {
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return fmt.Errorf("ensure document root: %w", err)
	}
	if replace {
		if err := clearDocumentRootContents(targetPath); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(stagePath)
	if err != nil {
		return fmt.Errorf("read staged website: %w", err)
	}
	for _, entry := range entries {
		if err := filesvc.CopyPath(filepath.Join(stagePath, entry.Name()), filepath.Join(targetPath, entry.Name())); err != nil {
			return fmt.Errorf("publish imported file %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func downloadFTPDirectory(ctx context.Context, input websiteImportInput, targetPath string) (websiteImportResult, error) {
	host := strings.Trim(strings.TrimSpace(input.Host), "[]")
	options := []ftpclient.DialOption{
		ftpclient.DialWithContext(ctx),
		ftpclient.DialWithTimeout(15 * time.Second),
		ftpclient.DialWithForceListHidden(true),
	}
	if input.Secure {
		options = append(options, ftpclient.DialWithExplicitTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		}))
	}

	connection, err := ftpclient.Dial(net.JoinHostPort(host, strconv.Itoa(input.Port)), options...)
	if err != nil {
		return websiteImportResult{}, fmt.Errorf("connect to source FTP server: %w", err)
	}
	defer connection.Quit()
	if err := connection.Login(input.Username, input.Password); err != nil {
		return websiteImportResult{}, fmt.Errorf("authenticate with source FTP server: %w", err)
	}

	root := path.Clean(strings.ReplaceAll(input.SourcePath, "\\", "/"))
	result := websiteImportResult{}
	if err := downloadFTPEntries(ctx, connection, root, targetPath, &result); err != nil {
		return websiteImportResult{}, err
	}
	return result, nil
}

func downloadFTPEntries(ctx context.Context, connection *ftpclient.ServerConn, remotePath, localPath string, result *websiteImportResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := connection.List(remotePath)
	if err != nil {
		return fmt.Errorf("list source directory %q: %w", remotePath, err)
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name
		if name == "." || name == ".." || name == "" {
			continue
		}
		if path.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return fmt.Errorf("source contains an unsafe path name %q", name)
		}

		remoteEntryPath := path.Join(remotePath, name)
		localEntryPath := filepath.Join(localPath, name)
		switch entry.Type {
		case ftpclient.EntryTypeFolder:
			if err := os.MkdirAll(localEntryPath, 0o755); err != nil {
				return fmt.Errorf("create local directory %q: %w", name, err)
			}
			if err := downloadFTPEntries(ctx, connection, remoteEntryPath, localEntryPath, result); err != nil {
				return err
			}
		case ftpclient.EntryTypeFile:
			if result.Files >= websiteImportMaxFiles || entry.Size > uint64(websiteImportMaxBytes-result.Bytes) {
				return fmt.Errorf("source exceeds the import limit of %d files or %d GiB", websiteImportMaxFiles, websiteImportMaxBytes>>30)
			}
			response, err := connection.Retr(remoteEntryPath)
			if err != nil {
				return fmt.Errorf("download source file %q: %w", remoteEntryPath, err)
			}
			file, createErr := os.OpenFile(localEntryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if createErr != nil {
				response.Close()
				return fmt.Errorf("create staged file %q: %w", name, createErr)
			}
			written, copyErr := io.Copy(file, io.LimitReader(response, websiteImportMaxBytes-result.Bytes+1))
			closeErr := errors.Join(file.Close(), response.Close())
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("download source file %q: %w", remoteEntryPath, errors.Join(copyErr, closeErr))
			}
			result.Files++
			result.Bytes += written
			if result.Bytes > websiteImportMaxBytes {
				return fmt.Errorf("source exceeds the import limit of %d GiB", websiteImportMaxBytes>>30)
			}
		}
	}

	return nil
}
