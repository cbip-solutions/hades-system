// SPDX-License-Identifier: MIT
// Package apply implements the Q1 D apply-phase boundary contract.
//
// # Q1 D split rationale (spec §1)
//
// Apply-phase has two distinct concerns with distinct failure surfaces:
//
//  1. Live correction inside the HRA inner loop — sequential single-
//     branch patch application at a worker's commit boundary. No
//     concurrent writers; the failure modes are patch-rejection,
//     test-regression, and dirty-tree. Owned by ApplyEngine (this
//     package), shipped real in Plan 5 Phase J.
//
//  2. Cross-worker integration — N candidate branches competing for the
//     same integration target; the 3-way merge problem. Failure modes
//     are textual conflicts, semantic conflicts, and reviewer
//     disagreement. Owned by MergeEngine (interface here, real in Plan
//     6 per research SOTA Topic 4: IntelliMerge / MergeBERT / LLMinus
//     test-driven merge).
//
// # Boundary diagram
//
//	┌─────────────────────────────┐
//	│ HRA inner loop (Phase H)    │
//	│  voting.FMV decides "fix X" │
//	└──────────────┬──────────────┘
//	               ▼
//	┌─────────────────────────────┐
//	│ ApplyEngine.ApplyFix        │  Plan 5 (this package, real)
//	│  git apply on worker branch │
//	│  revert if tests fail       │
//	└─────────────────────────────┘
//
//	┌─────────────────────────────┐
//	│ HRA architectural cadence   │
//	│  A=30min OR phase boundary  │
//	└──────────────┬──────────────┘
//	               ▼
//	┌─────────────────────────────┐
//	│ MergeEngine.Merge           │  Plan 6 (interface declared here)
//	│  test-driven 3-way merge    │
//	│  fast-forward winner        │
//	└─────────────────────────────┘
//
// # Invariant inv-zen-089 (transitive)
//
// This package imports stdlib only (no internal/store, no internal/queue,
// no eventlog, no workforce). The Phase A canonical eventlog wire codes
// (EvtApplyFixStarted/Succeeded/Reverted) are translated from
// apply-package-local apply.Event values by the Phase N
// internal/daemon/orchestratoradapter bridge. Apply engine never imports
// the eventlog package directly — narrow surface for audit-trail
// discipline.
//
// # Invariant inv-zen-097
//
// MergeEngineFake (merge_fake.go) is gated by `//go:build test`. The
// constructor invokes mustBeTestRun() so a misconfigured production
// build will panic on instantiation rather than silently exposing fake
// outcomes. Compliance test
// tests/compliance/inv_zen_097_no_fake_in_prod_test.go scans the
// production binary for the symbol and asserts absence.
package apply
