package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowpanel/internal/app"
	"flowpanel/internal/auth"
	"flowpanel/internal/caddy"
	"flowpanel/internal/config"
	"flowpanel/internal/db"
	"flowpanel/internal/domain"
	"flowpanel/internal/jobs"
	"flowpanel/internal/phpenv"

	"go.uber.org/zap"
)

type fakePHPManager struct{}

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
	router, err := NewRouter(app.New(
		cfg,
		logger,
		dbConn,
		domains,
		auth.NewSessionManager(cfg),
		jobs.NewScheduler(logger.Named("jobs"), false),
		caddy.NewRuntime(logger.Named("caddy"), cfg.PublicHTTPAddr, cfg.PublicHTTPSAddr, fakePHPManager{}),
		fakePHPManager{},
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
