// SPDX-License-Identifier: MIT
// Package knowledge — cold-rebuild orchestrator + incremental hot-path
// .
//
// Spec reference:
// - internal design record
// §"Task G-9" lines 3144-3348 (ColdRebuild — full index rebuild)
// §"Task G-10" lines 3349-3437 (IncrementalUpdate — single-file).
//
// `ColdRebuild` runs at daemon boot (and on operator request via
// `zen knowledge reindex --full` once the CLI lands in ). It
// idempotently re-populates the index from the given sources by
// chaining Scanner → Parser → Indexer. Per spec §4.5 the function
// honors `ctx.Deadline()` and returns `context.DeadlineExceeded`
// cleanly when the operator-configured 30 min cap is exceeded.
//
// `IncrementalUpdate` is the watcher's hot path: every debounced
// per-file change (G-8) lands in this function, which Parses the
// single file and Indexes it via the atomic upsert from G-5. No
// scanner walk, no DELETE-everything — milliseconds per call.
//
// Soft-error contract (ColdRebuild only): per-file failures (binary
// content, parse errors, indexer transient errors) accumulate into a
// `[]ReindexError` slice and DO NOT abort the rebuild. Only
// impossible-to-recover conditions return a hard error: ctx
// cancellation/deadline, the initial DELETE statements, and the
// Scanner's hard-error return (currently always nil but kept in the
// signature for forward-compat per scanner.go contract notes).
// IncrementalUpdate is single-file by design — no soft-error
// accumulation needed; it returns the (parse|index) error directly so
// the watcher glue can classify and act.
//
// Boundary stdlib + database/sql only — no internal/store import,
// no net/http. Reuses package-internal
// `Scanner`, `Parse`, and `IndexDoc`; does NOT introduce new dependencies.
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type ReindexError struct {
	Path string
	Err  error
}

func (re ReindexError) Error() string {
	return fmt.Sprintf("knowledge reindex: %q: %v", re.Path, re.Err)
}

func (re ReindexError) Unwrap() error { return re.Err }

// ColdRebuild clears the knowledge index and re-populates it from the
// given sources. Idempotent: safe to call repeatedly with the same
// sources — produces the same row count post-rebuild.
//
// Phases
// 1. Pre-flight ctx check (caller may have pre-cancelled).
// 2. DELETE FROM knowledge_meta + DELETE FROM knowledge_fts under
// ctx — atomic-enough for the WAL-mode single-writer contract.
// 3. Scanner.Scan() over all sources; per-source errors filter:
// ErrFileTooLarge is dropped (scanner already reported it via
// ScannerError; surfacing again here would double-count). All
// other scanner errors flow into the returned slice.
// 4. For each scanned file: ctx check, then Parse, then IndexDoc. Any
// ctx error here returns the partial reindexErrs along with the
// ctx err — the partially-rebuilt index is still consistent
// because IndexDoc uses a per-file transaction.
//
// Per spec §4.5, the function MUST honor ctx.Deadline(); the per-file
// loop checks ctx.Err() before each Parse so the operator-configured
// 30 min cap is respected with at most one file's worth of overrun.
//
// Per invariant: ColdRebuild does not directly touch
// audit_chain_anchor / ecosystem_join_keys / caronte_symbol_refs —
// it delegates to IndexDoc, which is already invariant-compliant
// (G-5 enforces). Future release / release / Caronte writers fill those
// columns at materialization time without rebuild churn.
//
// Goroutine-safety: SQLite WAL mode allows concurrent readers during
// the DELETE/INSERT sequence, but ColdRebuild is intended to run
// from the single watcher/daemon-boot goroutine; concurrent ColdRebuild
// invocations against the same DB are not a supported pattern.
func ColdRebuild(ctx context.Context, db *sql.DB, sources []ScannerSource) ([]ReindexError, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM knowledge_meta`); err != nil {
		return nil, fmt.Errorf("knowledge: clear meta: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM knowledge_fts`); err != nil {
		return nil, fmt.Errorf("knowledge: clear fts: %w", err)
	}

	scanner := NewScanner(MaxIndexableBytes)
	files, scanErrs, err := scanner.Scan(sources)
	if err != nil {
		return nil, fmt.Errorf("knowledge: scan: %w", err)
	}

	var reindexErrs []ReindexError
	for _, se := range scanErrs {
		if errors.Is(se.Err, ErrFileTooLarge) {
			continue
		}
		reindexErrs = append(reindexErrs, ReindexError{Path: se.Path, Err: se.Err})
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return reindexErrs, err
		}
		doc, err := Parse(f)
		if err != nil {
			reindexErrs = append(reindexErrs, ReindexError{Path: f.Path, Err: err})
			continue
		}
		if err := IndexDoc(ctx, db, doc); err != nil {
			reindexErrs = append(reindexErrs, ReindexError{Path: f.Path, Err: err})
			continue
		}
	}

	return reindexErrs, nil
}

// IncrementalUpdate re-indexes a SINGLE file via Parser → IndexDoc. This is
// the file watcher's hot path (G-8): every debounced fsnotify Write/Create
// event for an `.md` file under a tracked source dispatches here. Designed
// for milliseconds per call — no scanner walk, no DELETE-everything, just
// one Parse and one atomic upsert via IndexDoc.
//
// Idempotency contract: multiple calls with the SAME `sf` produce the same
// end state (one row at sf.Path with the latest content). IndexDoc uses an
// atomic upsert (rowid join preserved across replace) so duplicates do not
// leak even if the watcher's debounce window emits a rare double event.
//
// Boundary composes Parse + IndexDoc. No scanner walk. No fs traversal
// beyond the single ReadFile inside Parse. The caller (daemon glue, Phase
// I) is responsible for deciding which paths to dispatch —
// IncrementalUpdate trusts sf.Kind / sf.ProjectID / sf.ProjectAlias
// verbatim.
//
// Inv-zen-130: composition with Parse + IndexDoc inherits the
// NULL-discipline for audit_chain_anchor / ecosystem_join_keys /
// caronte_symbol_refs. A frontmatter blob with keys named like the
// extension hooks does NOT populate those columns (Parse leaves them
// zero, IndexDoc does not bind). Belt-and-suspenders test in
// reindex_test.go locks this against parser regressions.
//
// Error classification:
// - parse-side errors (binary content, read I/O failure, frontmatter
// edge cases that surface as hard errors) wrap as
// "knowledge: incremental parse: %w". errors.Is(err, ErrBinaryContent)
// works through the wrap;
// - index-side errors (schema CHECK violations, sql driver failures,
// ctx cancellation) propagate IndexDoc's own wrappers
// ("knowledge: insert meta: %w", "knowledge: begin tx: %w", etc.)
// verbatim — no double-wrap. Callers can errors.Is against
// context.Canceled / sql.ErrTxDone / etc. without string matching.
//
// Goroutine-safety: SQLite WAL mode allows concurrent readers during the
// single-file upsert; concurrent IncrementalUpdate calls against the SAME
// path are not a supported pattern (the watcher dispatch is
// single-goroutine per the IndexerSink contract documented in watcher.go).
func IncrementalUpdate(ctx context.Context, db *sql.DB, sf ScannedFile) error {
	doc, err := Parse(sf)
	if err != nil {
		return fmt.Errorf("knowledge: incremental parse: %w", err)
	}
	return IndexDoc(ctx, db, doc)
}
