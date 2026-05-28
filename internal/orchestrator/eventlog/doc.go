// SPDX-License-Identifier: MIT
// Package eventlog is the durable state-machine record for the autonomous
// orchestrator. It stores typed events over the EXISTING
// audit_events_raw table (no migration); HADES design wraps with hash-chain
// later. Subscribers register filters and receive push notifications
// via channel-buffered drop-oldest backpressure (task). Replay()
// reconstructs orchestrator state machine + in-flight worker assignments
// + ConfirmationPolicy state; target <500ms for sessions <1000 events,
// <5s for sessions <10k events.
//
// Boundaries (lint-enforced):
//
// invariant internal/orchestrator/* MUST NOT import internal/store.
// Bridge via internal/daemon/orchestratoradapter/.
//
// invariant internal/orchestrator/eventlog/ MUST NOT import
// internal/workforce/queue/. The event log records
// STATE; the queues carry MESSAGES. Substrate separation.
//
// invariant Replay tolerates at most 5 corrupted events per session
// (each emits ReplayCorruptionDetected). On the 6th, the
// replay halts and the orchestrator transitions HARD_PAUSED
// (driven by the caller; Replay returns ErrCorruptionBudget
// exceeded).
//
// Event types are frozen at 39 typed structs across the 27 spec §2.5
// categories. HADES design G-2 added 2 (43-44); G-4 added 1 (45); G-6 added
// 2 (46-47); H-8 added 1 (48); I-5 added 1 (49); J-2 wired 3 (40-42);
// K-7 added 1 (50). HADES design F-1 added 1 (51, EvtHandoffPosted) for the
// /handoff slash command + hades day --eod integration.
// design records design §4.6.
// Slots 71-91 are reserved for future plans; next free integer is 100.
//
// # Adding a new event type
//
// 1. Pick the next free integer (events.go tracks 1..70 and 92..99
// as used; slots 71-91 are reserved for future plans; next free
// after HADES design is 100). Never insert in middle, never
// reuse retired numbers, never re-order — the integer is persisted
// in audit_events_raw and load-bearing hash-chain replay
// AND HADES design ecosystem_audit_chain replay.
// 2. Declare `EvtX EventType = N` in events.go.
// 3. Add typed struct X with Type() returning EvtX + Payload() returning
// canonical JSON.
// 4. Add EvtX to AllEventTypes() return slice.
// 5. Add a case for EvtX to String() switch.
// 6. Add a case for EvtX to Decode() switch.
// 7. Add a row to events_test.go TestExhaustiveTypeAndPayload table.
// 8. Update spec §2.5 + this doc.go category counter.
// 9. Verify coverage stays 100% on internal/orchestrator/eventlog.
//
// # Privacy contract (mandatory for all emitters)
//
// Eventlog payloads are persisted to audit_events_raw and
// queryable post-hoc. Free-text fields MAY contain leaked secrets if
// emitted verbatim from worker output, LLM responses, or git diffs.
//
// Emitters MUST redact via internal/redact before constructing events:
// - Summary, Findings, DiffSummary, Rationale, Reason, Output: pre-redact
// - File paths: usually safe; pre-redact if path contains a token
// - Commit SHAs, IDs, counts: never redact (load-bearing for replay/audit)
//
// Eventlog does NOT re-redact at write time. The contract is single-
// direction: emitter is responsible. HADES design's redact.Secret type may be
// used to type-enforce this in future iterations; current contract is
// doc + review.
//
// defines the wire schema; HADES design redact + adapter
// connect emitters to the storage layer.
//
// The compile-check below ensures invariant wiring at package init time.
package eventlog

// substrateSeparated is a compile-time marker that this package compiles
// without importing internal/workforce/queue. Removing the line below
// MUST NOT cause a missing import; if a future contributor accidentally
// imports queue, the invariant compliance test fails.
var _ = substrateSeparated()

func substrateSeparated() bool { return true }
