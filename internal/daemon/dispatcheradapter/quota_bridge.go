// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/quota_bridge.go
//
//
// BuildPreFlightDeps assembles a quota.PreFlightDeps snapshot from the
// daemon-side cost ledger + override store + WFQ queue. The dispatcher
// (internal/daemon/dispatcher.PreFlightCheck) consumes the assembled
// PreFlightDeps to run the 3-layer pre-flight gate without itself
// touching internal/store — that is the invariant boundary this
// adapter package absorbs.
//
// Why a separate file (vs. extending dispatcheradapter.go): the
// existing Adapter struct in dispatcheradapter.go bridges the
// dispatcher CostSink path + orchestrator CostStore path + I-4 PinStore
// path. Adding a quota-flavoured bridge inline would couple the two
// subsystems unnecessarily. quota_bridge.go is a pure helper —
// stateless, no struct receiver, just an assembly function plus the
// CostLedgerReader interface that decouples it from any specific
// *store.Store implementation. Tests inject fakes; daemon wiring
// passes the *store.Store via a thin shim (release scheduler
// composition or a future cost_ledger reader implementation).
//
// invariant boundary: this file imports internal/quota +
// internal/doctrine + stdlib only. internal/store is reached
// indirectly via the CostLedgerReader interface — implementations
// (fakes in tests, *store.Store-backed shim in daemon) live elsewhere.

package dispatcheradapter

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

// CostLedgerReader is the slice of read-side cost-ledger API the bridge
// requires. Declared as an interface so tests can supply fakes without
// standing up SQLite, and so the daemon-side implementation can evolve
// (per-tier accounting, project-level cache, etc.) without touching
// this package.
//
// Implementations MUST be safe for concurrent calls — the daemon
// invokes BuildPreFlightDeps from the dispatcher hot path, possibly
// from many goroutines.
//
// Semantics
// - "Cap = 0" means "no cap configured" (downstream PreFlight
// short-circuits to OK for that scope).
// - "Used = 0" is a valid, non-error value for projects with no
// ledger entries yet.
// - Per-tier maps may be empty (no per-tier limits configured); the
// bridge guarantees a non-nil empty map downstream so PreFlight
// does not have to nil-guard.
type CostLedgerReader interface {
	ProjectUsage(ctx context.Context, alias string) (int64, error)

	ProjectCap(ctx context.Context, alias string) (int64, error)

	GlobalUsage(ctx context.Context) (int64, error)

	GlobalCap(ctx context.Context) (int64, error)

	PerTierUsage(ctx context.Context) (map[string]int64, error)

	PerTierCaps(ctx context.Context) (map[string]int64, error)
}

// BuildPreFlightDeps assembles a quota.PreFlightDeps snapshot from the
// runtime sources. The function is the single import-edge between the
// dispatcher's pre-flight gate and the daemon's persistence layer:
// dispatcher → BuildPreFlightDeps → CostLedgerReader (interface) →
// *store.Store (implementation, daemon-side). invariant boundary
// preserved: neither the dispatcher nor internal/quota sees
// *store.Store.
//
// All three runtime dependencies (CostLedgerReader, OverrideStore,
// WfqQueue) are required — passing nil for any of them is a wiring
// bug. The function returns a typed error rather than panicking
// because the daemon's pre-flight hot path is goroutine-driven; a
// panic would crash the worker rather than surface the misconfig at
// the caller.
//
// The returned PreFlightDeps captures a snapshot. Subsequent ledger
// updates are NOT reflected — callers wanting "live" reads must
// re-invoke BuildPreFlightDeps. quota.PreFlight is a pure function so
// the snapshot is sufficient for the decision.
//
// Unit (dollars / cents / tokens) is opaque to BuildPreFlightDeps; the
// caller's ledger + cap configuration MUST agree on a unit. The Plan
// 4 + release ledger conventionally uses USD-cents.
func BuildPreFlightDeps(
	ctx context.Context,
	alias string,
	d doctrine.Name,
	requestTier string,
	thresholds quota.Thresholds,
	cl CostLedgerReader,
	overrideStore quota.OverrideStore,
	wfq *quota.WfqQueue,
) (quota.PreFlightDeps, error) {
	if cl == nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: CostLedgerReader is nil")
	}
	if overrideStore == nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: OverrideStore is nil")
	}
	if wfq == nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: Wfq is nil")
	}
	_ = d

	used, err := cl.ProjectUsage(ctx, alias)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: project usage: %w", err)
	}
	capCents, err := cl.ProjectCap(ctx, alias)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: project cap: %w", err)
	}
	gUsed, err := cl.GlobalUsage(ctx)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: global usage: %w", err)
	}
	gCap, err := cl.GlobalCap(ctx)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: global cap: %w", err)
	}
	tUsed, err := cl.PerTierUsage(ctx)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: per-tier usage: %w", err)
	}
	if tUsed == nil {
		tUsed = map[string]int64{}
	}
	tCaps, err := cl.PerTierCaps(ctx)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: per-tier caps: %w", err)
	}
	if tCaps == nil {
		tCaps = map[string]int64{}
	}
	ov, err := overrideStore.Get(ctx, alias)
	if err != nil {
		return quota.PreFlightDeps{}, fmt.Errorf("dispatcheradapter.BuildPreFlightDeps: override get: %w", err)
	}
	return quota.PreFlightDeps{
		Thresholds:          thresholds,
		Used:                used,
		Cap:                 capCents,
		GlobalUsed:          gUsed,
		GlobalCap:           gCap,
		PerTierCaps:         tCaps,
		PerTierUsed:         tUsed,
		RequestTier:         requestTier,
		Wfq:                 wfq,
		CongestionThreshold: 0,
		Override:            ov,
		Now:                 time.Now,
	}, nil
}
