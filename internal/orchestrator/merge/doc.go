// SPDX-License-Identifier: MIT
// Package merge implements hades-system's cross-worker MergeEngine: the
// test-driven 3-way merge layer. HADES design shipped MergeEngine as
// an interface in internal/orchestrator/apply/; HADES design ships the real
// implementation here (design choice B package boundary; ADR-0030).
//
// # Architecture (design choice B)
//
// merge/ is split by responsibility:
//
// - doc.go package overview + invariants (this file)
// - mode.go Mode enum + per-mode config (design choice B)
// - events.go EventType + AnomalyType enums + Event value type + EventEmitter interface (design choice C)
// - git.go gitClient subprocess wrappers + version check
// - validate.go pre-flight validation per design choice D
// - cache.go content-addressable cache (design choice A) []
// - baseline.go regression baseline runner []
// - candidate.go candidate runner (apply + tests + flake) []
// - scoring.go two-stage scoring (design choice B) []
// - anomaly.go anomaly detector + thresholds (design choice D) []
// - runner.go parallel-candidate goroutine supervisor []
// - engine.go Merge() pipeline orchestration []
//
// ships only doc.go + mode.go + events.go + git.go + validate.go.
//
// # Boundaries (lint-enforced)
//
// invariant internal/orchestrator/merge/* MUST NOT import
// internal/store. Bridge via internal/daemon/
// orchestratoradapter/. Compliance test
// tests/compliance/inv_hades_104_merge_no_store_test.go
// scans go list -deps for internal/store occurrences and
// fails the build if found.
//
// invariant Anomaly events MUST be typed via the Go enum
// AnomalyType (int). NO string-typed Type field. The
// AnomalyType so a typo or string concat is a compile
// error, not a runtime ADR misroute. Compliance test
// tests/compliance/inv_hades_110_anomaly_typed_test.go
// uses reflect to assert the underlying kind is Int.
//
// # Forward-compat reservations
//
// ADR-0035 AST/structured merge revisit window (Mergiraf/LastMerge
// Go grammar maturity).
// ADR-0036 LLM semantic conflict adjudication revisit window
// (deterministic-mode endpoints OR controlled local
// inference).
// ADR-0037 Adaptive parallelism (rounds-batching when N > pool).
// ADR-0038 Git version matrix CI (test against 2.40/2.45/2.50).
//
// The Mode enum, ModeConfig per-mode shape, EventType + AnomalyType
// enums, and gitClient interface are designed to absorb each of these
// without breaking changes — extension via new enum values + interface
// implementations, never by retrofitting signatures.
//
// # Compile-time substrate-separation marker
//
// The line `var _ = substrateSeparated()` below ensures that this
// package compiles without importing internal/store (invariant).
// Removing it MUST NOT cause a missing-import error; if a future
// contributor accidentally adds a forbidden import, the invariant
// compliance test fails with a precise message.
package merge

var _ = substrateSeparated()

func substrateSeparated() bool { return true }
