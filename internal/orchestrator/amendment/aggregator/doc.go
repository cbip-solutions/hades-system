// SPDX-License-Identifier: MIT
// Package aggregator implements the release Q13 C per-rule
// SIGNAL with shared sliding-window machinery + 3 generic event-
// categories (cost / merge / recovery).
//
// Each aggregator subscribes to a bounded set of release eventlog
// PayloadEncoder values (typed event payloads), maps each event onto a
// (rule_path, anomalous bool) tuple via per-rule registration, and
// maintains a per-rule WindowState (bounded slice of SessionRecord +
// counters). Evaluate(rule, window) returns (pctPassing, totalSessions,
// lastApplied) so the TelemetrySubscriber can decide whether to dispatch
// Reverter.AutoRevert.
//
// Design pivot vs. the master plan:
//
// - release ships `eventlog.PayloadEncoder` (typed-event interface) +
// `EventType` enum. aggregators operate at PayloadEncoder
// level: callers pass the typed value (e.g.
// eventlog.BudgetDegradationApplied{...}) and the aggregator dispatches
// by EventType + optional payload predicate.
//
// - release events live in internal/orchestrator/merge as a separate
// package-local enum. avoids cross-importing merge directly
// by accepting any PayloadEncoder
// implementation. Bridge code at the release N adapter layer translates
// merge.Event → eventlog.PayloadEncoder if the merge aggregator needs
// to subscribe to anomaly events outside the eventlog package's own
// constants.
//
// # Boundaries
//
// - aggregator ⊥ internal/store
// - aggregator ⊥ internal/orchestrator/hra (uses eventlog payloads only;
// never imports HRA)
// - aggregator ⊥ internal/doctrine/parser (reads only via the
// TelemetrySubscriber's accessor; enforces invariant)
//
// Invariants enforced via aggregator:
//
// - invariant — per-rule revert cooldown is enforced by the
// TelemetrySubscriber dispatch (cost/merge/recovery aggregators are
// pure / stateless wrt time-keeping; cooldown lives in
// telemetry_subscriber.go + cooldown.go).
// - invariant — attribution: Evaluate returns the SourceADR of the
// LAST DoctrineAmendmentApplied for the rule's category within the
// rolling window; if none, "" sentinel signals "no revert
// candidate".
//
// Concurrency each aggregator's state map is guarded by its own
// RWMutex (reads >> writes; multiple goroutines calling Evaluate
// concurrent with a single RecordSession goroutine is the expected
// hot-path).
package aggregator
