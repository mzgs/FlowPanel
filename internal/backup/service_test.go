package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/mariadb"

	"go.uber.org/zap"
)

func TestCreateIncludesDataFilesAndDatabaseSnapshot(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	if _, err := dbConn.ExecContext(context.Background(), `CREATE TABLE notes (value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create notes table: %v", err)
	}
	if _, err := dbConn.ExecContext(context.Background(), `INSERT INTO notes (value) VALUES ('alpha')`); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataPath, "mariadb-root-password"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	if err := os.MkdirAll(backupPath, 0o755); err != nil {
		t.Fatalf("create backups directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "old.tar.gz"), []byte("ignore"), 0o644); err != nil {
		t.Fatalf("write existing backup: %v", err)
	}
	siteRoot := filepath.Join(t.TempDir(), "example.com")
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("create site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("<h1>site</h1>"), 0o644); err != nil {
		t.Fatalf("write site index: %v", err)
	}

	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{
			records: []domain.Record{
				{Hostname: "example.com", Kind: domain.KindStaticSite, Target: siteRoot},
				{Hostname: "api.example.com", Kind: domain.KindReverseProxy, Target: "http://127.0.0.1:9000"},
			},
		},
		fakeDatabaseSource{
			records: []mariadb.DatabaseRecord{
				{Name: "appdb"},
			},
			dumps: map[string][]byte{
				"appdb": []byte("CREATE DATABASE `appdb`;\nUSE `appdb`;\n"),
			},
		},
	)
	record, err := service.Create(context.Background(), CreateInput{
		IncludePanelData: true,
		IncludeSites:     true,
		IncludeDatabases: true,
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if !strings.Contains(record.Name, "full-backup") {
		t.Fatalf("backup name = %q, want full-backup prefix", record.Name)
	}

	archivePath, _, err := service.DownloadPath(record.Name)
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	entries := readArchiveEntries(t, archivePath)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}

	if !slices.Contains(names, "manifest.json") {
		t.Fatalf("archive entries %v do not contain manifest.json", names)
	}
	if !slices.Contains(names, "mariadb-root-password") {
		t.Fatalf("archive entries %v do not contain mariadb-root-password", names)
	}
	if !slices.Contains(names, "flowpanel.db") {
		t.Fatalf("archive entries %v do not contain flowpanel.db", names)
	}
	if !slices.Contains(names, "sites/example.com/index.html") {
		t.Fatalf("archive entries %v do not contain site files", names)
	}
	if !slices.Contains(names, "databases/appdb.sql") {
		t.Fatalf("archive entries %v do not contain database dump", names)
	}
	if slices.Contains(names, "backups/old.tar.gz") {
		t.Fatalf("archive entries %v unexpectedly contain nested backups", names)
	}
	if got := string(entries["mariadb-root-password"]); got != "secret" {
		t.Fatalf("password file = %q, want secret", got)
	}
	if got := string(entries["sites/example.com/index.html"]); got != "<h1>site</h1>" {
		t.Fatalf("site index = %q, want site content", got)
	}
	if got := string(entries["databases/appdb.sql"]); got != "CREATE DATABASE `appdb`;\nUSE `appdb`;\n" {
		t.Fatalf("database dump = %q, want dump content", got)
	}

	restoreDBPath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restoreDBPath, entries["flowpanel.db"], 0o644); err != nil {
		t.Fatalf("write restored db: %v", err)
	}
	restoreDB := openTestDB(t, restoreDBPath)

	var value string
	if err := restoreDB.QueryRowContext(context.Background(), `SELECT value FROM notes LIMIT 1`).Scan(&value); err != nil {
		t.Fatalf("query restored db: %v", err)
	}
	if value != "alpha" {
		t.Fatalf("restored note = %q, want alpha", value)
	}
}

func TestListDeleteAndDownloadPath(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	service := NewService(zap.NewNop(), dataPath, backupPath, dbPath, dbConn, fakeDomainSource{}, fakeDatabaseSource{})
	record, err := service.Create(context.Background(), CreateInput{
		IncludePanelData: true,
		IncludeSites:     true,
		IncludeDatabases: true,
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	list, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("backup count = %d, want 1", len(list))
	}
	if list[0].Name != record.Name {
		t.Fatalf("backup name = %q, want %q", list[0].Name, record.Name)
	}

	downloadPath, name, err := service.DownloadPath(record.Name)
	if err != nil {
		t.Fatalf("download path: %v", err)
	}
	if name != record.Name {
		t.Fatalf("download name = %q, want %q", name, record.Name)
	}
	if _, err := os.Stat(downloadPath); err != nil {
		t.Fatalf("stat download path: %v", err)
	}

	if err := service.Delete(context.Background(), record.Name); err != nil {
		t.Fatalf("delete backup: %v", err)
	}
	if _, _, err := service.DownloadPath(record.Name); err != ErrNotFound {
		t.Fatalf("download path after delete error = %v, want %v", err, ErrNotFound)
	}
}

func TestImportStoresValidatedArchive(t *testing.T) {
	t.Helper()

	sourceDataPath := t.TempDir()
	sourceBackupPath := filepath.Join(t.TempDir(), "source-backups")
	sourceDBPath := filepath.Join(sourceDataPath, "flowpanel.db")
	sourceDBConn := openTestDB(t, sourceDBPath)

	sourceService := NewService(zap.NewNop(), sourceDataPath, sourceBackupPath, sourceDBPath, sourceDBConn, fakeDomainSource{}, fakeDatabaseSource{
		records: []mariadb.DatabaseRecord{{Name: "flowpanel"}},
		dumps: map[string][]byte{
			"flowpanel": []byte("dump"),
		},
	})

	created, err := sourceService.Create(context.Background(), CreateInput{
		IncludeDatabases: true,
		DatabaseNames:    []string{"flowpanel"},
	})
	if err != nil {
		t.Fatalf("create source backup: %v", err)
	}

	archivePath, _, err := sourceService.DownloadPath(created.Name)
	if err != nil {
		t.Fatalf("source download path: %v", err)
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open source archive: %v", err)
	}
	defer archive.Close()

	targetDataPath := t.TempDir()
	targetBackupPath := filepath.Join(t.TempDir(), "target-backups")
	targetDBPath := filepath.Join(targetDataPath, "flowpanel.db")
	targetDBConn := openTestDB(t, targetDBPath)
	targetService := NewService(zap.NewNop(), targetDataPath, targetBackupPath, targetDBPath, targetDBConn, fakeDomainSource{}, fakeDatabaseSource{})

	importedName := "flowpanel-database-flowpanel-backup-imported.tar.gz"
	record, err := targetService.Import(context.Background(), importedName, archive)
	if err != nil {
		t.Fatalf("import backup: %v", err)
	}
	if record.Name != importedName {
		t.Fatalf("imported backup name = %q, want %q", record.Name, importedName)
	}
	if record.Size <= 0 {
		t.Fatalf("imported backup size = %d, want positive value", record.Size)
	}

	importedPath, _, err := targetService.DownloadPath(importedName)
	if err != nil {
		t.Fatalf("imported download path: %v", err)
	}
	entries := readArchiveEntries(t, importedPath)
	if got := string(entries["manifest.json"]); !strings.Contains(got, backupFormat) {
		t.Fatalf("manifest = %q, want backup format", got)
	}
}

func TestImportRejectsInvalidArchive(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	service := NewService(zap.NewNop(), dataPath, backupPath, dbPath, dbConn, fakeDomainSource{}, fakeDatabaseSource{})
	_, err := service.Import(context.Background(), "flowpanel-database-invalid-backup.tar.gz", strings.NewReader("not a gzip archive"))
	if !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("import error = %v, want %v", err, ErrInvalidArchive)
	}
}

func TestCreateCanLimitScope(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	if err := os.WriteFile(filepath.Join(dataPath, "runtime.txt"), []byte("panel"), 0o644); err != nil {
		t.Fatalf("write panel file: %v", err)
	}
	siteRoot := filepath.Join(t.TempDir(), "example.com")
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("create site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("site"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}

	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{
			records: []domain.Record{
				{Hostname: "example.com", Kind: domain.KindStaticSite, Target: siteRoot},
			},
		},
		fakeDatabaseSource{
			records: []mariadb.DatabaseRecord{{Name: "appdb"}},
			dumps: map[string][]byte{
				"appdb": []byte("dump"),
			},
		},
	)

	record, err := service.Create(context.Background(), CreateInput{
		IncludeSites: true,
	})
	if err != nil {
		t.Fatalf("create scoped backup: %v", err)
	}
	if !strings.Contains(record.Name, "sites-backup") {
		t.Fatalf("backup name = %q, want sites-backup prefix", record.Name)
	}

	archivePath, _, err := service.DownloadPath(record.Name)
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	entries := readArchiveEntries(t, archivePath)
	if _, ok := entries["sites/example.com/index.html"]; !ok {
		t.Fatal("scoped backup did not include site files")
	}
	if _, ok := entries["runtime.txt"]; ok {
		t.Fatal("scoped backup unexpectedly included panel data")
	}
	if _, ok := entries["flowpanel.db"]; ok {
		t.Fatal("scoped backup unexpectedly included sqlite snapshot")
	}
	if _, ok := entries["databases/appdb.sql"]; ok {
		t.Fatal("scoped backup unexpectedly included database dump")
	}
}

func TestCreateCanLimitBackupToSingleDatabase(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{},
		fakeDatabaseSource{
			records: []mariadb.DatabaseRecord{{Name: "appdb"}, {Name: "logsdb"}},
			dumps: map[string][]byte{
				"appdb":  []byte("app dump"),
				"logsdb": []byte("logs dump"),
			},
		},
	)

	record, err := service.Create(context.Background(), CreateInput{
		IncludeDatabases: true,
		DatabaseNames:    []string{"logsdb"},
	})
	if err != nil {
		t.Fatalf("create database backup: %v", err)
	}
	if !strings.Contains(record.Name, "database-logsdb-backup") {
		t.Fatalf("backup name = %q, want database-specific prefix", record.Name)
	}

	archivePath, _, err := service.DownloadPath(record.Name)
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	entries := readArchiveEntries(t, archivePath)
	if got := string(entries["databases/logsdb.sql"]); got != "logs dump" {
		t.Fatalf("logsdb dump = %q, want logs dump", got)
	}
	if _, ok := entries["databases/appdb.sql"]; ok {
		t.Fatal("database backup unexpectedly included appdb dump")
	}
}

func TestCreateCanLimitBackupToSingleSite(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	firstSiteRoot := filepath.Join(t.TempDir(), "example.com")
	if err := os.MkdirAll(firstSiteRoot, 0o755); err != nil {
		t.Fatalf("create first site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstSiteRoot, "index.html"), []byte("first"), 0o644); err != nil {
		t.Fatalf("write first site file: %v", err)
	}

	secondSiteRoot := filepath.Join(t.TempDir(), "admin.example.com")
	if err := os.MkdirAll(secondSiteRoot, 0o755); err != nil {
		t.Fatalf("create second site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secondSiteRoot, "index.html"), []byte("second"), 0o644); err != nil {
		t.Fatalf("write second site file: %v", err)
	}

	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{
			records: []domain.Record{
				{Hostname: "example.com", Kind: domain.KindStaticSite, Target: firstSiteRoot},
				{Hostname: "admin.example.com", Kind: domain.KindPHP, Target: secondSiteRoot},
			},
		},
		fakeDatabaseSource{},
	)

	record, err := service.Create(context.Background(), CreateInput{
		IncludeSites:  true,
		SiteHostnames: []string{"admin.example.com"},
	})
	if err != nil {
		t.Fatalf("create site backup: %v", err)
	}
	if !strings.Contains(record.Name, "site-admin.example.com-backup") {
		t.Fatalf("backup name = %q, want site-specific prefix", record.Name)
	}

	archivePath, _, err := service.DownloadPath(record.Name)
	if err != nil {
		t.Fatalf("download path: %v", err)
	}

	entries := readArchiveEntries(t, archivePath)
	if got := string(entries["sites/admin.example.com/index.html"]); got != "second" {
		t.Fatalf("site entry = %q, want second", got)
	}
	if _, ok := entries["sites/example.com/index.html"]; ok {
		t.Fatal("site backup unexpectedly included example.com")
	}
}

func TestCreateRejectsUnknownSiteHostnames(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{
			records: []domain.Record{
				{Hostname: "example.com", Kind: domain.KindStaticSite, Target: filepath.Join(t.TempDir(), "example.com")},
			},
		},
		fakeDatabaseSource{},
	)

	_, err := service.Create(context.Background(), CreateInput{
		IncludeSites:  true,
		SiteHostnames: []string{"missing.example.com"},
	})
	if err == nil {
		t.Fatal("create backup error = nil, want validation error")
	}

	var validation ValidationErrors
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want validation error", err)
	}
	if validation["site_hostnames"] != `Site "missing.example.com" was not found.` {
		t.Fatalf("site_hostnames error = %q, want missing site message", validation["site_hostnames"])
	}
}

func TestCreateRequiresAtLeastOneSelection(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	service := NewService(zap.NewNop(), dataPath, backupPath, "", nil, fakeDomainSource{}, fakeDatabaseSource{})
	if _, err := service.Create(context.Background(), CreateInput{}); err == nil {
		t.Fatal("create backup error = nil, want validation error")
	}
}

func TestRestoreAppliesPanelDataSitesAndDatabases(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
	backupPath := filepath.Join(t.TempDir(), "backups")
	sitesBasePath := filepath.Join(t.TempDir(), "sites")
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	if _, err := dbConn.ExecContext(context.Background(), `CREATE TABLE notes (value TEXT NOT NULL)`); err != nil {
		t.Fatalf("create notes table: %v", err)
	}
	if _, err := dbConn.ExecContext(context.Background(), `INSERT INTO notes (value) VALUES ('alpha')`); err != nil {
		t.Fatalf("insert note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "runtime.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("write runtime file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "mariadb-root-password"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	siteRoot := filepath.Join(sitesBasePath, "example.com")
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("create site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("site-original"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}

	databaseSource := fakeDatabaseSource{
		records: []mariadb.DatabaseRecord{{Name: "appdb"}},
		dumps: map[string][]byte{
			"appdb": []byte("CREATE DATABASE `appdb`;\nUSE `appdb`;\nCREATE TABLE test (id INT);\n"),
		},
		restored: make(map[string][]byte),
	}
	service := NewService(
		zap.NewNop(),
		dataPath,
		backupPath,
		dbPath,
		dbConn,
		fakeDomainSource{
			basePath: sitesBasePath,
			records: []domain.Record{
				{Hostname: "example.com", Kind: domain.KindStaticSite, Target: siteRoot},
			},
		},
		databaseSource,
	)

	record, err := service.Create(context.Background(), CreateInput{
		IncludePanelData: true,
		IncludeSites:     true,
		IncludeDatabases: true,
	})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataPath, "runtime.txt"), []byte("mutated"), 0o644); err != nil {
		t.Fatalf("mutate runtime file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "extra.txt"), []byte("extra"), 0o644); err != nil {
		t.Fatalf("write extra runtime file: %v", err)
	}
	if _, err := dbConn.ExecContext(context.Background(), `DELETE FROM notes`); err != nil {
		t.Fatalf("clear notes: %v", err)
	}
	if _, err := dbConn.ExecContext(context.Background(), `INSERT INTO notes (value) VALUES ('beta')`); err != nil {
		t.Fatalf("insert mutated note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("site-mutated"), 0o644); err != nil {
		t.Fatalf("mutate site file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "extra.txt"), []byte("extra-site"), 0o644); err != nil {
		t.Fatalf("write extra site file: %v", err)
	}

	result, err := service.Restore(context.Background(), record.Name)
	if err != nil {
		t.Fatalf("restore backup: %v", err)
	}

	if !result.RestoredPanelFiles {
		t.Fatal("expected panel files to be restored")
	}
	if !result.RestoredPanelDatabase {
		t.Fatal("expected panel database to be restored")
	}
	if !slices.Equal(result.RestoredSites, []string{"example.com"}) {
		t.Fatalf("restored sites = %v, want [example.com]", result.RestoredSites)
	}
	if !slices.Equal(result.RestoredDatabases, []string{"appdb"}) {
		t.Fatalf("restored databases = %v, want [appdb]", result.RestoredDatabases)
	}

	if got := string(mustReadFile(t, filepath.Join(dataPath, "runtime.txt"))); got != "original" {
		t.Fatalf("runtime file = %q, want original", got)
	}
	if _, err := os.Stat(filepath.Join(dataPath, "extra.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("extra runtime file stat error = %v, want not exists", err)
	}

	var note string
	if err := dbConn.QueryRowContext(context.Background(), `SELECT value FROM notes LIMIT 1`).Scan(&note); err != nil {
		t.Fatalf("query restored notes: %v", err)
	}
	if note != "alpha" {
		t.Fatalf("restored note = %q, want alpha", note)
	}

	if got := string(mustReadFile(t, filepath.Join(siteRoot, "index.html"))); got != "site-original" {
		t.Fatalf("site file = %q, want site-original", got)
	}
	if _, err := os.Stat(filepath.Join(siteRoot, "extra.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("extra site file stat error = %v, want not exists", err)
	}

	if got := string(databaseSource.restored["appdb"]); got != "CREATE DATABASE `appdb`;\nUSE `appdb`;\nCREATE TABLE test (id INT);\n" {
		t.Fatalf("restored database dump = %q, want original dump", got)
	}
}

func openTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	dbConn, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	return dbConn
}

func readArchiveEntries(t *testing.T, archivePath string) map[string][]byte {
	t.Helper()

	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("open gzip archive: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	entries := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar archive: %v", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		payload, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read archive entry %q: %v", header.Name, err)
		}
		entries[header.Name] = payload
	}

	return entries
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %q: %v", path, err)
	}

	return content
}

type fakeDomainSource struct {
	basePath string
	records  []domain.Record
}

func (f fakeDomainSource) List() []domain.Record {
	return append([]domain.Record(nil), f.records...)
}

func (f fakeDomainSource) BasePath() string {
	return f.basePath
}

type fakeDatabaseSource struct {
	records  []mariadb.DatabaseRecord
	dumps    map[string][]byte
	restored map[string][]byte
}

func (f fakeDatabaseSource) ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error) {
	return append([]mariadb.DatabaseRecord(nil), f.records...), nil
}

func (f fakeDatabaseSource) DumpDatabase(_ context.Context, name string) ([]byte, error) {
	return append([]byte(nil), f.dumps[name]...), nil
}

func (f fakeDatabaseSource) RestoreDatabase(_ context.Context, name string, dump []byte) error {
	if f.restored == nil {
		return nil
	}
	f.restored[name] = append([]byte(nil), dump...)
	return nil
}
