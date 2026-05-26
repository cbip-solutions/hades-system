package eventlog_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestAppendEventRoundTrip(t *testing.T) {
	em := &injectableEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := eventlog.New(em, fc)

	ctx := context.Background()
	id, err := log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: "sess-A",
		ProjectID: "proj-1",
		Payload: map[string]any{
			"worker_id": "w-7",
			"task_id":   "t-3",
			"tier":      "t1_bypass",
		},
	})
	if err != nil {
		t.Fatalf("Append(Event): %v", err)
	}
	if id == 0 {
		t.Fatalf("Append returned zero event_id")
	}

	rows, err := log.Query(ctx, "sess-A", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.EventType != eventlog.EvtWorkerDispatched {
		t.Errorf("EventType = %v, want EvtWorkerDispatched", r.EventType)
	}
	if r.SessionID != "sess-A" || r.ProjectID != "proj-1" {
		t.Errorf("identity tags wrong: session=%q project=%q", r.SessionID, r.ProjectID)
	}
	if r.Timestamp != fc.Now().UnixNano() {
		t.Errorf("Timestamp = %d, want clock-stamped %d", r.Timestamp, fc.Now().UnixNano())
	}

	if len(r.Payload) == 0 {
		t.Fatalf("Payload empty; want JSON map bytes")
	}
	if !strings.Contains(string(r.Payload), `"worker_id":"w-7"`) {
		t.Errorf("Payload missing worker_id; got %s", r.Payload)
	}
}

func TestAppendEventHonorsExplicitTimestamp(t *testing.T) {
	em := &injectableEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := eventlog.New(em, fc)

	want := time.Unix(1500000000, 123)
	_, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
		Timestamp: want,
		Payload:   map[string]any{"autonomy_mode": "semi"},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	rows, err := log.Query(context.Background(), "s", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if rows[0].Timestamp != want.UnixNano() {
		t.Errorf("Timestamp = %d, want %d (caller-supplied)", rows[0].Timestamp, want.UnixNano())
	}
}

func TestAppendEventCausalChain(t *testing.T) {
	em := &injectableEmitter{}
	log := eventlog.New(em, clock.NewFake(time.Unix(1700000000, 0)))

	chain := []string{"hash-A", "hash-B"}
	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:        eventlog.EvtOrchestratorStarted,
		SessionID:   "s",
		ProjectID:   "p",
		CausalChain: chain,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	sub := log.Subscribe(eventlog.Filter{}, 4)
	defer sub.Close()
	if _, err := log.Append(context.Background(), eventlog.Event{
		Type:        eventlog.EvtOrchestratorStopped,
		SessionID:   "s",
		ProjectID:   "p",
		CausalChain: []string{"hash-C"},
	}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	select {
	case rec := <-sub.Events():
		if len(rec.CausalChain) != 1 || rec.CausalChain[0] != "hash-C" {
			t.Errorf("CausalChain = %v, want [hash-C]", rec.CausalChain)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event in time")
	}
}

func TestAppendEventValidation(t *testing.T) {
	log := eventlog.New(&injectableEmitter{}, clock.NewFake(time.Unix(1700000000, 0)))

	_, err := log.Append(context.Background(), eventlog.Event{
		Type: eventlog.EvtOrchestratorStarted, ProjectID: "p",
	})
	if err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Errorf("empty session_id err = %v, want session_id mention", err)
	}
	_, err = log.Append(context.Background(), eventlog.Event{
		Type: eventlog.EvtOrchestratorStarted, SessionID: "s",
	})
	if err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Errorf("empty project_id err = %v, want project_id mention", err)
	}

	logNoEmit := eventlog.New(nil, clock.NewFake(time.Unix(1700000000, 0)))
	_, err = logNoEmit.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
	})
	if err != eventlog.ErrNoEmitter {
		t.Errorf("err = %v, want ErrNoEmitter", err)
	}
}

// TestAppendEventPayloadMarshalError surfaces an error when Payload contains
// a non-JSON-serializable value, and the error message MUST NOT include the
// payload contents (privacy IMP-3).
func TestAppendEventPayloadMarshalError(t *testing.T) {
	log := eventlog.New(&injectableEmitter{}, clock.NewFake(time.Unix(1700000000, 0)))

	_, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
		Payload:   map[string]any{"bad": make(chan int)},
	})
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
	if strings.Contains(err.Error(), "chan int") {

	}
	if strings.Contains(err.Error(), "{") {
		t.Errorf("error appears to leak payload structure: %v", err)
	}
}

func TestNewMemorySmoke(t *testing.T) {
	clk := clock.NewFake(time.Unix(1700000000, 0))
	log := eventlog.NewMemory(clk)

	id, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if id == 0 {
		t.Errorf("event_id zero")
	}
	rows, err := log.Query(context.Background(), "s", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}

func TestNewMemoryNilClockPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil clock")
		}
	}()
	_ = eventlog.NewMemory(nil)
}

func TestAppenderInterfaceSatisfiedByLog(t *testing.T) {
	var _ eventlog.Appender = (*eventlog.Log)(nil)

	var ap eventlog.Appender = eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	id, err := ap.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
	})
	if err != nil {
		t.Fatalf("Appender.Append: %v", err)
	}
	if id == 0 {
		t.Errorf("event_id zero")
	}
}

func TestSubscribeEventsFanOut(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	ch, cancel := log.SubscribeEvents(eventlog.Filter{
		Types: []eventlog.EventType{eventlog.EvtWorkerDispatched},
	})
	defer cancel()

	ctx := context.Background()
	if _, err := log.Append(ctx, eventlog.Event{
		Type: eventlog.EvtOrchestratorStarted, SessionID: "s", ProjectID: "p",
	}); err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if _, err := log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtWorkerDispatched,
		SessionID: "s", ProjectID: "p",
		Payload: map[string]any{"worker_id": "w-1"},
	}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed prematurely")
		}
		if ev.Type != eventlog.EvtWorkerDispatched {
			t.Errorf("Type = %v, want EvtWorkerDispatched", ev.Type)
		}
		if ev.SessionID != "s" || ev.ProjectID != "p" {
			t.Errorf("identity tags wrong: %+v", ev)
		}
		if ev.Payload["worker_id"] != "w-1" {
			t.Errorf("Payload[worker_id] = %v, want w-1", ev.Payload["worker_id"])
		}
		if ev.Timestamp.IsZero() {
			t.Errorf("Timestamp zero (should be reconstituted from Record.Timestamp)")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive expected event")
	}
}

func TestSubscribeEventsCancelIdempotent(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ch, cancel := log.SubscribeEvents(eventlog.Filter{})

	cancel()
	cancel()
	cancel()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("Event channel not closed after cancel")
}

func TestSubscribeEventsCancelClosesChannel(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ch, cancel := log.SubscribeEvents(eventlog.Filter{})

	cancel()

	timeout := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("channel not closed within 1s of cancel")
		}
	}
}

func TestSubscribeEventsDropOldest(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	ch, cancel := log.SubscribeEvents(eventlog.Filter{})
	defer cancel()

	const n = 250
	for i := 0; i < n; i++ {
		if _, err := log.Append(context.Background(), eventlog.Event{
			Type:      eventlog.EvtWorkerDispatched,
			SessionID: "s", ProjectID: "p",
			Payload: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)

	got := 0
collect:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break collect
			}
			got++
		case <-time.After(100 * time.Millisecond):
			break collect
		}
	}
	if got == 0 {
		t.Fatalf("got 0 events, want some delivered")
	}
	if got > 100 {
		t.Fatalf("got %d events, want <= 100 (buffer cap)", got)
	}
}

func TestRecordToEventEncodingParity(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	ch, cancel := log.SubscribeEvents(eventlog.Filter{})
	defer cancel()

	want := eventlog.Event{
		Type:        eventlog.EvtOrchestratorStarted,
		SessionID:   "sess-1",
		ProjectID:   "proj-1",
		Timestamp:   time.Unix(1234567890, 42),
		Payload:     map[string]any{"k": "v"},
		CausalChain: []string{"a", "b"},
	}
	if _, err := log.Append(context.Background(), want); err != nil {
		t.Fatalf("Append: %v", err)
	}

	select {
	case got := <-ch:
		if got.Type != want.Type {
			t.Errorf("Type = %v, want %v", got.Type, want.Type)
		}
		if got.SessionID != want.SessionID || got.ProjectID != want.ProjectID {
			t.Errorf("identity mismatch: got %+v want %+v", got, want)
		}
		if got.Timestamp.UnixNano() != want.Timestamp.UnixNano() {
			t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
		}
		if len(got.CausalChain) != 2 || got.CausalChain[0] != "a" || got.CausalChain[1] != "b" {
			t.Errorf("CausalChain = %v, want [a b]", got.CausalChain)
		}

		if got.Payload["k"] != "v" {
			t.Errorf("Payload[k] = %v, want v", got.Payload["k"])
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestAppendEventInvalidType(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	_, err := log.Append(context.Background(), eventlog.Event{
		SessionID: "s", ProjectID: "p",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid event type") {
		t.Errorf("zero-value type err = %v, want invalid event type", err)
	}

	_, err = log.Append(context.Background(), eventlog.Event{
		Type: eventlog.EventType(9999), SessionID: "s", ProjectID: "p",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid event type") {
		t.Errorf("oob type err = %v, want invalid event type", err)
	}
}

func TestAppendEventEmitErrorPath(t *testing.T) {
	em := &errEmitter{err: errors.New("disk-full simulated")}
	log := eventlog.New(em, clock.NewFake(time.Unix(1700000000, 0)))
	_, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
		Payload:   map[string]any{"secret": "shh"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "shh") || strings.Contains(err.Error(), "secret") {
		t.Errorf("error leaks payload: %v", err)
	}
	if !strings.Contains(err.Error(), "disk-full") {
		t.Errorf("error does not wrap underlying: %v", err)
	}
}

type errEmitter struct{ err error }

func (e *errEmitter) EmitRaw(ctx context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	return 0, e.err
}
func (e *errEmitter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	return nil, e.err
}

func TestAppendEventUnsupportedValueErrorRedacted(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	_, err := log.Append(context.Background(), eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
		Payload:   map[string]any{"x": math.Inf(1)},
	})
	if err == nil {
		t.Fatal("expected marshal error on +Inf")
	}
	if strings.Contains(err.Error(), "+Inf") || strings.Contains(err.Error(), "Inf") {
		t.Errorf("error leaks rendered value: %v", err)
	}
	if !strings.Contains(err.Error(), "redacted") {
		t.Errorf("expected redaction marker in error: %v", err)
	}
}

func TestQuerySkipsOtherSessions(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ctx := context.Background()
	for _, sess := range []string{"s-A", "s-B"} {
		if _, err := log.Append(ctx, eventlog.Event{
			Type: eventlog.EvtOrchestratorStarted, SessionID: sess, ProjectID: "p",
		}); err != nil {
			t.Fatalf("Append %s: %v", sess, err)
		}
	}
	rows, err := log.Query(ctx, "s-A", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0].SessionID != "s-A" {
		t.Fatalf("rows = %+v, want exactly s-A", rows)
	}
}

func TestQuerySincePastFirstEvent(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ctx := context.Background()
	id1, err := log.Append(ctx, eventlog.Event{
		Type: eventlog.EvtOrchestratorStarted, SessionID: "s", ProjectID: "p",
	})
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if _, err := log.Append(ctx, eventlog.Event{
		Type: eventlog.EvtOrchestratorStopped, SessionID: "s", ProjectID: "p",
	}); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	rows, err := log.Query(ctx, "s", id1)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0].EventType != eventlog.EvtOrchestratorStopped {
		t.Fatalf("expected only Stopped, got %+v", rows)
	}
}

func TestSubscribeEventsCancelManyTimes(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ctx := context.Background()

	const iterations = 200
	for i := 0; i < iterations; i++ {
		ch, cancel := log.SubscribeEvents(eventlog.Filter{})

		go func() {
			for j := 0; j < 8; j++ {
				_, _ = log.Append(ctx, eventlog.Event{
					Type:      eventlog.EvtWorkerDispatched,
					SessionID: "s",
					ProjectID: "p",
					Payload:   map[string]any{"j": j},
				})
			}
		}()
		cancel()

		drainEventCh(t, ch, 500*time.Millisecond)
	}
}

func drainEventCh(t *testing.T, ch <-chan eventlog.Event, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("event channel did not close before timeout")
		}
	}
}

func TestSubscribeEventsHandlesUnderlyingDone(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))
	ch, cancel := log.SubscribeEvents(eventlog.Filter{})
	cancel()

	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("event channel not closed")
		}
	}
}

type injectableEmitter struct {
	mu     sync.Mutex
	rows   []eventlog.Record
	nextID int64
}

func (e *injectableEmitter) EmitRaw(ctx context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextID++
	e.rows = append(e.rows, eventlog.Record{
		EventID:   e.nextID,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: eventlog.EventType(eventType),
		Payload:   append([]byte(nil), payload...),
		Timestamp: ts,
	})
	return e.nextID, nil
}
func (e *injectableEmitter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]eventlog.Record, 0, len(e.rows))
	for _, r := range e.rows {
		if r.SessionID == sessionID && r.EventID > since {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestNewMemoryEmitterCtxCancelled(t *testing.T) {
	log := eventlog.NewMemory(clock.NewFake(time.Unix(1700000000, 0)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtOrchestratorStarted,
		SessionID: "s",
		ProjectID: "p",
	})
	if err == nil {
		t.Fatal("expected ctx-cancel error")
	}
	if !strings.Contains(err.Error(), "ctx") {
		t.Errorf("err = %v, want ctx mention", err)
	}
}
