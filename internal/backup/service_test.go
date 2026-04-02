package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
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
	if err := os.MkdirAll(filepath.Join(dataPath, "backups"), 0o755); err != nil {
		t.Fatalf("create backups directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataPath, "backups", "old.tar.gz"), []byte("ignore"), 0o644); err != nil {
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
	dbPath := filepath.Join(dataPath, "flowpanel.db")
	dbConn := openTestDB(t, dbPath)

	service := NewService(zap.NewNop(), dataPath, dbPath, dbConn, fakeDomainSource{}, fakeDatabaseSource{})
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

func TestCreateCanLimitScope(t *testing.T) {
	t.Helper()

	dataPath := t.TempDir()
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

func TestCreateRequiresAtLeastOneSelection(t *testing.T) {
	t.Helper()

	service := NewService(zap.NewNop(), t.TempDir(), "", nil, fakeDomainSource{}, fakeDatabaseSource{})
	if _, err := service.Create(context.Background(), CreateInput{}); err == nil {
		t.Fatal("create backup error = nil, want validation error")
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

type fakeDomainSource struct {
	records []domain.Record
}

func (f fakeDomainSource) List() []domain.Record {
	return append([]domain.Record(nil), f.records...)
}

type fakeDatabaseSource struct {
	records []mariadb.DatabaseRecord
	dumps   map[string][]byte
}

func (f fakeDatabaseSource) ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error) {
	return append([]mariadb.DatabaseRecord(nil), f.records...), nil
}

func (f fakeDatabaseSource) DumpDatabase(_ context.Context, name string) ([]byte, error) {
	return append([]byte(nil), f.dumps[name]...), nil
}
