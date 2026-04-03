package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/mariadb"

	"go.uber.org/zap"
)

const backupExtension = ".tar.gz"

var (
	ErrNotFound       = errors.New("backup not found")
	ErrInvalidName    = errors.New("invalid backup name")
	ErrAlreadyExists  = errors.New("backup already exists")
	ErrInvalidArchive = errors.New("invalid backup archive")
)

const backupFormat = "flowpanel-backup-v1"

type Manager interface {
	List(context.Context) ([]Record, error)
	Create(context.Context, CreateInput) (Record, error)
	Import(context.Context, string, io.Reader) (Record, error)
	Restore(context.Context, string) (RestoreResult, error)
	Delete(context.Context, string) error
	DownloadPath(string) (string, string, error)
}

type Record struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	IncludePanelData bool     `json:"include_panel_data"`
	IncludeSites     bool     `json:"include_sites"`
	IncludeDatabases bool     `json:"include_databases"`
	SiteHostnames    []string `json:"site_hostnames,omitempty"`
	DatabaseNames    []string `json:"database_names,omitempty"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "validation failed"
}

type RestoreResult struct {
	RestoredPanelFiles    bool     `json:"restored_panel_files"`
	RestoredPanelDatabase bool     `json:"restored_panel_database"`
	RestoredSites         []string `json:"restored_sites,omitempty"`
	RestoredDatabases     []string `json:"restored_databases,omitempty"`
}

type Service struct {
	logger       *zap.Logger
	dataPath     string
	backupPath   string
	databasePath string
	db           *sql.DB
	domains      DomainSource
	mariaDB      DatabaseSource
}

type manifest struct {
	Format    string    `json:"format"`
	CreatedAt time.Time `json:"created_at"`
	Contents  []string  `json:"contents"`
	Sites     []string  `json:"sites,omitempty"`
	Databases []string  `json:"databases,omitempty"`
}

type DomainSource interface {
	List() []domain.Record
	BasePath() string
}

type DatabaseSource interface {
	ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error)
	DumpDatabase(context.Context, string) ([]byte, error)
	RestoreDatabase(context.Context, string, []byte) error
}

type siteArchive struct {
	Hostname string
	RootPath string
}

func NewService(logger *zap.Logger, dataPath, backupPath, databasePath string, db *sql.DB, domains DomainSource, mariaDB DatabaseSource) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	dataPath = filepath.Clean(strings.TrimSpace(dataPath))
	if dataPath == "." {
		dataPath = ""
	}
	backupPath = filepath.Clean(strings.TrimSpace(backupPath))
	if backupPath == "." {
		backupPath = ""
	}

	return &Service{
		logger:       logger,
		dataPath:     dataPath,
		backupPath:   backupPath,
		databasePath: filepath.Clean(strings.TrimSpace(databasePath)),
		db:           db,
		domains:      domains,
		mariaDB:      mariaDB,
	}
}

func (s *Service) List(context.Context) ([]Record, error) {
	if err := s.ensureBackupPath(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.backupPath)
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}

	backups := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), backupExtension) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat backup %q: %w", entry.Name(), err)
		}

		backups = append(backups, Record{
			Name:      entry.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().UTC(),
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		if backups[i].CreatedAt.Equal(backups[j].CreatedAt) {
			return backups[i].Name > backups[j].Name
		}
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Record, error) {
	if err := s.ensureBackupPath(); err != nil {
		return Record{}, err
	}
	input.SiteHostnames = normalizeSiteHostnames(input.SiteHostnames)
	input.DatabaseNames = normalizeDatabaseNames(input.DatabaseNames)
	if validation := validateCreateInput(input); len(validation) > 0 {
		return Record{}, validation
	}

	createdAt := time.Now().UTC()
	name := fmt.Sprintf("%s-%s%s", backupNamePrefix(input), createdAt.Format("20060102-150405-000000000"), backupExtension)
	targetPath := filepath.Join(s.backupPath, name)
	tempTargetPath := targetPath + ".tmp"

	stagingPath, err := os.MkdirTemp("", "flowpanel-backup-*")
	if err != nil {
		return Record{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	var (
		snapshotPath    string
		snapshotRelPath string
		sites           []siteArchive
		databaseDumps   []databaseDump
	)
	if input.IncludePanelData {
		snapshotPath, snapshotRelPath, err = s.createDatabaseSnapshot(ctx, stagingPath)
		if err != nil {
			return Record{}, err
		}
	}
	if input.IncludeSites {
		sites, err = s.collectSites(input.SiteHostnames)
		if err != nil {
			return Record{}, err
		}
	}
	if input.IncludeDatabases {
		databaseDumps, err = s.collectDatabaseDumps(ctx, input.DatabaseNames)
		if err != nil {
			return Record{}, err
		}
	}

	file, err := os.OpenFile(tempTargetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return Record{}, fmt.Errorf("create backup archive: %w", err)
	}

	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(tempTargetPath)
		}
	}()

	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	contents := make([]string, 0, 5)
	if input.IncludePanelData {
		contents = append(contents,
			"flowpanel data directory",
			"sqlite database snapshot",
			"panel-managed runtime secrets",
		)
	}
	if len(sites) > 0 {
		contents = append(contents, "site roots for static and php domains")
	}
	if len(databaseDumps) > 0 {
		contents = append(contents, "sql dumps for managed mariadb databases")
	}

	manifestPayload, err := json.MarshalIndent(manifest{
		Format:    backupFormat,
		CreatedAt: createdAt,
		Contents:  contents,
		Sites:     siteHostnames(sites),
		Databases: databaseDumpNames(databaseDumps),
	}, "", "  ")
	if err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return Record{}, fmt.Errorf("encode backup manifest: %w", err)
	}

	if err := writeTarBytes(tarWriter, "manifest.json", manifestPayload, createdAt); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return Record{}, err
	}

	if input.IncludePanelData {
		if err := s.writeDataArchive(tarWriter, snapshotPath, snapshotRelPath); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return Record{}, err
		}
	}
	if err := writeSiteArchives(tarWriter, sites); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return Record{}, err
	}
	if err := writeDatabaseDumps(tarWriter, databaseDumps, createdAt); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()
		return Record{}, err
	}

	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return Record{}, fmt.Errorf("close backup tar stream: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return Record{}, fmt.Errorf("close backup gzip stream: %w", err)
	}
	if err := file.Close(); err != nil {
		return Record{}, fmt.Errorf("close backup archive: %w", err)
	}
	if err := os.Rename(tempTargetPath, targetPath); err != nil {
		return Record{}, fmt.Errorf("finalize backup archive: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return Record{}, fmt.Errorf("stat backup archive: %w", err)
	}

	success = true
	s.logger.Info("created backup archive",
		zap.String("path", targetPath),
		zap.Int64("size", info.Size()),
	)

	return Record{
		Name:      name,
		Size:      info.Size(),
		CreatedAt: info.ModTime().UTC(),
	}, nil
}

func (s *Service) Import(_ context.Context, name string, archive io.Reader) (Record, error) {
	if archive == nil {
		return Record{}, ErrInvalidArchive
	}

	targetPath, err := s.resolveBackupPath(name)
	if err != nil {
		return Record{}, err
	}

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Record{}, ErrAlreadyExists
		}
		return Record{}, fmt.Errorf("create imported backup %q: %w", name, err)
	}

	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(targetPath)
		}
	}()

	written, err := io.Copy(file, archive)
	if err != nil {
		return Record{}, fmt.Errorf("write imported backup %q: %w", name, err)
	}
	if written == 0 {
		return Record{}, ErrInvalidArchive
	}
	if err := file.Close(); err != nil {
		return Record{}, fmt.Errorf("close imported backup %q: %w", name, err)
	}
	if err := validateImportedArchive(targetPath); err != nil {
		return Record{}, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return Record{}, fmt.Errorf("stat imported backup %q: %w", name, err)
	}

	success = true
	s.logger.Info("imported backup archive",
		zap.String("path", targetPath),
		zap.Int64("size", info.Size()),
	)

	return Record{
		Name:      filepath.Base(targetPath),
		Size:      info.Size(),
		CreatedAt: info.ModTime().UTC(),
	}, nil
}

func (s *Service) Restore(ctx context.Context, name string) (RestoreResult, error) {
	backupPath, err := s.resolveBackupPath(name)
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err := os.Stat(backupPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return RestoreResult{}, ErrNotFound
		}
		return RestoreResult{}, fmt.Errorf("stat backup %q: %w", name, err)
	}

	stagingPath, err := os.MkdirTemp("", "flowpanel-restore-*")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	if err := extractBackupArchive(backupPath, stagingPath); err != nil {
		return RestoreResult{}, err
	}

	result := RestoreResult{}
	snapshotRelPath, _ := archiveRelativePath(s.dataPath, s.databasePath)
	snapshotStagingPath := ""
	if snapshotRelPath != "" {
		candidate := filepath.Join(stagingPath, filepath.FromSlash(snapshotRelPath))
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			snapshotStagingPath = candidate
		}
	}

	if hasPanelEntries(stagingPath, snapshotRelPath) {
		if err := s.restorePanelFiles(stagingPath, snapshotRelPath); err != nil {
			return RestoreResult{}, err
		}
		result.RestoredPanelFiles = true
	}

	if snapshotStagingPath != "" {
		if err := s.restoreSQLiteSnapshot(ctx, snapshotStagingPath); err != nil {
			return RestoreResult{}, err
		}
		result.RestoredPanelDatabase = true
	}

	restoredSites, err := s.restoreSiteArchives(stagingPath)
	if err != nil {
		return RestoreResult{}, err
	}
	result.RestoredSites = restoredSites

	restoredDatabases, err := s.restoreDatabaseDumps(ctx, stagingPath)
	if err != nil {
		return RestoreResult{}, err
	}
	result.RestoredDatabases = restoredDatabases

	return result, nil
}

func (s *Service) Delete(_ context.Context, name string) error {
	backupPath, err := s.resolveBackupPath(name)
	if err != nil {
		return err
	}

	if err := os.Remove(backupPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete backup %q: %w", name, err)
	}

	return nil
}

func (s *Service) DownloadPath(name string) (string, string, error) {
	backupPath, err := s.resolveBackupPath(name)
	if err != nil {
		return "", "", err
	}

	if _, err := os.Stat(backupPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", ErrNotFound
		}
		return "", "", fmt.Errorf("stat backup %q: %w", name, err)
	}

	return backupPath, name, nil
}

func (s *Service) ensureBackupPath() error {
	if strings.TrimSpace(s.backupPath) == "" {
		return fmt.Errorf("backup path is not configured")
	}
	if err := os.MkdirAll(s.backupPath, 0o755); err != nil {
		return fmt.Errorf("create backup directory %q: %w", s.backupPath, err)
	}

	return nil
}

func (s *Service) resolveBackupPath(name string) (string, error) {
	if err := s.ensureBackupPath(); err != nil {
		return "", err
	}

	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), backupExtension) {
		return "", ErrInvalidName
	}

	return filepath.Join(s.backupPath, name), nil
}

func (s *Service) createDatabaseSnapshot(ctx context.Context, stagingPath string) (string, string, error) {
	if s.db == nil || strings.TrimSpace(s.databasePath) == "" || s.databasePath == ":memory:" {
		return "", "", nil
	}
	if !filepath.IsAbs(s.databasePath) {
		return "", "", nil
	}

	relPath, ok := archiveRelativePath(s.dataPath, s.databasePath)
	if !ok {
		return "", "", nil
	}

	snapshotPath := filepath.Join(stagingPath, filepath.Base(relPath))
	statement := fmt.Sprintf("VACUUM INTO %s", sqliteStringLiteral(snapshotPath))
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		return "", "", fmt.Errorf("create sqlite backup snapshot: %w", err)
	}

	return snapshotPath, relPath, nil
}

func (s *Service) collectSites(hostnames []string) ([]siteArchive, error) {
	if s.domains == nil {
		return nil, nil
	}

	selected := make(map[string]struct{}, len(hostnames))
	for _, hostname := range hostnames {
		selected[hostname] = struct{}{}
	}

	records := s.domains.List()
	available := make(map[string]struct{}, len(records))
	sites := make([]siteArchive, 0, len(records))
	for _, record := range records {
		switch record.Kind {
		case domain.KindStaticSite, domain.KindPHP:
		default:
			continue
		}

		available[record.Hostname] = struct{}{}
		if len(selected) > 0 {
			if _, ok := selected[record.Hostname]; !ok {
				continue
			}
		}

		rootPath := strings.TrimSpace(record.Target)
		if rootPath == "" || !filepath.IsAbs(rootPath) {
			continue
		}

		info, err := os.Stat(rootPath)
		if err != nil {
			return nil, fmt.Errorf("stat site root for %q: %w", record.Hostname, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("site root for %q is not a directory", record.Hostname)
		}

		sites = append(sites, siteArchive{
			Hostname: record.Hostname,
			RootPath: rootPath,
		})
	}

	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Hostname < sites[j].Hostname
	})

	for _, hostname := range hostnames {
		if _, ok := available[hostname]; !ok {
			return nil, ValidationErrors{
				"site_hostnames": fmt.Sprintf("Site %q was not found.", hostname),
			}
		}
	}

	return sites, nil
}

type databaseDump struct {
	Name    string
	Content []byte
}

func (s *Service) collectDatabaseDumps(ctx context.Context, names []string) ([]databaseDump, error) {
	if s.mariaDB == nil {
		return nil, nil
	}

	records, err := s.mariaDB.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mariadb databases for backup: %w", err)
	}

	selected := make(map[string]struct{}, len(names))
	for _, name := range names {
		selected[name] = struct{}{}
	}
	if len(selected) > 0 {
		available := make(map[string]struct{}, len(records))
		for _, record := range records {
			available[record.Name] = struct{}{}
		}
		for _, name := range names {
			if _, ok := available[name]; !ok {
				return nil, ValidationErrors{
					"database_names": fmt.Sprintf("Database %q was not found.", name),
				}
			}
		}
	}

	dumps := make([]databaseDump, 0, len(records))
	for _, record := range records {
		if len(selected) > 0 {
			if _, ok := selected[record.Name]; !ok {
				continue
			}
		}

		content, err := s.mariaDB.DumpDatabase(ctx, record.Name)
		if err != nil {
			return nil, fmt.Errorf("dump mariadb database %q: %w", record.Name, err)
		}
		dumps = append(dumps, databaseDump{
			Name:    record.Name,
			Content: content,
		})
	}

	sort.Slice(dumps, func(i, j int) bool {
		return dumps[i].Name < dumps[j].Name
	})

	return dumps, nil
}

func (s *Service) writeDataArchive(tarWriter *tar.Writer, snapshotPath, snapshotRelPath string) error {
	if strings.TrimSpace(s.dataPath) == "" {
		return nil
	}

	return filepath.WalkDir(s.dataPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk backup source: %w", walkErr)
		}

		if samePath(currentPath, s.backupPath) {
			return filepath.SkipDir
		}

		archivePath, ok := archiveRelativePath(s.dataPath, currentPath)
		if !ok {
			return nil
		}

		info, err := os.Lstat(currentPath)
		if err != nil {
			return fmt.Errorf("stat backup source %q: %w", currentPath, err)
		}

		sourcePath := currentPath
		if snapshotPath != "" && samePath(currentPath, s.databasePath) {
			sourcePath = snapshotPath
			info, err = os.Lstat(sourcePath)
			if err != nil {
				return fmt.Errorf("stat backup snapshot %q: %w", sourcePath, err)
			}
			archivePath = snapshotRelPath
		}

		if archivePath == "" {
			return nil
		}

		return writeTarEntry(tarWriter, sourcePath, archivePath, info)
	})
}

func writeSiteArchives(tarWriter *tar.Writer, sites []siteArchive) error {
	for _, site := range sites {
		err := filepath.WalkDir(site.RootPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk site root for %q: %w", site.Hostname, walkErr)
			}

			info, err := os.Lstat(currentPath)
			if err != nil {
				return fmt.Errorf("stat site path %q: %w", currentPath, err)
			}

			if samePath(currentPath, site.RootPath) {
				return nil
			}

			relPath, err := filepath.Rel(site.RootPath, currentPath)
			if err != nil {
				return fmt.Errorf("resolve site path %q: %w", currentPath, err)
			}
			if relPath == "." || relPath == "" {
				return nil
			}

			archivePath := filepath.Join("sites", site.Hostname, relPath)
			return writeTarEntry(tarWriter, currentPath, archivePath, info)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func writeDatabaseDumps(tarWriter *tar.Writer, dumps []databaseDump, modTime time.Time) error {
	for _, dump := range dumps {
		if err := writeTarBytes(tarWriter, filepath.ToSlash(filepath.Join("databases", dump.Name+".sql")), dump.Content, modTime); err != nil {
			return fmt.Errorf("write database dump for %q: %w", dump.Name, err)
		}
	}

	return nil
}

func writeTarBytes(tarWriter *tar.Writer, archivePath string, payload []byte, modTime time.Time) error {
	header := &tar.Header{
		Name:     archivePath,
		Mode:     0o644,
		Size:     int64(len(payload)),
		ModTime:  modTime,
		Typeflag: tar.TypeReg,
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write backup manifest header: %w", err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		return fmt.Errorf("write backup manifest: %w", err)
	}

	return nil
}

func writeTarEntry(tarWriter *tar.Writer, sourcePath, archivePath string, info fs.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("build tar header for %q: %w", sourcePath, err)
	}

	header.Name = filepath.ToSlash(strings.TrimPrefix(archivePath, "./"))
	if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
		header.Name += "/"
	}

	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("read symlink %q: %w", sourcePath, err)
		}
		header.Linkname = linkTarget
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %q: %w", sourcePath, err)
	}

	if !info.Mode().IsRegular() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open backup source %q: %w", sourcePath, err)
	}
	defer file.Close()

	if _, err := io.Copy(tarWriter, file); err != nil {
		return fmt.Errorf("write backup source %q: %w", sourcePath, err)
	}

	return nil
}

func extractBackupArchive(archivePath, targetRoot string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open backup archive gzip stream: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read backup archive: %w", err)
		}

		relativePath, ok := sanitizeArchivePath(header.Name)
		if !ok {
			return fmt.Errorf("backup archive contains invalid entry %q", header.Name)
		}
		if relativePath == "" {
			continue
		}

		targetPath := filepath.Join(targetRoot, filepath.FromSlash(relativePath))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create restore directory %q: %w", relativePath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create restore parent directory %q: %w", relativePath, err)
			}
			fileMode := header.FileInfo().Mode().Perm()
			if fileMode == 0 {
				fileMode = 0o644
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
			if err != nil {
				return fmt.Errorf("create restore file %q: %w", relativePath, err)
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				_ = file.Close()
				return fmt.Errorf("write restore file %q: %w", relativePath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close restore file %q: %w", relativePath, err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create restore symlink parent %q: %w", relativePath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("create restore symlink %q: %w", relativePath, err)
			}
		default:
			return fmt.Errorf("backup archive entry %q uses unsupported type", header.Name)
		}
	}
}

func validateImportedArchive(archivePath string) error {
	stagingPath, err := os.MkdirTemp("", "flowpanel-import-validate-*")
	if err != nil {
		return fmt.Errorf("create backup validation staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	if err := extractBackupArchive(archivePath, stagingPath); err != nil {
		return ErrInvalidArchive
	}

	manifestPayload, err := os.ReadFile(filepath.Join(stagingPath, "manifest.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrInvalidArchive
		}
		return fmt.Errorf("read backup manifest: %w", err)
	}

	var snapshot manifest
	if err := json.Unmarshal(manifestPayload, &snapshot); err != nil {
		return ErrInvalidArchive
	}
	if snapshot.Format != backupFormat {
		return ErrInvalidArchive
	}

	return nil
}

func hasPanelEntries(stagingPath, snapshotRelPath string) bool {
	entries, err := os.ReadDir(stagingPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "manifest.json" || name == "sites" || name == "databases" {
			continue
		}
		if snapshotRelPath != "" && filepath.Clean(filepath.FromSlash(snapshotRelPath)) == name {
			continue
		}
		return true
	}

	return false
}

func (s *Service) restorePanelFiles(stagingPath, snapshotRelPath string) error {
	if strings.TrimSpace(s.dataPath) == "" {
		return fmt.Errorf("data path is not configured")
	}
	if err := os.MkdirAll(s.dataPath, 0o755); err != nil {
		return fmt.Errorf("create data path %q: %w", s.dataPath, err)
	}

	preservedPaths := map[string]struct{}{}
	if snapshotRelPath != "" {
		preservedPaths[filepath.Join(s.dataPath, filepath.FromSlash(snapshotRelPath))] = struct{}{}
	}
	if err := clearDirectoryContents(s.dataPath, preservedPaths); err != nil {
		return err
	}

	return filepath.WalkDir(stagingPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk restore staging: %w", walkErr)
		}
		if samePath(currentPath, stagingPath) {
			return nil
		}

		relativePath, err := filepath.Rel(stagingPath, currentPath)
		if err != nil {
			return fmt.Errorf("resolve restore path %q: %w", currentPath, err)
		}
		relativePath = filepath.ToSlash(relativePath)
		if relativePath == "manifest.json" || relativePath == "sites" || relativePath == "databases" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(relativePath, "sites/") || strings.HasPrefix(relativePath, "databases/") {
			if entry.IsDir() && (relativePath == "sites" || relativePath == "databases") {
				return filepath.SkipDir
			}
			return nil
		}
		if snapshotRelPath != "" && filepath.Clean(filepath.FromSlash(relativePath)) == filepath.Clean(filepath.FromSlash(snapshotRelPath)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(s.dataPath, filepath.FromSlash(relativePath))
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		return copyPath(currentPath, targetPath)
	})
}

func (s *Service) restoreSiteArchives(stagingPath string) ([]string, error) {
	sitesPath := filepath.Join(stagingPath, "sites")
	entries, err := os.ReadDir(sitesPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read restore sites directory: %w", err)
	}

	restored := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		hostname := strings.TrimSpace(entry.Name())
		if hostname == "" {
			continue
		}

		targetRoot, err := s.siteRootPath(hostname)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			return nil, fmt.Errorf("create site restore directory %q: %w", targetRoot, err)
		}
		if err := clearDirectoryContents(targetRoot, nil); err != nil {
			return nil, err
		}

		sourceRoot := filepath.Join(sitesPath, hostname)
		if err := copyTreeContents(sourceRoot, targetRoot); err != nil {
			return nil, err
		}
		restored = append(restored, hostname)
	}

	sort.Strings(restored)
	return restored, nil
}

func (s *Service) restoreDatabaseDumps(ctx context.Context, stagingPath string) ([]string, error) {
	databasesPath := filepath.Join(stagingPath, "databases")
	entries, err := os.ReadDir(databasesPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read restore databases directory: %w", err)
	}
	if s.mariaDB == nil {
		return nil, fmt.Errorf("mariadb is not configured")
	}

	restored := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
			continue
		}

		databaseName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		dump, err := os.ReadFile(filepath.Join(databasesPath, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read restore database dump %q: %w", entry.Name(), err)
		}
		if err := s.mariaDB.RestoreDatabase(ctx, databaseName, dump); err != nil {
			return nil, fmt.Errorf("restore mariadb database %q: %w", databaseName, err)
		}
		restored = append(restored, databaseName)
	}

	sort.Strings(restored)
	return restored, nil
}

func (s *Service) siteRootPath(hostname string) (string, error) {
	if s.domains != nil {
		for _, record := range s.domains.List() {
			if record.Hostname != hostname {
				continue
			}
			switch record.Kind {
			case domain.KindStaticSite, domain.KindPHP:
				if strings.TrimSpace(record.Target) != "" {
					return record.Target, nil
				}
			}
		}
	}

	basePath := ""
	if s.domains != nil {
		basePath = strings.TrimSpace(s.domains.BasePath())
	}
	if basePath == "" {
		return "", fmt.Errorf("site base path is not configured")
	}

	return filepath.Join(basePath, hostname), nil
}

func (s *Service) restoreSQLiteSnapshot(ctx context.Context, snapshotPath string) error {
	if s.db == nil {
		return fmt.Errorf("sqlite database is not configured")
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open sqlite restore connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("ATTACH DATABASE %s AS restore", sqliteStringLiteral(snapshotPath))); err != nil {
		return fmt.Errorf("attach restore database: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DETACH DATABASE restore")
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite restore transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable sqlite foreign keys: %w", err)
	}

	currentObjects, err := listSQLiteObjects(ctx, tx, "main")
	if err != nil {
		return err
	}
	restoreObjects, err := listSQLiteObjects(ctx, tx, "restore")
	if err != nil {
		return err
	}

	if err := dropSQLiteObjects(ctx, tx, currentObjects); err != nil {
		return err
	}
	if err := createSQLiteTables(ctx, tx, restoreObjects); err != nil {
		return err
	}
	if err := copySQLiteTableData(ctx, tx, restoreObjects); err != nil {
		return err
	}
	if err := restoreSQLiteSequence(ctx, tx); err != nil {
		return err
	}
	if err := createSQLiteNonTableObjects(ctx, tx, restoreObjects); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite restore: %w", err)
	}

	return nil
}

type sqliteObject struct {
	Type string
	Name string
	SQL  string
}

func listSQLiteObjects(ctx context.Context, tx *sql.Tx, schema string) ([]sqliteObject, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
SELECT type, name, sql
FROM %s.sqlite_master
WHERE name NOT LIKE 'sqlite_%%'
  AND type IN ('table', 'view', 'index', 'trigger')
  AND sql IS NOT NULL
ORDER BY
  CASE type
    WHEN 'table' THEN 0
    WHEN 'view' THEN 1
    WHEN 'index' THEN 2
    WHEN 'trigger' THEN 3
    ELSE 4
  END,
  name ASC
`, schema))
	if err != nil {
		return nil, fmt.Errorf("list sqlite objects from %s: %w", schema, err)
	}
	defer rows.Close()

	objects := make([]sqliteObject, 0)
	for rows.Next() {
		var object sqliteObject
		if err := rows.Scan(&object.Type, &object.Name, &object.SQL); err != nil {
			return nil, fmt.Errorf("scan sqlite object from %s: %w", schema, err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite objects from %s: %w", schema, err)
	}

	return objects, nil
}

func dropSQLiteObjects(ctx context.Context, tx *sql.Tx, objects []sqliteObject) error {
	for index := len(objects) - 1; index >= 0; index-- {
		object := objects[index]
		statement := fmt.Sprintf("DROP %s IF EXISTS %s", strings.ToUpper(object.Type), quoteSQLiteIdentifier(object.Name))
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("drop sqlite %s %q: %w", object.Type, object.Name, err)
		}
	}

	return nil
}

func createSQLiteTables(ctx context.Context, tx *sql.Tx, objects []sqliteObject) error {
	for _, object := range objects {
		if object.Type != "table" {
			continue
		}
		if _, err := tx.ExecContext(ctx, object.SQL); err != nil {
			return fmt.Errorf("create sqlite table %q: %w", object.Name, err)
		}
	}

	return nil
}

func copySQLiteTableData(ctx context.Context, tx *sql.Tx, objects []sqliteObject) error {
	for _, object := range objects {
		if object.Type != "table" {
			continue
		}
		statement := fmt.Sprintf(
			"INSERT INTO main.%s SELECT * FROM restore.%s",
			quoteSQLiteIdentifier(object.Name),
			quoteSQLiteIdentifier(object.Name),
		)
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("copy sqlite table %q: %w", object.Name, err)
		}
	}

	return nil
}

func restoreSQLiteSequence(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM restore.sqlite_master
WHERE type = 'table' AND name = 'sqlite_sequence'
`).Scan(&count); err != nil {
		return fmt.Errorf("query restore sqlite_sequence: %w", err)
	}
	if count == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM main.sqlite_sequence`); err != nil {
		return fmt.Errorf("clear sqlite_sequence: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO main.sqlite_sequence(name, seq)
SELECT name, seq
FROM restore.sqlite_sequence
`); err != nil {
		return fmt.Errorf("restore sqlite_sequence: %w", err)
	}

	return nil
}

func createSQLiteNonTableObjects(ctx context.Context, tx *sql.Tx, objects []sqliteObject) error {
	for _, object := range objects {
		if object.Type == "table" {
			continue
		}
		if _, err := tx.ExecContext(ctx, object.SQL); err != nil {
			return fmt.Errorf("create sqlite %s %q: %w", object.Type, object.Name, err)
		}
	}

	return nil
}

func clearDirectoryContents(root string, preserved map[string]struct{}) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read directory %q: %w", root, err)
	}

	for _, entry := range entries {
		currentPath := filepath.Join(root, entry.Name())
		if shouldPreservePath(currentPath, preserved) {
			if entry.IsDir() {
				if err := clearDirectoryContents(currentPath, preserved); err != nil {
					return err
				}
			}
			continue
		}

		if err := os.RemoveAll(currentPath); err != nil {
			return fmt.Errorf("remove %q: %w", currentPath, err)
		}
	}

	return nil
}

func shouldPreservePath(targetPath string, preserved map[string]struct{}) bool {
	if len(preserved) == 0 {
		return false
	}

	targetPath = filepath.Clean(targetPath)
	for preservedPath := range preserved {
		preservedPath = filepath.Clean(preservedPath)
		if targetPath == preservedPath {
			return true
		}
		if strings.HasPrefix(preservedPath, targetPath+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

func copyTreeContents(sourceRoot, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk restore source: %w", walkErr)
		}
		if samePath(currentPath, sourceRoot) {
			return nil
		}

		relativePath, err := filepath.Rel(sourceRoot, currentPath)
		if err != nil {
			return fmt.Errorf("resolve restore source path %q: %w", currentPath, err)
		}
		targetPath := filepath.Join(targetRoot, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		return copyPath(currentPath, targetPath)
	})
}

func copyPath(sourcePath, targetPath string) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat restore source %q: %w", sourcePath, err)
	}

	if info.IsDir() {
		return os.MkdirAll(targetPath, info.Mode().Perm())
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create restore symlink parent %q: %w", targetPath, err)
		}
		if err := os.RemoveAll(targetPath); err != nil {
			return fmt.Errorf("remove existing restore target %q: %w", targetPath, err)
		}
		linkTarget, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("read restore symlink %q: %w", sourcePath, err)
		}
		if err := os.Symlink(linkTarget, targetPath); err != nil {
			return fmt.Errorf("create restore symlink %q: %w", targetPath, err)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("create restore parent %q: %w", targetPath, err)
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open restore source %q: %w", sourcePath, err)
	}
	defer sourceFile.Close()

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("open restore target %q: %w", targetPath, err)
	}
	if _, err := io.Copy(file, sourceFile); err != nil {
		_ = file.Close()
		return fmt.Errorf("copy restore file %q: %w", targetPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close restore target %q: %w", targetPath, err)
	}

	return nil
}

func sanitizeArchivePath(value string) (string, bool) {
	value = filepath.ToSlash(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return "", false
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == "" {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return cleaned, true
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func archiveRelativePath(rootPath, targetPath string) (string, bool) {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	targetPath = filepath.Clean(strings.TrimSpace(targetPath))
	if rootPath == "" || targetPath == "" {
		return "", false
	}

	relPath, err := filepath.Rel(rootPath, targetPath)
	if err != nil || relPath == "." || relPath == "" {
		return "", false
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(relPath), true
}

func siteHostnames(sites []siteArchive) []string {
	hostnames := make([]string, 0, len(sites))
	for _, site := range sites {
		hostnames = append(hostnames, site.Hostname)
	}

	return hostnames
}

func databaseDumpNames(dumps []databaseDump) []string {
	names := make([]string, 0, len(dumps))
	for _, dump := range dumps {
		names = append(names, dump.Name)
	}

	return names
}

func validateCreateInput(input CreateInput) ValidationErrors {
	if len(input.SiteHostnames) > 0 && !input.IncludeSites {
		return ValidationErrors{
			"site_hostnames": "Select site files before choosing specific domains.",
		}
	}
	if len(input.DatabaseNames) > 0 && !input.IncludeDatabases {
		return ValidationErrors{
			"database_names": "Select database dumps before choosing specific databases.",
		}
	}
	if input.IncludePanelData || input.IncludeSites || input.IncludeDatabases {
		return nil
	}

	return ValidationErrors{
		"scope": "Select at least one backup source.",
	}
}

func backupNamePrefix(input CreateInput) string {
	if input.IncludePanelData && input.IncludeSites && input.IncludeDatabases {
		return "flowpanel-full-backup"
	}
	if !input.IncludePanelData && input.IncludeSites && !input.IncludeDatabases && len(input.SiteHostnames) == 1 {
		return "flowpanel-site-" + input.SiteHostnames[0] + "-backup"
	}
	if !input.IncludePanelData && !input.IncludeSites && input.IncludeDatabases && len(input.DatabaseNames) == 1 {
		return "flowpanel-database-" + input.DatabaseNames[0] + "-backup"
	}

	parts := make([]string, 0, 3)
	if input.IncludePanelData {
		parts = append(parts, "panel")
	}
	if input.IncludeSites {
		parts = append(parts, "sites")
	}
	if input.IncludeDatabases {
		parts = append(parts, "databases")
	}

	return "flowpanel-" + strings.Join(parts, "-") + "-backup"
}

func samePath(left, right string) bool {
	left = filepath.Clean(strings.TrimSpace(left))
	right = filepath.Clean(strings.TrimSpace(right))
	if left == "" || right == "" {
		return false
	}

	return left == right
}

func normalizeDatabaseNames(names []string) []string {
	return normalizeNames(names)
}

func normalizeSiteHostnames(hostnames []string) []string {
	return normalizeNames(hostnames)
}

func normalizeNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(names))
	normalized := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}

	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
