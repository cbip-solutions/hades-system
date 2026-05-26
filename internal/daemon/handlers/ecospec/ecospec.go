// SPDX-License-Identifier: MIT
// Package ecospec declares the wire-side contracts for the Plan 14
// ecosystem HTTP surface — the EcosystemHandler interface that the
// daemon's per-route handlers consult, the PrunePreviewResult shape
// returned by PrunePreview, and the three sentinel errors handlers
// translate into status codes.
//
// Why this package exists (Plan 14 followup wiring extract):
//
// The cmd/zen-swarm-ctld/ecosystemwiring adapter that implements
// EcosystemHandler in production lives in a sub-package whose test
// binary CANNOT pull internal/daemon/handlers transitively — that
// import chain brings in internal/store + internal/knowledge which
// both register the "sqlite3" driver (via mattn + ncruces respectively),
// triggering a duplicate-registration panic at test init time.
//
// Extracting the load-bearing types into this dependency-free
// sub-package gives the adapter a stable wire contract to satisfy
// without polluting its test binary with both sqlite3 drivers. The
// handlers package re-exports these symbols (typed as aliases /
// constants of this package's types) so existing handler code +
// existing handler tests are unaffected by the move.
//
// Boundary this package MUST stay zero-dependency (stdlib only).
// Adding internal/store / internal/knowledge / internal/daemon imports
// here re-opens the driver-conflict path; any future feature
// addition belongs in handlers/, not here.
package ecospec

import (
	"context"
	"errors"
)

var ErrEcosystemPinAlreadyPinned = errors.New("ecosystem: version already pinned")

var ErrEcosystemVersionPinned = errors.New("ecosystem: version is pinned; unpin first")

var ErrEcosystemVersionNotFound = errors.New("ecosystem: (ecosystem, version) not found")

type EcosystemPrunePreviewResult struct {
	Ecosystem      string
	Version        string
	ChunkCount     int
	ChunkFP32Count int
	SymbolCount    int
	ChangeCount    int
	FTS5Count      int
	Pinned         bool
}

type EcosystemHandler interface {
	Pin(ctx context.Context, ecosystem, version string) error
	PrunePreview(ctx context.Context, ecosystem, version string) (EcosystemPrunePreviewResult, error)
	Prune(ctx context.Context, ecosystem, version string) error
	IngestDelta(ctx context.Context, ecosystem string) error
	SweepChunkFingerprints(ctx context.Context, ecosystem string) error
	SweepChangeNodes(ctx context.Context, ecosystem string) error
	RebuildSymbolIndex(ctx context.Context, ecosystem string) error
	CASGarbageCollect(ctx context.Context) error
	DetectNewVersions(ctx context.Context, ecosystem string) ([]string, error)
}
