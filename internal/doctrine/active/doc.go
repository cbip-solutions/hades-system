// SPDX-License-Identifier: MIT
// Package active is THE single public accessor for runtime doctrine reads
// in zen-swarm It is the runtime read substrate
// behind both the per-process default (Active) and the per-project
// effective doctrine (For).
//
// Consumers ( worker.Spawn, orchestrator, merge engine,
// active.Active() or active.For(projectID) rather than calling
// internal/doctrine/parser.Parse() directly. Direct parser calls are
// allowed ONLY at daemon startup init paths and CLI one-shot invocations.
// noDirectParserOutsideInitAnalyzer enforces this at compile
// time.
//
// # Concurrency model (per spec §4.5 + Q10 C)
//
// - Reads (Active() / For(projectID)) are the hot path: lock-free
// atomic.Pointer.Load (~10ns per call). invariant atomicity
// guarantee: in-flight workers see a stable *v1.Schema for their
// goroutine lifetime even if a Store happens concurrently.
//
// - Writes (SetRegistry / SetUserDefault / SetForProject /
// ClearForProject) use atomic.Pointer.Store. No half-loaded states
// observable. sync.Map is used for the per-project
// map so concurrent Insert/Delete avoids coarse-lock contention.
//
// # Resolution chain for For(projectID) (Q7 C hybrid override layout)
//
// 1. If projectID has a registered per-project schema (override merged
// with baseline at daemon startup OR at reload time) → return it.
//
// 2. Otherwise → return userDefault (set via SetUserDefault).
//
// 3. Otherwise → return registry["max-scope"] hardcoded last-resort
// fallback (defense-in-depth so consumers never see nil even if the
// daemon failed to populate userDefault).
//
// 4. Otherwise → panic with init-order diagnostic. invariant init-
// order guard: daemon startup MUST call SetRegistry before any
// consumer reads.
//
// Active() uses the same chain skipping the per-project arm.
//
// # phases that consume / extend this package
//
// - (builtin) provides the initial registry via
// builtin.LoadAll() called at daemon startup; daemon then calls
// active.SetRegistry(reg).
//
// - (reload) invokes active.SetForProject after the file-
// watcher detects an override change and the debounced
// re-parse + ValidateTighten succeeds; invokes
// active.SetUserDefault after operator-edit on user-default
// selection.
//
// - calls active.SetForProject after an
// amendment write succeeds (cross-branch additive).
//
// - (HTTP) /v1/doctrine/active reads via active.Active() and
// active.For() per request; no caching (reads are already ~10ns).
//
// # Package perimeter
//
// internal/doctrine/active/ imports stdlib only +
// github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1 +
// github.com/cbip-solutions/hades-system/internal/doctrine/errors.
//
// No imports of internal/store, internal/orchestrator, internal/daemon,
// internal/redact, private-tier1-module, internal/workforce,
// internal/budget, internal/notif, internal/providers, internal/cli,
// internal/client, internal/config, internal/mcp, internal/tui.
package active
