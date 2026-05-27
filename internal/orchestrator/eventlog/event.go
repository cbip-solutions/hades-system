// SPDX-License-Identifier: MIT
// internal/orchestrator/eventlog/event.go
package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

var corruptPayloadCount atomic.Uint64

func CorruptPayloadCount() uint64 {
	return corruptPayloadCount.Load()
}

// Event is the canonical in-memory shape consumers across phases
// (B, D, E, F, G, H, K, M) construct + emit. It is the LOAD-BEARING wire
// shape between orchestrator subsystems.
//
// Distinction from Record (the durable wire shape from Task A-3):
// - Event carries an unmarshaled Payload map for ergonomics; Append
// marshals to JSON before writing the Record.
// - Event.Timestamp is time.Time for caller convenience; Record.Timestamp
// is unix nanoseconds for storage compactness.
// - Both share Type/SessionID/ProjectID/CausalChain identity.
//
// READ-ONLY contract (N-2 carry-forward from Record): Event.Payload and
// Event.CausalChain are shared by reference once an Event is delivered via
// SubscribeEvents. Subscribers MUST NOT mutate them. Treat both as immutable;
// copy if retention or modification is needed.
type Event struct {
	Type        EventType      `json:"type"`
	SessionID   string         `json:"session_id"`
	ProjectID   string         `json:"project_id"`
	Timestamp   time.Time      `json:"timestamp"`
	Payload     map[string]any `json:"payload,omitempty"`
	CausalChain []string       `json:"causal_chain,omitempty"`
}

type Appender interface {
	Append(ctx context.Context, ev Event) (int64, error)
}

type CancelFunc func()

func (l *Log) Append(ctx context.Context, ev Event) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("eventlog.Append: ctx cancelled before start: %w", err)
	}
	if !ev.Type.IsValid() {
		return 0, fmt.Errorf("eventlog.Append: invalid event type %v (use a registered EvtX constant)", ev.Type)
	}
	if l.emit == nil {
		return 0, ErrNoEmitter
	}
	if ev.SessionID == "" {
		return 0, fmt.Errorf("eventlog.Append: empty session_id (event_type=%v)", ev.Type)
	}
	if ev.ProjectID == "" {
		return 0, fmt.Errorf("eventlog.Append: empty project_id (event_type=%v session_id=%q)", ev.Type, ev.SessionID)
	}
	var payload []byte
	if ev.Payload != nil {
		b, err := json.Marshal(ev.Payload)
		if err != nil {
			// IMP-3: do not echo payload contents; only event type.
			return 0, fmt.Errorf("eventlog.Append: marshal payload (event_type=%v): %w", ev.Type, sanitizeMarshalErr(err))
		}
		payload = b
	}
	ts := ev.Timestamp
	if ts.IsZero() {
		ts = l.clock.Now()
	}
	id, err := l.emit.EmitRaw(ctx, ev.ProjectID, ev.SessionID, int(ev.Type), payload, ts.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("eventlog.Append: emit (event_type=%v session_id=%q project_id=%q): %w",
			ev.Type, ev.SessionID, ev.ProjectID, err)
	}
	rec := Record{
		EventID:     id,
		SessionID:   ev.SessionID,
		ProjectID:   ev.ProjectID,
		EventType:   ev.Type,
		Payload:     payload,
		Timestamp:   ts.UnixNano(),
		CausalChain: ev.CausalChain,
	}
	l.subs.publish(rec)
	return id, nil
}

func sanitizeMarshalErr(err error) error {
	var uve *json.UnsupportedValueError
	if errors.As(err, &uve) {
		return errors.New("json: unsupported value (redacted for privacy)")
	}
	return err
}

func NewMemory(clk clock.Clock) *Log {
	if clk == nil {
		panic("eventlog.NewMemory: nil clock")
	}
	return New(newInMemoryEmitter(), clk)
}

func recordToEvent(r Record) Event {
	ev := Event{
		Type:        r.EventType,
		SessionID:   r.SessionID,
		ProjectID:   r.ProjectID,
		Timestamp:   time.Unix(0, r.Timestamp),
		CausalChain: r.CausalChain,
	}
	if len(r.Payload) > 0 {

		var m map[string]any
		if err := json.Unmarshal(r.Payload, &m); err != nil {
			corruptPayloadCount.Add(1)
		} else {
			ev.Payload = m
		}
	}
	return ev
}

const subscribeEventsBufferSize = 100

func (l *Log) SubscribeEvents(filter Filter) (<-chan Event, CancelFunc) {
	sub := l.Subscribe(filter, subscribeEventsBufferSize)
	out := make(chan Event, subscribeEventsBufferSize)
	done := make(chan struct{})
	var cancelOnce sync.Once

	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case <-sub.Done():
				return
			case rec := <-sub.Events():

				ev := recordToEvent(rec)
				// Non-blocking send with drop-oldest fallback. We do NOT
				// watch <-done here: if cancel fires mid-dispatch the
				// outer select picks it up on the next iteration (which
				// is bounded by the time it takes to push or drop one
				// event — microseconds). Keeping the inner select two-
				// way (send | default) ensures the publisher path stays
				// non-blocking under all conditions.
				select {
				case out <- ev:

				default:

					select {
					case <-out:
					default:
					}
					select {
					case out <- ev:
					default:

					}
				}
			}
		}
	}()

	cancel := CancelFunc(func() {
		cancelOnce.Do(func() {
			close(done)
			sub.Close()
		})
	})
	return out, cancel
}

type inMemoryRawEmitter struct {
	mu     sync.Mutex
	rows   []Record
	nextID int64
}

func newInMemoryEmitter() *inMemoryRawEmitter { return &inMemoryRawEmitter{} }

func (m *inMemoryRawEmitter) EmitRaw(ctx context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.rows = append(m.rows, Record{
		EventID:   id,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: EventType(eventType),
		Payload:   append([]byte(nil), payload...),
		Timestamp: ts,
	})
	return id, nil
}

func (m *inMemoryRawEmitter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]Record, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.rows))
	for _, r := range m.rows {
		if r.SessionID != sessionID {
			continue
		}
		if r.EventID <= since {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
