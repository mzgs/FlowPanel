package settings

import (
	"context"
	"database/sql"
	"testing"

	"flowpanel/internal/db"
)

func TestStoreRoundTripUsesKeyValueTable(t *testing.T) {
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

	initial := Record{
		PanelName:   "Ops",
		PanelURL:    "https://panel.example.com",
		GitHubToken: "github_pat_test_token",
	}
	if err := store.Upsert(ctx, initial); err != nil {
		t.Fatalf("upsert record: %v", err)
	}

	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}

	if got != initial {
		t.Fatalf("record = %#v, want %#v", got, initial)
	}

	var keyCount int
	if err := sqliteDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings WHERE key LIKE 'panel.%'`).Scan(&keyCount); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if keyCount != len(panelSettingKeys) {
		t.Fatalf("panel key count = %d, want %d", keyCount, len(panelSettingKeys))
	}
}

func TestStoreEnsureMigratesLegacyPanelSettingsTable(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := db.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		_ = sqliteDB.Close()
	}()

	if _, err := sqliteDB.ExecContext(ctx, `
CREATE TABLE panel_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    panel_name TEXT NOT NULL
);
`); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	if _, err := sqliteDB.ExecContext(ctx, `
INSERT INTO panel_settings (
    id,
    panel_name
)
VALUES (1, ?)
`,
		"FlowPanel",
	); err != nil {
		t.Fatalf("seed legacy table: %v", err)
	}

	store := NewStore(sqliteDB)
	if err := store.Ensure(ctx); err != nil {
		t.Fatalf("ensure store: %v", err)
	}

	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("get migrated record: %v", err)
	}
	if got.PanelName != "FlowPanel" {
		t.Fatalf("panel name = %q, want FlowPanel", got.PanelName)
	}
	if got.GitHubToken != "" {
		t.Fatalf("github token = %q, want empty string", got.GitHubToken)
	}
	if got.PanelURL != "" {
		t.Fatalf("panel url = %q, want empty string", got.PanelURL)
	}

	var legacyCount int
	if err := sqliteDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table' AND name = ?
`, legacyPanelSettingsTable).Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy tables: %v", err)
	}
	if legacyCount != 0 {
		t.Fatalf("legacy table count = %d, want 0", legacyCount)
	}
}

func TestStoreGetReturnsNoRowsWhenPanelSettingsAreMissing(t *testing.T) {
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

	_, err = store.Get(ctx)
	if err != sql.ErrNoRows {
		t.Fatalf("get error = %v, want %v", err, sql.ErrNoRows)
	}
}
