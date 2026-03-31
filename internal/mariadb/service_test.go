package mariadb

import "testing"

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
