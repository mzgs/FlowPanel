package ftp

import (
	"context"
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
