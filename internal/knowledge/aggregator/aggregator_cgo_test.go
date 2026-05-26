//go:build cgo
// +build cgo

package aggregator

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestAggregatorCloseDelegatesToDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "agg-close.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	a, err := New(Options{
		DB:       db,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close on real-DB Aggregator: %v", err)
	}
}

func TestInitFailsOnClosedDB(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "agg-closed.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := Init(context.Background(), db); err == nil {
		t.Fatal("Init on closed DB succeeded; expected error")
	}
}

func TestInitPragmaCancellable(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "agg-cancel.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Init(ctx, db); err == nil {
		t.Fatal("Init with pre-cancelled ctx succeeded; expected error")
	}
}

func TestAggregatorDBAccessorNonNilBranch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "agg-db-accessor.db")
	db, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Init(context.Background(), db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a, err := New(Options{
		DB:       db,
		Embedder: newMockEmbedder(384),
		Store:    newMockStore(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.DB() == nil {
		t.Error("DB() with Options.DB=non-nil: want non-nil; got nil")
	}
	if a.DB() != db {
		t.Errorf("DB() pointer = %p; want %p (must be the same instance)", a.DB(), db)
	}
}

var _ = sql.Drivers
