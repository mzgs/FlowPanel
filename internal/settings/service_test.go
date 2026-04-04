package settings

import (
	"context"
	"testing"

	"flowpanel/internal/db"
)

func TestServiceUpdateNormalizesPanelURL(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	store := NewStore(sqliteDB)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	service := NewService(store)
	record, err := service.Update(ctx, UpdateInput{
		PanelName: "Ops",
		PanelURL:  "panel.mzgs.net",
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}

	if record.PanelURL != "https://panel.mzgs.net" {
		t.Fatalf("panel_url = %q, want https://panel.mzgs.net", record.PanelURL)
	}
}

func TestServiceUpdateRejectsPanelURLWithPath(t *testing.T) {
	service := NewService(nil)

	_, err := service.Update(context.Background(), UpdateInput{
		PanelName: "Ops",
		PanelURL:  "https://panel.mzgs.net/settings",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validation, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("error type = %T, want ValidationErrors", err)
	}
	if validation["panel_url"] == "" {
		t.Fatal("missing panel_url validation error")
	}
}
