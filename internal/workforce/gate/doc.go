// SPDX-License-Identifier: MIT
// Package gate implements the OperatorGate pause/resume state machine.
//
// OperatorGate controls whether the workforce subsystem may dispatch new
// Workers, make LLM calls, or proceed after commits. It has four states:
//
// running — normal operation; all scopes pass IsPaused=false.
// paused_descriptive — visible pause; operator explicitly requested a stop;
// LLM calls, Worker dispatch, and post-commit actions
// all block until Resume.
// paused_quiet — silent pause; triggered by z-score anomaly or
// automatic budget enforcement; same blocking as
// descriptive but no operator-visible notification.
// paused_after_apply — pause scheduled for after the current apply stage
// completes; only blocks new Worker dispatch.
//
// Transitions are accepted only from daemon HTTP API callers that present
// a valid auth token (enforcement is in daemon/handlers, not here).
// wires /v1/workforce/gate/{pause,resume} endpoints.
//
// Persistence is decoupled via GatePersist (invariant: gate/* never
// imports internal/store). Production adapter: workforceadapter.GateAdapter.
//
// Consulted at three points (spec §2.2 + §3.3):
// - Worker dispatch (gate.IsPaused(ScopeWorkerDispatch))
// - After each commit (gate.IsPaused(ScopeAfterCommit))
// - Before each LLM call via budget enforce hook (gate.IsPaused(ScopeLLMPreCall))
//
// # Concurrency contract
//
// OperatorGate is safe for concurrent use across all public methods.
// The internal locking strategy uses a single sync.RWMutex (g.mu)
// guarding the state, log, and persist call surfaces:
//
// - State / IsPaused / TransitionLog: g.mu.RLock during read; multiple
// concurrent readers are allowed.
// - Pause / Resume: g.mu.Lock held across the ENTIRE sequence
// (validate → persist → mutate state → append log entry).
//
// Holding g.mu across the persist.SaveState I/O call is intentional.
// The trade-off:
// - Without lock-coverage of persist, two concurrent Pause calls could
// interleave their persist + mutate steps such that the in-memory
// state ends up reflecting one mode while disk reflects the other.
// - With lock-coverage, the second Pause blocks for the duration of
// the first SaveState (~ms on local SQLite WAL, not a hot path).
//
// IsPaused is scope-aware (I-7 fix): paused_after_apply returns true
// ONLY for ScopeWorkerDispatch; the LLMPreCall and AfterCommit scopes
// pass through so the in-flight apply stage finishes naturally.
//
// # Persist-first invariant
//
// Pause and Resume call persist.SaveState BEFORE mutating g.state. If
// SaveState returns an error, in-memory state is unchanged so a daemon
// crash mid-call leaves consistent state across memory + disk (no
// divergence on restart). The error is wrapped + returned to the
// caller; the caller is expected to retry, surface to operator, or
// handle via the audit hook.
//
// Buggy ordering (pre-I-6): mutate g.state → persist. If persist
// failed mid-call, in-memory said "paused" but on-disk said "running"
// → restart loaded "running" while the operator believed "paused".
//
// # Idempotency contract
//
// - NewOperatorGate is idempotent across multiple invocations.
// - State / IsPaused / TransitionLog are idempotent (no side effects).
// - Pause is idempotent in the sense that pausing while paused
// replaces the mode (e.g. quiet supersedes descriptive when an
// anomaly fires on top of an operator pause). Each Pause call
// persists + appends a log entry.
// - Resume is a no-op when already running (returns nil without
// calling persist or appending a log entry).
//
// # Validation defense-in-depth (N-3)
//
// LoadState is validated by the GateAdapter at the SQLite CHECK
// constraint boundary (the operator_gate_state.state column rejects
// values outside the four-state enum). The duplicated state-string
// switch in NewOperatorGate is intentional: it protects against
// alternative GatePersist implementations (in-memory test stubs,
// future storage backends) that might not enforce the CHECK. Two
// layers of validation catch one drift, per defense-in-depth doctrine.
package gate
