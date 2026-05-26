package replay_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/tests/replay"
)

type fixedQuerier struct{ rows []eventlog.Record }

func (q *fixedQuerier) QueryRaw(_ context.Context, _ string, since int64) ([]eventlog.Record, error) {
	out := make([]eventlog.Record, 0, len(q.rows))
	for _, r := range q.rows {
		if r.EventID > since {
			out = append(out, r)
		}
	}
	return out, nil
}

func makeFixedQuerier(t *testing.T, sessionID string) *fixedQuerier {
	t.Helper()
	t0 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	pl := func(p eventlog.PayloadEncoder) []byte {
		t.Helper()
		b, err := p.Payload()
		if err != nil {
			t.Fatalf("payload: %v", err)
		}
		return b
	}
	return &fixedQuerier{rows: []eventlog.Record{
		{EventID: 1, SessionID: sessionID, ProjectID: "p", EventType: eventlog.EvtOrchestratorStarted, Payload: pl(eventlog.OrchestratorStarted{SessionID: sessionID, ProjectID: "p", AutonomyMode: "semi"}), Timestamp: t0.UnixNano()},
		{EventID: 2, SessionID: sessionID, ProjectID: "p", EventType: eventlog.EvtWorkerDispatched, Payload: pl(eventlog.WorkerDispatched{WorkerID: "W1", TaskID: "T-1", Tier: "t1_bypass"}), Timestamp: t0.Add(time.Second).UnixNano()},
		{EventID: 3, SessionID: sessionID, ProjectID: "p", EventType: eventlog.EvtOrchestratorStopped, Payload: pl(eventlog.OrchestratorStopped{Outcome: "success"}), Timestamp: t0.Add(2 * time.Second).UnixNano()},
	}}
}

func TestLoadJSONL_RoundTripsCapture(t *testing.T) {
	q := makeFixedQuerier(t, "sess-rt")
	var buf bytes.Buffer
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-rt",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	loaded, err := replay.LoadJSONL(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	if len(loaded.Events) != 3 {
		t.Fatalf("loaded %d events, want 3", len(loaded.Events))
	}
	if loaded.Header.SessionID != "sess-rt" {
		t.Fatalf("header session mismatch: %s", loaded.Header.SessionID)
	}
	if loaded.Header.Version != 1 {
		t.Fatalf("header version = %d, want 1", loaded.Header.Version)
	}
	if loaded.Footer.EventCount != 3 || loaded.Footer.FirstEventID != 1 || loaded.Footer.LastEventID != 3 {
		t.Fatalf("footer mismatch: %+v", loaded.Footer)
	}

	for i, ce := range loaded.Events {
		if !ce.Event.Type.IsValid() {
			t.Errorf("event %d: invalid type %v", i, ce.Event.Type)
		}
		if ce.Event.Payload == nil {
			t.Errorf("event %d: nil payload", i)
		}
	}
}

func TestLoadJSONL_RejectsTruncated(t *testing.T) {
	bad := `{"kind":"header","version":1,"session_id":"x","captured_at":"2026-04-30T12:00:00Z","redacted":true,"metadata_sha256":"deadbeef"}` + "\n" +
		`{"kind":"event","event_id":1,"timestamp":"2026-04-30T12:00:01Z","event_type":"OrchestratorStarted","payload":{"session_id":"x","project_id":"p","autonomy_mode":"semi"}}` + "\n"

	_, err := replay.LoadJSONL(strings.NewReader(bad))
	if !errors.Is(err, replay.ErrTruncatedFixture) {
		t.Fatalf("got %v, want ErrTruncatedFixture", err)
	}
}

func TestLoadJSONL_RejectsBadSha(t *testing.T) {
	tampered := `{"kind":"header","version":1,"session_id":"x","captured_at":"2026-04-30T12:00:00Z","redacted":true,"metadata_sha256":"0000000000000000000000000000000000000000000000000000000000000000"}` + "\n" +
		`{"kind":"event","event_id":1,"timestamp":"2026-04-30T12:00:01Z","event_type":"OrchestratorStarted","payload":{"session_id":"x","project_id":"p","autonomy_mode":"semi"}}` + "\n" +
		`{"kind":"footer","event_count":1,"first_event_id":1,"last_event_id":1}` + "\n"
	_, err := replay.LoadJSONL(strings.NewReader(tampered))
	if !errors.Is(err, replay.ErrCorruptedFixture) {
		t.Fatalf("got %v, want ErrCorruptedFixture", err)
	}
}

func TestLoadJSONL_RejectsUnknownLineKind(t *testing.T) {
	bad := `{"kind":"header","version":1,"session_id":"x","captured_at":"2026-04-30T12:00:00Z","redacted":true,"metadata_sha256":"x"}` + "\n" +
		`{"kind":"trailer","extra":1}` + "\n"
	_, err := replay.LoadJSONL(strings.NewReader(bad))
	if !errors.Is(err, replay.ErrUnknownLineKind) {
		t.Fatalf("got %v, want ErrUnknownLineKind", err)
	}
}

func TestLoadJSONL_RejectsMalformedLine(t *testing.T) {
	bad := `{"kind":"header","version":1,"session_id":"x","captured_at":"2026-04-30T12:00:00Z","redacted":true,"metadata_sha256":"x"}` + "\n" +
		`not-json` + "\n"
	_, err := replay.LoadJSONL(strings.NewReader(bad))
	if err == nil {
		t.Fatalf("expected decode error, got nil")
	}
	if errors.Is(err, replay.ErrCorruptedFixture) || errors.Is(err, replay.ErrTruncatedFixture) {
		t.Fatalf("expected raw decode error, got sentinel: %v", err)
	}
}

func TestAssertEquivalentEvents_AllowTimestamp(t *testing.T) {

	payload := map[string]any{"worker_id": "W1", "task_id": "T-1", "tier": "t1_bypass"}
	e1 := eventlog.Event{Type: eventlog.EvtWorkerDispatched, Timestamp: time.Now(), Payload: payload}
	e2 := eventlog.Event{Type: eventlog.EvtWorkerDispatched, Timestamp: time.Now().Add(time.Hour), Payload: payload}

	tt := &testing.T{}
	replay.AssertEquivalentEvents(tt, []eventlog.Event{e1}, []eventlog.Event{e2})
	if tt.Failed() {
		t.Fatalf("AssertEquivalentEvents flagged equivalent events as different")
	}
}

func TestAssertEquivalentEvents_DetectsBodyDiff(t *testing.T) {
	e1 := eventlog.Event{Type: eventlog.EvtOrchestratorStarted, Payload: map[string]any{"session_id": "s", "project_id": "p", "autonomy_mode": "semi"}}
	e2 := eventlog.Event{Type: eventlog.EvtOrchestratorStarted, Payload: map[string]any{"session_id": "s", "project_id": "p", "autonomy_mode": "manual"}}
	tt := &testing.T{}
	replay.AssertEquivalentEvents(tt, []eventlog.Event{e1}, []eventlog.Event{e2})
	if !tt.Failed() {
		t.Fatalf("AssertEquivalentEvents missed real body diff (autonomy_mode field)")
	}
}

func TestAssertEquivalentEvents_DetectsLengthMismatch(t *testing.T) {
	e1 := eventlog.Event{Type: eventlog.EvtOrchestratorStarted, Payload: map[string]any{"session_id": "s"}}
	tt := &testing.T{}
	replay.AssertEquivalentEvents(tt, []eventlog.Event{e1}, []eventlog.Event{e1, e1})
	if !tt.Failed() {
		t.Fatalf("AssertEquivalentEvents missed length mismatch")
	}
}

func TestAssertEquivalentEvents_WithIgnorePayloadKeys(t *testing.T) {

	e1 := eventlog.Event{Type: eventlog.EvtWorkerDispatched, Payload: map[string]any{"worker_id": "W1", "task_id": "T-1", "tier": "t1_bypass"}}
	e2 := eventlog.Event{Type: eventlog.EvtWorkerDispatched, Payload: map[string]any{"worker_id": "W2", "task_id": "T-1", "tier": "t1_bypass"}}
	tt := &testing.T{}
	replay.AssertEquivalentEvents(tt, []eventlog.Event{e1}, []eventlog.Event{e2},
		replay.WithIgnorePayloadKeys(eventlog.EvtWorkerDispatched, "worker_id"))
	if tt.Failed() {
		t.Fatalf("WithIgnorePayloadKeys mask did not apply")
	}

	tt2 := &testing.T{}
	replay.AssertEquivalentEvents(tt2, []eventlog.Event{e1}, []eventlog.Event{e2})
	if !tt2.Failed() {
		t.Fatalf("worker_id diff should fail without mask")
	}
}

func TestEventsOf_ProjectsCapturedEvents(t *testing.T) {
	q := makeFixedQuerier(t, "sess-eo")
	var buf bytes.Buffer
	if _, err := eventlog.Capture(context.Background(), q, eventlog.CaptureOptions{
		SessionID:  "sess-eo",
		CapturedAt: time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		Output:     &buf,
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	loaded, err := replay.LoadJSONL(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadJSONL: %v", err)
	}
	evs := loaded.EventsOf()
	if len(evs) != len(loaded.Events) {
		t.Fatalf("EventsOf len = %d, want %d", len(evs), len(loaded.Events))
	}
	for i, ev := range evs {
		if ev.Type != loaded.Events[i].Event.Type {
			t.Errorf("event[%d] type drift: %v vs %v", i, ev.Type, loaded.Events[i].Event.Type)
		}
	}
}

func TestLoadJSONL_RejectsUnknownEventType(t *testing.T) {

	bad := `{"kind":"event","event_id":1,"timestamp":"2026-04-30T12:00:01Z","event_type":"NotAnEventType","payload":{}}` + "\n"

	_, err := replay.LoadJSONL(strings.NewReader(bad))
	if err == nil {
		t.Fatalf("expected error for unknown event_type")
	}
	if !strings.Contains(err.Error(), "unknown event_type") {
		t.Fatalf("expected unknown event_type error, got: %v", err)
	}
}
