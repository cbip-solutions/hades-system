// SPDX-License-Identifier: MIT
// Command hades-knowledge-watcher runs the knowledge file watcher as a
// standalone subprocess. The daemon may inline the watcher when CPU
// allows; this binary provides process-isolation when the daemon is
// CPU-budget-constrained.
//
// - Spec ref: design records design
// §"task" lines 3908-4061.
// - Master plan §2.1 declares this binary OPTIONAL — the daemon's
// in-proc watcher is the canonical wiring; this subprocess covers
// the case where operators want explicit per-project watcher
// isolation (e.g., a heavy plan or research re-index burst that
// would jitter the daemon's HTTP loop).
//
// # Lifecycle
//
// 1. parseConfig: -roots (comma-separated absolute paths) + -index
// (knowledge index DB path; defaults to canonical ~/.cache path).
// 2. signal.NotifyContext on SIGTERM + SIGINT — the daemon supervisor
// (or systemd) signals graceful shutdown via SIGTERM; Ctrl-C from
// an operator CLI uses SIGINT.
// 3. knowledge.Open + knowledge.Init — opens the FTS5 + meta tables
// idempotently. Identical to the daemon's wiring; the subprocess
// and daemon share the same DB schema.
// 4. NewWatcher + AddProject(per root) — fsnotify subscriptions on the
// 5 canonical HADES design source dirs (memory, ADRs, specs, plans, root
// for .hades/session.md).
// 5. Watcher.Run(ctx) — blocks until ctx is canceled. On
// debounced.md events, the watcher dispatches Reindex/Delete to
// this binary's indexerSink.
// 6. indexerSink.Reindex composes inferKind (path → FileType) +
// IncrementalUpdate. Soft-error contract: parser/index errors are
// logged but do not abort the watcher loop.
// 7. Clean shutdown: ctx.Err() == context.Canceled is treated as
// success (exit 0). Any other error propagates as exit 1.
//
// Boundary stdlib + internal/knowledge only — no internal/store, no
// internal/projectctx, no net/http. The daemon HTTP API is the
// canonical resolver for project metadata; this subprocess uses
// best-effort path-based inference for FileType (sufficient for the
// "log a typed event" contract because the FTS5 index does not depend
// on FileType for retrieval; FileType is a structured-filter prefix).
//
// invariant: composes knowledge.IncrementalUpdate which already
// honors the NULL-discipline for the three extension-hook columns.
// This binary does not bind audit_chain_anchor / ecosystem_join_keys /
// caronte_symbol_refs at all — HADES design / HADES design / caronte engine own
// those writes.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

type config struct {
	Roots     []string
	IndexPath string
}

func parseConfig(fs *flag.FlagSet, args []string) (config, error) {
	rootsRaw := fs.String("roots", "", "comma-separated absolute project root paths to watch")
	idxPath := fs.String("index", "", "knowledge index DB path (default: ~/.cache/hades-system/knowledge-index/index.db)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if *idxPath == "" {
		p, err := knowledge.ResolveIndexPath()
		if err != nil {
			return config{}, fmt.Errorf("resolve default index path: %w", err)
		}
		*idxPath = p
	}

	roots := make([]string, 0, 4)
	for _, r := range strings.Split(*rootsRaw, ",") {
		if r == "" {
			continue
		}
		roots = append(roots, r)
	}
	if len(roots) == 0 {
		return config{}, errors.New("-roots required (comma-separated absolute project paths)")
	}

	return config{Roots: roots, IndexPath: *idxPath}, nil
}

func inferKind(path string) knowledge.FileType {

	p := filepath.Clean(path)
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)

	if base == ".hades/session.md" {
		return knowledge.FileTypeHandoff
	}

	dirSlash := filepath.ToSlash(dir)
	switch {
	case strings.HasSuffix(dirSlash, "/architecture records"):
		return knowledge.FileTypeADR
	case strings.HasSuffix(dirSlash, "/design records"):
		return knowledge.FileTypeSpec
	case strings.HasSuffix(dirSlash, "/design records"):
		return knowledge.FileTypePlan
	}

	return knowledge.FileTypeMemory
}

type indexerSink struct {
	ctx context.Context
	db  *sql.DB
}

func newIndexerSink(ctx context.Context, db *sql.DB) *indexerSink {
	return &indexerSink{ctx: ctx, db: db}
}

func (s *indexerSink) Reindex(path string) error {
	sf := knowledge.ScannedFile{
		Path: path,
		Kind: inferKind(path),
	}
	return knowledge.IncrementalUpdate(s.ctx, s.db, sf)
}

func (s *indexerSink) Delete(path string) error {
	return knowledge.Delete(s.ctx, s.db, path)
}

func run(ctx context.Context, cfg config) error {
	db, err := knowledge.Open(ctx, cfg.IndexPath)
	if err != nil {
		return fmt.Errorf("knowledge.Open: %w", err)
	}
	defer db.Close()

	if err := knowledge.Init(ctx, db); err != nil {
		return fmt.Errorf("knowledge.Init: %w", err)
	}

	sink := newIndexerSink(ctx, db)
	w, err := knowledge.NewWatcher(sink)
	if err != nil {
		return fmt.Errorf("knowledge.NewWatcher: %w", err)
	}

	for _, root := range cfg.Roots {

		if err := w.AddProject(root); err != nil {
			return fmt.Errorf("knowledge.AddProject(%q): %w", root, err)
		}
	}

	if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("knowledge.Watcher.Run: %w", err)
	}
	return nil
}

func mainImpl(parentCtx context.Context, args []string, logOut *log.Logger) int {
	fs := flag.NewFlagSet("hades-knowledge-watcher", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg, err := parseConfig(fs, args)
	if err != nil {

		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		logOut.Printf("FATAL: %v", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(parentCtx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	logOut.Printf("starting: roots=%v index=%s", cfg.Roots, cfg.IndexPath)

	if err := run(ctx, cfg); err != nil {
		logOut.Printf("FATAL: %v", err)
		return 1
	}
	logOut.Printf("clean shutdown")
	return 0
}

func main() {
	logger := log.New(os.Stderr, "hades-knowledge-watcher: ", log.LstdFlags)
	os.Exit(mainImpl(context.Background(), os.Args[1:], logger))
}
