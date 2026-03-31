package mariadb

import (
	"context"
	"testing"

	"flowpanel/internal/db"
)

func TestStoreRoundTrip(t *testing.T) {
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

	initial := DatabaseRecord{
		Name:     "flowpanel",
		Username: "flowpanel_user",
		Password: "secret123",
		Host:     localhostHost,
	}
	if err := store.Upsert(ctx, initial); err != nil {
		t.Fatalf("insert record: %v", err)
	}

	updated := DatabaseRecord{
		Name:     "flowpanel",
		Username: "flowpanel_owner",
		Password: "secret456",
		Host:     localhostHost,
	}
	if err := store.Upsert(ctx, updated); err != nil {
		t.Fatalf("update record: %v", err)
	}

	records, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}

	record, ok := records["flowpanel"]
	if !ok {
		t.Fatal("record not found")
	}
	if record != updated {
		t.Fatalf("record = %#v, want %#v", record, updated)
	}

	if err := store.Delete(ctx, updated.Name); err != nil {
		t.Fatalf("delete record: %v", err)
	}

	records, err = store.List(ctx)
	if err != nil {
		t.Fatalf("list records after delete: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("record count = %d, want 0", len(records))
	}
}
