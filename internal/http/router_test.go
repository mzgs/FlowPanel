package httpx

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	flowcron "flowpanel/internal/cron"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
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
	router, _, _ := newTestCronRouter(t, false)

	outputPath := filepath.Join(t.TempDir(), "cron-run.txt")
	command := "printf manual-run > " + outputPath
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
	))
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	return router, domains, store
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

	domains := domain.NewService(domainStore)
	fileManager, err := files.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("new file manager: %v", err)
	}

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
