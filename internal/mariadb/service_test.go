package mariadb

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.uber.org/zap"
)

func TestParseVersionDetectsMariaDB(t *testing.T) {
	product, version := parseVersion("mariadb  Ver 15.1 Distrib 11.4.5-MariaDB, for Linux (x86_64)")

	if product != "MariaDB" {
		t.Fatalf("product = %q, want MariaDB", product)
	}
	if version != "mariadb  Ver 15.1 Distrib 11.4.5-MariaDB, for Linux (x86_64)" {
		t.Fatalf("version = %q, want input line", version)
	}
}

func TestParseVersionDetectsMySQL(t *testing.T) {
	product, version := parseVersion("mysql  Ver 8.4.3 for Linux on x86_64 (MySQL Community Server - GPL)")

	if product != "MySQL" {
		t.Fatalf("product = %q, want MySQL", product)
	}
	if version != "mysql  Ver 8.4.3 for Linux on x86_64 (MySQL Community Server - GPL)" {
		t.Fatalf("version = %q, want input line", version)
	}
}

func TestRootPasswordReadsFromFile(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "mariadb-root-password")
	if err := os.WriteFile(passwordFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	t.Setenv("FLOWPANEL_MARIADB_PASSWORD_FILE", passwordFile)
	t.Setenv("FLOWPANEL_MARIADB_PASSWORD", "")

	service := NewService(zap.NewNop())

	password, configured, err := service.RootPassword(context.Background())
	if err != nil {
		t.Fatalf("root password: %v", err)
	}
	if !configured {
		t.Fatal("configured = false, want true")
	}
	if password != "from-file" {
		t.Fatalf("password = %q, want from-file", password)
	}
}

func TestRootPasswordPrefersEnvOverFile(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "mariadb-root-password")
	if err := os.WriteFile(passwordFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write password file: %v", err)
	}

	t.Setenv("FLOWPANEL_MARIADB_PASSWORD_FILE", passwordFile)
	t.Setenv("FLOWPANEL_MARIADB_PASSWORD", "from-env")

	service := NewService(zap.NewNop())

	password, configured, err := service.RootPassword(context.Background())
	if err != nil {
		t.Fatalf("root password: %v", err)
	}
	if !configured {
		t.Fatal("configured = false, want true")
	}
	if password != "from-env" {
		t.Fatalf("password = %q, want from-env", password)
	}
}

func TestDatabaseCredentialsFileRoundTrip(t *testing.T) {
	credentialsFile := filepath.Join(t.TempDir(), "mariadb-database-credentials.json")
	expected := map[string]databaseCredential{
		"flowpanel": {
			Username: "flowpanel_user",
			Password: "secret123",
			Host:     "localhost",
		},
	}

	if err := writeDatabaseCredentialsFile(credentialsFile, expected); err != nil {
		t.Fatalf("write credentials file: %v", err)
	}

	actual, err := readDatabaseCredentialsFile(credentialsFile)
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("credentials = %#v, want %#v", actual, expected)
	}
}

func TestReadDatabaseCredentialsFileReturnsEmptyWhenMissing(t *testing.T) {
	credentialsFile := filepath.Join(t.TempDir(), "missing.json")

	credentials, err := readDatabaseCredentialsFile(credentialsFile)
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}

	if len(credentials) != 0 {
		t.Fatalf("credential count = %d, want 0", len(credentials))
	}
}

func TestWriteDatabaseCredentialsFileRejectsEmptyPath(t *testing.T) {
	err := writeDatabaseCredentialsFile("", map[string]databaseCredential{})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if err.Error() != "mariadb database credentials file path is empty" {
		t.Fatalf("error = %v, want empty path error", err)
	}
}
