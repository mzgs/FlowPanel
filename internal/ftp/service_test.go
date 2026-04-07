package ftp

import (
	"context"
	"path/filepath"
	"testing"

	"flowpanel/internal/db"
	"flowpanel/internal/domain"
)

func TestServiceProvisionResetAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	domains := domain.NewServiceWithBasePath(t.TempDir(), domainStore)
	record, err := domains.Create(ctx, domain.CreateInput{
		Hostname: "example.com",
		Kind:     domain.KindStaticSite,
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	service := NewService(ftpStore, domains)
	if err := service.ReconcileDomain(ctx, record); err != nil {
		t.Fatalf("reconcile domain ftp account: %v", err)
	}

	status, err := service.GetDomainStatus(ctx, record.ID)
	if err != nil {
		t.Fatalf("get domain ftp status: %v", err)
	}
	if !status.Supported {
		t.Fatal("ftp should be supported for static sites")
	}
	if status.Username != "example.com" {
		t.Fatalf("username = %q, want example.com", status.Username)
	}
	if status.Enabled {
		t.Fatal("ftp should be disabled by default")
	}
	if status.HasPassword {
		t.Fatal("ftp password should not exist before reset")
	}

	resetStatus, password, err := service.ResetPassword(ctx, record.ID)
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if password == "" {
		t.Fatal("expected generated password")
	}
	if !resetStatus.HasPassword {
		t.Fatal("ftp password should exist after reset")
	}

	updatedStatus, err := service.UpdateDomain(ctx, record.ID, UpdateInput{
		Username: "example.com",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("enable ftp account: %v", err)
	}
	if !updatedStatus.Enabled {
		t.Fatal("ftp should be enabled")
	}

	authStatus, ok, err := service.Authenticate(ctx, "example.com", password)
	if err != nil {
		t.Fatalf("authenticate ftp account: %v", err)
	}
	if !ok {
		t.Fatal("expected authentication to succeed")
	}
	if authStatus.RootPath != record.Target {
		t.Fatalf("root path = %q, want %q", authStatus.RootPath, record.Target)
	}
}

func TestServiceUpdateDomainSetsPasswordOnSave(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	domains := domain.NewServiceWithBasePath(t.TempDir(), domainStore)
	record, err := domains.Create(ctx, domain.CreateInput{
		Hostname: "example.com",
		Kind:     domain.KindStaticSite,
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	service := NewService(ftpStore, domains)
	status, err := service.UpdateDomain(ctx, record.ID, UpdateInput{
		Username: "example.com",
		Enabled:  true,
		Password: "MyCustomFTPPassword1",
	})
	if err != nil {
		t.Fatalf("update domain ftp account: %v", err)
	}
	if !status.Enabled {
		t.Fatal("ftp should be enabled")
	}
	if !status.HasPassword {
		t.Fatal("ftp password should be stored after save")
	}

	_, ok, err := service.Authenticate(ctx, "example.com", "MyCustomFTPPassword1")
	if err != nil {
		t.Fatalf("authenticate ftp account: %v", err)
	}
	if !ok {
		t.Fatal("expected authentication to succeed with saved password")
	}
}

func TestServiceUpdateDomainRequiresPasswordWhenEnablingWithoutOne(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	domains := domain.NewServiceWithBasePath(t.TempDir(), domainStore)
	record, err := domains.Create(ctx, domain.CreateInput{
		Hostname: "example.com",
		Kind:     domain.KindStaticSite,
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	service := NewService(ftpStore, domains)
	_, err = service.UpdateDomain(ctx, record.ID, UpdateInput{
		Username: "example.com",
		Enabled:  true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error type = %T, want ValidationErrors", err)
	}
	if validation["password"] == "" {
		t.Fatal("missing password validation error")
	}
}

func TestServiceReturnsUnsupportedForNonSiteDomains(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	domains := domain.NewServiceWithBasePath(t.TempDir(), domainStore)
	record, err := domains.Create(ctx, domain.CreateInput{
		Hostname: "app.example.com",
		Kind:     domain.KindApp,
		Target:   "3000",
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	service := NewService(ftpStore, domains)
	status, err := service.GetDomainStatus(ctx, record.ID)
	if err != nil {
		t.Fatalf("get domain ftp status: %v", err)
	}
	if status.Supported {
		t.Fatal("ftp should be unsupported for app domains")
	}
}

func TestServiceCreateAndUpdateManagedAccount(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	sitesBasePath := t.TempDir()
	domains := domain.NewServiceWithBasePath(sitesBasePath, domainStore)
	record, err := domains.Create(ctx, domain.CreateInput{
		Hostname: "example.com",
		Kind:     domain.KindStaticSite,
	})
	if err != nil {
		t.Fatalf("create domain: %v", err)
	}

	service := NewService(ftpStore, domains)
	account, err := service.CreateAccount(ctx, CreateAccountInput{
		Username: "deploy-user",
		Password: "MyCustomFTPPassword1",
		RootPath: "example.com/releases/current",
		DomainID: record.ID,
		Enabled:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("create ftp account: %v", err)
	}
	if account.DomainID != record.ID {
		t.Fatalf("domain id = %q, want %q", account.DomainID, record.ID)
	}
	if want := filepath.Join(sitesBasePath, "example.com", "releases", "current"); account.RootPath != want {
		t.Fatalf("root path = %q, want %q", account.RootPath, want)
	}
	if !account.Enabled {
		t.Fatal("expected account to be enabled")
	}

	updated, err := service.UpdateAccount(ctx, account.ID, UpdateAccountInput{
		Username: "deploy-user",
		RootPath: filepath.Join(sitesBasePath, "example.com", "public"),
		DomainID: record.ID,
		Enabled:  boolPtr(true),
	})
	if err != nil {
		t.Fatalf("update ftp account: %v", err)
	}
	if want := filepath.Join(sitesBasePath, "example.com", "public"); updated.RootPath != want {
		t.Fatalf("updated root path = %q, want %q", updated.RootPath, want)
	}

	status, ok, err := service.StatusForUsername(ctx, "deploy-user")
	if err != nil {
		t.Fatalf("status for username: %v", err)
	}
	if !ok {
		t.Fatal("expected status lookup to succeed")
	}
	if status.RootPath != updated.RootPath {
		t.Fatalf("status root path = %q, want %q", status.RootPath, updated.RootPath)
	}
}

func TestServiceRejectsManagedAccountRootOutsideSitesBasePath(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	domainStore := domain.NewStore(sqliteDB)
	if err := domainStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure domain store: %v", err)
	}
	ftpStore := NewStore(sqliteDB)
	if err := ftpStore.Ensure(ctx); err != nil {
		t.Fatalf("ensure ftp store: %v", err)
	}

	domains := domain.NewServiceWithBasePath(t.TempDir(), domainStore)
	service := NewService(ftpStore, domains)

	_, err = service.CreateAccount(ctx, CreateAccountInput{
		Username: "deploy-user",
		Password: "MyCustomFTPPassword1",
		RootPath: "../outside",
		Enabled:  boolPtr(true),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error type = %T, want ValidationErrors", err)
	}
	if validation["root_path"] == "" {
		t.Fatal("missing root_path validation error")
	}
}

func boolPtr(value bool) *bool {
	return &value
}
