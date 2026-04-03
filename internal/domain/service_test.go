package domain

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"flowpanel/internal/db"
)

type previewGeneratorFunc func(ctx context.Context, targetURL string) ([]byte, error)

func (f previewGeneratorFunc) Capture(ctx context.Context, targetURL string) ([]byte, error) {
	return f(ctx, targetURL)
}

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

func TestCreatePHPCreatesSiteDirectory(t *testing.T) {
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

	expectedTarget := filepath.Join(basePath, "php.example.com")
	if record.Target != expectedTarget {
		t.Fatalf("target = %q, want %q", record.Target, expectedTarget)
	}

	if _, err := os.Stat(expectedTarget); err != nil {
		t.Fatalf("stat php site directory: %v", err)
	}

	indexPath := filepath.Join(expectedTarget, "index.php")
	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read php site index: %v", err)
	}

	if !strings.Contains(string(indexContent), `"php.example.com"`) {
		t.Fatalf("php site index missing hostname: %s", string(indexContent))
	}
}

func TestCreatePHPDoesNotOverwriteExistingIndex(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "var", "www")
	siteRoot := filepath.Join(basePath, "php.example.com")

	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("mkdir php site root: %v", err)
	}

	const existingIndex = "<?php echo 'custom php site';"
	indexPath := filepath.Join(siteRoot, "index.php")
	if err := os.WriteFile(indexPath, []byte(existingIndex), 0o644); err != nil {
		t.Fatalf("write existing php index: %v", err)
	}

	service := newService(basePath, nil)
	record, err := service.Create(context.Background(), CreateInput{
		Hostname: "php.example.com",
		Kind:     KindPHP,
	})
	if err != nil {
		t.Fatalf("create php site: %v", err)
	}

	expectedTarget := filepath.Join(basePath, "php.example.com")
	if record.Target != expectedTarget {
		t.Fatalf("target = %q, want %q", record.Target, expectedTarget)
	}

	indexContent, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read existing php index: %v", err)
	}

	if string(indexContent) != existingIndex {
		t.Fatalf("index content = %q, want %q", string(indexContent), existingIndex)
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

func TestCreateRejectsInvalidHostnameFormat(t *testing.T) {
	service := newService(t.TempDir(), nil)

	_, err := service.Create(context.Background(), CreateInput{
		Hostname: "example..com",
		Kind:     KindStaticSite,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error = %T, want ValidationErrors", err)
	}

	if validation["hostname"] != "Enter a valid domain like example.com." {
		t.Fatalf("hostname validation = %q, want valid domain message", validation["hostname"])
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
		Hostname:     "app.example.com",
		Kind:         KindReverseProxy,
		Target:       "https://backend.example.com",
		CacheEnabled: true,
	})
	if err != nil {
		t.Fatalf("update domain: %v", err)
	}

	if previous.ID != record.ID ||
		previous.Hostname != record.Hostname ||
		previous.Kind != record.Kind ||
		previous.Target != record.Target ||
		previous.CacheEnabled != record.CacheEnabled ||
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
	if !updated.CacheEnabled {
		t.Fatal("updated cache_enabled = false, want true")
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
		records[0].CacheEnabled != updated.CacheEnabled ||
		!records[0].CreatedAt.Equal(updated.CreatedAt) {
		t.Fatalf("persisted record = %#v, want %#v", records[0], updated)
	}
}

func TestEnsurePreviewCachesFetchedImage(t *testing.T) {
	tempDir := t.TempDir()
	service := newService(tempDir, nil)
	service.previewCachePath = filepath.Join(tempDir, "cache")
	service.previewTTL = 24 * time.Hour

	if _, err := service.Create(context.Background(), CreateInput{
		Hostname: "preview.example.com",
		Kind:     KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	var requestCount atomic.Int32
	service.SetPreviewGenerator(previewGeneratorFunc(func(context.Context, string) ([]byte, error) {
		requestCount.Add(1)
		return []byte("png-preview"), nil
	}))

	firstPath, err := service.EnsurePreview(context.Background(), "preview.example.com")
	if err != nil {
		t.Fatalf("ensure preview: %v", err)
	}

	if filepath.Base(firstPath) != "preview.example.com.png" {
		t.Fatalf("preview filename = %q, want preview.example.com.png", filepath.Base(firstPath))
	}

	cachedData, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read preview cache: %v", err)
	}
	if string(cachedData) != "png-preview" {
		t.Fatalf("preview cache = %q, want png-preview", string(cachedData))
	}

	secondPath, err := service.EnsurePreview(context.Background(), "preview.example.com")
	if err != nil {
		t.Fatalf("ensure cached preview: %v", err)
	}
	if secondPath != firstPath {
		t.Fatalf("cached path = %q, want %q", secondPath, firstPath)
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

func TestThumbnailPreviewImageProducesCardSizedPNG(t *testing.T) {
	sourceImage := image.NewRGBA(image.Rect(0, 0, 160, 640))
	for y := 0; y < sourceImage.Bounds().Dy(); y++ {
		for x := 0; x < sourceImage.Bounds().Dx(); x++ {
			sourceImage.Set(x, y, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
		}
	}

	var source bytes.Buffer
	if err := png.Encode(&source, sourceImage); err != nil {
		t.Fatalf("encode source image: %v", err)
	}

	thumbnail, err := thumbnailPreviewImage(source.Bytes())
	if err != nil {
		t.Fatalf("thumbnail preview image: %v", err)
	}

	decodedThumbnail, err := png.Decode(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if decodedThumbnail.Bounds().Dx() != defaultDomainPreviewWidth || decodedThumbnail.Bounds().Dy() != defaultDomainPreviewHeight {
		t.Fatalf("thumbnail bounds = %v, want %dx%d", decodedThumbnail.Bounds(), defaultDomainPreviewWidth, defaultDomainPreviewHeight)
	}
}

func TestEnsurePreviewReturnsStaleCacheWhenRefreshFails(t *testing.T) {
	tempDir := t.TempDir()
	service := newService(tempDir, nil)
	service.previewCachePath = filepath.Join(tempDir, "cache")
	service.previewTTL = time.Hour
	service.now = func() time.Time {
		return time.Unix(1_800, 0)
	}

	if _, err := service.Create(context.Background(), CreateInput{
		Hostname: "stale.example.com",
		Kind:     KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	cachePath := service.previewPath("stale.example.com")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir preview cache: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("stale-preview"), 0o644); err != nil {
		t.Fatalf("write stale preview: %v", err)
	}
	oldTime := time.Unix(0, 0)
	if err := os.Chtimes(cachePath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes preview: %v", err)
	}

	service.SetPreviewGenerator(previewGeneratorFunc(func(context.Context, string) ([]byte, error) {
		return nil, errors.New("preview generation failed")
	}))

	path, err := service.EnsurePreview(context.Background(), "stale.example.com")
	if err != nil {
		t.Fatalf("ensure preview with stale fallback: %v", err)
	}
	if path != cachePath {
		t.Fatalf("path = %q, want %q", path, cachePath)
	}

	cachedData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stale preview: %v", err)
	}
	if string(cachedData) != "stale-preview" {
		t.Fatalf("stale preview = %q, want stale-preview", string(cachedData))
	}
}

func TestRefreshPreviewForcesRefetch(t *testing.T) {
	tempDir := t.TempDir()
	service := newService(tempDir, nil)
	service.previewCachePath = filepath.Join(tempDir, "cache")
	service.previewTTL = 7 * 24 * time.Hour

	if _, err := service.Create(context.Background(), CreateInput{
		Hostname: "refresh.example.com",
		Kind:     KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	var requestCount atomic.Int32
	service.SetPreviewGenerator(previewGeneratorFunc(func(_ context.Context, targetURL string) ([]byte, error) {
		currentCount := requestCount.Add(1)
		if currentCount == 1 {
			if strings.Contains(targetURL, "flowpanel_preview_refresh") {
				t.Fatalf("first request unexpectedly forced refresh: %q", targetURL)
			}
			return []byte("preview-v1"), nil
		}

		if !strings.Contains(targetURL, "flowpanel_preview_refresh=") {
			t.Fatalf("forced refresh query missing: %q", targetURL)
		}
		return []byte("preview-v2"), nil
	}))

	firstPath, err := service.EnsurePreview(context.Background(), "refresh.example.com")
	if err != nil {
		t.Fatalf("ensure preview: %v", err)
	}
	firstData, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read first preview: %v", err)
	}
	if string(firstData) != "preview-v1" {
		t.Fatalf("first preview = %q, want preview-v1", string(firstData))
	}

	refreshedPath, err := service.RefreshPreview(context.Background(), "refresh.example.com")
	if err != nil {
		t.Fatalf("refresh preview: %v", err)
	}
	refreshedData, err := os.ReadFile(refreshedPath)
	if err != nil {
		t.Fatalf("read refreshed preview: %v", err)
	}
	if string(refreshedData) != "preview-v2" {
		t.Fatalf("refreshed preview = %q, want preview-v2", string(refreshedData))
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
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

func TestUpdateRejectsInvalidHostnameFormat(t *testing.T) {
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
		Hostname: "app..example.com",
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
	if validation["hostname"] != "Enter a valid domain like example.com." {
		t.Fatalf("hostname validation = %q, want valid domain message", validation["hostname"])
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
		records[0].CacheEnabled != record.CacheEnabled ||
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
		Hostname:     "app.example.com",
		Kind:         KindApp,
		Target:       "3000",
		CacheEnabled: true,
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
		records[0].CacheEnabled != record.CacheEnabled ||
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
		ID:           "domain-1",
		Hostname:     "restored.example.com",
		Kind:         KindReverseProxy,
		Target:       "https://backend.example.com",
		CacheEnabled: true,
		CreatedAt:    time.Unix(1711972800, 123456789).UTC(),
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
		records[0].CacheEnabled != expected.CacheEnabled ||
		!records[0].CreatedAt.Equal(expected.CreatedAt) {
		t.Fatalf("loaded record = %#v, want %#v", records[0], expected)
	}
}

func TestEnsureAddsCacheEnabledColumnForExistingDomainsTable(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.ExecContext(ctx, `
CREATE TABLE domains (
	id TEXT PRIMARY KEY,
	hostname TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	target TEXT NOT NULL,
	created_at INTEGER NOT NULL
)`); err != nil {
		t.Fatalf("create legacy domains table: %v", err)
	}

	store := NewStore(conn)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
INSERT INTO domains (id, hostname, kind, target, cache_enabled, created_at)
VALUES ('domain-1', 'legacy.example.com', 'App', '3000', 1, ?)
`, time.Now().UTC().UnixNano()); err != nil {
		t.Fatalf("insert upgraded domain row: %v", err)
	}
}
