package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flowpanel/internal/db"
)

func TestCreateStaticSiteCreatesSiteDirectory(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "var", "www")

	service := newService(basePath, nil)
	record, err := service.Create(context.Background(), CreateInput{
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

	service := newService(basePath, nil)
	record, err := service.Create(context.Background(), CreateInput{
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

	service := newService(basePath, nil)
	record, err := service.Create(context.Background(), CreateInput{
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
	service := newService(t.TempDir(), nil)

	_, err := service.Create(context.Background(), CreateInput{
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
	service := newService(t.TempDir(), nil)

	record, err := service.Create(context.Background(), CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	_, removed, err := service.Delete(context.Background(), record.ID)
	if err != nil {
		t.Fatalf("delete domain: %v", err)
	}
	if !removed {
		t.Fatal("expected delete to succeed")
	}

	if got := service.List(); len(got) != 0 {
		t.Fatalf("list length = %d, want 0", len(got))
	}
}

func TestUpdatePersistsDomain(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	store := NewStore(conn)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	service := newService(t.TempDir(), store)
	record, err := service.Create(ctx, CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	updated, previous, err := service.Update(ctx, record.ID, UpdateInput{
		Hostname: "app.example.com",
		Kind:     KindReverseProxy,
		Target:   "https://backend.example.com",
	})
	if err != nil {
		t.Fatalf("update domain: %v", err)
	}

	if previous.ID != record.ID ||
		previous.Hostname != record.Hostname ||
		previous.Kind != record.Kind ||
		previous.Target != record.Target ||
		!previous.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("previous record = %#v, want %#v", previous, record)
	}

	if updated.ID != record.ID {
		t.Fatalf("updated id = %q, want %q", updated.ID, record.ID)
	}
	if updated.Hostname != "app.example.com" {
		t.Fatalf("updated hostname = %q, want app.example.com", updated.Hostname)
	}
	if updated.Kind != KindReverseProxy {
		t.Fatalf("updated kind = %q, want %q", updated.Kind, KindReverseProxy)
	}
	if updated.Target != "https://backend.example.com" {
		t.Fatalf("updated target = %q, want https://backend.example.com", updated.Target)
	}
	if !updated.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("updated created_at changed: got %v want %v", updated.CreatedAt, record.CreatedAt)
	}

	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list store domains: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("persisted domain count = %d, want 1", len(records))
	}
	if records[0].ID != updated.ID ||
		records[0].Hostname != updated.Hostname ||
		records[0].Kind != updated.Kind ||
		records[0].Target != updated.Target ||
		!records[0].CreatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("persisted record = %#v, want %#v", records[0], updated)
	}
}

func TestUpdateRejectsHostnameChange(t *testing.T) {
	service := newService(t.TempDir(), nil)

	record, err := service.Create(context.Background(), CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	_, _, err = service.Update(context.Background(), record.ID, UpdateInput{
		Hostname: "proxy.example.com",
		Kind:     KindReverseProxy,
		Target:   "https://backend.example.com",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error = %T, want ValidationErrors", err)
	}
	if validation["hostname"] != "Domain cannot be changed after creation." {
		t.Fatalf("hostname validation = %q, want immutable domain message", validation["hostname"])
	}
}

func TestRestoreReinsertsDeletedDomain(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	store := NewStore(conn)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	service := newService(t.TempDir(), store)
	record, err := service.Create(ctx, CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	deleted, removed, err := service.Delete(ctx, record.ID)
	if err != nil {
		t.Fatalf("delete domain: %v", err)
	}
	if !removed {
		t.Fatal("expected delete to succeed")
	}

	if err := service.Restore(ctx, deleted); err != nil {
		t.Fatalf("restore domain: %v", err)
	}

	records := service.List()
	if len(records) != 1 {
		t.Fatalf("record count = %d, want 1", len(records))
	}
	if records[0].ID != record.ID ||
		records[0].Hostname != record.Hostname ||
		records[0].Kind != record.Kind ||
		records[0].Target != record.Target ||
		!records[0].CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("restored record = %#v, want %#v", records[0], record)
	}

	persisted, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list store domains: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted domain count = %d, want 1", len(persisted))
	}
}

func TestCreatePersistsDomain(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	store := NewStore(conn)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	service := newService(t.TempDir(), store)
	record, err := service.Create(ctx, CreateInput{
		Hostname: "app.example.com",
		Kind:     KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list store domains: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("persisted domain count = %d, want 1", len(records))
	}

	if records[0].ID != record.ID ||
		records[0].Hostname != record.Hostname ||
		records[0].Kind != record.Kind ||
		records[0].Target != record.Target ||
		!records[0].CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("persisted record = %#v, want %#v", records[0], record)
	}
}

func TestLoadRestoresPersistedDomains(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	store := NewStore(conn)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	expected := Record{
		ID:        "domain-1",
		Hostname:  "restored.example.com",
		Kind:      KindReverseProxy,
		Target:    "https://backend.example.com",
		CreatedAt: time.Unix(1711972800, 123456789).UTC(),
	}

	if err := store.Insert(ctx, expected); err != nil {
		t.Fatalf("insert domain: %v", err)
	}

	service := newService(t.TempDir(), store)
	if err := service.Load(ctx); err != nil {
		t.Fatalf("load persisted domains: %v", err)
	}

	records := service.List()
	if len(records) != 1 {
		t.Fatalf("loaded domain count = %d, want 1", len(records))
	}

	if records[0].ID != expected.ID ||
		records[0].Hostname != expected.Hostname ||
		records[0].Kind != expected.Kind ||
		records[0].Target != expected.Target ||
		!records[0].CreatedAt.Equal(expected.CreatedAt) {
		t.Fatalf("loaded record = %#v, want %#v", records[0], expected)
	}
}
