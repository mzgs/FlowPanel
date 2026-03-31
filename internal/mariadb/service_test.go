package mariadb

import (
	"context"
	"os"
	"path/filepath"
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
