// SPDX-License-Identifier: MIT
// internal/orchestrator/eventlog/eventlog.go
package eventlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

// Record is the durable wire shape of an event row, returned by Query
// and propagated to subscribers (task). HADES design will hash-chain
// these via prev_hash; leaves CausalChain as the seam.
//
// Payload is the raw JSON bytes encoded by PayloadEncoder.Payload().
// Callers decode via Decode(et, payload). Timestamp is unix nanoseconds
// stamped at Append time via the injected clock.Clock.
//
// READ-ONLY contract (N-2): Record.Payload is shared by reference with
// the originating PayloadEncoder and the future task subscriber
// fan-out path. Subscribers and Query callers MUST NOT mutate the
// slice — mutation corrupts later subscriber deliveries and any
// concurrent Query reading the same row from a caching emitter.
// Treat Payload as immutable; copy with append([]byte(nil), p...) if
// the caller needs to retain or modify it.
type Record struct {
	EventID     int64
	SessionID   string
	ProjectID   string
	EventType   EventType
	Payload     []byte
	Timestamp   int64
	CausalChain []string
}

type RawEmitter interface {
	EmitRaw(ctx context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error)

	QueryRaw(ctx context.Context, sessionID string, since int64) ([]Record, error)
}

// Log is the orchestrator's append-only event log over audit_events_raw.
// All mutations go through Append; reads through Query. Subscribers
// receive in-process notifications after the durable write succeeds
// (task).
//
// Goroutine safety (I-3): Log itself holds no mutable state — it is
// safe for concurrent use by any number of goroutines. Serialization
// is delegated downstream:
// - RawEmitter implementations own their own synchronization
// (the in-memory test emitter uses sync.Mutex; the
// store-backed adapter relies on SQLite's WAL serialization).
// - subscriberHub (subscriber.go) owns synchronization of its
// subscriber set and per-mailbox publish path; Subscribe/publish/
// Close are all goroutine-safe.
//
// Eight downstream HADES design phases (D, E, F, G, H, K, M plus Replay)
// emit through Log from concurrent worker + reviewer goroutines;
// Append/Query MUST remain lock-free at this layer. Reviewers of any
// future change to Log MUST verify this property still holds.
type Log struct {
	emit  RawEmitter
	clock clock.Clock
	subs  *subscriberHub
}

var ErrNoEmitter = errors.New("eventlog: no RawEmitter wired")

// New constructs a Log. clk MUST be non-nil — passing nil panics
// because clock injection is the load-bearing primitive of
// emit MAY be nil; in that case Append/Query return ErrNoEmitter.
func New(emit RawEmitter, clk clock.Clock) *Log {
	if clk == nil {
		panic("eventlog: New called with nil clock")
	}
	return &Log{
		emit:  emit,
		clock: clk,
		subs:  newSubscriberHub(),
	}
}

// appendTyped serializes evt and writes a row to audit_events_raw via the
// configured RawEmitter. Returns the new event_id (monotonic per
// session). After the durable write succeeds, in-process subscribers
// are notified via subscriberHub.publish; back-pressure is drop-oldest
// (subscriber.go).
//
// appendTyped is the typed-PayloadEncoder path used internally by
// (Replay's corruption-detection emit) and by package-internal tests
// exercising the typed-encoder error paths. The method is intentionally
// PACKAGE-PRIVATE (I-2 from A-5b code review): cross-stage consumers
// (Phases B/D/E/F/G/H/K/M) MUST use the canonical Event-shape Append
// (event.go) which satisfies the Appender interface. There is no external
// caller of appendTyped — the rename locks the canonical surface to
// Append(Event) so downstream phases cannot pick the wrong API.
//
// Validation order (fail-fast):
// 1. ctx not already cancelled / deadline-exceeded (I-1: short-circuit
// before any work — wasted validation + spurious emit on a future
// non-cancel-aware emitter).
// 2. evt non-nil
// 3. evt.Type() must be IsValid() — rejects EvtUnknown (zero value)
// and any out-of-range values unavailable into AllEventTypes()
// (IMP-2 from task fix pass; HADES design J-2 promoted the
// reserved-for- slots 40-42 to valid).
// 4. sessionID + projectID non-empty (invariant tagging contract)
// 5. payload encode
// 6. emitter call
//
// Errors NEVER include payload bytes (privacy contract IMP-3): they
// must be safe to log at error level even though payloads can carry
// pre-redacted secrets that emitters take responsibility for.
func (l *Log) appendTyped(ctx context.Context, sessionID, projectID string, evt PayloadEncoder) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("eventlog.appendTyped: ctx cancelled before start: %w", err)
	}
	if evt == nil {
		return 0, errors.New("eventlog.appendTyped: nil PayloadEncoder")
	}
	et := evt.Type()
	if !et.IsValid() {
		return 0, fmt.Errorf("eventlog.appendTyped: invalid event type %v (use a registered EvtX constant)", et)
	}
	if l.emit == nil {
		return 0, ErrNoEmitter
	}
	if sessionID == "" {
		return 0, fmt.Errorf("eventlog.appendTyped: empty session_id (event_type=%v)", et)
	}
	if projectID == "" {
		return 0, fmt.Errorf("eventlog.appendTyped: empty project_id (event_type=%v session_id=%q)", et, sessionID)
	}
	payload, err := evt.Payload()
	if err != nil {
		// IMP-3: do not log raw payload here; only the event type.
		return 0, fmt.Errorf("eventlog.appendTyped: encode payload (event_type=%v): %w", et, err)
	}
	ts := l.clock.Now().UnixNano()
	id, err := l.emit.EmitRaw(ctx, projectID, sessionID, int(et), payload, ts)
	if err != nil {

		return 0, fmt.Errorf("eventlog.appendTyped: emit (event_type=%v session_id=%q project_id=%q): %w", et, sessionID, projectID, err)
	}
	rec := Record{
		EventID:   id,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: et,
		Payload:   payload,
		Timestamp: ts,
	}
	l.subs.publish(rec)
	return id, nil
}

// Query returns all records for sessionID with event_id strictly > since,
// ordered by event_id ascending. Caller decodes payload bytes via
// Decode(rec.EventType, rec.Payload).
//
// Use since=0 to read all events for the session — audit_events_raw
// event_ids are 1-indexed by the adapter, so "strictly > 0" is
// the canonical "all events" sentinel. All RawEmitter implementations
// MUST honor
// this contract; task Replay relies on it for crash recovery from
// a fresh boot. Use since=lastSeenID to resume replay from a checkpoint.
//
// Like Append, Query short-circuits on a pre-cancelled ctx (I-1).
func (l *Log) Query(ctx context.Context, sessionID string, since int64) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("eventlog.Query: ctx cancelled before start: %w", err)
	}
	if l.emit == nil {
		return nil, ErrNoEmitter
	}
	return l.emit.QueryRaw(ctx, sessionID, since)
}
