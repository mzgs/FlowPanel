package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
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

	"flowpanel/internal/dockercontainer"
	"flowpanel/internal/domain"
	"flowpanel/internal/googledrive"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/pm2"
	"flowpanel/internal/settings"

	"go.uber.org/zap"
)

const (
	backupExtension     = ".tar.gz"
	LocationLocal       = "local"
	LocationGoogleDrive = "google_drive"
	maxManifestSize     = 1 << 20
	adminTLSArchiveDir  = "admin_tls"
	adminTLSCertArchive = adminTLSArchiveDir + "/certificate.pem"
	adminTLSKeyArchive  = adminTLSArchiveDir + "/private.key"
)

var (
	ErrNotFound        = errors.New("backup not found")
	ErrInvalidName     = errors.New("invalid backup name")
	ErrAlreadyExists   = errors.New("backup already exists")
	ErrInvalidArchive  = errors.New("invalid backup archive")
	ErrInvalidLocation = errors.New("invalid backup location")
)

const backupFormat = "flowpanel-backup-v1"

type Manager interface {
	List(context.Context) ([]Record, error)
	Directory() string
	Create(context.Context, CreateInput) (Record, error)
	Import(context.Context, string, io.Reader) (Record, error)
	Restore(context.Context, string, string) (RestoreResult, error)
	Delete(context.Context, string, string) error
	OpenDownload(context.Context, string, string) (DownloadResult, error)
}

type Record struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Location  string    `json:"location"`
}

type CreateInput struct {
	IncludePanelData  bool     `json:"include_panel_data"`
	IncludeDockerData bool     `json:"include_docker_data"`
	IncludeSites      bool     `json:"include_sites"`
	IncludeDatabases  bool     `json:"include_databases"`
	SiteHostnames     []string `json:"site_hostnames,omitempty"`
	DatabaseNames     []string `json:"database_names,omitempty"`
	Location          string   `json:"location,omitempty"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "validation failed"
}

type RestoreResult struct {
	RestoredPanelFiles    bool     `json:"restored_panel_files"`
	RestoredPanelDatabase bool     `json:"restored_panel_database"`
	RestoredAdminTLS      bool     `json:"restored_admin_tls"`
	RestoredDockerData    bool     `json:"restored_docker_data"`
	RestoredContainers    []string `json:"restored_docker_containers,omitempty"`
	RestoredSites         []string `json:"restored_sites,omitempty"`
	RestoredDatabases     []string `json:"restored_databases,omitempty"`
	Warnings              []string `json:"warnings,omitempty"`
}

type RestoreProgress struct {
	Label   string `json:"label"`
	Percent int    `json:"percent"`
}

const (
	RequirementDocker  = "docker"
	RequirementGolang  = "golang"
	RequirementMariaDB = "mariadb"
	RequirementNodeJS  = "nodejs"
	RequirementPHP     = "php"
	RequirementPM2     = "pm2"
	RequirementPython  = "python"
)

type RestoreRequirement struct {
	Kind    string `json:"kind"`
	Version string `json:"version,omitempty"`
}

type RestorePreflight struct {
	Requirements []RestoreRequirement `json:"requirements,omitempty"`
	Warnings     []string             `json:"warnings,omitempty"`
}

type ProgressManager interface {
	RestoreWithProgress(context.Context, string, string, func(RestoreProgress)) (RestoreResult, error)
}

type PreflightManager interface {
	Preflight(context.Context, string, string) (RestorePreflight, error)
}

type DownloadResult struct {
	Name   string
	Size   int64
	Reader io.ReadCloser
}

type Service struct {
	logger        *zap.Logger
	dataPath      string
	backupPath    string
	databasePath  string
	adminCertPath string
	adminKeyPath  string
	db            *sql.DB
	store         *Store
	domains       DomainSource
	mariaDB       DatabaseSource
	settings      *settings.Service
	googleDrive   *googledrive.Service
	pm2           PM2Syncer
}

type manifest struct {
	Format       string               `json:"format"`
	CreatedAt    time.Time            `json:"created_at"`
	Contents     []string             `json:"contents"`
	Sites        []string             `json:"sites,omitempty"`
	Databases    []string             `json:"databases,omitempty"`
	Requirements []RestoreRequirement `json:"requirements,omitempty"`
}

type DomainSource interface {
	List() []domain.Record
	BasePath() string
}

type DatabaseSource interface {
	ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error)
	DumpDatabase(context.Context, string) ([]byte, error)
	RestoreDatabase(context.Context, string, io.Reader) error
	RestoreDatabaseAccess(context.Context, mariadb.DatabaseRecord) error
}

type PM2Syncer interface {
	Sync(context.Context) ([]pm2.Process, error)
}

type siteArchive struct {
	Hostname string
	RootPath string
}

func NewService(
	logger *zap.Logger,
	dataPath string,
	backupPath string,
	databasePath string,
	adminCertPath string,
	adminKeyPath string,
	db *sql.DB,
	domains DomainSource,
	mariaDB DatabaseSource,
	settingsService *settings.Service,
	googleDriveService *googledrive.Service,
	pm2Syncer PM2Syncer,
) *Service {
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
		logger:        logger,
		dataPath:      dataPath,
		backupPath:    backupPath,
		databasePath:  filepath.Clean(strings.TrimSpace(databasePath)),
		adminCertPath: strings.TrimSpace(adminCertPath),
		adminKeyPath:  strings.TrimSpace(adminKeyPath),
		db:            db,
		store:         NewStore(db),
		domains:       domains,
		mariaDB:       mariaDB,
		settings:      settingsService,
		googleDrive:   googleDriveService,
		pm2:           pm2Syncer,
	}
}

func (s *Service) Directory() string {
	return s.backupPath
}

func (s *Service) List(ctx context.Context) ([]Record, error) {
	localBackups, err := s.listLocalBackups()
	if err != nil {
		return nil, err
	}

	driveBackups, err := s.listPersistedGoogleDriveBackups(ctx)
	if err != nil {
		return nil, err
	}
	records := append(localBackups, driveBackups...)
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].Name > records[j].Name
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})

	return records, nil
}

func (s *Service) listPersistedGoogleDriveBackups(ctx context.Context) ([]Record, error) {
	if s.store == nil {
		return []Record{}, nil
	}

	records, err := s.store.ListGoogleDrive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list persisted google drive backups: %w", err)
	}

	return records, nil
}

func (s *Service) listLocalBackups() ([]Record, error) {
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

		backups = append(backups, localRecord(entry.Name(), info.Size(), info.ModTime().UTC()))
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
	input.Location = normalizeLocation(input.Location)
	input.SiteHostnames = normalizeSiteHostnames(input.SiteHostnames)
	input.DatabaseNames = normalizeDatabaseNames(input.DatabaseNames)
	if validation := validateCreateInput(input); len(validation) > 0 {
		return Record{}, validation
	}

	refreshToken := ""
	switch input.Location {
	case LocationLocal:
		if err := s.ensureBackupPath(); err != nil {
			return Record{}, err
		}
	case LocationGoogleDrive:
		token, ok, err := s.googleDriveRefreshToken(ctx)
		if err != nil {
			return Record{}, err
		}
		if !ok {
			return Record{}, fmt.Errorf("google drive is not connected")
		}
		refreshToken = token
	default:
		return Record{}, ErrInvalidLocation
	}

	if input.IncludePanelData && s.pm2 != nil {
		if _, err := s.pm2.Sync(ctx); err != nil {
			return Record{}, fmt.Errorf("sync pm2 processes: %w", err)
		}
	}

	if input.Location == LocationGoogleDrive {
		return s.createGoogleDriveBackup(ctx, input, refreshToken)
	}

	createdAt := time.Now().UTC()
	name := fmt.Sprintf("%s-%s%s", backupNamePrefix(input), createdAt.Format("20060102-150405"), backupExtension)
	targetPath := filepath.Join(s.backupPath, name)
	return s.createLocalArchive(ctx, input, name, targetPath, createdAt)
}

func (s *Service) createLocalArchive(ctx context.Context, input CreateInput, name string, targetPath string, createdAt time.Time) (Record, error) {
	tempTargetPath := targetPath + ".tmp"

	stagingPath, err := os.MkdirTemp("", "flowpanel-backup-*")
	if err != nil {
		return Record{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	var (
		snapshotPath     string
		snapshotRelPath  string
		adminCert        []byte
		adminKey         []byte
		includeAdminTLS  bool
		sites            []siteArchive
		databaseDumps    []databaseDump
		dockerContainers []dockercontainer.Record
	)
	if input.IncludePanelData {
		snapshotPath, snapshotRelPath, err = s.createDatabaseSnapshot(ctx, stagingPath)
		if err != nil {
			return Record{}, err
		}
		adminCert, adminKey, includeAdminTLS, err = s.readAdminTLSFiles()
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
	if input.IncludeDockerData {
		dockerContainers, err = dockercontainer.Snapshot(ctx)
		if err != nil {
			return Record{}, fmt.Errorf("snapshot Docker containers: %w", err)
		}
	}

	file, err := os.OpenFile(tempTargetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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

	contents := make([]string, 0, 6)
	if input.IncludePanelData {
		contents = append(contents,
			"flowpanel data directory",
			"sqlite database snapshot",
			"panel-managed runtime secrets",
		)
		if includeAdminTLS {
			contents = append(contents, "configured admin TLS certificate and private key")
		}
	}
	if input.IncludeDockerData {
		contents = append(contents, "flowpanel-managed docker volume data and container definitions, including environment variable values")
	}
	if len(sites) > 0 {
		contents = append(contents, "site roots for static, php, and node.js domains")
	}
	if len(databaseDumps) > 0 {
		contents = append(contents, "sql dumps for managed mariadb databases")
	}
	requirements := s.backupRequirements(ctx, input, sites)

	manifestPayload, err := json.MarshalIndent(manifest{
		Format:       backupFormat,
		CreatedAt:    createdAt,
		Contents:     contents,
		Sites:        siteHostnames(sites),
		Databases:    databaseDumpNames(databaseDumps),
		Requirements: requirements,
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
		if includeAdminTLS {
			if err := writeTarBytesMode(tarWriter, adminTLSCertArchive, adminCert, createdAt, 0o644); err != nil {
				_ = tarWriter.Close()
				_ = gzipWriter.Close()
				return Record{}, err
			}
			if err := writeTarBytesMode(tarWriter, adminTLSKeyArchive, adminKey, createdAt, 0o600); err != nil {
				_ = tarWriter.Close()
				_ = gzipWriter.Close()
				return Record{}, err
			}
		}
	}
	if input.IncludeDockerData {
		if err := s.writeDockerDataArchive(tarWriter); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return Record{}, err
		}
		if err := writeDockerContainerArchive(tarWriter, dockerContainers, createdAt); err != nil {
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

	return localRecord(name, info.Size(), info.ModTime().UTC()), nil
}

func (s *Service) createGoogleDriveBackup(ctx context.Context, input CreateInput, refreshToken string) (Record, error) {
	stagingPath, err := os.MkdirTemp("", "flowpanel-drive-backup-*")
	if err != nil {
		return Record{}, fmt.Errorf("create google drive backup staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	createdAt := time.Now().UTC()
	name := fmt.Sprintf("%s-%s%s", backupNamePrefix(input), createdAt.Format("20060102-150405"), backupExtension)
	targetPath := filepath.Join(stagingPath, name)
	if _, err := s.createLocalArchive(ctx, input, name, targetPath, createdAt); err != nil {
		return Record{}, err
	}

	archive, err := os.Open(targetPath)
	if err != nil {
		return Record{}, fmt.Errorf("open google drive backup staging archive: %w", err)
	}
	defer archive.Close()

	uploaded, err := s.googleDrive.UploadBackup(ctx, refreshToken, name, archive)
	if err != nil {
		return Record{}, err
	}

	record := Record{
		ID:        uploaded.ID,
		Name:      uploaded.Name,
		Size:      uploaded.Size,
		CreatedAt: uploaded.CreatedAt,
		Location:  LocationGoogleDrive,
	}
	if err := s.persistGoogleDriveBackup(ctx, record); err != nil {
		if cleanupErr := s.googleDrive.DeleteBackup(ctx, refreshToken, uploaded.ID); cleanupErr != nil {
			s.logger.Error("delete google drive backup after metadata persistence failure failed",
				zap.String("id", uploaded.ID),
				zap.Error(cleanupErr),
			)
		}
		return Record{}, err
	}

	return record, nil
}

func (s *Service) Import(_ context.Context, name string, archive io.Reader) (Record, error) {
	if archive == nil {
		return Record{}, ErrInvalidArchive
	}

	targetPath, err := s.resolveBackupPath(name)
	if err != nil {
		return Record{}, err
	}

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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

	return localRecord(filepath.Base(targetPath), info.Size(), info.ModTime()), nil
}

func (s *Service) Restore(ctx context.Context, id string, location string) (RestoreResult, error) {
	return s.RestoreWithProgress(ctx, id, location, nil)
}

func (s *Service) Preflight(ctx context.Context, id string, location string) (RestorePreflight, error) {
	download, err := s.OpenDownload(ctx, id, location)
	if err != nil {
		return RestorePreflight{}, err
	}
	defer download.Reader.Close()

	snapshot, err := readBackupManifest(download.Reader, false)
	if err != nil {
		return RestorePreflight{}, err
	}
	return RestorePreflight{Requirements: normalizeRestoreRequirements(snapshot.Requirements)}, nil
}

func (s *Service) RestoreWithProgress(ctx context.Context, id string, location string, report func(RestoreProgress)) (RestoreResult, error) {
	switch normalizeLocation(location) {
	case LocationLocal:
		return s.restoreLocalBackup(ctx, id, report)
	case LocationGoogleDrive:
		return s.restoreGoogleDriveBackup(ctx, id, report)
	default:
		return RestoreResult{}, ErrInvalidLocation
	}
}

func (s *Service) restoreLocalBackup(ctx context.Context, name string, report func(RestoreProgress)) (RestoreResult, error) {
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

	return s.restoreArchive(ctx, backupPath, report)
}

func (s *Service) restoreArchive(ctx context.Context, backupPath string, report func(RestoreProgress)) (RestoreResult, error) {
	reportRestoreProgress(report, "Preparing backup…", 5)
	stagingPath, err := os.MkdirTemp("", "flowpanel-restore-*")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create restore staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	reportRestoreProgress(report, "Extracting backup…", 15)
	if err := extractBackupArchive(backupPath, stagingPath); err != nil {
		return RestoreResult{}, err
	}

	result := RestoreResult{}
	recordFailure := func(scope, warning string, err error) {
		s.logger.Error("restore backup scope failed", zap.String("scope", scope), zap.Error(err))
		result.addWarning(fmt.Sprintf("%s: %v", warning, err))
	}
	snapshotRelPath := databaseArchivePath(s.dataPath, s.databasePath)
	snapshotStagingPath := ""
	if snapshotRelPath != "" {
		candidate := filepath.Join(stagingPath, filepath.FromSlash(snapshotRelPath))
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			snapshotStagingPath = candidate
		}
	}

	if hasPanelEntries(stagingPath, snapshotRelPath) {
		reportRestoreProgress(report, "Restoring panel files…", 30)
		if err := s.restorePanelFiles(stagingPath, snapshotRelPath); err != nil {
			recordFailure("panel_files", "Panel files may be only partially restored", err)
		} else {
			result.RestoredPanelFiles = true
		}
	}

	if restored, err := s.restoreAdminTLS(stagingPath); err != nil {
		recordFailure("admin_tls", "Admin TLS certificate and key were not restored", err)
	} else {
		result.RestoredAdminTLS = restored
	}

	if snapshotStagingPath != "" {
		reportRestoreProgress(report, "Restoring panel database…", 45)
		if err := s.restoreSQLiteSnapshot(ctx, snapshotStagingPath); err != nil {
			recordFailure("panel_data", "Panel data may be only partially restored", err)
		} else {
			result.RestoredPanelDatabase = true
		}
	}

	reportRestoreProgress(report, "Restoring site files…", 60)
	restoredSites, err := s.restoreSiteArchives(stagingPath)
	result.RestoredSites = restoredSites
	if err != nil {
		recordFailure("sites", "Sites may be only partially restored", err)
	}

	reportRestoreProgress(report, "Restoring databases…", 75)
	restoredDatabases, err := s.restoreDatabaseDumps(ctx, stagingPath)
	result.RestoredDatabases = restoredDatabases
	if err != nil {
		recordFailure("databases", "Databases may be only partially restored", err)
	}

	reportRestoreProgress(report, "Restoring Docker data…", 88)
	dockerContainers, err := readDockerContainerArchive(stagingPath)
	if err != nil {
		recordFailure("docker_containers", "Docker containers were not restored", err)
		return result, nil
	}
	if err := dockercontainer.Stop(ctx, dockerContainers); err != nil {
		recordFailure("docker_containers", "Docker containers were not restored", err)
		return result, nil
	}
	result.RestoredDockerData, err = s.restoreDockerData(stagingPath)
	if err != nil {
		recordFailure("docker_data", "Docker data and containers were not restored", err)
		return result, nil
	}
	result.RestoredContainers, err = dockercontainer.Restore(ctx, dockerContainers, s.dockerDataPath(), func(progress dockercontainer.RestoreProgress) {
		percent := 92
		if progress.Pulling {
			percent = 89
		}
		if progress.Total > 0 {
			percent += (progress.Current - 1) * 2 / progress.Total
		}
		label := fmt.Sprintf("Restoring Docker container %s (%d/%d)…", progress.Container, progress.Current, progress.Total)
		if progress.Pulling {
			label = fmt.Sprintf("Pulling Docker image %s (%d/%d)…", progress.Image, progress.Current, progress.Total)
		}
		reportRestoreProgress(report, label, percent)
	})
	if err != nil {
		recordFailure("docker_containers", "Some Docker containers were not restored", err)
	}

	return result, nil
}

func reportRestoreProgress(report func(RestoreProgress), label string, percent int) {
	if report != nil {
		report(RestoreProgress{Label: label, Percent: percent})
	}
}

func (s *Service) backupRequirements(ctx context.Context, input CreateInput, sites []siteArchive) []RestoreRequirement {
	requirements := make([]RestoreRequirement, 0, 6)
	if input.IncludeDockerData {
		requirements = append(requirements, RestoreRequirement{Kind: RequirementDocker})
	}
	if input.IncludeDatabases {
		requirements = append(requirements, RestoreRequirement{Kind: RequirementMariaDB})
	}

	includedSites := make(map[string]struct{}, len(sites))
	for _, site := range sites {
		includedSites[site.Hostname] = struct{}{}
	}
	defaultPHPVersion := ""
	if s.settings != nil {
		if record, err := s.settings.Get(ctx); err == nil {
			defaultPHPVersion = record.DefaultPHPVersion
		}
	}
	if s.domains == nil {
		return normalizeRestoreRequirements(requirements)
	}
	for _, record := range s.domains.List() {
		if !input.IncludePanelData {
			if _, ok := includedSites[record.Hostname]; !ok {
				continue
			}
		}
		switch record.Kind {
		case domain.KindPHP:
			version := strings.TrimSpace(record.PHPVersion)
			if version == "" {
				version = defaultPHPVersion
			}
			requirements = append(requirements, RestoreRequirement{Kind: RequirementPHP, Version: version})
		case domain.KindNodeJS:
			requirements = append(requirements,
				RestoreRequirement{Kind: RequirementNodeJS},
				RestoreRequirement{Kind: RequirementPM2},
			)
		case domain.KindPython:
			requirements = append(requirements,
				RestoreRequirement{Kind: RequirementNodeJS},
				RestoreRequirement{Kind: RequirementPM2},
				RestoreRequirement{Kind: RequirementPython},
			)
		case domain.KindApplication:
			requirements = append(requirements,
				RestoreRequirement{Kind: RequirementNodeJS},
				RestoreRequirement{Kind: RequirementPM2},
			)
			if strings.Contains(strings.ToLower(strings.Join(strings.Fields(record.AppBuildCommand), " ")), "go build") {
				requirements = append(requirements, RestoreRequirement{Kind: RequirementGolang})
			}
		}
	}
	return normalizeRestoreRequirements(requirements)
}

func normalizeRestoreRequirements(requirements []RestoreRequirement) []RestoreRequirement {
	seen := make(map[string]struct{}, len(requirements))
	result := make([]RestoreRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		requirement.Kind = strings.TrimSpace(strings.ToLower(requirement.Kind))
		requirement.Version = strings.TrimSpace(requirement.Version)
		if requirement.Kind == "" {
			continue
		}
		key := requirement.Kind + "\x00" + requirement.Version
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, requirement)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Version < result[j].Version
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func (r *RestoreResult) addWarning(message string) {
	if message = strings.TrimSpace(message); message != "" {
		r.Warnings = append(r.Warnings, message)
	}
}

func (s *Service) Delete(ctx context.Context, id string, location string) error {
	switch normalizeLocation(location) {
	case LocationLocal:
		return s.deleteLocalBackup(id)
	case LocationGoogleDrive:
		return s.deleteGoogleDriveBackup(ctx, id)
	default:
		return ErrInvalidLocation
	}
}

func (s *Service) deleteLocalBackup(name string) error {
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

func (s *Service) OpenDownload(ctx context.Context, id string, location string) (DownloadResult, error) {
	switch normalizeLocation(location) {
	case LocationLocal:
		return s.openLocalDownload(id)
	case LocationGoogleDrive:
		return s.openGoogleDriveDownload(ctx, id)
	default:
		return DownloadResult{}, ErrInvalidLocation
	}
}

func (s *Service) openLocalDownload(name string) (DownloadResult, error) {
	backupPath, err := s.resolveBackupPath(name)
	if err != nil {
		return DownloadResult{}, err
	}

	info, err := os.Stat(backupPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DownloadResult{}, ErrNotFound
		}
		return DownloadResult{}, fmt.Errorf("stat backup %q: %w", name, err)
	}

	file, err := os.Open(backupPath)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("open backup %q: %w", name, err)
	}

	return DownloadResult{
		Name:   name,
		Size:   info.Size(),
		Reader: file,
	}, nil
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

func (s *Service) restoreGoogleDriveBackup(ctx context.Context, id string, report func(RestoreProgress)) (RestoreResult, error) {
	reportRestoreProgress(report, "Downloading backup…", 3)
	download, err := s.openGoogleDriveDownload(ctx, id)
	if err != nil {
		return RestoreResult{}, err
	}
	defer download.Reader.Close()

	stagingPath, err := os.MkdirTemp("", "flowpanel-drive-restore-*")
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create google drive restore staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)

	name := strings.TrimSpace(download.Name)
	if name == "" || filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), backupExtension) {
		return RestoreResult{}, ErrInvalidName
	}
	targetPath := filepath.Join(stagingPath, name)
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("create google drive restore archive: %w", err)
	}
	if _, err := io.Copy(file, download.Reader); err != nil {
		_ = file.Close()
		return RestoreResult{}, fmt.Errorf("write google drive restore archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return RestoreResult{}, fmt.Errorf("close google drive restore archive: %w", err)
	}

	return s.restoreArchive(ctx, targetPath, report)
}

func (s *Service) deleteGoogleDriveBackup(ctx context.Context, id string) error {
	refreshToken, ok, err := s.googleDriveRefreshToken(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	if err := s.googleDrive.DeleteBackup(ctx, refreshToken, id); err != nil {
		if errors.Is(err, googledrive.ErrNotFound) {
			return s.deletePersistedGoogleDriveBackup(ctx, id)
		}
		return err
	}

	return s.deletePersistedGoogleDriveBackup(ctx, id)
}

func (s *Service) openGoogleDriveDownload(ctx context.Context, id string) (DownloadResult, error) {
	refreshToken, ok, err := s.googleDriveRefreshToken(ctx)
	if err != nil {
		return DownloadResult{}, err
	}
	if !ok {
		return DownloadResult{}, ErrNotFound
	}

	reader, file, err := s.googleDrive.DownloadBackup(ctx, refreshToken, id)
	if err != nil {
		if errors.Is(err, googledrive.ErrNotFound) {
			if purgeErr := s.deletePersistedGoogleDriveBackup(ctx, id); purgeErr != nil {
				s.logger.Error("delete stale google drive backup metadata failed",
					zap.String("id", id),
					zap.Error(purgeErr),
				)
			}
			return DownloadResult{}, ErrNotFound
		}
		return DownloadResult{}, err
	}

	return DownloadResult{
		Name:   file.Name,
		Size:   file.Size,
		Reader: reader,
	}, nil
}

func (s *Service) persistGoogleDriveBackup(ctx context.Context, record Record) error {
	if s.store == nil {
		return nil
	}

	if err := s.store.UpsertGoogleDrive(ctx, record); err != nil {
		return fmt.Errorf("persist google drive backup metadata: %w", err)
	}

	return nil
}

func (s *Service) deletePersistedGoogleDriveBackup(ctx context.Context, id string) error {
	if s.store == nil {
		return nil
	}

	if err := s.store.DeleteGoogleDrive(ctx, id); err != nil {
		return fmt.Errorf("delete google drive backup metadata: %w", err)
	}

	return nil
}

func (s *Service) googleDriveRefreshToken(ctx context.Context) (string, bool, error) {
	if s.googleDrive == nil || !s.googleDrive.Enabled() || s.settings == nil {
		return "", false, nil
	}

	record, err := s.settings.Get(ctx)
	if err != nil {
		return "", false, err
	}

	refreshToken := strings.TrimSpace(record.GoogleDriveRefreshToken)
	if refreshToken == "" {
		return "", false, nil
	}

	return refreshToken, true, nil
}

func localRecord(name string, size int64, createdAt time.Time) Record {
	return Record{
		ID:        name,
		Name:      name,
		Size:      size,
		CreatedAt: createdAt.UTC(),
		Location:  LocationLocal,
	}
}

func (s *Service) ensureBackupPath() error {
	if strings.TrimSpace(s.backupPath) == "" {
		return fmt.Errorf("backup path is not configured")
	}
	if err := os.MkdirAll(s.backupPath, 0o700); err != nil {
		return fmt.Errorf("create backup directory %q: %w", s.backupPath, err)
	}
	if err := os.Chmod(s.backupPath, 0o700); err != nil {
		return fmt.Errorf("secure backup directory %q: %w", s.backupPath, err)
	}
	entries, err := os.ReadDir(s.backupPath)
	if err != nil {
		return fmt.Errorf("read backup directory %q: %w", s.backupPath, err)
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(strings.ToLower(entry.Name()), backupExtension) {
			if err := os.Chmod(filepath.Join(s.backupPath, entry.Name()), 0o600); err != nil {
				return fmt.Errorf("secure backup file %q: %w", entry.Name(), err)
			}
		}
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

	relPath := databaseArchivePath(s.dataPath, s.databasePath)
	if relPath == "" {
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
		if !domain.SupportsManagedDocumentRoot(record.Kind) {
			continue
		}

		available[record.Hostname] = struct{}{}
		if len(selected) > 0 {
			if _, ok := selected[record.Hostname]; !ok {
				continue
			}
		}

		rootPath, err := domain.ResolveDocumentRoot(s.domains.BasePath(), record)
		if err != nil {
			return nil, fmt.Errorf("resolve site root for %q: %w", record.Hostname, err)
		}

		info, err := os.Stat(rootPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if err := os.MkdirAll(rootPath, 0o755); err != nil {
					return nil, fmt.Errorf("create site root for %q: %w", record.Hostname, err)
				}
				info, err = os.Stat(rootPath)
			}
			if err != nil {
				return nil, fmt.Errorf("stat site root for %q: %w", record.Hostname, err)
			}
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
	writeSnapshot := func() error {
		if snapshotPath == "" {
			return nil
		}
		info, err := os.Lstat(snapshotPath)
		if err != nil {
			return fmt.Errorf("stat backup snapshot %q: %w", snapshotPath, err)
		}
		return writeTarEntry(tarWriter, snapshotPath, snapshotRelPath, info)
	}
	if strings.TrimSpace(s.dataPath) == "" {
		return writeSnapshot()
	}

	snapshotWritten := false
	err := filepath.WalkDir(s.dataPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk backup source: %w", walkErr)
		}

		if samePath(currentPath, s.backupPath) {
			return filepath.SkipDir
		}
		if samePath(currentPath, s.dockerDataPath()) {
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
			snapshotWritten = true
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
	if err != nil || snapshotWritten {
		return err
	}
	return writeSnapshot()
}

func (s *Service) readAdminTLSFiles() ([]byte, []byte, bool, error) {
	if s.adminCertPath == "" && s.adminKeyPath == "" {
		return nil, nil, false, nil
	}
	if s.adminCertPath == "" || s.adminKeyPath == "" {
		return nil, nil, false, fmt.Errorf("admin TLS certificate and key paths must both be configured")
	}
	cert, err := os.ReadFile(s.adminCertPath)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read admin TLS certificate: %w", err)
	}
	key, err := os.ReadFile(s.adminKeyPath)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read admin TLS private key: %w", err)
	}
	return cert, key, true, nil
}

func (s *Service) writeDockerDataArchive(tarWriter *tar.Writer) error {
	root := s.dockerDataPath()
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat Docker data directory %q: %w", root, err)
	}

	return filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk Docker data directory: %w", walkErr)
		}
		relativePath, err := filepath.Rel(root, currentPath)
		if err != nil {
			return fmt.Errorf("resolve Docker data path %q: %w", currentPath, err)
		}
		archivePath := "docker_volumes"
		if relativePath != "." {
			archivePath = filepath.Join(archivePath, relativePath)
		}
		info, err := os.Lstat(currentPath)
		if err != nil {
			return fmt.Errorf("stat Docker data path %q: %w", currentPath, err)
		}
		return writeTarEntry(tarWriter, currentPath, archivePath, info)
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

func writeDockerContainerArchive(tarWriter *tar.Writer, records []dockercontainer.Record, modTime time.Time) error {
	payload, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Docker container definitions: %w", err)
	}
	return writeTarBytes(tarWriter, "docker/containers.json", payload, modTime)
}

func readDockerContainerArchive(stagingPath string) ([]dockercontainer.Record, error) {
	payload, err := os.ReadFile(filepath.Join(stagingPath, "docker", "containers.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Docker container definitions: %w", err)
	}
	var records []dockercontainer.Record
	if err := json.Unmarshal(payload, &records); err != nil {
		return nil, ErrInvalidArchive
	}
	return records, nil
}

func writeTarBytes(tarWriter *tar.Writer, archivePath string, payload []byte, modTime time.Time) error {
	return writeTarBytesMode(tarWriter, archivePath, payload, modTime, 0o644)
}

func writeTarBytesMode(tarWriter *tar.Writer, archivePath string, payload []byte, modTime time.Time, mode int64) error {
	header := &tar.Header{
		Name:     archivePath,
		Mode:     mode,
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
		if err := ensureArchiveParentsAreDirectories(targetRoot, relativePath); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if info, err := os.Lstat(targetPath); err == nil && !info.IsDir() {
				return fmt.Errorf("backup archive entry %q conflicts with an existing entry", header.Name)
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("inspect restore directory %q: %w", relativePath, err)
			}
			if err := os.MkdirAll(targetPath, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("create restore directory %q: %w", relativePath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if _, err := os.Lstat(targetPath); err == nil {
				return fmt.Errorf("backup archive contains duplicate entry %q", header.Name)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("inspect restore file %q: %w", relativePath, err)
			}
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("create restore parent directory %q: %w", relativePath, err)
			}
			fileMode := header.FileInfo().Mode().Perm()
			if fileMode == 0 {
				fileMode = 0o644
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
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
			if !validArchiveSymlink(relativePath, header.Linkname) {
				return fmt.Errorf("backup archive symlink %q escapes its restore scope", header.Name)
			}
			if _, err := os.Lstat(targetPath); err == nil {
				return fmt.Errorf("backup archive contains duplicate entry %q", header.Name)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("inspect restore symlink %q: %w", relativePath, err)
			}
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

func ensureArchiveParentsAreDirectories(targetRoot, relativePath string) error {
	current := targetRoot
	for _, name := range strings.Split(filepath.Dir(filepath.FromSlash(relativePath)), string(filepath.Separator)) {
		if name == "." || name == "" {
			continue
		}
		current = filepath.Join(current, name)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect restore parent %q: %w", relativePath, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup archive entry %q has an unsafe parent", relativePath)
		}
	}
	return nil
}

func validArchiveSymlink(relativePath, linkName string) bool {
	linkName = filepath.ToSlash(linkName)
	if linkName == "" || path.IsAbs(linkName) {
		return false
	}
	parts := strings.Split(relativePath, "/")
	scope := "."
	if len(parts) >= 2 && parts[0] == "sites" {
		scope = path.Join(parts[0], parts[1])
	} else if len(parts) > 0 && parts[0] == "docker_volumes" {
		scope = parts[0]
	}
	resolved := path.Clean(path.Join(path.Dir(relativePath), linkName))
	if scope == "." {
		return resolved != ".." && !strings.HasPrefix(resolved, "../")
	}
	return resolved == scope || strings.HasPrefix(resolved, scope+"/")
}

func validateImportedArchive(archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer file.Close()
	_, err = readBackupManifest(file, true)
	return err
}

func readBackupManifest(reader io.Reader, validateAll bool) (manifest, error) {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return manifest{}, ErrInvalidArchive
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	var snapshot manifest
	manifestFound := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return manifest{}, ErrInvalidArchive
		}

		relativePath, ok := sanitizeArchivePath(header.Name)
		if !ok {
			return manifest{}, ErrInvalidArchive
		}

		switch header.Typeflag {
		case tar.TypeDir, tar.TypeReg, tar.TypeRegA, tar.TypeSymlink:
		default:
			return manifest{}, ErrInvalidArchive
		}
		if header.Typeflag == tar.TypeSymlink && !validArchiveSymlink(relativePath, header.Linkname) {
			return manifest{}, ErrInvalidArchive
		}

		if relativePath != "manifest.json" {
			continue
		}
		if manifestFound || header.Typeflag == tar.TypeDir || header.Size < 0 || header.Size > maxManifestSize {
			return manifest{}, ErrInvalidArchive
		}

		manifestPayload, err := io.ReadAll(io.LimitReader(tarReader, maxManifestSize+1))
		if err != nil {
			return manifest{}, ErrInvalidArchive
		}
		if int64(len(manifestPayload)) != header.Size {
			return manifest{}, ErrInvalidArchive
		}

		if err := json.Unmarshal(manifestPayload, &snapshot); err != nil {
			return manifest{}, ErrInvalidArchive
		}
		if snapshot.Format != backupFormat {
			return manifest{}, ErrInvalidArchive
		}

		manifestFound = true
		if !validateAll {
			return snapshot, nil
		}
	}

	if !manifestFound {
		return manifest{}, ErrInvalidArchive
	}

	return snapshot, nil
}

func hasPanelEntries(stagingPath, snapshotRelPath string) bool {
	entries, err := os.ReadDir(stagingPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "manifest.json" || name == adminTLSArchiveDir || name == "docker" || name == "docker_volumes" || name == "sites" || name == "databases" {
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
	if err := os.MkdirAll(s.dataPath, 0o700); err != nil {
		return fmt.Errorf("create data path %q: %w", s.dataPath, err)
	}
	if err := os.Chmod(s.dataPath, 0o700); err != nil {
		return fmt.Errorf("secure data path %q: %w", s.dataPath, err)
	}

	preservedPaths := map[string]struct{}{}
	if dockerDataPath := s.dockerDataPath(); dockerDataPath != "" {
		preservedPaths[dockerDataPath] = struct{}{}
	}
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
		if relativePath == "manifest.json" || relativePath == adminTLSArchiveDir || relativePath == "docker" || relativePath == "docker_volumes" || relativePath == "sites" || relativePath == "databases" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(relativePath, adminTLSArchiveDir+"/") || strings.HasPrefix(relativePath, "docker/") || strings.HasPrefix(relativePath, "docker_volumes/") || strings.HasPrefix(relativePath, "sites/") || strings.HasPrefix(relativePath, "databases/") {
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

func (s *Service) restoreAdminTLS(stagingPath string) (bool, error) {
	cert, certErr := os.ReadFile(filepath.Join(stagingPath, filepath.FromSlash(adminTLSCertArchive)))
	key, keyErr := os.ReadFile(filepath.Join(stagingPath, filepath.FromSlash(adminTLSKeyArchive)))
	if errors.Is(certErr, fs.ErrNotExist) && errors.Is(keyErr, fs.ErrNotExist) {
		return false, nil
	}
	if certErr != nil || keyErr != nil {
		return false, fmt.Errorf("read admin TLS backup: %w", errors.Join(certErr, keyErr))
	}
	if s.adminCertPath == "" || s.adminKeyPath == "" {
		return false, fmt.Errorf("admin TLS certificate and key paths are not configured")
	}
	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return false, fmt.Errorf("validate admin TLS certificate and key: %w", err)
	}
	if err := writeFileAtomic(s.adminKeyPath, key, 0o600); err != nil {
		return false, fmt.Errorf("restore admin TLS private key: %w", err)
	}
	if err := writeFileAtomic(s.adminCertPath, cert, 0o644); err != nil {
		return false, fmt.Errorf("restore admin TLS certificate: %w", err)
	}
	return true, nil
}

func (s *Service) restoreDockerData(stagingPath string) (bool, error) {
	sourceRoot := filepath.Join(stagingPath, "docker_volumes")
	info, err := os.Stat(sourceRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat Docker restore directory: %w", err)
	}
	if !info.IsDir() {
		return false, ErrInvalidArchive
	}

	targetRoot := s.dockerDataPath()
	if targetRoot == "" {
		return false, fmt.Errorf("data path is not configured")
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return false, fmt.Errorf("create Docker data directory %q: %w", targetRoot, err)
	}
	if err := clearDirectoryContents(targetRoot, nil); err != nil {
		return false, err
	}
	if err := copyTreeContents(sourceRoot, targetRoot); err != nil {
		return false, err
	}
	return true, nil
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
			return restored, err
		}
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			return restored, fmt.Errorf("create site restore directory %q: %w", targetRoot, err)
		}
		if err := clearDirectoryContents(targetRoot, nil); err != nil {
			return restored, err
		}

		sourceRoot := filepath.Join(sitesPath, hostname)
		if err := copyTreeContents(sourceRoot, targetRoot); err != nil {
			return restored, err
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
		dump, err := os.Open(filepath.Join(databasesPath, entry.Name()))
		if err != nil {
			return restored, fmt.Errorf("open restore database dump %q: %w", entry.Name(), err)
		}
		restoreErr := s.mariaDB.RestoreDatabase(ctx, databaseName, dump)
		closeErr := dump.Close()
		if restoreErr != nil {
			return restored, fmt.Errorf("restore mariadb database %q: %w", databaseName, restoreErr)
		}
		if closeErr != nil {
			return restored, fmt.Errorf("close restore database dump %q: %w", entry.Name(), closeErr)
		}
		restored = append(restored, databaseName)
	}
	if err := s.restoreDatabaseAccess(ctx, restored); err != nil {
		return restored, err
	}

	sort.Strings(restored)
	return restored, nil
}

func (s *Service) restoreDatabaseAccess(ctx context.Context, restored []string) error {
	if len(restored) == 0 {
		return nil
	}
	records, err := s.mariaDB.ListDatabases(ctx)
	if err != nil {
		return fmt.Errorf("list restored mariadb databases: %w", err)
	}
	restoredSet := make(map[string]struct{}, len(restored))
	for _, name := range restored {
		restoredSet[name] = struct{}{}
	}
	for _, record := range records {
		if _, ok := restoredSet[record.Name]; !ok || record.Username == "" || record.Password == "" {
			continue
		}
		if err := s.mariaDB.RestoreDatabaseAccess(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) siteRootPath(hostname string) (string, error) {
	if s.domains != nil {
		for _, record := range s.domains.List() {
			if record.Hostname != hostname {
				continue
			}
			if domain.SupportsManagedDocumentRoot(record.Kind) {
				return domain.ResolveDocumentRoot(s.domains.BasePath(), record)
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
	return filepath.WalkDir(sourceRoot, func(currentPath string, _ fs.DirEntry, walkErr error) error {
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

func writeFileAtomic(targetPath string, content []byte, mode fs.FileMode) error {
	directory := filepath.Dir(targetPath)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".flowpanel-restore-*")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, targetPath)
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

func databaseArchivePath(dataPath, databasePath string) string {
	databasePath = filepath.Clean(strings.TrimSpace(databasePath))
	if databasePath == "" || databasePath == "." || databasePath == ":memory:" || !filepath.IsAbs(databasePath) {
		return ""
	}
	if relPath, ok := archiveRelativePath(dataPath, databasePath); ok {
		return relPath
	}
	return filepath.Base(databasePath)
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
	if input.Location != LocationLocal && input.Location != LocationGoogleDrive {
		return ValidationErrors{
			"location": "Select a valid backup location.",
		}
	}
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
	if input.IncludePanelData || input.IncludeDockerData || input.IncludeSites || input.IncludeDatabases {
		return nil
	}

	return ValidationErrors{
		"scope": "Select at least one backup source.",
	}
}

func backupNamePrefix(input CreateInput) string {
	if input.IncludePanelData && input.IncludeDockerData && input.IncludeSites && input.IncludeDatabases {
		return "flowpanel-full-backup"
	}
	if !input.IncludePanelData && !input.IncludeDockerData && input.IncludeSites && !input.IncludeDatabases && len(input.SiteHostnames) == 1 {
		return "flowpanel-site-" + input.SiteHostnames[0] + "-backup"
	}
	if !input.IncludePanelData && !input.IncludeDockerData && !input.IncludeSites && input.IncludeDatabases && len(input.DatabaseNames) == 1 {
		return "flowpanel-database-" + input.DatabaseNames[0] + "-backup"
	}

	parts := make([]string, 0, 4)
	if input.IncludePanelData {
		parts = append(parts, "panel")
	}
	if input.IncludeDockerData {
		parts = append(parts, "docker")
	}
	if input.IncludeSites {
		parts = append(parts, "sites")
	}
	if input.IncludeDatabases {
		parts = append(parts, "databases")
	}

	return "flowpanel-" + strings.Join(parts, "-") + "-backup"
}

func (s *Service) dockerDataPath() string {
	if strings.TrimSpace(s.dataPath) == "" {
		return ""
	}
	return filepath.Join(s.dataPath, "docker_volumes")
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

func normalizeLocation(location string) string {
	location = strings.TrimSpace(strings.ToLower(location))
	if location == "" {
		return LocationLocal
	}

	return location
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
