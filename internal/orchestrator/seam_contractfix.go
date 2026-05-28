// SPDX-License-Identifier: MIT
// internal/orchestrator/seam_contractfix.go
//
// the L10 coordinated dual-worktree fix (D5).
//
// Declares the ContractFixAutonomyOracle name as a TYPE ALIAS for the
// coordinated.AutonomyOracle interface (which declares the actual
// method). The alias resolves a fundamental import-cycle issue:
//
// - The Coordinator (in internal/caronte/coordinated) needs the seam
// interface as a field type on OrchestratorCoordinator.Autonomy.
// - The seam's Decision method consumes coordinated.ContractBreakage
// as its parameter type.
// - If the interface were declared in this orchestrator package,
// coordinated would have to import orchestrator (for the field
// type) AND orchestrator would have to import coordinated (for
// ContractBreakage) — a cycle.
//
// Resolution per Go's structural typing + type aliases: declare the
// interface ONCE in coordinated.AutonomyOracle (the value-types
// owner); declare an alias here so the master C-9
// ContractFixAutonomyOracle name + doc-comment + boundary semantics
// remain owned by internal/orchestrator (the design layer per master
// C-9 + invariant). Both identifiers refer to the SAME interface so
// implementors of either satisfy the other via structural typing —
// production adapter is constructed against
// orchestrator.ContractFixAutonomyOracle (the master-named type);
// the coordinated tests fake the interface via the local
// coordinated.AutonomyOracle name.
//
// Layering rationale (§"D5 decoupling" — spec §15 AS-BUILT NOTE):
// ConfirmationPolicy, G-9/G-10 merge tie-break/mode) are
// seam-for-future — neither consuming flow is daemon-active
// (handlers.NewMergeHandler runs with NIL engine per main.go:460
// comment "F.7 wires"; HRA + NewConfirmationPolicy never constructed
// at runtime). HADES design's L10 therefore implements its OWN orchestrator
// subsystem (internal/caronte/coordinated/), INDEPENDENT of F.7
// wiring. The autonomy oracle is the seam that lets the daemon supply
// the doctrine-driven policy WITHOUT pulling
// HRA/ConfirmationPolicy/MergeEngine into the coordinated package's
// import graph.
//
// Style mirror — seam_blastradius.go:
// - pure interface (here: the alias to a pure interface), no struct,
// no constructor, no concrete fields;
// - the production impl lives downstream ( narrow daemon
// adapter consuming the doctrine resolver + workspace
// RiskThreshold);
// - the daemon composition root (
// buildContractFederation per master C-15) wires the production
// impl into OrchestratorCoordinator.Autonomy.
//
// invariant family + invariant:
// - invariant forbids bypass/providers/dispatcher/orchestrator
// from importing internal/store directly; the C-9 seam keeps
// caronte/coordinated free of any
// internal/orchestrator/{hra,merge,confirmation_policy} import
// (the invariant AST scan asserts this — the F.7 anti-coupling
// boundary that proves the Coordinator is independent of F.7
// wiring).
// - The sole orchestrator→caronte/coordinated reference is this
// file's type-alias declaration (analogous to
// seam_blastradius.go's Verdict-in-orchestrator pattern, reversed
// because ContractBreakage is caronte-side per the data-flow
// direction). The reversal preserves boundary discipline: the
// orchestrator package owns the SEMANTIC NAME ContractFixAutonomyOracle
// (the seam interface, used downstream by production
// adapter), the caronte/coordinated package owns the underlying
// interface declaration (so caronte's Coordinator can have a typed
// field on the seam). The alias makes both languages refer to the
// SAME interface.

package orchestrator

import "github.com/cbip-solutions/hades-system/internal/caronte/coordinated"

// ContractFixAutonomyOracle is the master C-9 seam name: a type alias
// for the coordinated.AutonomyOracle interface (declared in
// internal/caronte/coordinated/types.go). The alias keeps the master
// C-9 naming + boundary semantics owned by internal/orchestrator while
// the underlying interface declaration lives in coordinated to break
// the import cycle (see this file's package doc-comment for the full
// rationale).
//
// Implementations satisfying coordinated.AutonomyOracle automatically
// satisfy orchestrator.ContractFixAutonomyOracle and vice-versa (Go
// type aliases produce a SINGLE type, not two structurally-identical
// types).
//
// Decision returns coordinated.ModeAutonomy iff ALL THREE hold
// (single-method seam per the doctrine; see
// coordinated.AutonomyOracle's doc-comment for the full layered
// conditions). Returns coordinated.ModeSurface otherwise (incl.
// zero-value defense-in-depth).
//
// Implementations MUST be deterministic for a fixed ContractBreakage —
// the audit trail captures the decision; a non-deterministic oracle
// would make the audit row misleading.
//
// The alias signature is reflect-pinned by the sister-test
// TestContractFixAutonomyOracleMethodSet — adding/removing/renaming a
// method on the underlying coordinated.AutonomyOracle is a deliberate
// cross-stage contract change that breaks the test.
type ContractFixAutonomyOracle = coordinated.AutonomyOracle
