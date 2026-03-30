package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateStaticSiteCreatesSiteDirectory(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "var", "www")

	service := newService(basePath)
	record, err := service.Create(CreateInput{
		Hostname: "Example.com",
		Kind:     KindStaticSite,
	})
	if err != nil {
		t.Fatalf("create static site: %v", err)
	}

	expectedTarget := filepath.Join(basePath, "example.com")
	if record.Target != expectedTarget {
		t.Fatalf("target = %q, want %q", record.Target, expectedTarget)
	}

	if _, err := os.Stat(expectedTarget); err != nil {
		t.Fatalf("stat site directory: %v", err)
	}
}

func TestCreatePHPCreatePublicDirectory(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "var", "www")

	service := newService(basePath)
	record, err := service.Create(CreateInput{
		Hostname: "php.example.com",
		Kind:     KindPHP,
	})
	if err != nil {
		t.Fatalf("create php site: %v", err)
	}

	expectedTarget := filepath.Join(basePath, "php.example.com", "public")
	if record.Target != expectedTarget {
		t.Fatalf("target = %q, want %q", record.Target, expectedTarget)
	}

	if _, err := os.Stat(expectedTarget); err != nil {
		t.Fatalf("stat php public directory: %v", err)
	}
}
