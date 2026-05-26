//go:build cgo
// +build cgo

package parser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}

func TestIndexFileWritesNodes(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	idx := NewIndexer(p, s)
	ctx := context.Background()

	src := readFixture(t, "basic.go.txt")
	report, err := idx.IndexFile(ctx, "pkg/x/x.go", src)
	if err != nil {
		t.Fatalf("IndexFile: %v", err)
	}
	if report.Written == 0 {
		t.Fatal("first index wrote 0 nodes; want > 0")
	}
	if report.Skipped != 0 {
		t.Errorf("first index skipped %d; want 0 (nothing was in the store yet)", report.Skipped)
	}

	funcs, err := s.ListNodesByKind(ctx, store.KindFunction)
	if err != nil {
		t.Fatalf("ListNodesByKind: %v", err)
	}
	var foundRun bool
	for _, n := range funcs {
		if n.Name == "Run" {
			foundRun = true
		}
	}
	if !foundRun {
		t.Error("function Run not persisted to the store")
	}
}

func TestIndexFileSkipsUnchanged(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	idx := NewIndexer(p, s)
	ctx := context.Background()
	src := readFixture(t, "basic.go.txt")

	if _, err := idx.IndexFile(ctx, "pkg/x/x.go", src); err != nil {
		t.Fatalf("first IndexFile: %v", err)
	}
	report, err := idx.IndexFile(ctx, "pkg/x/x.go", src)
	if err != nil {
		t.Fatalf("second IndexFile: %v", err)
	}
	if report.Written != 0 {
		t.Errorf("re-index of unchanged source wrote %d nodes; want 0 (all skipped)", report.Written)
	}
	if report.Skipped == 0 {
		t.Error("re-index of unchanged source skipped 0; the content-hash skip did not fire")
	}
}

func TestIndexFileWritesOnlyChanged(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	idx := NewIndexer(p, s)
	ctx := context.Background()
	src := readFixture(t, "basic.go.txt")

	if _, err := idx.IndexFile(ctx, "pkg/x/x.go", src); err != nil {
		t.Fatalf("first IndexFile: %v", err)
	}

	edited := []byte(string(src) + "\n\nfunc Added() {}\n")
	report, err := idx.IndexFile(ctx, "pkg/x/x.go", edited)
	if err != nil {
		t.Fatalf("edited IndexFile: %v", err)
	}
	if report.Written != 1 {
		t.Errorf("edited re-index wrote %d; want 1 (only the appended Added)", report.Written)
	}
	if report.Skipped == 0 {
		t.Error("edited re-index skipped 0; unchanged symbols should be skipped")
	}
}

func TestIndexerSatisfiesIndexerSink(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	var _ IndexerSink = NewIndexerSink(p, s, func(string) ([]byte, error) { return nil, nil })
}

func TestIndexerSinkReindexReadsFile(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")
	reader := func(path string) ([]byte, error) { return src, nil }
	sink := NewIndexerSink(p, s, reader)
	if err := sink.Reindex("pkg/x/x.go"); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	funcs, _ := s.ListNodesByKind(context.Background(), store.KindFunction)
	if len(funcs) == 0 {
		t.Error("Reindex did not persist any function via the file reader")
	}
}

func TestIndexerDropTree(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	idx := NewIndexer(p, s)
	ctx := context.Background()
	src := readFixture(t, "basic.go.txt")

	if _, err := idx.ReindexIncremental(ctx, "pkg/x/x.go", nil, src); err != nil {
		t.Fatalf("ReindexIncremental: %v", err)
	}
	if p.cache().len() == 0 {
		t.Fatal("cache empty after ReindexIncremental; cannot test DropTree")
	}

	idx.DropTree("pkg/x/x.go")
	if _, ok := p.cache().get("pkg/x/x.go"); ok {
		t.Error("tree still present after DropTree; incremental cache was not cleared")
	}

	idx.DropTree("nonexistent.go")
}

func TestIndexerClose(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	idx := NewIndexer(p, s)
	ctx := context.Background()
	src := []byte("package x\nfunc F() {}\n")

	if _, err := idx.ReindexIncremental(ctx, "a.go", nil, src); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if p.cache().len() == 0 {
		t.Fatal("cache empty after seeding; cannot test Close")
	}
	idx.Close()
	if p.cache().len() != 0 {
		t.Errorf("cache len = %d after Close; want 0", p.cache().len())
	}
}

func TestIndexerSinkDeleteDropsTree(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	src := readFixture(t, "basic.go.txt")

	sink := NewIndexerSink(p, s, func(string) ([]byte, error) { return src, nil })

	if err := sink.Reindex("pkg/x/x.go"); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if p.cache().len() == 0 {
		t.Fatal("cache empty after Reindex; cannot test Delete")
	}

	if err := sink.Delete("pkg/x/x.go"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, ok := p.cache().get("pkg/x/x.go"); ok {
		t.Error("tree still present after sink.Delete; DropTree was not called")
	}
}

func TestIndexerSinkReindexReadError(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	readErr := fmt.Errorf("disk read failure")
	sink := NewIndexerSink(p, s, func(string) ([]byte, error) { return nil, readErr })
	err := sink.Reindex("pkg/x/x.go")
	if err == nil {
		t.Fatal("Reindex with failing reader returned nil error; want propagated error")
	}
	if !errors.Is(err, readErr) {
		t.Errorf("error = %v; want errors.Is(err, readErr)", err)
	}
}

func nodeCountForFile(t *testing.T, s *store.Store, filePath string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM graph_nodes WHERE file_path = ?`, filePath,
	).Scan(&n); err != nil {
		t.Fatalf("nodeCountForFile(%q): %v", filePath, err)
	}
	return n
}

func TestIndexerSinkDeleteSweepsStoreRows(t *testing.T) {
	s := newTestStore(t)
	p, _ := NewParser()
	ctx := context.Background()
	src := readFixture(t, "basic.go.txt")

	idx := NewIndexer(p, s)
	if _, err := idx.IndexFile(ctx, "pkg/x/x.go", src); err != nil {
		t.Fatalf("IndexFile (seed): %v", err)
	}

	if _, err := idx.IndexFile(ctx, "pkg/y/y.go", src); err != nil {
		t.Fatalf("IndexFile (other file): %v", err)
	}

	before := nodeCountForFile(t, s, "pkg/x/x.go")
	if before == 0 {
		t.Fatal("no nodes for pkg/x/x.go after IndexFile; cannot test Delete sweep")
	}
	otherBefore := nodeCountForFile(t, s, "pkg/y/y.go")
	if otherBefore == 0 {
		t.Fatal("no nodes for pkg/y/y.go after IndexFile; cannot test survivor check")
	}

	sink := NewIndexerSink(p, s, func(string) ([]byte, error) { return nil, nil })
	if err := sink.Delete("pkg/x/x.go"); err != nil {
		t.Fatalf("sink.Delete: %v", err)
	}

	if cnt := nodeCountForFile(t, s, "pkg/x/x.go"); cnt != 0 {
		t.Errorf("nodes for deleted file = %d; want 0 (row sweep did not run)", cnt)
	}

	if cnt := nodeCountForFile(t, s, "pkg/y/y.go"); cnt != otherBefore {
		t.Errorf("nodes for unrelated file = %d; want %d (survivor check failed)", cnt, otherBefore)
	}

	if _, ok := p.cache().get("pkg/x/x.go"); ok {
		t.Error("CST tree still present after sink.Delete; DropTree was not called")
	}
}
