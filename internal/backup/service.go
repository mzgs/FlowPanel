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
	ErrNotFound    = errors.New("backup not found")
	ErrInvalidName = errors.New("invalid backup name")
)

type Manager interface {
	List(context.Context) ([]Record, error)
	Create(context.Context, CreateInput) (Record, error)
	Delete(context.Context, string) error
	DownloadPath(string) (string, string, error)
}

type Record struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	IncludePanelData bool `json:"include_panel_data"`
	IncludeSites     bool `json:"include_sites"`
	IncludeDatabases bool `json:"include_databases"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "validation failed"
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
}

type DatabaseSource interface {
	ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error)
	DumpDatabase(context.Context, string) ([]byte, error)
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
		sites, err = s.collectSites()
		if err != nil {
			return Record{}, err
		}
	}
	if input.IncludeDatabases {
		databaseDumps, err = s.collectDatabaseDumps(ctx)
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
		Format:    "flowpanel-backup-v1",
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

func (s *Service) collectSites() ([]siteArchive, error) {
	if s.domains == nil {
		return nil, nil
	}

	records := s.domains.List()
	sites := make([]siteArchive, 0, len(records))
	for _, record := range records {
		switch record.Kind {
		case domain.KindStaticSite, domain.KindPHP:
		default:
			continue
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

	return sites, nil
}

type databaseDump struct {
	Name    string
	Content []byte
}

func (s *Service) collectDatabaseDumps(ctx context.Context) ([]databaseDump, error) {
	if s.mariaDB == nil {
		return nil, nil
	}

	records, err := s.mariaDB.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mariadb databases for backup: %w", err)
	}

	dumps := make([]databaseDump, 0, len(records))
	for _, record := range records {
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

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
