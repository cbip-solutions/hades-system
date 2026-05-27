package eventlog

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

type inMemoryEmitter struct {
	mu     sync.Mutex
	rows   []Record
	nextID int64
}

func (m *inMemoryEmitter) EmitRaw(ctx context.Context, projectID, sessionID string, et int, payload []byte, ts int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	id := m.nextID
	m.rows = append(m.rows, Record{
		EventID:   id,
		SessionID: sessionID,
		ProjectID: projectID,
		EventType: EventType(et),
		Payload:   append([]byte(nil), payload...),
		Timestamp: ts,
	})
	return id, nil
}

func (m *inMemoryEmitter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]Record, error) {
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

func TestLogAppendThenQuery(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	ctx := context.Background()
	id1, err := log.appendTyped(ctx, "sess-1", "proj-A", OrchestratorStarted{
		SessionID: "sess-1", ProjectID: "proj-A", AutonomyMode: "semi",
	})
	if err != nil {
		t.Fatalf("Append 1: %v", err)
	}
	if id1 == 0 {
		t.Errorf("Append returned zero event_id")
	}
	fc.Advance(2 * time.Second)
	id2, err := log.appendTyped(ctx, "sess-1", "proj-A", WorkerDispatched{
		WorkerID: "w-1", TaskID: "t-1", Tier: "t1_bypass",
	})
	if err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	if id2 <= id1 {
		t.Errorf("event_id not monotonic: id1=%d id2=%d", id1, id2)
	}

	rows, err := log.Query(ctx, "sess-1", 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Query returned %d rows, want 2", len(rows))
	}
	if rows[0].EventType != EvtOrchestratorStarted {
		t.Errorf("rows[0] type = %v want OrchestratorStarted", rows[0].EventType)
	}
	if rows[0].ProjectID != "proj-A" {
		t.Errorf("rows[0] project_id = %q want proj-A", rows[0].ProjectID)
	}
	if rows[0].SessionID != "sess-1" {
		t.Errorf("rows[0] session_id = %q want sess-1", rows[0].SessionID)
	}
	if rows[0].Timestamp == rows[1].Timestamp {
		t.Errorf("timestamps did not advance with Fake clock")
	}
}

func TestLogQueryFiltersBySession(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	ctx := context.Background()
	if _, err := log.appendTyped(ctx, "sess-A", "proj-1", OrchestratorStarted{SessionID: "sess-A"}); err != nil {
		t.Fatalf("append A1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "sess-B", "proj-1", OrchestratorStarted{SessionID: "sess-B"}); err != nil {
		t.Fatalf("append B: %v", err)
	}
	if _, err := log.appendTyped(ctx, "sess-A", "proj-1", OrchestratorStopped{Outcome: "success"}); err != nil {
		t.Fatalf("append A2: %v", err)
	}

	rowsA, err := log.Query(ctx, "sess-A", 0)
	if err != nil {
		t.Fatalf("Query A: %v", err)
	}
	if len(rowsA) != 2 {
		t.Errorf("session-A query: got %d want 2", len(rowsA))
	}
	rowsB, err := log.Query(ctx, "sess-B", 0)
	if err != nil {
		t.Fatalf("Query B: %v", err)
	}
	if len(rowsB) != 1 {
		t.Errorf("session-B query: got %d want 1", len(rowsB))
	}
}

func TestLogQuerySinceCursor(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	id1, err := log.appendTyped(ctx, "sess-1", "proj-1", OrchestratorStarted{})
	if err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "sess-1", "proj-1", WorkerDispatched{WorkerID: "w-1"}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	rows, err := log.Query(ctx, "sess-1", id1)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("since cursor: got %d rows want 1", len(rows))
	}
	if rows[0].EventType != EvtWorkerDispatched {
		t.Errorf("since cursor returned wrong row")
	}
}

type errorEmitter struct{ err error }

func (e *errorEmitter) EmitRaw(ctx context.Context, _, _ string, _ int, _ []byte, _ int64) (int64, error) {
	return 0, e.err
}
func (e *errorEmitter) QueryRaw(ctx context.Context, _ string, _ int64) ([]Record, error) {
	return nil, e.err
}

func TestLogAppendEmitterError(t *testing.T) {
	em := &errorEmitter{err: context.DeadlineExceeded}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "s", "p", OrchestratorStarted{})
	if err == nil {
		t.Fatalf("Append did not propagate emitter error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected wrapped DeadlineExceeded, got %v", err)
	}
}

func TestLogQueryEmitterError(t *testing.T) {
	em := &errorEmitter{err: context.Canceled}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	rows, err := log.Query(context.Background(), "s", 0)
	if err == nil {
		t.Fatalf("Query did not propagate emitter error")
	}
	if rows != nil {
		t.Errorf("Query returned non-nil rows on error: %v", rows)
	}
}

func TestLogAppendNoEmitter(t *testing.T) {
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(nil, fc)
	_, err := log.appendTyped(context.Background(), "s", "p", OrchestratorStarted{})
	if !errors.Is(err, ErrNoEmitter) {
		t.Errorf("expected ErrNoEmitter, got %v", err)
	}
}

func TestLogQueryNoEmitter(t *testing.T) {
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(nil, fc)
	_, err := log.Query(context.Background(), "s", 0)
	if !errors.Is(err, ErrNoEmitter) {
		t.Errorf("expected ErrNoEmitter, got %v", err)
	}
}

func TestLogNewNilClockPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("New with nil clock did not panic")
		}
	}()
	_ = New(&inMemoryEmitter{}, nil)
}

func TestLogAppendNilEvent(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "s", "p", nil)
	if err == nil {
		t.Fatalf("Append did not reject nil PayloadEncoder")
	}
}

func TestLogAppendEmptySession(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "", "p", OrchestratorStarted{})
	if err == nil {
		t.Fatalf("Append did not reject empty session_id")
	}
}

func TestLogAppendEmptyProject(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "s", "", OrchestratorStarted{})
	if err == nil {
		t.Fatalf("Append did not reject empty project_id")
	}
}

type badEventWithUnknownType struct{}

func (badEventWithUnknownType) Type() EventType          { return 0 }
func (badEventWithUnknownType) Payload() ([]byte, error) { return []byte("{}"), nil }

func TestLogAppendRejectsInvalidEventType(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "sess", "proj", badEventWithUnknownType{})
	if err == nil {
		t.Fatalf("Append did not reject EvtUnknown event type")
	}

	if strings.Contains(err.Error(), "{}") {
		t.Errorf("Append error leaked payload bytes (IMP-3 violation): %v", err)
	}
}

type reservedEvent struct{}

func (reservedEvent) Type() EventType          { return EventType(100) }
func (reservedEvent) Payload() ([]byte, error) { return []byte("{}"), nil }

func TestLogAppendRejectsReservedEventType(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "sess", "proj", reservedEvent{})
	if err == nil {
		t.Fatalf("Append did not reject out-of-range event type")
	}
}

type payloadErrorEvent struct{}

func (payloadErrorEvent) Type() EventType { return EvtOrchestratorStarted }
func (payloadErrorEvent) Payload() ([]byte, error) {
	return nil, errors.New("synthetic-payload-encode-failure")
}

func TestLogAppendPayloadEncodeError(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	_, err := log.appendTyped(context.Background(), "sess", "proj", payloadErrorEvent{})
	if err == nil {
		t.Fatalf("Append did not propagate payload-encode error")
	}
	if !strings.Contains(err.Error(), "synthetic-payload-encode-failure") {
		t.Errorf("payload error not wrapped: %v", err)
	}
}

func TestLogAppendRejectsCancelledContext(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := log.appendTyped(ctx, "sess", "proj", OrchestratorStarted{
		SessionID: "sess", ProjectID: "proj", AutonomyMode: "semi",
	})
	if err == nil {
		t.Fatalf("Append did not reject pre-cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got %v", err)
	}

	em.mu.Lock()
	defer em.mu.Unlock()
	if len(em.rows) != 0 {
		t.Errorf("Append wrote %d row(s) on cancelled ctx; expected 0", len(em.rows))
	}
}

func TestLogQueryRejectsCancelledContext(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rows, err := log.Query(ctx, "sess", 0)
	if err == nil {
		t.Fatalf("Query did not reject pre-cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected wrapped context.Canceled, got %v", err)
	}
	if rows != nil {
		t.Errorf("Query returned non-nil rows on cancelled ctx: %v", rows)
	}
}

// I-2: since=0 is the documented public sentinel for "all events for
// the session" — audit_events_raw event_ids are 1-indexed by the
// adapter, so "strictly > 0" returns every row. Task A-4
// Replay depends on this contract; hash-chain emitters MUST
// preserve it.
func TestLogQuerySinceZeroReturnsAll(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	if _, err := log.appendTyped(ctx, "sess", "proj", OrchestratorStarted{
		SessionID: "sess", ProjectID: "proj", AutonomyMode: "semi",
	}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "sess", "proj", WorkerDispatched{
		WorkerID: "w-1", TaskID: "t-1", Tier: "t1_bypass",
	}); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if _, err := log.appendTyped(ctx, "sess", "proj", OrchestratorStopped{
		Outcome: "success",
	}); err != nil {
		t.Fatalf("append 3: %v", err)
	}

	rows, err := log.Query(ctx, "sess", 0)
	if err != nil {
		t.Fatalf("Query since=0: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("since=0 returned %d records; expected 3 (1-indexed event_ids)", len(rows))
	}
	if rows[0].EventType != EvtOrchestratorStarted ||
		rows[1].EventType != EvtWorkerDispatched ||
		rows[2].EventType != EvtOrchestratorStopped {
		t.Errorf("since=0 returned wrong order: %v", rows)
	}
}
