package domain

import (
	"os"
	"path/filepath"
	"strings"
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

	indexPath := filepath.Join(expectedTarget, "index.html")
	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read site index: %v", err)
	}

	if !strings.Contains(string(indexContent), "<title>example.com</title>") {
		t.Fatalf("site index missing hostname title: %s", string(indexContent))
	}
}

func TestCreateStaticSiteDoesNotOverwriteExistingIndex(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "var", "www")
	siteRoot := filepath.Join(basePath, "example.com")

	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("mkdir site root: %v", err)
	}

	const existingIndex = "<html><body>custom site</body></html>"
	indexPath := filepath.Join(siteRoot, "index.html")
	if err := os.WriteFile(indexPath, []byte(existingIndex), 0o644); err != nil {
		t.Fatalf("write existing index: %v", err)
	}

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

	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read existing index: %v", err)
	}

	if string(indexContent) != existingIndex {
		t.Fatalf("index content = %q, want %q", string(indexContent), existingIndex)
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

func TestCreateReverseProxyRejectsPathTargets(t *testing.T) {
	service := newService(t.TempDir())

	_, err := service.Create(CreateInput{
		Hostname: "proxy.example.com",
		Kind:     KindReverseProxy,
		Target:   "https://backend.example.com/base",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error = %T, want ValidationErrors", err)
	}

	if validation["target"] == "" {
		t.Fatalf("target validation error missing: %#v", validation)
	}
}

func TestDeleteRemovesMatchingRecord(t *testing.T) {
	service := newService(t.TempDir())

	record, err := service.Create(CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	if !service.Delete(record.ID) {
		t.Fatal("expected delete to succeed")
	}

	if got := service.List(); len(got) != 0 {
		t.Fatalf("list length = %d, want 0", len(got))
	}
}
