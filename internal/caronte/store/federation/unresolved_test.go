//go:build cgo

package federation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTestFedDB(t *testing.T) *WorkspaceFederationDB {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "workspace.db")
	db, err := Open(context.Background(), statePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.RegisterWorkspace(context.Background(), WorkspaceRow{
		WorkspaceID: "ws-u", OwningProject: "client-app",
		PolicyLocked: false, CreatedAt: 0, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	return db
}

func TestUnresolvedStoreInsertGetList(t *testing.T) {
	db := openTestFedDB(t)
	ctx := context.Background()
	store := db.UnresolvedStore()

	row := UnresolvedRow{
		WorkspaceID: "ws-u", CallID: "c1", CallRepo: "client-app",
		BaseURLRef: "UNKNOWN_URL", Reason: "no manifest entry",
		RecordedAt: 1_700_000_000_000_000_000,
	}
	if err := store.Insert(ctx, row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := db.GetUnresolved(ctx, "ws-u", "c1", "client-app")
	if err != nil {
		t.Fatalf("GetUnresolved: %v", err)
	}
	if got != row {
		t.Errorf("round-trip drift:\n got=%+v\nwant=%+v", got, row)
	}

	list, err := db.ListUnresolvedByWorkspace(ctx, "ws-u", 0)
	if err != nil {
		t.Fatalf("ListUnresolvedByWorkspace: %v", err)
	}
	if len(list) != 1 || list[0].CallID != "c1" {
		t.Errorf("list = %+v; want 1 row with CallID=c1", list)
	}
}

func TestUnresolvedStoreInsertReplacesOnConflict(t *testing.T) {
	db := openTestFedDB(t)
	ctx := context.Background()
	store := db.UnresolvedStore()

	row1 := UnresolvedRow{
		WorkspaceID: "ws-u", CallID: "c2", CallRepo: "client-app",
		BaseURLRef: "OLD", Reason: "old reason", RecordedAt: 1,
	}
	row2 := row1
	row2.BaseURLRef = "NEW"
	row2.Reason = "new reason"
	row2.RecordedAt = 2

	if err := store.Insert(ctx, row1); err != nil {
		t.Fatalf("Insert row1: %v", err)
	}
	if err := store.Insert(ctx, row2); err != nil {
		t.Fatalf("Insert row2 (conflict): %v", err)
	}
	got, err := db.GetUnresolved(ctx, "ws-u", "c2", "client-app")
	if err != nil {
		t.Fatalf("GetUnresolved: %v", err)
	}
	if got.BaseURLRef != "NEW" || got.Reason != "new reason" || got.RecordedAt != 2 {
		t.Errorf("ON CONFLICT REPLACE failed: %+v", got)
	}
}

func TestUnresolvedStoreGetNotFound(t *testing.T) {
	db := openTestFedDB(t)
	_, err := db.GetUnresolved(context.Background(), "ws-u", "nope", "client-app")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUnresolved(missing) = %v; want ErrNotFound", err)
	}
}
