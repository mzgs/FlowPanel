package httpx

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/domain"

	"github.com/klauspost/compress/zstd"
)

const pleskBackupPollInterval = 2 * time.Second

type pleskBackupPacket struct {
	BackupManager struct {
		BackupWebspace pleskBackupOperation `xml:"backup-webspace"`
		GetTasksInfo   pleskBackupOperation `xml:"get-tasks-info"`
		RemoveFile     pleskBackupOperation `xml:"remove-file"`
	} `xml:"backup-manager"`
}

type pleskBackupOperation struct {
	Result struct {
		Status string `xml:"status"`
		Error  string `xml:"errtext"`
		TaskID string `xml:"task-id"`
		Task   struct {
			ID       string `xml:"id"`
			Status   string `xml:"status"`
			Filename string `xml:"filename"`
		} `xml:"task"`
	} `xml:"result"`
}

func importPleskBackup(ctx context.Context, record domain.Record, basePath string, input websiteImportInput) (websiteImportResult, []byte, error) {
	importCtx, cancel := context.WithTimeout(ctx, websiteImportTimeout)
	defer cancel()
	client := newPanelHTTPClient(*input.Panel, 0)
	prefix := "flowpanel-import-" + strconv.FormatInt(time.Now().Unix(), 10)
	taskID, err := createPleskBackup(importCtx, client, *input.Panel, input.SubscriptionID, prefix)
	if err != nil {
		return websiteImportResult{}, nil, err
	}
	filename, err := waitForPleskBackup(importCtx, client, *input.Panel, taskID)
	if err != nil {
		return websiteImportResult{}, nil, err
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = removePleskBackup(cleanupCtx, client, *input.Panel, input.SubscriptionID, filename)
	}()

	workPath, err := os.MkdirTemp("", "flowpanel-plesk-backup-")
	if err != nil {
		return websiteImportResult{}, nil, fmt.Errorf("create Plesk backup staging directory: %w", err)
	}
	defer os.RemoveAll(workPath)
	archivePath := filepath.Join(workPath, "plesk-backup")
	if err := downloadPleskBackup(importCtx, client, *input.Panel, input.SubscriptionID, filename, archivePath); err != nil {
		return websiteImportResult{}, nil, err
	}
	repositoryPath := filepath.Join(workPath, "repository")
	repositoryStats := websiteImportResult{}
	if err := extractPleskArchive(archivePath, repositoryPath, &repositoryStats); err != nil {
		return websiteImportResult{}, nil, fmt.Errorf("extract Plesk backup: %w", err)
	}

	siteArchive, err := findPleskSiteArchive(repositoryPath, input.SiteHostname, strings.HasPrefix(input.SiteID, "site:"))
	if err != nil {
		return websiteImportResult{}, nil, err
	}
	targetPath, err := resolveDomainDocumentRoot(record, basePath)
	if err != nil {
		return websiteImportResult{}, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return websiteImportResult{}, nil, fmt.Errorf("ensure document root parent: %w", err)
	}
	stagePath, err := os.MkdirTemp(filepath.Dir(targetPath), ".flowpanel-import-")
	if err != nil {
		return websiteImportResult{}, nil, fmt.Errorf("create import staging directory: %w", err)
	}
	defer os.RemoveAll(stagePath)
	result := websiteImportResult{}
	if err := extractPleskArchive(siteArchive, stagePath, &result); err != nil {
		return websiteImportResult{}, nil, fmt.Errorf("extract website content from Plesk backup: %w", err)
	}
	if result.Files == 0 {
		return websiteImportResult{}, nil, errors.New("the Plesk backup does not contain website files")
	}

	var dump []byte
	if input.Database != nil {
		dump, err = extractPleskDatabaseDump(repositoryPath, input.Database.SourceName, workPath)
		if err != nil {
			return websiteImportResult{}, nil, err
		}
	}
	if err := publishImportedWebsite(stagePath, targetPath, input.ReplaceTargetFiles); err != nil {
		return websiteImportResult{}, nil, err
	}
	return result, dump, nil
}

func newPanelHTTPClient(input panelConnectionInput, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: panelTLSConfig(input.VerifyTLS)},
	}
}

func panelTLSConfig(verify bool) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verify}
}

func callPleskXML(ctx context.Context, client *http.Client, input panelConnectionInput, body string) ([]byte, error) {
	req, err := newPleskRequest(ctx, input, body)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to Plesk API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Plesk API returned %s", response.Status)
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read Plesk API response: %w", err)
	}
	return bodyBytes, nil
}

func createPleskBackup(ctx context.Context, client *http.Client, input panelConnectionInput, siteID, prefix string) (string, error) {
	request := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><packet version="1.6.9.1"><backup-manager><backup-webspace><webspace-id>%s</webspace-id><local/><prefix>%s</prefix><description>Temporary FlowPanel website import</description><split-size>0</split-size><only-hosting/></backup-webspace></backup-manager></packet>`, xmlEscape(pleskSubscriptionID(siteID)), xmlEscape(prefix))
	body, err := callPleskXML(ctx, client, input, request)
	if err != nil {
		return "", err
	}
	var packet pleskBackupPacket
	if err := xml.Unmarshal(body, &packet); err != nil {
		return "", fmt.Errorf("decode Plesk backup response: %w", err)
	}
	result := packet.BackupManager.BackupWebspace.Result
	if result.Status != "ok" || result.TaskID == "" {
		return "", fmt.Errorf("Plesk could not create a backup: %s", firstNonEmptyString(strings.TrimSpace(result.Error), "backup access is unavailable for this account"))
	}
	return result.TaskID, nil
}

func waitForPleskBackup(ctx context.Context, client *http.Client, input panelConnectionInput, taskID string) (string, error) {
	for {
		request := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><packet version="1.6.9.1"><backup-manager><get-tasks-info><task-id>%s</task-id></get-tasks-info></backup-manager></packet>`, xmlEscape(taskID))
		body, err := callPleskXML(ctx, client, input, request)
		if err != nil {
			return "", err
		}
		var packet pleskBackupPacket
		if err := xml.Unmarshal(body, &packet); err != nil {
			return "", fmt.Errorf("decode Plesk backup status: %w", err)
		}
		result := packet.BackupManager.GetTasksInfo.Result
		if result.Status != "ok" {
			return "", fmt.Errorf("Plesk backup status failed: %s", firstNonEmptyString(strings.TrimSpace(result.Error), "unknown error"))
		}
		switch result.Task.Status {
		case "finished":
			if strings.TrimSpace(result.Task.Filename) == "" {
				return "", errors.New("Plesk finished the backup without returning a filename")
			}
			return strings.TrimSpace(result.Task.Filename), nil
		case "failed", "stopped":
			return "", fmt.Errorf("Plesk backup task %s", result.Task.Status)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("Plesk backup did not finish: %w", ctx.Err())
		case <-time.After(pleskBackupPollInterval):
		}
	}
}

func downloadPleskBackup(ctx context.Context, client *http.Client, input panelConnectionInput, siteID, filename, targetPath string) error {
	request := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><packet version="1.6.9.1"><backup-manager><download-file><webspace-id>%s</webspace-id><filename>%s</filename></download-file></backup-manager></packet>`, xmlEscape(pleskSubscriptionID(siteID)), xmlEscape(filename))
	req, err := newPleskRequest(ctx, input, request)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download Plesk backup: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download Plesk backup: Plesk returned %s", response.Status)
	}
	reader := bufio.NewReader(response.Body)
	if prefix, _ := reader.Peek(64); strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "xml") || strings.HasPrefix(strings.TrimSpace(string(prefix)), "<") {
		body, _ := io.ReadAll(io.LimitReader(reader, 1<<20))
		return fmt.Errorf("download Plesk backup: %s", firstNonEmptyString(pleskError(body), strings.TrimSpace(string(body))))
	}
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, websiteImportMaxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return fmt.Errorf("save Plesk backup: %w", errors.Join(copyErr, closeErr))
	}
	if written > websiteImportMaxBytes {
		return fmt.Errorf("Plesk backup exceeds the %d GiB import limit", websiteImportMaxBytes>>30)
	}
	return nil
}

func newPleskRequest(ctx context.Context, input panelConnectionInput, body string) (*http.Request, error) {
	endpoint := "https://" + net.JoinHostPort(input.Host, strconv.Itoa(input.Port)) + "/enterprise/control/agent.php"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml")
	if input.AuthType == "token" {
		req.Header.Set("KEY", input.Secret)
	} else {
		req.Header.Set("HTTP_AUTH_LOGIN", input.Username)
		req.Header.Set("HTTP_AUTH_PASSWD", input.Secret)
	}
	return req, nil
}

func pleskError(body []byte) string {
	var payload struct {
		Error string `xml:"backup-manager>download-file>result>errtext"`
	}
	_ = xml.Unmarshal(body, &payload)
	return strings.TrimSpace(payload.Error)
}

func removePleskBackup(ctx context.Context, client *http.Client, input panelConnectionInput, siteID, filename string) error {
	request := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><packet version="1.6.9.1"><backup-manager><remove-file><webspace-id>%s</webspace-id><filename>%s</filename></remove-file></backup-manager></packet>`, xmlEscape(pleskSubscriptionID(siteID)), xmlEscape(filename))
	_, err := callPleskXML(ctx, client, input, request)
	return err
}

func pleskSubscriptionID(siteID string) string {
	return strings.TrimPrefix(strings.TrimPrefix(siteID, "webspace:"), "site:")
}

func xmlEscape(value string) string {
	var output strings.Builder
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func findPleskSiteArchive(root, hostname string, addon bool) (string, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	var exact, rootLevel []string
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !isPleskContentArchive(entry.Name()) {
			return err
		}
		relative, relErr := filepath.Rel(root, current)
		if relErr != nil {
			return relErr
		}
		parts := strings.Split(strings.ToLower(filepath.ToSlash(relative)), "/")
		matched := false
		for _, part := range parts[:len(parts)-1] {
			if part == hostname || strings.HasPrefix(part, hostname+"_") {
				matched = true
				break
			}
		}
		if matched {
			exact = append(exact, current)
		} else if !strings.Contains(strings.ToLower(filepath.ToSlash(relative)), "/sites/") {
			rootLevel = append(rootLevel, current)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("inspect Plesk backup: %w", err)
	}
	candidates := exact
	if !addon && len(candidates) == 0 {
		candidates = rootLevel
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("Plesk backup does not contain website content for %s", hostname)
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1], nil
}

func isPleskContentArchive(name string) bool {
	name = strings.ToLower(name)
	return strings.HasPrefix(name, "backup_user-data") && supportedPleskArchive(name)
}

func extractPleskDatabaseDump(root, databaseName, workPath string) ([]byte, error) {
	var candidates []string
	databaseName = strings.ToLower(strings.TrimSpace(databaseName))
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !supportedPleskArchive(entry.Name()) {
			return err
		}
		parts := strings.Split(strings.ToLower(filepath.ToSlash(current)), "/")
		for index, part := range parts {
			if part == "databases" && index+1 < len(parts) && (parts[index+1] == databaseName || strings.HasPrefix(parts[index+1], databaseName+"_")) {
				candidates = append(candidates, current)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("inspect Plesk database backup: %w", err)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("Plesk backup does not contain database %s", databaseName)
	}
	sort.Strings(candidates)
	dumpPath := filepath.Join(workPath, "database-dump")
	dumpStats := websiteImportResult{}
	if err := extractPleskArchive(candidates[len(candidates)-1], dumpPath, &dumpStats); err != nil {
		return nil, fmt.Errorf("extract Plesk database backup: %w", err)
	}
	var selected string
	var selectedSize int64
	err = filepath.WalkDir(dumpPath, func(current string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Size() > selectedSize && !strings.HasSuffix(strings.ToLower(entry.Name()), ".xml") {
			selected, selectedSize = current, info.Size()
		}
		return nil
	})
	if err != nil || selected == "" {
		return nil, errors.New("Plesk database backup did not contain a database dump")
	}
	if selectedSize > websiteImportMaxDatabaseBytes {
		return nil, fmt.Errorf("database export exceeds the %d GiB import limit", websiteImportMaxDatabaseBytes>>30)
	}
	return os.ReadFile(selected)
}

func supportedPleskArchive(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".tar") || strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tzst") || strings.HasSuffix(name, ".tar.zst")
}

func extractPleskArchive(archivePath, destination string, result *websiteImportResult) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	magic := make([]byte, 4)
	_, _ = io.ReadFull(file, magic)
	_, _ = file.Seek(0, io.SeekStart)
	if string(magic[:2]) == "PK" {
		info, statErr := file.Stat()
		if statErr != nil {
			return statErr
		}
		reader, zipErr := zip.NewReader(file, info.Size())
		if zipErr != nil {
			return zipErr
		}
		return extractPleskZip(reader, destination, result)
	}
	var stream io.Reader = file
	var closer io.Closer
	switch {
	case magic[0] == 0x1f && magic[1] == 0x8b:
		gzipReader, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			return gzipErr
		}
		stream, closer = gzipReader, gzipReader
	case magic[0] == 0x28 && magic[1] == 0xb5 && magic[2] == 0x2f && magic[3] == 0xfd:
		zstdReader, zstdErr := zstd.NewReader(file)
		if zstdErr != nil {
			return zstdErr
		}
		stream, closer = zstdReader, zstdReader.IOReadCloser()
	}
	if closer != nil {
		defer closer.Close()
	}
	return extractPleskTar(tar.NewReader(stream), destination, result)
}

func extractPleskTar(reader *tar.Reader, destination string, result *websiteImportResult) error {
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		target, err := safePleskArchiveTarget(destination, header.Name)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := writePleskArchiveFile(target, reader, header.FileInfo().Mode().Perm(), header.Size, result); err != nil {
			return err
		}
	}
}

func extractPleskZip(reader *zip.Reader, destination string, result *websiteImportResult) error {
	for _, entry := range reader.File {
		target, err := safePleskArchiveTarget(destination, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		err = writePleskArchiveFile(target, source, entry.Mode().Perm(), int64(entry.UncompressedSize64), result)
		closeErr := source.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	return nil
}

func safePleskArchiveTarget(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(name, "\\", "/")))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Plesk backup contains an unsafe path %q", name)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Plesk backup contains an unsafe path %q", name)
	}
	return target, nil
}

func writePleskArchiveFile(target string, source io.Reader, mode os.FileMode, size int64, result *websiteImportResult) error {
	if result != nil && (result.Files >= websiteImportMaxFiles || size > websiteImportMaxBytes-result.Bytes) {
		return fmt.Errorf("source exceeds the import limit of %d files or %d GiB", websiteImportMaxFiles, websiteImportMaxBytes>>30)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0o644
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(source, websiteImportMaxBytes+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	if result != nil {
		result.Files++
		result.Bytes += written
		if result.Bytes > websiteImportMaxBytes {
			return fmt.Errorf("source exceeds the import limit of %d GiB", websiteImportMaxBytes>>30)
		}
	}
	return nil
}
