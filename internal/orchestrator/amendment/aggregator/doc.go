// SPDX-License-Identifier: MIT
// Package aggregator implements the Plan 8 Phase H Q13 C per-rule
// SIGNAL with shared sliding-window machinery + 3 generic event-
// categories (cost / merge / recovery).
//
// Each aggregator subscribes to a bounded set of Plan 5 eventlog
// PayloadEncoder values (typed event payloads), maps each event onto a
// (rule_path, anomalous bool) tuple via per-rule registration, and
// maintains a per-rule WindowState (bounded slice of SessionRecord +
// counters). Evaluate(rule, window) returns (pctPassing, totalSessions,
// lastApplied) so the TelemetrySubscriber can decide whether to dispatch
// Reverter.AutoRevert.
//
// Design pivot vs. the master plan:
//
//   - Plan 5 ships `eventlog.PayloadEncoder` (typed-event interface) +
//     `EventType` enum. Phase H aggregators operate at PayloadEncoder
//     level: callers pass the typed value (e.g.
//     eventlog.BudgetDegradationApplied{...}) and the aggregator dispatches
//     by EventType + optional payload predicate.
//
//   - Plan 6 events live in internal/orchestrator/merge as a separate
//     package-local enum. Phase H avoids cross-importing merge directly
//     (boundary inv-zen-104) by accepting any PayloadEncoder
//     implementation. Bridge code at the Plan 5 N adapter layer translates
//     merge.Event → eventlog.PayloadEncoder if the merge aggregator needs
//     to subscribe to anomaly events outside the eventlog package's own
//     constants.
//
// Boundaries
//
//   - aggregator ⊥ internal/store (inv-zen-133 generalized)
//   - aggregator ⊥ internal/orchestrator/hra (uses eventlog payloads only;
//     never imports HRA)
//   - aggregator ⊥ internal/doctrine/parser (reads only via the
//     TelemetrySubscriber's accessor; enforces inv-zen-134)
//
// Invariants enforced via aggregator:
//
//   - inv-zen-139 — per-rule revert cooldown is enforced by the
//     TelemetrySubscriber dispatch (cost/merge/recovery aggregators are
//     pure / stateless wrt time-keeping; cooldown lives in
//     telemetry_subscriber.go + cooldown.go).
//   - inv-zen-141 — attribution: Evaluate returns the SourceADR of the
//     LAST DoctrineAmendmentApplied for the rule's category within the
//     rolling window; if none, "" sentinel signals "no revert
//     candidate".
//
// Concurrency each aggregator's state map is guarded by its own
// RWMutex (reads >> writes; multiple goroutines calling Evaluate
// concurrent with a single RecordSession goroutine is the expected
// hot-path).
package aggregator
