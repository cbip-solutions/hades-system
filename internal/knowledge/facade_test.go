package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewIndexReturnsNonNil(t *testing.T) {
	db, _ := openTestIndex(t)
	idx := NewIndex(db)
	if idx == nil {
		t.Fatal("NewIndex returned nil; expected non-nil *Index")
	}
}

func TestNewIndexClosesOverDBHandle(t *testing.T) {
	db, _ := openTestIndex(t)
	doc := Doc{
		FilePath:     "/p/internal-platform-x/memory/wired.md",
		ProjectID:    "internal-platform-x",
		ProjectAlias: "internal-platform-x",
		FileType:     FileTypeMemory,
		Title:        "wired",
		ContentText:  "facade closes over db",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("IndexDoc seed: %v", err)
	}
	idx := NewIndex(db)
	res, err := idx.Query(context.Background(), Query{ProjectFilter: []string{"internal-platform-x"}, Limit: 10})
	if err != nil {
		t.Fatalf("Index.Query: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("Index.Query: len(res) = %d, want 1 (proves DB handle wired)", len(res))
	}
	if res[0].Doc.FilePath != doc.FilePath {
		t.Errorf("Index.Query: FilePath = %q, want %q", res[0].Doc.FilePath, doc.FilePath)
	}
}

func TestIndexQueryForwardsToExecute(t *testing.T) {
	db, _ := openTestIndex(t)
	docs := []Doc{
		{
			FilePath: "/p/internal-platform-x/memory/alpha.md", ProjectID: "internal-platform-x", ProjectAlias: "internal-platform-x",
			FileType: FileTypeMemory, Title: "alpha", ContentText: "hello world alpha",
			LastModified: time.Now().Add(-1 * time.Hour),
			LastIndexed:  time.Now(),
		},
		{
			FilePath: "/p/internal-platform-x/memory/beta.md", ProjectID: "internal-platform-x", ProjectAlias: "internal-platform-x",
			FileType: FileTypeMemory, Title: "beta", ContentText: "hello world beta",
			LastModified: time.Now().Add(-2 * time.Hour),
			LastIndexed:  time.Now(),
		},
	}
	for _, d := range docs {
		if err := IndexDoc(context.Background(), db, d); err != nil {
			t.Fatalf("seed IndexDoc %s: %v", d.FilePath, err)
		}
	}

	q := Query{FreeText: "hello", Limit: 10}
	want, err := Execute(context.Background(), db, q)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	idx := NewIndex(db)
	got, err := idx.Query(context.Background(), q)
	if err != nil {
		t.Fatalf("Index.Query: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Index.Query result count = %d, Execute = %d (forwarding mismatch)",
			len(got), len(want))
	}
	for i := range want {
		if got[i].Doc.FilePath != want[i].Doc.FilePath {
			t.Errorf("row %d: Index.Query FilePath = %q, Execute = %q",
				i, got[i].Doc.FilePath, want[i].Doc.FilePath)
		}
	}
}

func TestIndexQueryPropagatesError(t *testing.T) {
	db, _ := openTestIndex(t)
	idx := NewIndex(db)
	_, err := idx.Query(context.Background(), Query{Limit: -1})
	if err == nil {
		t.Fatal("Index.Query with Limit=-1: expected error, got nil")
	}
}

// TestIndexReindexNoSourcesIsNoOp asserts the spec line 4354 contract:
// a fresh *Index whose sources field is still nil (SetSources never
// called) MUST return nil from Reindex and MUST NOT mutate the index.
// Daemon doctor probes the missing-sources state separately; the façade
// just stays silent so partial bootstrap doesn't error.
func TestIndexReindexNoSourcesIsNoOp(t *testing.T) {
	db, _ := openTestIndex(t)

	doc := Doc{
		FilePath:     "/p/internal-platform-x/memory/preexisting.md",
		ProjectID:    "internal-platform-x",
		ProjectAlias: "internal-platform-x",
		FileType:     FileTypeMemory,
		Title:        "pre",
		ContentText:  "existed before reindex",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, doc); err != nil {
		t.Fatalf("seed: %v", err)
	}

	idx := NewIndex(db)
	if err := idx.Reindex(context.Background()); err != nil {
		t.Fatalf("Index.Reindex with no sources: got error %v, want nil (no-op)", err)
	}

	// Verify the seeded row is still present — Reindex with no sources
	// MUST NOT trigger ColdRebuild's wipe step.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("after no-op Reindex, count = %d, want 1 (existing row preserved)", count)
	}
}

func TestIndexReindexWithSourcesInvokesColdRebuild(t *testing.T) {
	db, _ := openTestIndex(t)

	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "memory.md")
	if err := os.WriteFile(mdPath, []byte("# rebuild me\n\nbody content"), 0o644); err != nil {
		t.Fatalf("write tmp .md: %v", err)
	}

	idx := NewIndex(db)
	idx.SetSources([]ScannerSource{
		{
			Root:         tmp,
			Kind:         FileTypeMemory,
			ProjectID:    "p-rebuild",
			ProjectAlias: "rebuild",
			Recursive:    true,
		},
	})

	if err := idx.Reindex(context.Background()); err != nil {
		t.Fatalf("Index.Reindex: %v", err)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`,
		mdPath,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("after Reindex with sources, indexed count for %s = %d, want 1",
			mdPath, count)
	}
}

func TestIndexReindexPropagatesColdRebuildError(t *testing.T) {
	db, _ := openTestIndex(t)
	idx := NewIndex(db)
	idx.SetSources([]ScannerSource{
		{
			Root:         t.TempDir(),
			Kind:         FileTypeMemory,
			ProjectID:    "p",
			ProjectAlias: "p",
		},
	})
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := idx.Reindex(context.Background())
	if err == nil {
		t.Fatal("Index.Reindex on closed DB: expected error, got nil (façade swallowed it)")
	}
}

// TestIndexSetSourcesReplacesPriorSources proves SetSources is a
// total-overwrite (vs. append). Each daemon bootstrap pass MUST be able
// to install a fresh slice without leaking the previous configuration.
func TestIndexSetSourcesReplacesPriorSources(t *testing.T) {
	db, _ := openTestIndex(t)
	idx := NewIndex(db)
	idx.SetSources([]ScannerSource{{Root: "/old", Kind: FileTypeMemory, ProjectID: "old", ProjectAlias: "old"}})
	idx.SetSources([]ScannerSource{{Root: "/new", Kind: FileTypeMemory, ProjectID: "new", ProjectAlias: "new"}})
	if len(idx.sources) != 1 {
		t.Fatalf("sources len = %d, want 1 (replaced not appended)", len(idx.sources))
	}
	if idx.sources[0].ProjectID != "new" {
		t.Errorf("sources[0].ProjectID = %q, want %q (replaced not appended)",
			idx.sources[0].ProjectID, "new")
	}
}

func TestIndexSetSourcesNilClearsSources(t *testing.T) {
	db, _ := openTestIndex(t)
	idx := NewIndex(db)
	idx.SetSources([]ScannerSource{{Root: "/x", Kind: FileTypeMemory, ProjectID: "x", ProjectAlias: "x"}})
	idx.SetSources(nil)
	if idx.sources != nil {
		t.Fatalf("after SetSources(nil), sources = %v, want nil", idx.sources)
	}
	// Reindex MUST be a no-op again per spec line 4354.
	if err := idx.Reindex(context.Background()); err != nil {
		t.Errorf("after SetSources(nil), Reindex: got error %v, want nil", err)
	}
}
