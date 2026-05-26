// SPDX-License-Identifier: MIT
// Package merge implements zen-swarm's cross-worker MergeEngine: the
// test-driven 3-way merge layer. Plan 5 Phase J shipped MergeEngine as
// an interface in internal/orchestrator/apply/; Plan 6 ships the real
// implementation here (Q1 B package boundary; ADR-0030).
//
// # Architecture (Q1 B)
//
// merge/ is split by responsibility:
//
//   - doc.go               package overview + invariants (this file)
//   - mode.go              Mode enum + per-mode config (Q7 B)
//   - events.go            EventType + AnomalyType enums + Event value type + EventEmitter interface (Q10 C)
//   - git.go               gitClient subprocess wrappers + version check
//   - validate.go          pre-flight validation per Q9 D
//   - cache.go             content-addressable cache (Q5 A)        [Phase B]
//   - baseline.go          regression baseline runner               [Phase B]
//   - candidate.go         candidate runner (apply + tests + flake) [Phase B]
//   - scoring.go           two-stage scoring (Q4 B)                 [Phase C]
//   - anomaly.go           anomaly detector + thresholds (Q11 D)    [Phase C]
//   - runner.go            parallel-candidate goroutine supervisor  [Phase D]
//   - engine.go            Merge() pipeline orchestration           [Phase D]
//
// Phase A ships only doc.go + mode.go + events.go + git.go + validate.go.
//
// # Boundaries (lint-enforced)
//
//	inv-zen-104  internal/orchestrator/merge/* MUST NOT import
//	             internal/store. Bridge via internal/daemon/
//	             orchestratoradapter/ (Plan 5 Phase N). Compliance test
//	             tests/compliance/inv_zen_104_merge_no_store_test.go
//	             scans go list -deps for internal/store occurrences and
//	             fails the build if found.
//
//	inv-zen-110  Anomaly events MUST be typed via the Go enum
//	             AnomalyType (int). NO string-typed Type field. The
//	             AnomalyType so a typo or string concat is a compile
//	             error, not a runtime ADR misroute. Compliance test
//	             tests/compliance/inv_zen_110_anomaly_typed_test.go
//	             uses reflect to assert the underlying kind is Int.
//
// # Forward-compat reservations (do NOT retrofit Phase A primitives)
//
//	ADR-0035  AST/structured merge revisit window (Mergiraf/LastMerge
//	          Go grammar maturity).
//	ADR-0036  LLM semantic conflict adjudication revisit window
//	          (deterministic-mode endpoints OR controlled local
//	          inference).
//	ADR-0037  Adaptive parallelism (rounds-batching when N > pool).
//	ADR-0038  Git version matrix CI (test against 2.40/2.45/2.50).
//
// The Mode enum, ModeConfig per-mode shape, EventType + AnomalyType
// enums, and gitClient interface are designed to absorb each of these
// without breaking changes — extension via new enum values + interface
// implementations, never by retrofitting Phase A signatures.
//
// # Compile-time substrate-separation marker
//
// The line `var _ = substrateSeparated()` below ensures that this
// package compiles without importing internal/store (inv-zen-104).
// Removing it MUST NOT cause a missing-import error; if a future
// contributor accidentally adds a forbidden import, the inv-zen-104
// compliance test fails with a precise message.
package merge

var _ = substrateSeparated()

func substrateSeparated() bool { return true }
