package main

import (
	"bytes"
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

func TestParseConfigAcceptsRoots(t *testing.T) {
	fs := flag.NewFlagSet("zen-knowledge-watcher", flag.ContinueOnError)
	cfg, err := parseConfig(fs, []string{"-roots", "/p1,/p2", "-index", "/tmp/index.db"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Roots) != 2 {
		t.Errorf("Roots = %v, want 2", cfg.Roots)
	}
	if !strings.HasSuffix(cfg.IndexPath, "index.db") {
		t.Errorf("IndexPath = %q, want suffix 'index.db'", cfg.IndexPath)
	}
}

func TestParseConfigDefaultIndexPath(t *testing.T) {
	fs := flag.NewFlagSet("zen-knowledge-watcher", flag.ContinueOnError)
	cfg, err := parseConfig(fs, []string{"-roots", "/p1"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.IndexPath == "" {
		t.Errorf("IndexPath empty; expected ResolveIndexPath default")
	}

	if !strings.HasSuffix(cfg.IndexPath, filepath.Join("knowledge-index", "index.db")) {
		t.Errorf("IndexPath = %q, want suffix 'knowledge-index/index.db'", cfg.IndexPath)
	}
}

func TestParseConfigEmptyRootsRejected(t *testing.T) {
	fs := flag.NewFlagSet("zen-knowledge-watcher", flag.ContinueOnError)
	_, err := parseConfig(fs, []string{"-roots", "", "-index", "/tmp/x.db"})
	if err == nil {
		t.Fatal("expected error for empty -roots; got nil")
	}
	if !strings.Contains(err.Error(), "roots") {
		t.Errorf("error message must mention 'roots'; got: %v", err)
	}
}

func TestParseConfigSingleRootNoComma(t *testing.T) {
	fs := flag.NewFlagSet("zen-knowledge-watcher", flag.ContinueOnError)
	cfg, err := parseConfig(fs, []string{"-roots", "/single", "-index", "/tmp/x.db"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(cfg.Roots) != 1 || cfg.Roots[0] != "/single" {
		t.Errorf("Roots = %v, want [\"/single\"]", cfg.Roots)
	}
}

func TestParseConfigUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("zen-knowledge-watcher", flag.ContinueOnError)

	fs.SetOutput(discardWriter{})
	_, err := parseConfig(fs, []string{"--no-such-flag-xyz"})
	if err == nil {
		t.Fatal("expected error for unknown flag; got nil")
	}
}

func TestInferKindPathMapping(t *testing.T) {
	cases := []struct {
		name string
		path string
		want knowledge.FileType
	}{
		{"adr", "/repo/docs/decisions/0001-foo.md", knowledge.FileTypeADR},
		{"spec", "/repo/docs/superpowers/specs/2026-04-29-bar.md", knowledge.FileTypeSpec},
		{"plan", "/repo/docs/superpowers/plans/2026-04-29-baz.md", knowledge.FileTypePlan},
		{"handoff", "/repo/HANDOFF.md", knowledge.FileTypeHandoff},
		{"memory_basename", "/repo/memory/feedback_xyz.md", knowledge.FileTypeMemory},
		{"unknown_default_memory", "/some/random/path/notes.md", knowledge.FileTypeMemory},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := inferKind(c.path)
			if got != c.want {
				t.Errorf("inferKind(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestIndexerSinkReindexAndDelete(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "index.db")

	db, err := knowledge.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	defer db.Close()
	if err := knowledge.Init(ctx, db); err != nil {
		t.Fatalf("knowledge.Init: %v", err)
	}

	mdPath := filepath.Join(tmp, "note.md")
	if err := os.WriteFile(mdPath, []byte("# First\nbody-v1\n"), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}

	sink := newIndexerSink(ctx, db)

	if err := sink.Reindex(mdPath); err != nil {
		t.Fatalf("Reindex(insert): %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, mdPath,
	).Scan(&n); err != nil {
		t.Fatalf("count after insert: %v", err)
	}
	if n != 1 {
		t.Errorf("rows after insert = %d, want 1", n)
	}

	if err := os.WriteFile(mdPath, []byte("# Second\nbody-v2\n"), 0o644); err != nil {
		t.Fatalf("write md v2: %v", err)
	}
	if err := sink.Reindex(mdPath); err != nil {
		t.Fatalf("Reindex(replace): %v", err)
	}
	var title string
	if err := db.QueryRowContext(ctx,
		`SELECT title FROM knowledge_meta WHERE file_path = ?`, mdPath,
	).Scan(&title); err != nil {
		t.Fatalf("title after replace: %v", err)
	}
	if title != "Second" {
		t.Errorf("title after replace = %q, want %q", title, "Second")
	}

	if err := sink.Delete(mdPath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM knowledge_meta WHERE file_path = ?`, mdPath,
	).Scan(&n); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if n != 0 {
		t.Errorf("rows after delete = %d, want 0", n)
	}
}

func TestIndexerSinkReindexBinaryFileSoftError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "index.db")
	db, err := knowledge.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := knowledge.Init(ctx, db); err != nil {
		t.Fatalf("Init: %v", err)
	}

	bin := filepath.Join(tmp, "blob.md")

	if err := os.WriteFile(bin, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	sink := newIndexerSink(ctx, db)
	if err := sink.Reindex(bin); err == nil {
		t.Errorf("Reindex(binary) should error; got nil")
	}
}

func TestIndexerSinkDeleteIdempotent(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	db, err := knowledge.Open(ctx, filepath.Join(tmp, "x.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := knowledge.Init(ctx, db); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sink := newIndexerSink(ctx, db)
	if err := sink.Delete("/never/indexed.md"); err != nil {
		t.Errorf("Delete on never-indexed path: %v (want nil — idempotent)", err)
	}
}

func TestRunGracefulShutdownOnContextCancel(t *testing.T) {
	tmp := t.TempDir()
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(projectRoot, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	dbPath := filepath.Join(tmp, "index.db")

	cfg := config{
		Roots:     []string{projectRoot},
		IndexPath: dbPath,
	}

	ctx, cancel := context.WithCancel(context.Background())

	time.AfterFunc(100*time.Millisecond, cancel)

	if err := run(ctx, cfg); err != nil {
		t.Errorf("run() under context cancel = %v, want nil (clean shutdown)", err)
	}
}

func TestRunPropagatesOpenError(t *testing.T) {
	cfg := config{
		Roots: []string{t.TempDir()},

		IndexPath: "",
	}
	err := run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from run() with empty IndexPath; got nil")
	}
}

func TestMainImplFlagParseError(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	code := mainImpl(context.Background(), []string{"--nonexistent-flag-xyz"}, logger)
	if code != 2 {
		t.Errorf("mainImpl(invalid flag) exit code = %d, want 2", code)
	}
	if !strings.Contains(buf.String(), "FATAL") {
		t.Errorf("expected FATAL log on flag-parse error; got: %s", buf.String())
	}
}

func TestMainImplHelpExitsZero(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = w.Close()
		_ = r.Close()
	}()
	code := mainImpl(context.Background(), []string{"-h"}, logger)
	if code != 0 {
		t.Errorf("mainImpl(-h) exit code = %d, want 0", code)
	}
}

// TestMainImplGracefulCancelExitsZero exercises the happy path: the
// parent context is cancelled shortly after mainImpl reaches the
// Watcher.Run blocking loop. signal.NotifyContext propagates Done(),
// Watcher.Run returns context.Canceled, run() classifies that as a
// clean shutdown (returns nil), and mainImpl exits 0. Covers startup
// logging + clean shutdown logging.
//
// We do NOT pre-cancel the parent because knowledge.Open's PingContext
// would observe the cancellation and fail before run() reaches the
// watcher loop. The 100ms AfterFunc gives Open + Init + NewWatcher +
// AddProject ~95ms of headroom to complete (≪ default test timeout).
func TestMainImplGracefulCancelExitsZero(t *testing.T) {
	tmp := t.TempDir()
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(projectRoot, "memory"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	dbPath := filepath.Join(tmp, "index.db")

	ctx, cancel := context.WithCancel(context.Background())

	time.AfterFunc(100*time.Millisecond, cancel)

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	code := mainImpl(ctx, []string{
		"-roots", projectRoot,
		"-index", dbPath,
	}, logger)
	if code != 0 {
		t.Errorf("mainImpl(cancelled) exit code = %d, want 0; logs: %s", code, buf.String())
	}
	logged := buf.String()
	if !strings.Contains(logged, "starting") {
		t.Errorf("missing startup log line; got: %s", logged)
	}
	if !strings.Contains(logged, "clean shutdown") {
		t.Errorf("missing clean shutdown log line; got: %s", logged)
	}
}

func TestMainImplRunFailureExitsOne(t *testing.T) {
	tmp := t.TempDir()
	regular := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("write regular: %v", err)
	}

	dbPath := filepath.Join(regular, "index.db")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	code := mainImpl(context.Background(), []string{
		"-roots", projectRoot,
		"-index", dbPath,
	}, logger)
	if code != 1 {
		t.Errorf("mainImpl(unwritable index) exit code = %d, want 1; logs: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "FATAL") {
		t.Errorf("expected FATAL log line on run failure; got: %s", buf.String())
	}
}

func TestRunInitFailure(t *testing.T) {
	tmp := t.TempDir()

	dbPath := filepath.Join(tmp, "index.db")
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatalf("mkdir dbPath: %v", err)
	}
	cfg := config{
		Roots:     []string{tmp},
		IndexPath: dbPath,
	}
	err := run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from run() with directory-as-IndexPath; got nil")
	}

	if !strings.Contains(err.Error(), "knowledge.") {
		t.Errorf("error must wrap a knowledge.* call; got: %v", err)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
