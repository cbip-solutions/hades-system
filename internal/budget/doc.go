// SPDX-License-Identifier: MIT
// Package budget implements the daemon-side budget engine
// of hades-system.
//
// # Architecture
//
// The cost_ledger table is the append-only write path for cost rows
// (one row per LLM request, idempotency-keyed). This package layers
// four engines on top:
//
// - axes: multi-axis attribution (project x doctrine x stage x task + operation + worker_id) via cost_axis_tags. PostCall writes axis tags atomically; missing axes emit axis_tag_loss events.
// - enforce: hierarchical hard-pause cap check across 4 scopes (project / doctrine / stage / worker_id). PreCall returns BlockedScopes sorted most-restrictive first.
// - anomaly: per-scope z-score sliding window using Welford's online algorithm (epsilon-stable, permutation-invariant). Triggers pause when |z| > threshold (default 4.0, doctrine-tunable).
// - pause: 4-scope state machine + auto-resume scheduler. Durable in budget_pauses. Goroutine-driven cooldown clear.
//
// # Boundary (invariant)
//
// This package never imports internal/store directly. All storage access
// goes through the BudgetStore interface declared in axes.go; the
// concrete implementation lives in
// internal/daemon/dispatcheradapter/budget_hooks.go which is allowed to
// import both internal/store and internal/budget. The dispatcher
// (internal/daemon/dispatcher) calls only the adapter's PreCall / PostCall
// methods; an AST-grep compliance test
// (tests/compliance/inv_hades_076_test.go) enforces this boundary once
//
// # Concurrency contract
//
// All four engines (Gate, AxisTagger, AnomalyDetector, Pauser) are
// goroutine-safe by virtue of stateless dispatch + serialised storage:
//
// - Gate.Check is read-only; rollupWindow is set once at startup via
// SetRollupWindow and never mutated thereafter.
// - AxisTagger.Tag writes via INSERT OR IGNORE (idempotent under retry).
// - AnomalyDetector.Update serialises the window-read + maybe-append
// region per-scope via a sync.Map of *scopeState. Concurrent Updates
// on the SAME scope are linearised; same-sample retries collapse to
// ONE budget_anomalies row via an in-memory fingerprint dedupe.
// Different scopes proceed without contention.
// - Pauser uses persist-first ordering:
// PauseSet returns success BEFORE Trigger returns; IsPaused reflects
// the persisted state; no in-memory cache divergence. The auto-resume
// scheduler goroutine (StartScheduler) observes the same store all
// other callers write to.
//
// # Invariants
//
// Compile-time anchors for the four budget invariants:
//
// invariant: preCallEnforcedBeforeUpstream (enforce.go)
// Every dispatch path goes through Gate.Check before
// backend.Forward(...). AST-grep enforced via
// tests/compliance/inv_hades_076_test.go (skip-with-reason
// pre ; full assertion post ).
//
// invariant: axisTagCompleteness (axes.go)
// Every cost row ends with all four axis tags present
// OR an axis_tag_loss event recorded. Compliance test
// in tests/compliance/inv_hades_077_test.go drives 1k
// synthetic cost_ids with 5%-strip and asserts
// len(present_axes) + len(loss_events) == 4 forEach.
//
// invariant: anomalyDeterministic (anomaly.go)
// ComputeZScore is permutation-invariant + bit-equal
// across calls with the same input. Golden dataset in
// tests/compliance/inv_hades_078_test.go (LCG seed=12345,
// 200 samples; 4 reproducibility cases + 5-shuffle
// stability at 5000 samples).
//
// invariant: hierarchicalPrecedence (enforce.go)
// BlockedScopes is sorted most-restrictive first
// (worker_id < stage < doctrine < project). Compliance
// test in tests/compliance/inv_hades_079_test.go covers
// 4 scenarios (all-blocked, mixed pause+cap, single
// scope, none-blocked).
//
// # Adversarial coverage
//
// tests/adversarial/budget_attack_test.go (build tag adversarial)
// exercises:
//
// - race-condition double-tag (UNIQUE constraint integrity under
// 100 goroutines x 10 retries x 6 cost_ids)
// - low-and-slow anomaly evasion (1440 samples drift; PROVISIONAL
// pending HADES design F-1 merge — documents z-score gap; project-cap
// defense Option A coordinated; assertion is t.Logf today,
// hard t.Errorf once cost_ledger merges per the in-test TODO marker)
// - concurrent pause flapping (50 goroutines x 100 iters; final
// state self-consistent)
// - concurrent anomaly trigger (100 goroutines firing same outlier
// on same scope; per-scope mutex + same-sample dedupe → exactly
// 1 budget_anomalies row, post-review C-3 fix)
// - tag-injection malicious axis names (empty values rejected;
// unknown names silently ignored)
// - concurrent different cost_ids (200 goroutines; no cross-row
// contamination)
//
// # Option A coordination (METHODOLOGY.md §4.7.5)
//
// sibling branch but not yet merged to main. Two adapter behaviours
// in dispatcheradapter/budget_hooks.go are provisional pending that
// merge:
//
// - RolledUSDByAxis returns (0, nil); future body will JOIN
// cost_axis_tags with cost_ledger to compute SUM(cost_usd).
// - PostCallWithCost takes an explicit costUSD parameter (cleaner
// than the plan's read-after-write JOIN); the dispatcher passes
// the upstream response's cost_usd directly.
//
// Tests assert the (0, nil) contract today. HADES design + a
// follow-up coordination task will replace the body and update the
// adversarial low-and-slow test from "documents gap" to "asserts cap
// blocks at sample ~800".
package budget
