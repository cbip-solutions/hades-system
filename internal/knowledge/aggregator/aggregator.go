// SPDX-License-Identifier: MIT
// Package aggregator — Aggregator struct + New constructor + DI seams.
//
// The Aggregator is the design choice C cross-project knowledge index: it owns
// aggregator.db (knowledge_pin_index + FTS5 + vec0 + wikilinks) and
// orchestrates promote / list / search operations across the operator's
// authorised projects.
//
// Why a single struct vs. distributed services: aggregator.db's tables
// are tightly coupled (FTS5 external-content + vec0 + wikilinks all
// reference knowledge_pin_index.note_id; promotes write to all four
// in a single SQLite transaction for atomicity). A service split would
// fight that contract and require distributed-transaction primitives
// SQLite cannot provide.
//
// Boundary (invariant): this package imports NO internal/store. The
// PerProjectKnowledgeStore interface is satisfied at daemon-boot time
// by internal/daemon/knowledgeadapter, the only package that
// legitimately imports both internal/store AND this aggregator
// package. Compliance test in tests/compliance/inv_hades_031_*.go enforces.
//
// stage ownership:
// - D-2 (this file): struct + constructor + DI seams + sentinels
// - D-3..D-7: Promote / Search / Embed / wikilink resolver methods
// - D-8: PerProjectKnowledgeStore real implementation in
// internal/daemon/knowledgeadapter
// - D-9: invariant Promote runtime enforcement
// - D-10: invariant search-no-remote runtime check
// - D-11: ChainAnchorComputer real wiring to
// - D-12: PerProjectKnowledgeStore daemon glue
// - D-13: hades-CLI wiring + audit-anchor reverse lookup
package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Aggregator struct {
	db       *sql.DB
	embedder Embedder
	store    PerProjectKnowledgeStore
	chain    ChainAnchorComputer
	clock    Clock

	mu        sync.Mutex
	degraded  bool
	rebuildCh chan VaultChangeEvent
}

type Options struct {
	DB *sql.DB

	Embedder Embedder

	Store PerProjectKnowledgeStore

	Chain ChainAnchorComputer

	Clock Clock
}

func New(opts Options) (*Aggregator, error) {
	if err := aggregatorBoundaryRespectSentinel(); err != nil {
		return nil, err
	}
	if err := aggregatorNoWebSentinel(); err != nil {
		return nil, err
	}
	if err := promoteRequiresReasonSentinel(); err != nil {
		return nil, err
	}
	if opts.Embedder == nil {
		return nil, errors.New("aggregator: Embedder required")
	}
	if opts.Store == nil {
		return nil, errors.New("aggregator: Store required")
	}
	if opts.Embedder.Dimensions() != vecDimensions {
		return nil, fmt.Errorf(
			"aggregator: Embedder dim %d != vecDimensions %d (mpnet-base-v2 / gte-small)",
			opts.Embedder.Dimensions(), vecDimensions,
		)
	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}
	if opts.Chain == nil {
		opts.Chain = noopChainAnchorComputer{}
	}
	return &Aggregator{
		db:       opts.DB,
		embedder: opts.Embedder,
		store:    opts.Store,
		chain:    opts.Chain,
		clock:    opts.Clock,
	}, nil
}

func (a *Aggregator) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// DB returns the underlying *sql.DB if Options.DB was supplied at
// construction; otherwise returns nil. HADES design C-9 amendment
// :
//
// this directly for the cross-ecosystem query surface (BinaryTop200 +
// FTS5Top200 + HydrateChunks per master §3.13 IndexerQueryAdapter
// contract). Without this accessor the dispatcher cannot bridge
// from its aggregator-owned *sql.DB to the Indexer's read methods.
//
// Why additive (vs constructor-time injection): the aggregator is the
// canonical owner of the *sql.DB lifecycle for the aggregator.db
// . HADES design's per-ecosystem.db handles live in a DIFFERENT
// SQLite file (one per ecosystem per design contract=A), so the aggregator
// DOES NOT own the Indexer's *sql.DB — only its own. The DB() accessor
// surfaces the read-only handle for daemon glue to wire into
// per-ecosystem Indexer constructors. The accessor is additive: it does
// NOT change the existing aggregator surface; D-3..D-13 methods + the
// degraded-mode toggle continue to use the same a.db field they always
// did.
//
// Boundary note (invariant spirit): this exposes the read-side
// handle intentionally; HADES design Indexer is the SOLE intended caller.
// A compliance test (tests/compliance/) enforces that no other
// importer reads DB() without an explicit allowlist entry. The
// aggregator package itself remains the canonical owner of writes.
//
// Returns nil when Options.DB was nil at construction (e.g., unit tests
// that exercise the constructor branches without touching the schema).
// Callers MUST nil-check before using the returned *sql.DB.
func (a *Aggregator) DB() *sql.DB {
	return a.db
}

func (a *Aggregator) Degraded() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.degraded
}

func (a *Aggregator) markDegraded() {
	a.mu.Lock()
	a.degraded = true
	a.mu.Unlock()
}

type ChainAnchorComputer interface {
	// ComputeAnchor returns the canonical audit chain anchor
	// `<partition>:<event_id>:<record_hash>` for the given event.
	// Implementations MUST resolve prevHash internally (e.g., via the
	// auditadapter chain tip) and call chain.Compute + chain.Anchor.
	//
	// The createdAt timestamp determines the partition (`YYYY_MM`).
	// The eventID + payload determine the recordHash. The eventType
	// is part of the chain.Compute input.
	ComputeAnchor(
		ctx context.Context,
		eventID, eventType string,
		payload []byte,
		createdAt time.Time,
	) (string, error)
}

type noopChainAnchorComputer struct{}

func (noopChainAnchorComputer) ComputeAnchor(
	_ context.Context,
	eventID, _ string,
	_ []byte,
	createdAt time.Time,
) (string, error) {
	partition := createdAt.UTC().Format("2006_01")
	return fmt.Sprintf("%s:%s:noop-pre-stage-b", partition, eventID), nil
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
