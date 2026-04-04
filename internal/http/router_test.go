package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/backup"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	flowcron "flowpanel/internal/cron"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/events"
	"flowpanel/internal/files"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"
	"flowpanel/internal/phpmyadmin"

	"go.uber.org/zap"
)

type fakePHPManager struct{}

type fakeMariaDBManager struct{}

type fakePHPMyAdminManager struct{}

type installablePHPMyAdminManager struct {
	status phpmyadmin.Status
}

type previewGeneratorFunc func(ctx context.Context, targetURL string) ([]byte, error)

func (f previewGeneratorFunc) Capture(ctx context.Context, targetURL string) ([]byte, error) {
	return f(ctx, targetURL)
}

func (fakePHPManager) Status(context.Context) phpenv.Status {
	return phpenv.Status{
		Ready:         true,
		ListenAddress: "127.0.0.1:9000",
	}
}

func (fakePHPManager) Install(context.Context) error {
	return nil
}

func (fakePHPManager) Start(context.Context) error {
	return nil
}

func (fakeMariaDBManager) Status(context.Context) mariadb.Status {
	return mariadb.Status{
		Product:          "MariaDB",
		ServerInstalled:  true,
		ServiceRunning:   true,
		Ready:            true,
		State:            "ready",
		Message:          "MariaDB is accepting local connections on 127.0.0.1:3306.",
		ListenAddress:    "127.0.0.1:3306",
		Version:          "mariadb  Ver 15.1 Distrib 11.4.5-MariaDB, for Linux (x86_64)",
		InstallAvailable: false,
	}
}

func (fakeMariaDBManager) Install(context.Context) error {
	return nil
}

func (fakeMariaDBManager) RootPassword(context.Context) (string, bool, error) {
	password, configured := os.LookupEnv("FLOWPANEL_MARIADB_PASSWORD")
	if !configured || strings.TrimSpace(password) == "" {
		return "", false, nil
	}

	return strings.TrimSpace(password), true, nil
}

func (fakeMariaDBManager) SetRootPassword(_ context.Context, password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return mariadb.ValidationErrors{
			"password": "Password must be at least 8 characters.",
		}
	}

	return os.Setenv("FLOWPANEL_MARIADB_PASSWORD", password)
}

func (fakeMariaDBManager) ListDatabases(context.Context) ([]mariadb.DatabaseRecord, error) {
	return []mariadb.DatabaseRecord{
		{
			Name:     "flowpanel",
			Username: "flowpanel_user",
			Host:     "localhost",
		},
	}, nil
}

func (fakeMariaDBManager) CreateDatabase(context.Context, mariadb.CreateDatabaseInput) (mariadb.DatabaseRecord, error) {
	return mariadb.DatabaseRecord{
		Name:     "flowpanel",
		Username: "flowpanel_user",
		Host:     "localhost",
	}, nil
}

func (fakeMariaDBManager) DumpDatabase(_ context.Context, name string) ([]byte, error) {
	return []byte("CREATE DATABASE `" + strings.TrimSpace(name) + "`;\n"), nil
}

func (fakeMariaDBManager) RestoreDatabase(_ context.Context, _ string, _ []byte) error {
	return nil
}

func (fakeMariaDBManager) UpdateDatabase(context.Context, string, mariadb.UpdateDatabaseInput) (mariadb.DatabaseRecord, error) {
	return mariadb.DatabaseRecord{
		Name:     "flowpanel",
		Username: "flowpanel_user",
		Host:     "localhost",
	}, nil
}

func (fakeMariaDBManager) DeleteDatabase(context.Context, string, mariadb.DeleteDatabaseInput) error {
	return nil
}

func (fakePHPMyAdminManager) Status(context.Context) phpmyadmin.Status {
	return phpmyadmin.Status{
		Installed:        true,
		InstallPath:      "/usr/share/phpmyadmin",
		State:            "installed",
		Message:          "phpMyAdmin is installed.",
		InstallAvailable: false,
	}
}

func (fakePHPMyAdminManager) Install(context.Context) error {
	return nil
}

func (m *installablePHPMyAdminManager) Status(context.Context) phpmyadmin.Status {
	return m.status
}

func (m *installablePHPMyAdminManager) Install(context.Context) error {
	m.status.Installed = true
	m.status.State = "installed"
	m.status.Message = "phpMyAdmin is installed."
	return nil
}

func TestPHPMyAdminInstallSyncsCaddy(t *testing.T) {
	t.Helper()

	installPath := t.TempDir()
	themesDir := filepath.Join(installPath, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatalf("mkdir themes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(themesDir, "test.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	phpMyAdminAddr := reserveTCPAddress(t)
	manager := &installablePHPMyAdminManager{
		status: phpmyadmin.Status{
			InstallPath: installPath,
		},
	}
	runtime := caddy.NewRuntime(zap.NewNop(), ":0", ":0", fakePHPManager{}, manager, phpMyAdminAddr)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start caddy runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Stop(context.Background())
	})
	cfg := config.Config{
		Env:             "test",
		AdminListenAddr: ":18080",
		PublicHTTPAddr:  ":0",
		PublicHTTPSAddr: ":0",
		PHPMyAdminAddr:  phpMyAdminAddr,
		Session: config.SessionConfig{
			Secret:     strings.Repeat("s", 32),
			CookieName: "flowpanel_test",
			Lifetime:   time.Hour,
		},
		Cron: config.CronConfig{
			Enabled: false,
		},
	}

	dbConn, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	store := domain.NewStore(dbConn)
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	cronStore := flowcron.NewStore(dbConn)
	if err := cronStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure cron store: %v", err)
	}
	domains := domain.NewService(store)

	router, err := NewRouter(&app.App{
		Config:     cfg,
		Logger:     zap.NewNop(),
		DB:         dbConn,
		Domains:    domains,
		Sessions:   auth.NewSessionManager(cfg),
		Cron:       flowcron.NewScheduler(zap.NewNop(), false, cronStore),
		Caddy:      runtime,
		PHP:        fakePHPManager{},
		PHPMyAdmin: manager,
	})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/phpmyadmin/install", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + phpMyAdminAddr + "/themes/test.css")
	if err != nil {
		t.Fatalf("request phpmyadmin asset: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestPHPMyAdminRedirectSyncsCaddyForManualInstall(t *testing.T) {
	t.Helper()

	installPath := t.TempDir()
	themesDir := filepath.Join(installPath, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatalf("mkdir themes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(themesDir, "test.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	phpMyAdminAddr := reserveTCPAddress(t)
	manager := &installablePHPMyAdminManager{
		status: phpmyadmin.Status{
			Installed:   true,
			InstallPath: installPath,
			State:       "installed",
			Message:     "phpMyAdmin is installed.",
		},
	}
	runtime := caddy.NewRuntime(zap.NewNop(), ":0", ":0", fakePHPManager{}, manager, phpMyAdminAddr)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start caddy runtime: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Stop(context.Background())
	})

	cfg := config.Config{
		Env:             "test",
		AdminListenAddr: ":18080",
		PublicHTTPAddr:  ":0",
		PublicHTTPSAddr: ":0",
		PHPMyAdminAddr:  phpMyAdminAddr,
		Session: config.SessionConfig{
			Secret:     strings.Repeat("s", 32),
			CookieName: "flowpanel_test",
			Lifetime:   time.Hour,
		},
		Cron: config.CronConfig{
			Enabled: false,
		},
	}

	dbConn, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	store := domain.NewStore(dbConn)
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	cronStore := flowcron.NewStore(dbConn)
	if err := cronStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure cron store: %v", err)
	}
	domains := domain.NewService(store)

	router, err := NewRouter(&app.App{
		Config:     cfg,
		Logger:     zap.NewNop(),
		DB:         dbConn,
		Domains:    domains,
		Sessions:   auth.NewSessionManager(cfg),
		Cron:       flowcron.NewScheduler(zap.NewNop(), false, cronStore),
		Caddy:      runtime,
		PHP:        fakePHPManager{},
		PHPMyAdmin: manager,
	})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(server.URL + "/phpmyadmin/themes/test.css")
	if err != nil {
		t.Fatalf("request phpmyadmin asset through redirect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestCreateDomainRollsBackWhenPublishFails(t *testing.T) {
	router, domains, store := newTestDomainRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/domains", strings.NewReader(`{"hostname":"app.example.com","kind":"App","target":"3000"}`))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "failed to publish domain" {
		t.Fatalf("error = %q, want failed to publish domain", payload["error"])
	}

	if got := domains.List(); len(got) != 0 {
		t.Fatalf("domain count after failed publish = %d, want 0", len(got))
	}

	persisted, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list persisted domains: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("persisted domain count after failed publish = %d, want 0", len(persisted))
	}
}

func TestPHPMyAdminExternalURLUsesRequestHostForWildcardListenAddr(t *testing.T) {
	target, err := phpMyAdminExternalURL(":32109", "panel.example.test:8080", "/index.php")
	if err != nil {
		t.Fatalf("phpMyAdminExternalURL(): %v", err)
	}

	if got := target.String(); got != "http://panel.example.test:32109/index.php" {
		t.Fatalf("target = %q, want http://panel.example.test:32109/index.php", got)
	}
}

func TestUpdateDomainRollsBackWhenPublishFails(t *testing.T) {
	router, domains, store := newTestDomainRouter(t)

	record, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "app.example.com",
		Kind:     domain.KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/domains/"+record.ID, strings.NewReader(`{"hostname":"app.example.com","kind":"Reverse proxy","target":"https://backend.example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "failed to update domain" {
		t.Fatalf("error = %q, want failed to update domain", payload["error"])
	}

	current := domains.List()
	if len(current) != 1 {
		t.Fatalf("domain count after failed update = %d, want 1", len(current))
	}
	assertDomainRecordEqual(t, current[0], record)

	persisted, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list persisted domains: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted domain count after failed update = %d, want 1", len(persisted))
	}
	assertDomainRecordEqual(t, persisted[0], record)
}

func TestUpdateDomainRejectsHostnameChange(t *testing.T) {
	router, domains, _ := newTestDomainRouter(t)

	record, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "app.example.com",
		Kind:     domain.KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/domains/"+record.ID, strings.NewReader(`{"hostname":"proxy.example.com","kind":"Reverse proxy","target":"https://backend.example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload struct {
		Error       string            `json:"error"`
		FieldErrors map[string]string `json:"field_errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != "validation failed" {
		t.Fatalf("error = %q, want validation failed", payload.Error)
	}
	if payload.FieldErrors["hostname"] != "Domain cannot be changed after creation." {
		t.Fatalf("hostname validation = %q, want immutable domain message", payload.FieldErrors["hostname"])
	}
}

func TestDeleteDomainRollsBackWhenPublishFails(t *testing.T) {
	router, domains, store := newTestDomainRouter(t)

	record, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "app.example.com",
		Kind:     domain.KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/domains/"+record.ID, nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "failed to delete domain" {
		t.Fatalf("error = %q, want failed to delete domain", payload["error"])
	}

	current := domains.List()
	if len(current) != 1 {
		t.Fatalf("domain count after failed delete = %d, want 1", len(current))
	}
	assertDomainRecordEqual(t, current[0], record)

	persisted, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list persisted domains: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted domain count after failed delete = %d, want 1", len(persisted))
	}
	assertDomainRecordEqual(t, persisted[0], record)
}

func TestDomainPreviewEndpointServesCachedThumbnail(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("FLOWPANEL_DOMAIN_PREVIEW_CACHE_PATH", cacheDir)

	router, domains, _ := newTestDomainRouter(t)
	requestCount := 0
	domains.SetPreviewGenerator(previewGeneratorFunc(func(context.Context, string) ([]byte, error) {
		requestCount++
		return []byte("preview-image"), nil
	}))
	if _, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "preview.example.com",
		Kind:     domain.KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, httptest.NewRequest(http.MethodGet, "/api/domains/preview.example.com/preview", nil))

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body = %s", firstRecorder.Code, http.StatusOK, firstRecorder.Body.String())
	}
	if firstRecorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content-type = %q, want image/png", firstRecorder.Header().Get("Content-Type"))
	}
	if firstRecorder.Body.String() != "preview-image" {
		t.Fatalf("preview body = %q, want preview-image", firstRecorder.Body.String())
	}

	secondRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondRecorder, httptest.NewRequest(http.MethodGet, "/api/domains/preview.example.com/preview", nil))

	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d, body = %s", secondRecorder.Code, http.StatusOK, secondRecorder.Body.String())
	}
	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1", requestCount)
	}
}

func TestDomainPreviewEndpointRefreshesWhenRequested(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("FLOWPANEL_DOMAIN_PREVIEW_CACHE_PATH", cacheDir)

	router, domains, _ := newTestDomainRouter(t)
	requestCount := 0
	domains.SetPreviewGenerator(previewGeneratorFunc(func(_ context.Context, targetURL string) ([]byte, error) {
		requestCount++
		if requestCount == 1 {
			return []byte("preview-v1"), nil
		}
		if !strings.Contains(targetURL, "flowpanel_preview_refresh=") {
			t.Fatalf("forced refresh query missing: %q", targetURL)
		}
		return []byte("preview-v2"), nil
	}))
	if _, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "refresh.example.com",
		Kind:     domain.KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	firstRecorder := httptest.NewRecorder()
	router.ServeHTTP(firstRecorder, httptest.NewRequest(http.MethodGet, "/api/domains/refresh.example.com/preview", nil))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d, body = %s", firstRecorder.Code, http.StatusOK, firstRecorder.Body.String())
	}
	if firstRecorder.Body.String() != "preview-v1" {
		t.Fatalf("first preview body = %q, want preview-v1", firstRecorder.Body.String())
	}

	refreshRecorder := httptest.NewRecorder()
	router.ServeHTTP(refreshRecorder, httptest.NewRequest(http.MethodGet, "/api/domains/refresh.example.com/preview?refresh=1", nil))
	if refreshRecorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d, body = %s", refreshRecorder.Code, http.StatusOK, refreshRecorder.Body.String())
	}
	if refreshRecorder.Body.String() != "preview-v2" {
		t.Fatalf("refresh preview body = %q, want preview-v2", refreshRecorder.Body.String())
	}
	if requestCount != 2 {
		t.Fatalf("request count = %d, want 2", requestCount)
	}
}

func TestDomainPreviewEndpointReturnsBadGatewayWhenGenerationFails(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("FLOWPANEL_DOMAIN_PREVIEW_CACHE_PATH", cacheDir)

	router, domains, _ := newTestDomainRouter(t)
	domains.SetPreviewGenerator(previewGeneratorFunc(func(context.Context, string) ([]byte, error) {
		return nil, errors.New("generation failed")
	}))
	if _, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "preview.example.com",
		Kind:     domain.KindStaticSite,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/domains/preview.example.com/preview", nil))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "failed to load domain preview" {
		t.Fatalf("error = %q, want %q", payload["error"], "failed to load domain preview")
	}
}

func TestDomainComposerInstallHandlerRunsComposer(t *testing.T) {
	router, domains, _ := newTestDomainRouter(t)
	installFakeComposer(t, "#!/bin/sh\nif [ \"$1\" = \"install\" ]; then\ncat <<'EOF' > composer.lock\n{\"packages\":[{\"name\":\"laravel/framework\",\"version\":\"v11.0.0\"}],\"packages-dev\":[]}\nEOF\nexit 0\nfi\necho \"unexpected args: $*\" >&2\nexit 1\n")

	projectPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectPath, "composer.json"), []byte("{\"require\":{\"laravel/framework\":\"^11.0\"}}\n"), 0o644); err != nil {
		t.Fatalf("write composer.json: %v", err)
	}

	record := domain.Record{
		ID:        "example.com-1",
		Hostname:  "example.com",
		Kind:      domain.KindPHP,
		Target:    projectPath,
		CreatedAt: time.Now().UTC(),
	}
	if err := domains.Restore(context.Background(), record); err != nil {
		t.Fatalf("restore domain: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/domains/example.com/composer/install", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("ok = false, want true")
	}
	if _, err := os.Stat(filepath.Join(projectPath, "composer.lock")); err != nil {
		t.Fatalf("composer.lock missing after install: %v", err)
	}
}

func TestDomainLogsEndpointReturnsFilteredLogs(t *testing.T) {
	router, domains, _ := newTestDomainRouter(t)

	logsRoot := t.TempDir()
	domains.SetLogsBasePath(logsRoot)

	alpha, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "alpha.example.com",
		Kind:     domain.KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create alpha domain: %v", err)
	}
	beta, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "beta.example.com",
		Kind:     domain.KindApp,
		Target:   "3001",
	})
	if err != nil {
		t.Fatalf("create beta domain: %v", err)
	}

	if err := os.MkdirAll(alpha.Logs.Directory, 0o755); err != nil {
		t.Fatalf("mkdir alpha logs: %v", err)
	}
	if err := os.WriteFile(alpha.Logs.Access, []byte("{\"msg\":\"alpha request 1\"}\n{\"msg\":\"alpha request 2\"}\n"), 0o644); err != nil {
		t.Fatalf("write alpha access log: %v", err)
	}
	if err := os.WriteFile(alpha.Logs.Error, []byte("{\"msg\":\"alpha failed\"}\n"), 0o644); err != nil {
		t.Fatalf("write alpha error log: %v", err)
	}

	if err := os.MkdirAll(beta.Logs.Directory, 0o755); err != nil {
		t.Fatalf("mkdir beta logs: %v", err)
	}
	if err := os.WriteFile(beta.Logs.Access, []byte("{\"msg\":\"beta request\"}\n"), 0o644); err != nil {
		t.Fatalf("write beta access log: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/domains/logs?hostname=alpha.example.com&type=access&search=alpha&limit=1", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var payload struct {
		Hostnames []string `json:"hostnames"`
		Filters   struct {
			Hostname string `json:"hostname"`
			Type     string `json:"type"`
			Search   string `json:"search"`
			Limit    int    `json:"limit"`
		} `json:"filters"`
		Logs []struct {
			Hostname     string   `json:"hostname"`
			Type         string   `json:"type"`
			Path         string   `json:"path"`
			Available    bool     `json:"available"`
			TotalMatches int      `json:"total_matches"`
			Truncated    bool     `json:"truncated"`
			Lines        []string `json:"lines"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Hostnames) != 2 {
		t.Fatalf("hostname count = %d, want 2", len(payload.Hostnames))
	}
	if payload.Filters.Hostname != "alpha.example.com" {
		t.Fatalf("hostname filter = %q, want alpha.example.com", payload.Filters.Hostname)
	}
	if payload.Filters.Type != "access" {
		t.Fatalf("type filter = %q, want access", payload.Filters.Type)
	}
	if payload.Filters.Search != "alpha" {
		t.Fatalf("search filter = %q, want alpha", payload.Filters.Search)
	}
	if payload.Filters.Limit != 1 {
		t.Fatalf("limit filter = %d, want 1", payload.Filters.Limit)
	}
	if len(payload.Logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(payload.Logs))
	}

	logEntry := payload.Logs[0]
	if logEntry.Hostname != alpha.Hostname {
		t.Fatalf("hostname = %q, want %q", logEntry.Hostname, alpha.Hostname)
	}
	if logEntry.Type != "access" {
		t.Fatalf("type = %q, want access", logEntry.Type)
	}
	if logEntry.Path != alpha.Logs.Access {
		t.Fatalf("path = %q, want %q", logEntry.Path, alpha.Logs.Access)
	}
	if !logEntry.Available {
		t.Fatal("expected log to be available")
	}
	if logEntry.TotalMatches != 2 {
		t.Fatalf("total matches = %d, want 2", logEntry.TotalMatches)
	}
	if !logEntry.Truncated {
		t.Fatal("expected truncated log result")
	}
	if len(logEntry.Lines) != 1 || !strings.Contains(logEntry.Lines[0], "alpha request 2") {
		t.Fatalf("lines = %#v, want only the last matching log line", logEntry.Lines)
	}
}

func TestCronListCreateAndDeleteEndpoints(t *testing.T) {
	router, _, store := newTestCronRouter(t, false)

	initialRecorder := httptest.NewRecorder()
	router.ServeHTTP(initialRecorder, httptest.NewRequest(http.MethodGet, "/api/cron", nil))

	if initialRecorder.Code != http.StatusOK {
		t.Fatalf("initial status = %d, want %d", initialRecorder.Code, http.StatusOK)
	}

	var initialPayload struct {
		Enabled bool              `json:"enabled"`
		Started bool              `json:"started"`
		Jobs    []flowcron.Record `json:"jobs"`
	}
	if err := json.Unmarshal(initialRecorder.Body.Bytes(), &initialPayload); err != nil {
		t.Fatalf("decode initial response: %v", err)
	}
	if initialPayload.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if initialPayload.Started {
		t.Fatal("started = true, want false")
	}
	if len(initialPayload.Jobs) != 0 {
		t.Fatalf("job count = %d, want 0", len(initialPayload.Jobs))
	}
	if !strings.Contains(initialRecorder.Body.String(), `"jobs":[]`) {
		t.Fatalf("body = %s, want jobs to serialize as []", initialRecorder.Body.String())
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/cron", strings.NewReader(`{"name":"Warm cache","schedule":"*/15 * * * *","command":"echo cache"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createPayload struct {
		Job flowcron.Record `json:"job"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Job.Name != "Warm cache" {
		t.Fatalf("name = %q, want Warm cache", createPayload.Job.Name)
	}
	if createPayload.Job.Schedule != "*/15 * * * *" {
		t.Fatalf("schedule = %q, want */15 * * * *", createPayload.Job.Schedule)
	}

	updateRequest := httptest.NewRequest(http.MethodPut, "/api/cron/"+createPayload.Job.ID, strings.NewReader(`{"name":"Warm cache now","schedule":"0 * * * *","command":"echo cache-refresh"}`))
	updateRequest.Header.Set("Content-Type", "application/json")
	updateRecorder := httptest.NewRecorder()
	router.ServeHTTP(updateRecorder, updateRequest)

	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d, body = %s", updateRecorder.Code, http.StatusOK, updateRecorder.Body.String())
	}

	var updatePayload struct {
		Job flowcron.Record `json:"job"`
	}
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updatePayload); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updatePayload.Job.ID != createPayload.Job.ID {
		t.Fatalf("updated job id = %q, want %q", updatePayload.Job.ID, createPayload.Job.ID)
	}
	if updatePayload.Job.Name != "Warm cache now" {
		t.Fatalf("updated name = %q, want Warm cache now", updatePayload.Job.Name)
	}
	if updatePayload.Job.Schedule != "0 * * * *" {
		t.Fatalf("updated schedule = %q, want 0 * * * *", updatePayload.Job.Schedule)
	}
	if updatePayload.Job.Command != "echo cache-refresh" {
		t.Fatalf("updated command = %q, want echo cache-refresh", updatePayload.Job.Command)
	}

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/cron", nil))

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
	}

	var listPayload struct {
		Jobs []flowcron.Record `json:"jobs"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Jobs) != 1 {
		t.Fatalf("job count = %d, want 1", len(listPayload.Jobs))
	}
	if listPayload.Jobs[0].ID != createPayload.Job.ID {
		t.Fatalf("job id = %q, want %q", listPayload.Jobs[0].ID, createPayload.Job.ID)
	}
	if listPayload.Jobs[0].Name != "Warm cache now" {
		t.Fatalf("listed name = %q, want Warm cache now", listPayload.Jobs[0].Name)
	}

	persisted, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list persisted cron jobs: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted job count = %d, want 1", len(persisted))
	}
	if persisted[0].Name != "Warm cache now" {
		t.Fatalf("persisted name = %q, want Warm cache now", persisted[0].Name)
	}
	if persisted[0].Schedule != "0 * * * *" {
		t.Fatalf("persisted schedule = %q, want 0 * * * *", persisted[0].Schedule)
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/cron/"+createPayload.Job.ID, nil))

	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteRecorder.Code, http.StatusOK)
	}

	persisted, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("list persisted cron jobs after delete: %v", err)
	}
	if len(persisted) != 0 {
		t.Fatalf("persisted job count after delete = %d, want 0", len(persisted))
	}
}

func TestCronRunEndpointExecutesSavedCommand(t *testing.T) {
	router, _, store := newTestCronRouter(t, false)

	outputPath := filepath.Join(t.TempDir(), "cron-run.txt")
	command := "printf manual-run | tee " + outputPath
	requestBody := `{"name":"Manual trigger","schedule":"@daily","command":"` + command + `"}`

	createRequest := httptest.NewRequest(http.MethodPost, "/api/cron", strings.NewReader(requestBody))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createPayload struct {
		Job flowcron.Record `json:"job"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	runRecorder := httptest.NewRecorder()
	router.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/cron/"+createPayload.Job.ID+"/run", nil))

	if runRecorder.Code != http.StatusAccepted {
		t.Fatalf("run status = %d, want %d, body = %s", runRecorder.Code, http.StatusAccepted, runRecorder.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		content, err := os.ReadFile(outputPath)
		if err == nil {
			if string(content) != "manual-run" {
				t.Fatalf("manual run output = %q, want manual-run", string(content))
			}
			break
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read manual run output: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("manual run did not produce output at %s", outputPath)
		}

		time.Sleep(25 * time.Millisecond)
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		logs, err := store.ListExecutionLogs(context.Background(), 10)
		if err != nil {
			t.Fatalf("list execution logs: %v", err)
		}

		executions := logs[createPayload.Job.ID]
		if len(executions) > 0 {
			if executions[0].Status != "success" {
				t.Fatalf("execution status = %q, want success", executions[0].Status)
			}
			if executions[0].Output != "manual-run" {
				t.Fatalf("execution output = %q, want manual-run", executions[0].Output)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("manual run execution log was not persisted")
		}

		time.Sleep(25 * time.Millisecond)
	}

	deadline = time.Now().Add(2 * time.Second)
	for {
		listRecorder := httptest.NewRecorder()
		router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/cron", nil))

		if listRecorder.Code != http.StatusOK {
			t.Fatalf("list status = %d, want %d", listRecorder.Code, http.StatusOK)
		}

		var listPayload struct {
			Jobs []flowcron.Record `json:"jobs"`
		}
		if err := json.Unmarshal(listRecorder.Body.Bytes(), &listPayload); err != nil {
			t.Fatalf("decode cron list: %v", err)
		}

		if len(listPayload.Jobs) == 1 && len(listPayload.Jobs[0].Executions) > 0 {
			execution := listPayload.Jobs[0].Executions[0]
			if execution.Status != "success" {
				t.Fatalf("listed execution status = %q, want success", execution.Status)
			}
			if execution.Output != "manual-run" {
				t.Fatalf("listed execution output = %q, want manual-run", execution.Output)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("manual run execution log was not returned by the cron API")
		}

		time.Sleep(25 * time.Millisecond)
	}
}

func TestEventsEndpointReturnsRecordedMutations(t *testing.T) {
	router, _, _ := newTestCronRouter(t, false)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/cron", strings.NewReader(`{"name":"Nightly backup","schedule":"0 3 * * *","command":"echo backup"}`))
	createRequest.Header.Set("Content-Type", "application/json")

	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	eventsRecorder := httptest.NewRecorder()
	router.ServeHTTP(eventsRecorder, httptest.NewRequest(http.MethodGet, "/api/events?limit=10", nil))

	if eventsRecorder.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d, body = %s", eventsRecorder.Code, http.StatusOK, eventsRecorder.Body.String())
	}

	var payload struct {
		Events []struct {
			Category      string `json:"category"`
			Action        string `json:"action"`
			ResourceType  string `json:"resource_type"`
			ResourceLabel string `json:"resource_label"`
			Status        string `json:"status"`
			Message       string `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(eventsRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode events response: %v", err)
	}
	if len(payload.Events) == 0 {
		t.Fatal("event count = 0, want at least 1")
	}

	event := payload.Events[0]
	if event.Category != "cron" {
		t.Fatalf("category = %q, want cron", event.Category)
	}
	if event.Action != "create" {
		t.Fatalf("action = %q, want create", event.Action)
	}
	if event.ResourceType != "cron_job" {
		t.Fatalf("resource_type = %q, want cron_job", event.ResourceType)
	}
	if event.ResourceLabel != "Nightly backup" {
		t.Fatalf("resource_label = %q, want Nightly backup", event.ResourceLabel)
	}
	if event.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", event.Status)
	}
	if !strings.Contains(event.Message, "Created cron job") {
		t.Fatalf("message = %q, want create message", event.Message)
	}
}

func TestCronCreateEndpointValidatesInput(t *testing.T) {
	router, _, _ := newTestCronRouter(t, false)

	request := httptest.NewRequest(http.MethodPost, "/api/cron", strings.NewReader(`{"name":"","schedule":"bad cron","command":""}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var payload struct {
		Error       string            `json:"error"`
		FieldErrors map[string]string `json:"field_errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != "validation failed" {
		t.Fatalf("error = %q, want validation failed", payload.Error)
	}
	if payload.FieldErrors["name"] != "Name is required." {
		t.Fatalf("name error = %q, want required message", payload.FieldErrors["name"])
	}
	if payload.FieldErrors["schedule"] == "" {
		t.Fatal("schedule validation is empty")
	}
	if payload.FieldErrors["command"] != "Command is required." {
		t.Fatalf("command error = %q, want required message", payload.FieldErrors["command"])
	}
}

func TestBackupsCreateListDownloadAndDeleteEndpoints(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/backups", nil))

	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createPayload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create backup response: %v", err)
	}
	if createPayload.Backup.Name == "" {
		t.Fatal("backup name is empty")
	}
	if createPayload.Backup.Size <= 0 {
		t.Fatalf("backup size = %d, want positive value", createPayload.Backup.Size)
	}

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/backups", nil))

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	var listPayload struct {
		Backups []backup.Record `json:"backups"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list backups response: %v", err)
	}
	if len(listPayload.Backups) != 1 {
		t.Fatalf("backup count = %d, want 1", len(listPayload.Backups))
	}
	if listPayload.Backups[0].Name != createPayload.Backup.Name {
		t.Fatalf("listed backup name = %q, want %q", listPayload.Backups[0].Name, createPayload.Backup.Name)
	}

	downloadRecorder := httptest.NewRecorder()
	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/backups/"+createPayload.Backup.Name+"/download", nil)
	router.ServeHTTP(downloadRecorder, downloadRequest)

	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d, body = %s", downloadRecorder.Code, http.StatusOK, downloadRecorder.Body.String())
	}
	if disposition := downloadRecorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, createPayload.Backup.Name) {
		t.Fatalf("content-disposition = %q, want filename %q", disposition, createPayload.Backup.Name)
	}
	if downloadRecorder.Body.Len() == 0 {
		t.Fatal("download body is empty")
	}

	restoreRecorder := httptest.NewRecorder()
	restoreRequest := httptest.NewRequest(http.MethodPost, "/api/backups/"+createPayload.Backup.Name+"/restore", nil)
	router.ServeHTTP(restoreRecorder, restoreRequest)

	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want %d, body = %s", restoreRecorder.Code, http.StatusOK, restoreRecorder.Body.String())
	}

	var restorePayload struct {
		Restore backup.RestoreResult `json:"restore"`
	}
	if err := json.Unmarshal(restoreRecorder.Body.Bytes(), &restorePayload); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if len(restorePayload.Restore.RestoredDatabases) != 1 || restorePayload.Restore.RestoredDatabases[0] != "flowpanel" {
		t.Fatalf("restored databases = %v, want [flowpanel]", restorePayload.Restore.RestoredDatabases)
	}

	deleteRecorder := httptest.NewRecorder()
	router.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/backups/"+createPayload.Backup.Name, nil))

	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d, body = %s", deleteRecorder.Code, http.StatusOK, deleteRecorder.Body.String())
	}

	missingRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/backups/"+createPayload.Backup.Name+"/download", nil))

	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d, body = %s", missingRecorder.Code, http.StatusNotFound, missingRecorder.Body.String())
	}
}

func TestBackupsImportEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/backups", strings.NewReader(`{"include_panel_data":false,"include_sites":false,"include_databases":true,"database_names":["flowpanel"]}`)))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createPayload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	downloadRecorder := httptest.NewRecorder()
	router.ServeHTTP(downloadRecorder, httptest.NewRequest(http.MethodGet, "/api/backups/"+createPayload.Backup.Name+"/download", nil))
	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d, body = %s", downloadRecorder.Code, http.StatusOK, downloadRecorder.Body.String())
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("backup", "flowpanel-database-flowpanel-backup-imported.tar.gz")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(downloadRecorder.Body.Bytes()); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	importRequest := httptest.NewRequest(http.MethodPost, "/api/backups/import", &body)
	importRequest.Header.Set("Content-Type", writer.FormDataContentType())
	importRecorder := httptest.NewRecorder()
	router.ServeHTTP(importRecorder, importRequest)

	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("import status = %d, want %d, body = %s", importRecorder.Code, http.StatusCreated, importRecorder.Body.String())
	}

	var importPayload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(importRecorder.Body.Bytes(), &importPayload); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if importPayload.Backup.Name != "flowpanel-database-flowpanel-backup-imported.tar.gz" {
		t.Fatalf("imported name = %q, want imported archive name", importPayload.Backup.Name)
	}

	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/backups", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}

	var listPayload struct {
		Backups []backup.Record `json:"backups"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listPayload.Backups) != 2 {
		t.Fatalf("backup count = %d, want 2", len(listPayload.Backups))
	}
}

func TestBackupsEndpointsHandleImportedNamesWithSpaces(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	createRequest := httptest.NewRequest(http.MethodPost, "/api/backups", strings.NewReader(`{"include_panel_data":false,"include_sites":false,"include_databases":true,"database_names":["flowpanel"]}`))
	createRequest.Header.Set("Content-Type", "application/json")
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createRequest)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", createRecorder.Code, http.StatusCreated, createRecorder.Body.String())
	}

	var createPayload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	downloadSourceRecorder := httptest.NewRecorder()
	router.ServeHTTP(downloadSourceRecorder, httptest.NewRequest(http.MethodGet, "/api/backups/"+createPayload.Backup.Name+"/download", nil))
	if downloadSourceRecorder.Code != http.StatusOK {
		t.Fatalf("download source status = %d, want %d, body = %s", downloadSourceRecorder.Code, http.StatusOK, downloadSourceRecorder.Body.String())
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	importedName := "flowpanel-database-flowpanel-backup-20260402-165246 (1).tar.gz"
	part, err := writer.CreateFormFile("backup", importedName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(downloadSourceRecorder.Body.Bytes()); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	importRequest := httptest.NewRequest(http.MethodPost, "/api/backups/import", &body)
	importRequest.Header.Set("Content-Type", writer.FormDataContentType())
	importRecorder := httptest.NewRecorder()
	router.ServeHTTP(importRecorder, importRequest)
	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("import status = %d, want %d, body = %s", importRecorder.Code, http.StatusCreated, importRecorder.Body.String())
	}

	downloadImportedRecorder := httptest.NewRecorder()
	router.ServeHTTP(downloadImportedRecorder, httptest.NewRequest(http.MethodGet, "/api/backups/"+url.PathEscape(importedName)+"/download", nil))
	if downloadImportedRecorder.Code != http.StatusOK {
		t.Fatalf("download imported status = %d, want %d, body = %s", downloadImportedRecorder.Code, http.StatusOK, downloadImportedRecorder.Body.String())
	}

	restoreRecorder := httptest.NewRecorder()
	router.ServeHTTP(restoreRecorder, httptest.NewRequest(http.MethodPost, "/api/backups/"+url.PathEscape(importedName)+"/restore", nil))
	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("restore status = %d, want %d, body = %s", restoreRecorder.Code, http.StatusOK, restoreRecorder.Body.String())
	}
}

func TestBackupsCreateEndpointValidatesScope(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	request := httptest.NewRequest(http.MethodPost, "/api/backups", strings.NewReader(`{"include_panel_data":false,"include_sites":false,"include_databases":false}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}

	var payload struct {
		Error       string            `json:"error"`
		FieldErrors map[string]string `json:"field_errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != "validation failed" {
		t.Fatalf("error = %q, want validation failed", payload.Error)
	}
	if payload.FieldErrors["scope"] != "Select at least one backup source." {
		t.Fatalf("scope error = %q, want selection message", payload.FieldErrors["scope"])
	}
}

func TestBackupsCreateEndpointAcceptsDatabaseNames(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	request := httptest.NewRequest(http.MethodPost, "/api/backups", strings.NewReader(`{"include_panel_data":false,"include_sites":false,"include_databases":true,"database_names":["flowpanel"]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var payload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(payload.Backup.Name, "database-flowpanel-backup") {
		t.Fatalf("backup name = %q, want database-specific prefix", payload.Backup.Name)
	}
}

func TestBackupsCreateEndpointAcceptsSiteHostnames(t *testing.T) {
	router, domains, _ := newTestDomainRouter(t)

	siteRoot := filepath.Join(t.TempDir(), "example.com")
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		t.Fatalf("create site root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteRoot, "index.html"), []byte("site"), 0o644); err != nil {
		t.Fatalf("write site file: %v", err)
	}
	if _, err := domains.Create(context.Background(), domain.CreateInput{
		Hostname: "example.com",
		Kind:     domain.KindStaticSite,
		Target:   siteRoot,
	}); err != nil {
		t.Fatalf("create domain: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/backups", strings.NewReader(`{"include_panel_data":false,"include_sites":true,"include_databases":false,"site_hostnames":["example.com"]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}

	var payload struct {
		Backup backup.Record `json:"backup"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(payload.Backup.Name, "site-example.com-backup") {
		t.Fatalf("backup name = %q, want site-specific prefix", payload.Backup.Name)
	}
}

func TestWriteBackupErrorUsesConcreteMessageForUnexpectedError(t *testing.T) {
	recorder := httptest.NewRecorder()

	writeBackupError(recorder, errors.New("site base path is not configured"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != "site base path is not configured" {
		t.Fatalf("error = %q, want concrete error message", payload.Error)
	}
}

func TestSystemStatusEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/system", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		System struct {
			Cores int `json:"cores"`
		} `json:"system"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.System.Cores <= 0 {
		t.Fatalf("cores = %d, want positive value", payload.System.Cores)
	}
}

func TestMariaDBStatusEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/mariadb", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		MariaDB struct {
			Ready         bool   `json:"ready"`
			ListenAddress string `json:"listen_address"`
			Product       string `json:"product"`
		} `json:"mariadb"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.MariaDB.Ready {
		t.Fatal("ready = false, want true")
	}
	if payload.MariaDB.Product != "MariaDB" {
		t.Fatalf("product = %q, want MariaDB", payload.MariaDB.Product)
	}
	if payload.MariaDB.ListenAddress != "127.0.0.1:3306" {
		t.Fatalf("listen_address = %q, want 127.0.0.1:3306", payload.MariaDB.ListenAddress)
	}
}

func TestMariaDBRootPasswordEndpoint(t *testing.T) {
	t.Setenv("FLOWPANEL_MARIADB_PASSWORD", "super-secret-root")
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/mariadb/root-password", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		RootPassword string `json:"root_password"`
		Configured   bool   `json:"configured"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Configured {
		t.Fatal("configured = false, want true")
	}
	if payload.RootPassword != "super-secret-root" {
		t.Fatalf("root_password = %q, want super-secret-root", payload.RootPassword)
	}
}

func TestMariaDBRootPasswordUpdateEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/mariadb/root-password", strings.NewReader(`{"password":"new-secret-root-01"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		RootPassword string `json:"root_password"`
		Configured   bool   `json:"configured"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Configured {
		t.Fatal("configured = false, want true")
	}
	if payload.RootPassword != "new-secret-root-01" {
		t.Fatalf("root_password = %q, want new-secret-root-01", payload.RootPassword)
	}
}

func TestMariaDBInstallEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/mariadb/install", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		MariaDB struct {
			Product string `json:"product"`
		} `json:"mariadb"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.MariaDB.Product != "MariaDB" {
		t.Fatalf("product = %q, want MariaDB", payload.MariaDB.Product)
	}
}

func TestMariaDBDatabasesListEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/mariadb/databases", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Databases []struct {
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"databases"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Databases) != 1 {
		t.Fatalf("database count = %d, want 1", len(payload.Databases))
	}
	if payload.Databases[0].Name != "flowpanel" {
		t.Fatalf("name = %q, want flowpanel", payload.Databases[0].Name)
	}
}

func TestMariaDBDatabaseBackupEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/mariadb/databases/flowpanel/backup", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "flowpanel-") || !strings.Contains(disposition, ".sql") {
		t.Fatalf("content-disposition = %q, want sql filename for flowpanel", disposition)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "CREATE DATABASE `flowpanel`;") {
		t.Fatalf("body = %q, want database dump", body)
	}
}

func TestMariaDBCreateDatabaseEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/mariadb/databases", strings.NewReader(`{"name":"flowpanel","username":"flowpanel_user","password":"secret123"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}

	var payload struct {
		Database struct {
			Name string `json:"name"`
		} `json:"database"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Database.Name != "flowpanel" {
		t.Fatalf("name = %q, want flowpanel", payload.Database.Name)
	}
}

func TestMariaDBUpdateDatabaseEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/mariadb/databases/flowpanel", strings.NewReader(`{"current_username":"flowpanel_user","username":"flowpanel_user","password":"secret123"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestMariaDBDeleteDatabaseEndpoint(t *testing.T) {
	router, _, _ := newTestDomainRouter(t)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/mariadb/databases/flowpanel?username=flowpanel_user", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestNewPanelHandlerRejectsMissingReferencedAsset(t *testing.T) {
	_, err := newPanelHandlerWithFS(fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><html><head><script type="module" src="/assets/index.js"></script></head><body><div id="root"></div></body></html>`),
		},
	})
	if err == nil {
		t.Fatal("expected error for missing referenced asset")
	}
	if !strings.Contains(err.Error(), "assets/index.js") {
		t.Fatalf("error = %q, want missing asset path", err)
	}
}

func TestPanelHandlerDoesNotFallbackToIndexForMissingAssets(t *testing.T) {
	handler, err := newPanelHandlerWithFS(fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><html><head><link rel="stylesheet" href="/assets/index.css"><script type="module" src="/assets/index.js"></script></head><body><div id="root"></div></body></html>`),
		},
		"assets/index.css": {
			Data: []byte("body { background: #fff; }"),
		},
		"assets/index.js": {
			Data: []byte("console.log('ok')"),
		},
	})
	if err != nil {
		t.Fatalf("new panel handler: %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestPanelHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	handler, err := newPanelHandlerWithFS(fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><html><head><link rel="stylesheet" href="/assets/index.css"><script type="module" src="/assets/index.js"></script></head><body><div id="root"></div></body></html>`),
		},
		"assets/index.css": {
			Data: []byte("body { background: #fff; }"),
		},
		"assets/index.js": {
			Data: []byte("console.log('ok')"),
		},
	})
	if err != nil {
		t.Fatalf("new panel handler: %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/domains", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("body = %q, want index html", recorder.Body.String())
	}
}

func TestPanelHandlerFallsBackToIndexForClientRoutesWithDots(t *testing.T) {
	handler, err := newPanelHandlerWithFS(fstest.MapFS{
		"index.html": {
			Data: []byte(`<!doctype html><html><head><link rel="stylesheet" href="/assets/index.css"><script type="module" src="/assets/index.js"></script></head><body><div id="root"></div></body></html>`),
		},
		"assets/index.css": {
			Data: []byte("body { background: #fff; }"),
		},
		"assets/index.js": {
			Data: []byte("console.log('ok')"),
		},
	})
	if err != nil {
		t.Fatalf("new panel handler: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/domains/home.mzgs.net-1775070267872817000", nil)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("body = %q, want index html", recorder.Body.String())
	}
}

func newTestDomainRouter(t *testing.T) (http.Handler, *domain.Service, *domain.Store) {
	t.Helper()

	cfg := config.Config{
		Env:             "test",
		AdminListenAddr: ":18080",
		PublicHTTPAddr:  ":19080",
		PublicHTTPSAddr: ":19443",
		PHPMyAdminAddr:  ":32109",
		Database: config.DatabaseConfig{
			Path: ":memory:",
		},
		Session: config.SessionConfig{
			Secret:     strings.Repeat("s", 32),
			CookieName: "flowpanel_test",
			Lifetime:   time.Hour,
		},
		Cron: config.CronConfig{
			Enabled: false,
		},
	}

	logger := zap.NewNop()
	dbConn, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	store := domain.NewStore(dbConn)
	if err := store.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure store: %v", err)
	}
	cronStore := flowcron.NewStore(dbConn)
	if err := cronStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure cron store: %v", err)
	}
	eventStore := events.NewStore(dbConn)
	if err := eventStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure event store: %v", err)
	}

	domains := domain.NewService(store)
	fileManager, err := files.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}
	backupManager := backup.NewService(logger.Named("backup"), t.TempDir(), filepath.Join(t.TempDir(), "backups"), cfg.Database.Path, dbConn, domains, fakeMariaDBManager{})

	router, err := NewRouter(app.New(
		cfg,
		logger,
		dbConn,
		domains,
		auth.NewSessionManager(cfg),
		flowcron.NewScheduler(logger.Named("cron"), false, cronStore),
		caddy.NewRuntime(
			logger.Named("caddy"),
			cfg.PublicHTTPAddr,
			cfg.PublicHTTPSAddr,
			fakePHPManager{},
			fakePHPMyAdminManager{},
			cfg.PHPMyAdminAddr,
		),
		fakeMariaDBManager{},
		fakePHPManager{},
		fakePHPMyAdminManager{},
		fileManager,
		events.NewService(logger.Named("events"), eventStore),
		backupManager,
	))
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	return router, domains, store
}

func installFakeComposer(t *testing.T, script string) string {
	t.Helper()

	binDir := t.TempDir()
	composerPath := filepath.Join(binDir, "composer")
	if err := os.WriteFile(composerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake composer: %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return composerPath
}

func newTestCronRouter(t *testing.T, enabled bool) (http.Handler, *flowcron.Scheduler, *flowcron.Store) {
	t.Helper()

	cfg := config.Config{
		Env:             "test",
		AdminListenAddr: ":18080",
		PublicHTTPAddr:  ":19080",
		PublicHTTPSAddr: ":19443",
		PHPMyAdminAddr:  ":32109",
		Database: config.DatabaseConfig{
			Path: ":memory:",
		},
		Session: config.SessionConfig{
			Secret:     strings.Repeat("s", 32),
			CookieName: "flowpanel_test",
			Lifetime:   time.Hour,
		},
		Cron: config.CronConfig{
			Enabled: enabled,
		},
	}

	logger := zap.NewNop()
	dbConn, err := db.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	domainStore := domain.NewStore(dbConn)
	if err := domainStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	cronStore := flowcron.NewStore(dbConn)
	if err := cronStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure cron store: %v", err)
	}
	eventStore := events.NewStore(dbConn)
	if err := eventStore.Ensure(context.Background()); err != nil {
		t.Fatalf("ensure event store: %v", err)
	}

	domains := domain.NewService(domainStore)
	fileManager, err := files.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}
	backupManager := backup.NewService(logger.Named("backup"), t.TempDir(), filepath.Join(t.TempDir(), "backups"), cfg.Database.Path, dbConn, domains, fakeMariaDBManager{})

	scheduler := flowcron.NewScheduler(logger.Named("cron"), enabled, cronStore)
	if err := scheduler.Load(context.Background()); err != nil {
		t.Fatalf("load cron scheduler: %v", err)
	}

	router, err := NewRouter(app.New(
		cfg,
		logger,
		dbConn,
		domains,
		auth.NewSessionManager(cfg),
		scheduler,
		caddy.NewRuntime(
			logger.Named("caddy"),
			cfg.PublicHTTPAddr,
			cfg.PublicHTTPSAddr,
			fakePHPManager{},
			fakePHPMyAdminManager{},
			cfg.PHPMyAdminAddr,
		),
		fakeMariaDBManager{},
		fakePHPManager{},
		fakePHPMyAdminManager{},
		fileManager,
		events.NewService(logger.Named("events"), eventStore),
		backupManager,
	))
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	return router, scheduler, cronStore
}

func assertDomainRecordEqual(t *testing.T, got domain.Record, want domain.Record) {
	t.Helper()

	if got.ID != want.ID ||
		got.Hostname != want.Hostname ||
		got.Kind != want.Kind ||
		got.Target != want.Target ||
		got.CacheEnabled != want.CacheEnabled ||
		!got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("record = %#v, want %#v", got, want)
	}
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp address: %v", err)
	}
	defer listener.Close()

	return listener.Addr().String()
}
