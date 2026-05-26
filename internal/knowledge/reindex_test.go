package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestColdRebuildEmptySources(t *testing.T) {
	db, _ := openTestIndex(t)
	errs, err := ColdRebuild(context.Background(), db, nil)
	if err != nil {
		t.Fatalf("ColdRebuild(empty): %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected reindex errors on empty sources: %v", errs)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 0 {
		t.Errorf("empty-sources rebuild produced %d rows, want 0", count)
	}
}

func TestColdRebuildFromSourcesPopulatesIndex(t *testing.T) {
	db, _ := openTestIndex(t)
	root := makeScannerFixture(t)

	srcs := []ScannerSource{
		{Kind: FileTypeMemory, Root: filepath.Join(root, "claude-projects", "internal-platform-x", "memory"),
			ProjectID: "internal-platform-x", ProjectAlias: "internal-platform-x", Recursive: true},
		{Kind: FileTypeADR, Root: filepath.Join(root, "internal-platform-x", "docs", "decisions"),
			ProjectID: "internal-platform-x", ProjectAlias: "internal-platform-x", Recursive: true},
	}
	errs, err := ColdRebuild(context.Background(), db, srcs)
	if err != nil {
		t.Fatalf("ColdRebuild: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("unexpected reindex errors: %v", errs)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count < 5 {
		t.Errorf("indexed count = %d, want >= 5", count)
	}

	// Cross-check FTS5 row count parity — every meta row MUST have an
	// FTS5 row (rowid join contract per index.go). If they diverge, the
	// MATCH query in G-6 returns wrong results silently.
	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("count fts: %v", err)
	}
	if ftsCount != count {
		t.Errorf("FTS row count = %d, meta row count = %d; rowid join broken", ftsCount, count)
	}
}

func TestColdRebuildIdempotent(t *testing.T) {
	db, _ := openTestIndex(t)
	root := makeScannerFixture(t)
	srcs := []ScannerSource{
		{Kind: FileTypeMemory, Root: filepath.Join(root, "claude-projects", "internal-platform-x", "memory"),
			ProjectID: "internal-platform-x", Recursive: true},
	}
	if _, err := ColdRebuild(context.Background(), db, srcs); err != nil {
		t.Fatalf("first ColdRebuild: %v", err)
	}
	var c1 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&c1); err != nil {
		t.Fatalf("count meta #1: %v", err)
	}
	if c1 == 0 {
		t.Fatalf("first ColdRebuild produced 0 rows; idempotency check meaningless")
	}
	if _, err := ColdRebuild(context.Background(), db, srcs); err != nil {
		t.Fatalf("second ColdRebuild: %v", err)
	}
	var c2 int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta`).Scan(&c2); err != nil {
		t.Fatalf("count meta #2: %v", err)
	}
	if c1 != c2 {
		t.Errorf("idempotency broken: c1=%d c2=%d", c1, c2)
	}
}

func TestColdRebuildClearsBeforeReindex(t *testing.T) {
	db, _ := openTestIndex(t)

	stalePath := "/tmp/stale-cold-rebuild-fixture.md"
	stale := Doc{
		FilePath:     stalePath,
		ProjectID:    "p",
		FileType:     FileTypeMemory,
		Title:        "stale",
		ContentText:  "stale",
		LastModified: time.Now(),
		LastIndexed:  time.Now(),
	}
	if err := IndexDoc(context.Background(), db, stale); err != nil {
		t.Fatalf("seed stale row: %v", err)
	}
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, stalePath).Scan(&seeded); err != nil {
		t.Fatalf("verify seed: %v", err)
	}
	if seeded != 1 {
		t.Fatalf("seed verification: stale row count = %d, want 1", seeded)
	}

	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "fresh.md"), []byte("# fresh\n\nbody"), 0o644); err != nil {
		t.Fatalf("write fresh.md: %v", err)
	}
	srcs := []ScannerSource{{Kind: FileTypeMemory, Root: tmp, ProjectID: "p", Recursive: true}}
	if _, err := ColdRebuild(context.Background(), db, srcs); err != nil {
		t.Fatalf("ColdRebuild: %v", err)
	}

	var staleRemaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, stalePath).Scan(&staleRemaining); err != nil {
		t.Fatalf("count after rebuild: %v", err)
	}
	if staleRemaining != 0 {
		t.Errorf("stale row not cleared: count=%d", staleRemaining)
	}

	var freshCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`,
		filepath.Join(tmp, "fresh.md")).Scan(&freshCount); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if freshCount != 1 {
		t.Errorf("fresh row not indexed: count=%d", freshCount)
	}
}

func TestColdRebuildHonorsContextCancellation(t *testing.T) {
	db, _ := openTestIndex(t)
	root := makeScannerFixture(t)
	srcs := []ScannerSource{
		{Kind: FileTypeMemory, Root: filepath.Join(root, "claude-projects", "internal-platform-x", "memory"),
			ProjectID: "p", Recursive: true},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ColdRebuild(ctx, db, srcs)
	if err == nil {
		t.Fatalf("expected ctx-cancelled error, got nil")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected error to contain 'context canceled', got %v", err)
	}
}

// TestColdRebuildAccumulatesParseErrors verifies the soft-error contract
// for binary content. A binary `.md` file in the source dir is rejected
// by Parse with ErrBinaryContent; the rebuild MUST continue past it and
// the failure MUST appear in the returned []ReindexError.
func TestColdRebuildAccumulatesParseErrors(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte("# Good\n\nbody"), 0o644); err != nil {
		t.Fatalf("write good.md: %v", err)
	}

	binBytes := make([]byte, 4096)
	for i := range binBytes {
		binBytes[i] = 0x00
	}
	if err := os.WriteFile(filepath.Join(tmp, "binary.md"), binBytes, 0o644); err != nil {
		t.Fatalf("write binary.md: %v", err)
	}

	srcs := []ScannerSource{{Kind: FileTypeMemory, Root: tmp, ProjectID: "p", Recursive: true}}
	errs, err := ColdRebuild(context.Background(), db, srcs)
	if err != nil {
		t.Fatalf("ColdRebuild hard error: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 reindex error for binary.md, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Path, "binary.md") {
		t.Errorf("reindex error path = %q, want path containing binary.md", errs[0].Path)
	}
	if errs[0].Err == nil {
		t.Errorf("reindex error has nil Err")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`,
		filepath.Join(tmp, "good.md")).Scan(&count); err != nil {
		t.Fatalf("count good.md: %v", err)
	}
	if count != 1 {
		t.Errorf("good.md not indexed despite binary.md failure: count=%d", count)
	}
}

func TestColdRebuildAccumulatesScannerErrors(t *testing.T) {
	db, _ := openTestIndex(t)

	srcs := []ScannerSource{
		{Kind: FileTypeMemory, Root: "/nonexistent/cold-rebuild-fixture", ProjectID: "p", Recursive: true},
	}
	errs, err := ColdRebuild(context.Background(), db, srcs)
	if err != nil {
		t.Fatalf("ColdRebuild hard error: %v", err)
	}
	if len(errs) == 0 {
		t.Errorf("expected >=1 reindex error for missing root; got 0")
	}
}

func TestReindexErrorImplementsError(t *testing.T) {
	re := ReindexError{Path: "/tmp/x.md", Err: ErrBinaryContent}
	msg := re.Error()
	if !strings.Contains(msg, "/tmp/x.md") {
		t.Errorf("ReindexError message %q missing path", msg)
	}
	if !strings.Contains(msg, ErrBinaryContent.Error()) {
		t.Errorf("ReindexError message %q missing wrapped err", msg)
	}
}

func TestReindexErrorUnwrap(t *testing.T) {
	re := ReindexError{Path: "/tmp/x.md", Err: ErrBinaryContent}
	if !errors.Is(re, ErrBinaryContent) {
		t.Errorf("errors.Is(re, ErrBinaryContent) = false; Unwrap broken")
	}
	if errors.Is(re, ErrFileTooLarge) {
		t.Errorf("errors.Is(re, ErrFileTooLarge) = true; should be false")
	}

	if got := re.Unwrap(); got != ErrBinaryContent {
		t.Errorf("Unwrap() = %v, want ErrBinaryContent", got)
	}
}

func TestColdRebuildFiltersOversizeFromScannerErrors(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, "ok.md"), []byte("# ok\n\nbody"), 0o644); err != nil {
		t.Fatalf("write ok.md: %v", err)
	}
	big := strings.Repeat("x", 2048)
	if err := os.WriteFile(filepath.Join(tmp, "big.md"), []byte(big), 0o644); err != nil {
		t.Fatalf("write big.md: %v", err)
	}

	huge := strings.Repeat("y", 6*1024*1024)
	if err := os.WriteFile(filepath.Join(tmp, "huge.md"), []byte(huge), 0o644); err != nil {
		t.Fatalf("write huge.md: %v", err)
	}

	srcs := []ScannerSource{{Kind: FileTypeMemory, Root: tmp, ProjectID: "p", Recursive: true}}
	errs, err := ColdRebuild(context.Background(), db, srcs)
	if err != nil {
		t.Fatalf("ColdRebuild hard error: %v", err)
	}

	for _, re := range errs {
		if errors.Is(re.Err, ErrFileTooLarge) {
			t.Errorf("ErrFileTooLarge leaked into ReindexError slice: %v", re)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`,
		filepath.Join(tmp, "ok.md")).Scan(&count); err != nil {
		t.Fatalf("count ok.md: %v", err)
	}
	if count != 1 {
		t.Errorf("ok.md not indexed despite oversize sibling: count=%d", count)
	}
}

func TestColdRebuildIndexErrorAccumulates(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "bad.md"), []byte("# bad\n\nbody"), 0o644); err != nil {
		t.Fatalf("write bad.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte("# good\n\nbody"), 0o644); err != nil {
		t.Fatalf("write good.md: %v", err)
	}

	// Two separate sources — same Root, different Kind values. The
	// "bad" Kind violates the schema CHECK constraint, so Index will
	// reject it. The "good" Kind is valid. Order matters: with the
	// scanner's per-source order, files from the bad-Kind source come
	// first in the aggregated slice; the rebuild MUST continue past
	// the bad ones and still index the good ones.
	srcs := []ScannerSource{
		{Kind: FileType("not-a-real-kind"), Root: tmp, ProjectID: "p", Recursive: true},
		{Kind: FileTypeMemory, Root: tmp, ProjectID: "p", Recursive: true},
	}
	errs, err := ColdRebuild(context.Background(), db, srcs)
	if err != nil {
		t.Fatalf("ColdRebuild hard error: %v", err)
	}

	if len(errs) < 2 {
		t.Errorf("expected >=2 ReindexError entries (bad-Kind insertions), got %d: %v", len(errs), errs)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_type = ?`,
		string(FileTypeMemory)).Scan(&count); err != nil {
		t.Fatalf("count valid: %v", err)
	}
	if count != 2 {
		t.Errorf("valid-Kind rows = %d, want 2 (rebuild continued past Index failures)", count)
	}
}

func TestColdRebuildClearMetaErrorPropagates(t *testing.T) {
	db, _ := openTestIndex(t)
	if err := db.Close(); err != nil {
		t.Fatalf("pre-test close: %v", err)
	}

	_, err := ColdRebuild(context.Background(), db, nil)
	if err == nil {
		t.Fatalf("expected hard error after DB close, got nil")
	}
	if !strings.Contains(err.Error(), "knowledge: clear meta") {
		t.Errorf("expected 'knowledge: clear meta' wrapper, got %v", err)
	}
}

func TestColdRebuildMidRebuildContextCancellation(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()

	for i := 0; i < 50; i++ {
		p := filepath.Join(tmp, "f"+strings.Repeat("a", i+1)+".md")
		if err := os.WriteFile(p, []byte("# title\n\nbody"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	srcs := []ScannerSource{{Kind: FileTypeMemory, Root: tmp, ProjectID: "p", Recursive: true}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {

		time.Sleep(1 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	_, err := ColdRebuild(ctx, db, srcs)

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("expected nil or context.Canceled, got %v", err)
	}
}

func TestIncrementalUpdateInsertsNewFile(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "new.md")
	if err := os.WriteFile(p, []byte("# new\n\nbody"), 0o644); err != nil {
		t.Fatalf("write new.md: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         13,
		ModTime:      time.Now().UnixNano(),
	}
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	var ftsCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM knowledge_fts f
		JOIN knowledge_meta m ON m.rowid = f.rowid
		WHERE m.file_path = ?
	`, p).Scan(&ftsCount); err != nil {
		t.Fatalf("count fts join: %v", err)
	}
	if ftsCount != 1 {
		t.Errorf("fts join count = %d, want 1 (rowid join broken)", ftsCount)
	}

	var title string
	if err := db.QueryRow(`SELECT title FROM knowledge_meta WHERE file_path = ?`, p).Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "new" {
		t.Errorf("title = %q, want %q (H1 fallback)", title, "new")
	}
}

// TestIncrementalUpdateReplacesExistingFile verifies the upsert path: a
// second IncrementalUpdate against the SAME path with DIFFERENT content
// replaces the prior row in-place. The FTS5 row is updated atomically
// (rowid join preserved). Spec §G-10 acceptance: "modified file (upsert)".
//
// Load-bearing for the watcher hot path — every save in an editor produces
// an fsnotify Write event that lands here. The index MUST converge to the
// latest content without leaving stale rows behind, because hybrid query
// (G-6) returns rows by FilePath and stale duplicates would surface as
// phantom hits.
func TestIncrementalUpdateReplacesExistingFile(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "u.md")

	if err := os.WriteFile(p, []byte("# v1\n\noriginal"), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         15,
		ModTime:      time.Now().UnixNano(),
	}
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("first IncrementalUpdate: %v", err)
	}

	var rowidBefore int64
	if err := db.QueryRow(`SELECT rowid FROM knowledge_meta WHERE file_path = ?`, p).Scan(&rowidBefore); err != nil {
		t.Fatalf("rowid before: %v", err)
	}

	if err := os.WriteFile(p, []byte("# v2\n\nnew content"), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	sf.Size = 18
	sf.ModTime = time.Now().UnixNano()
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("second IncrementalUpdate: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 1 {
		t.Errorf("count after upsert = %d, want 1 (duplicates leaked)", count)
	}

	var content string
	if err := db.QueryRow(`
		SELECT f.content_text FROM knowledge_meta m
		JOIN knowledge_fts f ON f.rowid = m.rowid
		WHERE m.file_path = ?
	`, p).Scan(&content); err != nil {
		t.Fatalf("read content: %v", err)
	}
	if !strings.Contains(content, "new content") {
		t.Errorf("upsert did not replace content: %q", content)
	}
	if strings.Contains(content, "original") {
		t.Errorf("upsert leaked v1 content into v2 row: %q", content)
	}

	var title string
	if err := db.QueryRow(`SELECT title FROM knowledge_meta WHERE file_path = ?`, p).Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "v2" {
		t.Errorf("title after upsert = %q, want %q", title, "v2")
	}
}

// TestIncrementalUpdateIdempotent verifies running IncrementalUpdate twice
// against the SAME path with the SAME content produces no row drift.
// The watcher debounces aggressively (3s window), but a quiescent system
// can still receive a rare double-event for the same content (mtime not
// changing between two events of the same write); the index MUST tolerate
// this without thrashing.
func TestIncrementalUpdateIdempotent(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "idem.md")
	if err := os.WriteFile(p, []byte("# idem\n\nstable"), 0o644); err != nil {
		t.Fatalf("write idem.md: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         16,
		ModTime:      time.Now().UnixNano(),
	}
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("first IncrementalUpdate: %v", err)
	}
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("second IncrementalUpdate: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 1 {
		t.Errorf("idempotency broken: count = %d, want 1", count)
	}
}

// TestIncrementalUpdateParseError verifies the soft-error contract for
// the parse-failure case: a binary `.md` file (NUL-saturated head) is
// rejected by Parse with ErrBinaryContent. IncrementalUpdate MUST return
// the error wrapped with "knowledge: incremental parse:" so the watcher
// glue can log + classify.
//
// The index MUST remain unchanged — no half-inserted row, no FTS5 orphan.
func TestIncrementalUpdateParseError(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "binary.md")

	bin := make([]byte, 4096)
	for i := range bin {
		bin[i] = 0x00
	}
	if err := os.WriteFile(p, bin, 0o644); err != nil {
		t.Fatalf("write binary.md: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         4096,
		ModTime:      time.Now().UnixNano(),
	}
	err := IncrementalUpdate(context.Background(), db, sf)
	if err == nil {
		t.Fatalf("expected parse error for binary file, got nil")
	}
	if !errors.Is(err, ErrBinaryContent) {
		t.Errorf("expected errors.Is(err, ErrBinaryContent) = true, got %v", err)
	}
	if !strings.Contains(err.Error(), "incremental parse") {
		t.Errorf("expected error wrapper 'incremental parse', got %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 0 {
		t.Errorf("parse error left a row in the index: count = %d, want 0", count)
	}
}

func TestIncrementalUpdateReadError(t *testing.T) {
	db, _ := openTestIndex(t)
	sf := ScannedFile{
		Path:      filepath.Join(t.TempDir(), "does-not-exist.md"),
		Kind:      FileTypeMemory,
		ProjectID: "p",
		Size:      0,
		ModTime:   time.Now().UnixNano(),
	}
	err := IncrementalUpdate(context.Background(), db, sf)
	if err == nil {
		t.Fatalf("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "incremental parse") {
		t.Errorf("expected error wrapper 'incremental parse', got %v", err)
	}
}

func TestIncrementalUpdateIndexError(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "bad-kind.md")
	if err := os.WriteFile(p, []byte("# bad\n\nbody"), 0o644); err != nil {
		t.Fatalf("write bad-kind.md: %v", err)
	}

	sf := ScannedFile{
		Path:         p,
		Kind:         FileType("not-a-real-kind"),
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         12,
		ModTime:      time.Now().UnixNano(),
	}
	err := IncrementalUpdate(context.Background(), db, sf)
	if err == nil {
		t.Fatalf("expected index error for invalid Kind, got nil")
	}

	if strings.Contains(err.Error(), "incremental parse") {
		t.Errorf("did not expect 'incremental parse' wrapper for index-side error: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 0 {
		t.Errorf("index error left a row: count = %d, want 0", count)
	}
}

func TestIncrementalUpdateHonorsContext(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "ctx.md")
	if err := os.WriteFile(p, []byte("# ctx\n\nbody"), 0o644); err != nil {
		t.Fatalf("write ctx.md: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         12,
		ModTime:      time.Now().UnixNano(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := IncrementalUpdate(ctx, db, sf)
	if err == nil {
		t.Fatalf("expected ctx-cancelled error, got nil")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, p).Scan(&count); err != nil {
		t.Fatalf("count meta: %v", err)
	}
	if count != 0 {
		t.Errorf("ctx-cancelled IncrementalUpdate left a row: count = %d, want 0", count)
	}
}

func TestIncrementalUpdateRespectsInvZen130(t *testing.T) {
	db, _ := openTestIndex(t)
	tmp := t.TempDir()
	p := filepath.Join(tmp, "fm.md")
	body := "---\n" +
		"title: with-frontmatter\n" +
		"audit_chain_anchor: forbidden-anchor\n" +
		"ecosystem_join_keys: forbidden-keys\n" +
		"caronte_symbol_refs: forbidden-refs\n" +
		"---\n" +
		"# Body\n\ncontent"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write fm.md: %v", err)
	}
	sf := ScannedFile{
		Path:         p,
		Kind:         FileTypeMemory,
		ProjectID:    "p",
		ProjectAlias: "p",
		Size:         int64(len(body)),
		ModTime:      time.Now().UnixNano(),
	}
	if err := IncrementalUpdate(context.Background(), db, sf); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}

	// Extension-hook columns MUST be NULL even though the frontmatter
	// declared values for them.
	var anchor, keys, refs sql.NullString
	if err := db.QueryRow(`
		SELECT audit_chain_anchor, ecosystem_join_keys, caronte_symbol_refs
		FROM knowledge_meta
		WHERE file_path = ?
	`, p).Scan(&anchor, &keys, &refs); err != nil {
		t.Fatalf("read extension columns: %v", err)
	}
	if anchor.Valid {
		t.Errorf("inv-zen-130 violation: audit_chain_anchor = %q, want NULL", anchor.String)
	}
	if keys.Valid {
		t.Errorf("inv-zen-130 violation: ecosystem_join_keys = %q, want NULL", keys.String)
	}
	if refs.Valid {
		t.Errorf("inv-zen-130 violation: caronte_symbol_refs = %q, want NULL", refs.String)
	}
}
