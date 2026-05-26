// SPDX-License-Identifier: MIT
// internal/orchestrator/eventlog/replay.go
package eventlog

import (
	"context"
	"errors"
	"fmt"
)

// ReplayCorruptionBudget is the inv-zen-095 cap: at most this many
// corrupted events tolerated per session replay before the orchestrator
// MUST transition to HARD_PAUSED. Surfaced as an exported constant so
// callers (Phase D HARD_PAUSED handler, Phase E recovery, Phase H
// observability dashboards) read the same magic number that doc.go
// references — never duplicate the literal "5" elsewhere.
const ReplayCorruptionBudget = 5

// ErrCorruptionBudgetExceeded is returned by Replay on the
// (ReplayCorruptionBudget+1)th corrupted event encountered for a given
// session. The caller MUST transition the orchestrator to HARD_PAUSED
// (Phase D wires this; Phase E uses it as a recovery signal). Replay
// returns the partial ReplayState alongside this error so the caller
// can audit how far reconstruction progressed before halting.
var ErrCorruptionBudgetExceeded = errors.New("eventlog: replay corruption budget exceeded (inv-zen-095)")

type ReplayState struct {
	SessionID       string
	EventsReplayed  int
	EventsCorrupted int

	// LastEventID is the highest event_id consumed by the row-fold loop
	// (NIT-3). 0 when no rows were processed (empty session, pre-start
	// cancel). Advances even across corrupt rows because the cursor
	// MUST move past them — Phase E warm-resume + Phase H recovery_progress
	// metrics use this as the since= cursor for an incremental
	// Query(since=LastEventID) on the next read.
	LastEventID int64

	LastTransition string

	InFlightWorkers map[string]WorkerDispatched

	PendingWaves map[string]ReviewerWaveStarted

	OpenConfirmations map[string]ConfirmationRequested
}

// Replay reads every event for sessionID via QueryRaw(since=0) and
// reconstructs ReplayState by folding each decoded event into the
// state accumulator.
//
// Inv-zen-095 contract: at most ReplayCorruptionBudget corrupted events
// are tolerated. Each skip is itself audited via a ReplayCorruptionDetected
// event re-appended to the log (best-effort; emit failures during the
// audit-of-corruption do NOT abort replay because the corruption signal
// is more important to preserve than the audit perfection). On the
// (budget+1)th corruption, Replay emits the audit row for that
// breaching corruption FIRST (IMP-2: every skip is audited, including
// the one that trips HARD_PAUSED — post-mortem must see N+1 not just
// N), then returns ErrCorruptionBudgetExceeded alongside the partial
// state.
//
// IMP-3 privacy contract: when emitting ReplayCorruptionDetected, the
// Reason field MUST NOT include raw payload bytes — only event-type
// metadata + the corrupt row's event_id offset + a brief error class.
// This holds even when Decode's error message itself doesn't leak
// payload bytes, because future Decode implementations might (e.g.,
// json.Unmarshal returning the offending substring); we treat the
// error string as untrusted by construction.
//
// IMP-2 IsValid guard (carry-forward from A-2): rows whose event_type
// is not in AllEventTypes() — i.e., out-of-range values not yet wired
// by a Plan phase — fall through Decode's default branch and are
// counted as corruption. This is the load-bearing closed-set
// invariant: a row that the eventlog package does not recognize is
// indistinguishable from a corrupted row at the recovery layer. (Plan
// 5 J-2 wired the apply-engine slots 40-42 into the closed set; future
// reservations follow the same wire-then-validate pattern.)
//
// I-1 ctx.Err() short-circuit (carry-forward from A-3): a pre-cancelled
// ctx aborts before the QueryRaw call, mirroring Append/Query.
//
// Performance for sessions <1000 events, replay completes in <500ms
// (spec target). The hot loop is Decode + a small switch; allocation
// is bounded by the row count. Phase A asserts <2s in CI as the
// upper-bound guard.
func (l *Log) Replay(ctx context.Context, sessionID string) (*ReplayState, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("eventlog.Replay: ctx cancelled before start: %w", err)
	}
	if l.emit == nil {
		return nil, ErrNoEmitter
	}
	rows, err := l.emit.QueryRaw(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("eventlog.Replay: query: %w", err)
	}
	st := &ReplayState{
		SessionID:         sessionID,
		InFlightWorkers:   make(map[string]WorkerDispatched),
		PendingWaves:      make(map[string]ReviewerWaveStarted),
		OpenConfirmations: make(map[string]ConfirmationRequested),
	}
	for i, r := range rows {
		// IMP-1 mid-loop cancel honor: large sessions (10k events) push
		// the <5s recovery spec; daemon shutdown / Phase E timeout MUST
		// preempt the row-fold. We check every 256 iterations starting
		// at i=256 (the top-of-Replay check above already covers i=0).
		// 256 is chosen so the per-check overhead is amortized across
		// dozens of decode+apply cycles while still bounding worst-case
		// latency-to-honor-cancel to ~256 rows of work.
		if i > 0 && i&0xFF == 0 {
			if err := ctx.Err(); err != nil {
				return st, fmt.Errorf("eventlog.Replay: ctx cancelled mid-replay after %d/%d rows: %w", i, len(rows), err)
			}
		}
		st.EventsReplayed++
		st.LastEventID = r.EventID
		decoded, derr := Decode(r.EventType, r.Payload)
		if derr != nil {
			st.EventsCorrupted++

			_, _ = l.appendTyped(ctx, sessionID, r.ProjectID, ReplayCorruptionDetected{
				EventOffset: r.EventID,
				Reason:      sanitizeCorruptionReason(r.EventType),
			})
			if st.EventsCorrupted > ReplayCorruptionBudget {
				return st, ErrCorruptionBudgetExceeded
			}
			continue
		}
		applyToState(st, decoded)
	}
	return st, nil
}

// sanitizeCorruptionReason returns a fixed-shape, payload-free reason
// string for ReplayCorruptionDetected. The shape is stable across
// Decode implementations: changing Decode's internal error wording
// MUST NOT change the audit log's Reason wording. (IMP-3 privacy
// contract: the Reason field is queryable post-hoc by any operator
// with audit_events_raw read access, so it MUST be free of raw payload
// bytes — even error-string-derived ones.)
func sanitizeCorruptionReason(et EventType) string {
	if !et.IsValid() {
		return fmt.Sprintf("decode failed: unknown or reserved event_type=%d", int(et))
	}
	return fmt.Sprintf("decode failed: malformed payload for event_type=%s", et)
}

func applyToState(st *ReplayState, ev PayloadEncoder) {
	switch e := ev.(type) {
	case OrchestratorStateTransition:
		st.LastTransition = e.To
	case WorkerDispatched:
		st.InFlightWorkers[e.WorkerID] = e
	case WorkerCheckpoint:
		delete(st.InFlightWorkers, e.WorkerID)
	case WorkerDeath:
		delete(st.InFlightWorkers, e.WorkerID)
	case WorkerRedispatched:

		_ = e
	case ReviewerWaveStarted:
		st.PendingWaves[e.Layer] = e
	case ReviewerWaveComplete:
		delete(st.PendingWaves, e.Layer)
	case ConfirmationRequested:
		st.OpenConfirmations[e.EventID] = e
	case OperatorConfirmation:
		delete(st.OpenConfirmations, e.EventID)
	}
}
