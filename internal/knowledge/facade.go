// SPDX-License-Identifier: MIT
// Package knowledge — `Index` façade.
//
// Spec reference: internal design record
// §"Task G-17" lines 4295–4368 (canonical) — method-bound aggregator
// over the G-1..G-16 free-function entry points (Open, Init, IndexDoc,
// Delete, Execute, ColdRebuild, IncrementalUpdate,...). The façade
// exists so HTTP handlers and the CLI surface (G-11) can hold
// a single value-type and invoke queries + reindexes without
// re-assembling state on every call.
//
// review CRITICAL #6 reconciliation (2026-05-01): the free-
// function form (`knowledge.Execute`, `knowledge.ColdRebuild`,
// `knowledge.IncrementalUpdate`, `knowledge.IndexDoc`) is preserved
// for the watcher hot path and compliance tests; the `Index` struct is
// the **method-bound façade** that closes over the *sql.DB handle and
// the configured ScannerSources so handlers don't reconstruct on every
// call. Both surfaces ship side-by-side.
//
// Naming note (G-17 amendment): the writer entry point that was named
// `Index` in earlier drafts of the plan was renamed to `IndexDoc` so
// that the package-level `Index` identifier could be reused for this
// struct. Go disallows a function and type to share a name in the same
// package; the doctrine "no stubs / código completo" mandates the
// canonical surface ships intact, so the writer was renamed at the same
// commit as this façade lands. The free-function `IndexDoc` remains the
// canonical writer; the façade does not expose it as a method (writers
// run inside ColdRebuild + IncrementalUpdate; HTTP handlers do not bind
// individual docs from the wire).
//
// Boundary (inv-hades-031): the façade lives entirely in
// `internal/knowledge` and takes a *sql.DB the caller has already
// constructed via knowledge.Open + knowledge.Init. It does NOT import
// internal/store; the daemon adapter in
// internal/daemon/dispatcheradapter (or a future
// internal/daemon/knowledgeadapter) is responsible for wiring this
// façade's Reindex into a event emit when the rebuild
// completes.
//
// Boundary (inv-hades-129): the façade's Query method delegates to
// Execute, which validates `Remote` + `AuditChain` against the deferred
// sentinels in sentinel.go. The façade itself never speaks net/http;
// the only way a remote/audit-chain flag reaches the index is through
// Execute's validation, which 4xx-equivalent errors back to the caller
// per the deferred-feature contract.
package knowledge

import (
	"context"
	"database/sql"
)

type Index struct {
	db      *sql.DB
	sources []ScannerSource
}

func NewIndex(db *sql.DB) *Index {
	return &Index{db: db}
}

func (i *Index) SetSources(sources []ScannerSource) {
	i.sources = sources
}

func (i *Index) Query(ctx context.Context, q Query) ([]Result, error) {
	return Execute(ctx, i.db, q)
}

func (i *Index) Reindex(ctx context.Context) error {
	if i.sources == nil {
		return nil
	}
	_, err := ColdRebuild(ctx, i.db, i.sources)
	return err
}
