package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/files"
	"flowpanel/internal/jobs"
	"flowpanel/internal/mariadb"
	"flowpanel/internal/phpenv"

	"go.uber.org/zap"
)

type fakePHPManager struct{}

type fakeMariaDBManager struct{}

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

func newTestDomainRouter(t *testing.T) (http.Handler, *domain.Service, *domain.Store) {
	t.Helper()

	cfg := config.Config{
		Env:             "test",
		AdminListenAddr: ":18080",
		PublicHTTPAddr:  ":19080",
		PublicHTTPSAddr: ":19443",
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

	domains := domain.NewService(store)
	fileManager, err := files.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}

	router, err := NewRouter(app.New(
		cfg,
		logger,
		dbConn,
		domains,
		auth.NewSessionManager(cfg),
		jobs.NewScheduler(logger.Named("jobs"), false),
		caddy.NewRuntime(logger.Named("caddy"), cfg.PublicHTTPAddr, cfg.PublicHTTPSAddr, fakePHPManager{}),
		fakeMariaDBManager{},
		fakePHPManager{},
		fileManager,
	))
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	return router, domains, store
}

func assertDomainRecordEqual(t *testing.T, got domain.Record, want domain.Record) {
	t.Helper()

	if got.ID != want.ID ||
		got.Hostname != want.Hostname ||
		got.Kind != want.Kind ||
		got.Target != want.Target ||
		!got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("record = %#v, want %#v", got, want)
	}
}
