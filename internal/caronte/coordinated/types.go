// SPDX-License-Identifier: MIT
// Package coordinated owns the L10 coordinator value types + the
// Coordinator interface + the OrchestratorCoordinator production impl
// that turn a ContractBreakage event ( bcdetect.Pipeline.Fan
// output) into either an autonomous dispatch across affected client
// repos OR a structured
// surface recommendation (ModeSurface — surfaced via §10 MCP +
// F7 TUI panel). Every dispatch emits a release Tessera audit row via
// the C-11 federation.EmitAudit helper (invariant chokepoint).
//
// is the production owner of the Coordinator behavioural surface
// (master C-8, ADR-0115). + seeded the ConsumerRef value
// type additively here in W4 because consumes
// (*link.Linker).ConsumersFor returning []coordinated.ConsumerRef +
// emits ContractBreakage events for to dispatch on.
//
// Boundary invariant / invariant / invariant: this package MUST
// NOT import internal/orchestrator/{hra,merge,confirmation_policy} (the
// F.7 hook packages — release N-4 verified those are seam-for-future,
// the D5 decoupling); MUST NOT import internal/store (the daemon store);
// the SOLE permitted orchestrator-side bridge is the capability-detected
// worktreepool.Pool interface + the orchestrator.ContractFixAutonomyOracle
// seam (declared in internal/orchestrator/seam_contractfix.go, consumed
// here through a typed interface field on OrchestratorCoordinator —
// reverse direction G-6's seam_blastradius.go but the same
// boundary discipline).
//
// Build tag: NONE — value-type-only file + the Coordinator interface +
// the Production impl algorithm path are all CGO-agnostic. Only the
// integration tests that open a real caronte.db (members for the
// Workspace) carry //go:build cgo.
package coordinated

import (
	"context"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type DispatchMode string

const (
	ModeAutonomy DispatchMode = "autonomy"
	ModeSurface  DispatchMode = "surface"
)

type ConsumerRef struct {
	Repo   string
	CallID string
	NodeID string
	File   string
	Line   int
}

type LoreAttribution struct {
	Author     string
	CommitSHA  string
	ADRRefs    []string
	Supersedes []string
}

type ContractBreakage struct {
	Change            store.BreakingChange
	AffectedConsumers []ConsumerRef
	Workspace         *store.Workspace
	LoreAttribution   *LoreAttribution
}

type DispatchResult struct {
	Mode            DispatchMode
	DispatchedRepos []string
	SurfaceMessage  string
	AuditID         tessera.LeafID
}

// DispatchDecision is the durable summary of a single Dispatch call
// retained in the OrchestratorCoordinator's in-memory ring-buffer cache
// for fast-access surfaces ( TUI + the §10 MCP
// get_recent_dispatches surface).
//
// NOTE(release) — the persistent ledger of all dispatches is the release Tessera
// audit chain (see AuditID below — it is the durable handle to the
// Tessera leaf). The ring-buffer is a FAST-ACCESS CACHE for TUI/CLI
// surfaces only, NOT the durable record. Every entry corresponds to a
// Tessera leaf, but the Tessera leaves outlive the ring (the ring
// rotates on cap; Tessera retains forever). Any consumer that needs the
// durable history MUST query Tessera; the ring is for UX latency only.
//
// Field shape (reflect-pinned by types_blackbox_test.go's
// TestDispatchDecisionFieldSet — drift requires deliberate cross-phase
// contract change with ):
type DispatchDecision struct {
	ChangeID        string
	Mode            DispatchMode
	DispatchedRepos []string
	AuditID         tessera.LeafID
	DecidedAt       time.Time
}

type Coordinator interface {
	Dispatch(ctx context.Context, b ContractBreakage) (DispatchResult, error)
}

// AutonomyOracle is the master C-9 ContractFixAutonomyOracle seam
// declared here (in the coordinated package) to break the import cycle:
// the OrchestratorCoordinator.Autonomy field needs the seam interface
// type, but the seam's Decision parameter is coordinated.ContractBreakage,
// so the seam can't live in internal/orchestrator without coordinated
// importing orchestrator (the cycle).
//
// Resolution the interface lives HERE; internal/orchestrator/seam_contractfix.go
// declares `type ContractFixAutonomyOracle = coordinated.AutonomyOracle`
// (a Go type alias — both identifiers refer to the SAME interface, so
// implementors of either satisfy the other via structural typing). This
// preserves the master C-9 naming ( production adapter is
// constructed via orchestrator.ContractFixAutonomyOracle; tests in
// coordinated use coordinated.AutonomyOracle) AND preserves the
// invariant boundary (coordinated does NOT import orchestrator;
// orchestrator only imports coordinated for the value-types parameter).
//
// Decision returns ModeAutonomy iff ALL THREE hold (single-method seam;
// the layered conditions are implementation detail of the production
// adapter):
//
// (a) doctrine grants autonomy for cross-repo contract-fix
// (capa-firewall denies; default allows assisted; max-scope allows
// full),
//
// (b) blast-radius does not exceed the workspace's RiskThreshold
// (release G mirror — composite of cone-cardinality +
// cochange-coupling + LoC-percentile),
//
// (c) workspace.PrivacyLocked()=false OR affected consumers all live
// in the owning project (capa-firewall consistency double-gate
// with Workspace.AuthorizeProjects).
//
// Returns ModeSurface otherwise. The Coordinator MUST default to
// surface on (a) explicit deny or (b) ANY future enum value the oracle
// may add (zero-value safety: an empty DispatchMode renders as ""
// which the Coordinator treats as ModeSurface in modes.go's switch).
//
// Implementations MUST be deterministic for a fixed ContractBreakage
// (same input → same DispatchMode) — the audit trail captures the
// decision, and a non-deterministic oracle would make the audit row
// misleading. The default production policy is
// doctrine-resolver-driven and inherently deterministic; tests fake
// this interface with a stub that returns a fixed mode.
type AutonomyOracle interface {
	Decision(b ContractBreakage) DispatchMode
}
