package eventlog

import (
	"context"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

type malformedJSONEvent struct{}

func (malformedJSONEvent) Type() EventType          { return EvtOrchestratorStarted }
func (malformedJSONEvent) Payload() ([]byte, error) { return []byte("not-json"), nil }

func TestCorruptPayloadCountIncrementsOnUnmarshalFailure(t *testing.T) {
	before := CorruptPayloadCount()

	r := Record{
		EventID:   1,
		SessionID: "s",
		ProjectID: "p",
		EventType: EvtOrchestratorStarted,
		Payload:   []byte("not-json"),
		Timestamp: time.Now().UnixNano(),
	}
	ev := recordToEvent(r)

	if ev.Payload != nil {
		t.Errorf("expected Payload nil on malformed JSON, got %+v", ev.Payload)
	}
	after := CorruptPayloadCount()
	if delta := after - before; delta != 1 {
		t.Errorf("CorruptPayloadCount delta = %d, want 1", delta)
	}

	_ = recordToEvent(r)
	if delta := CorruptPayloadCount() - before; delta != 2 {
		t.Errorf("after 2 corruptions: delta = %d, want 2", delta)
	}

	rOK := Record{
		EventID:   2,
		SessionID: "s",
		ProjectID: "p",
		EventType: EvtOrchestratorStarted,
		Payload:   []byte(`{"k":"v"}`),
		Timestamp: time.Now().UnixNano(),
	}
	_ = recordToEvent(rOK)
	if delta := CorruptPayloadCount() - before; delta != 2 {
		t.Errorf("after valid row: delta = %d, want 2 (no increment expected)", delta)
	}

	rEmpty := Record{
		EventID:   3,
		SessionID: "s",
		ProjectID: "p",
		EventType: EvtOrchestratorStarted,
		Timestamp: time.Now().UnixNano(),
	}
	_ = recordToEvent(rEmpty)
	if delta := CorruptPayloadCount() - before; delta != 2 {
		t.Errorf("after empty-payload row: delta = %d, want 2 (no increment expected)", delta)
	}
}

func TestSubscribeEventsCorruptPayloadYieldsNilMap(t *testing.T) {
	em := &inMemoryEmitter{}
	log := New(em, clock.NewFake(time.Unix(1700000000, 0)))

	ch, cancel := log.SubscribeEvents(Filter{})
	defer cancel()

	if _, err := log.Append(context.Background(), Event{
		Type:      EvtOrchestratorStarted,
		SessionID: "s", ProjectID: "p",
		Payload: map[string]any{"k": "v"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("seed not received")
	}

	if _, err := log.appendTyped(context.Background(), "s", "p", malformedJSONEvent{}); err != nil {
		t.Fatalf("appendTyped malformed: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Payload != nil {
			t.Errorf("expected nil Payload on malformed JSON, got %+v", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("malformed event not delivered")
	}
}
