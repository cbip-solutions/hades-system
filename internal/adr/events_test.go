package adr_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/adr"
)

func TestEventTypeStrings(t *testing.T) {
	cases := []struct {
		name string
		et   adr.EventType
		want string
	}{
		{"proposed", adr.EvtADRProposed, "adr.proposed"},
		{"accepted", adr.EvtADRAccepted, "adr.accepted"},
		{"rejected", adr.EvtADRRejected, "adr.rejected"},
		{"superseded", adr.EvtADRSuperseded, "adr.superseded"},
		{"deprecated", adr.EvtADRDeprecated, "adr.deprecated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.et) != tc.want {
				t.Errorf("EventType %q: got %q, want %q", tc.name, string(tc.et), tc.want)
			}
		})
	}
}

func TestEventPayloadJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	orig := adr.EventPayload{
		ADRID:      "ADR-0042",
		StatusFrom: adr.StatusProposed,
		StatusTo:   adr.StatusAccepted,
		OperatorID: "testuser",
		Reason:     "approved in planning session",
		Timestamp:  ts,
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got adr.EventPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ADRID != orig.ADRID {
		t.Errorf("ADRID: got %q, want %q", got.ADRID, orig.ADRID)
	}
	if got.StatusFrom != orig.StatusFrom {
		t.Errorf("StatusFrom: got %q, want %q", got.StatusFrom, orig.StatusFrom)
	}
	if got.StatusTo != orig.StatusTo {
		t.Errorf("StatusTo: got %q, want %q", got.StatusTo, orig.StatusTo)
	}
	if got.OperatorID != orig.OperatorID {
		t.Errorf("OperatorID: got %q, want %q", got.OperatorID, orig.OperatorID)
	}
	if got.Reason != orig.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, orig.Reason)
	}
	if !got.Timestamp.Equal(orig.Timestamp) {
		t.Errorf("Timestamp: got %v, want %v", got.Timestamp, orig.Timestamp)
	}
}

func TestEventSinkRecordedEvents(t *testing.T) {
	sink := &adr.RecordingEventSink{}

	payload := adr.EventPayload{
		ADRID:      "ADR-0001",
		StatusFrom: adr.StatusProposed,
		StatusTo:   adr.StatusAccepted,
		OperatorID: "testuser",
		Reason:     "approved",
		Timestamp:  time.Now(),
	}

	if err := sink.Emit(adr.EvtADRProposed, payload); err != nil {
		t.Fatalf("Emit(EvtADRProposed): %v", err)
	}
	if err := sink.Emit(adr.EvtADRAccepted, payload); err != nil {
		t.Fatalf("Emit(EvtADRAccepted): %v", err)
	}

	recorded := sink.Recorded
	if len(recorded) != 2 {
		t.Fatalf("len(Recorded): got %d, want 2", len(recorded))
	}
	if recorded[0].Type != adr.EvtADRProposed {
		t.Errorf("Recorded[0].Type: got %q, want %q", recorded[0].Type, adr.EvtADRProposed)
	}
	if recorded[1].Type != adr.EvtADRAccepted {
		t.Errorf("Recorded[1].Type: got %q, want %q", recorded[1].Type, adr.EvtADRAccepted)
	}

	sink.Reset()
	if len(sink.Recorded) != 0 {
		t.Errorf("after Reset: len(Recorded) = %d, want 0", len(sink.Recorded))
	}

	noop := &adr.NoopEventSink{}
	if err := noop.Emit(adr.EvtADRProposed, payload); err != nil {
		t.Errorf("NoopEventSink.Emit: unexpected error: %v", err)
	}
}

func TestEventTypeForTransition(t *testing.T) {
	valid := []struct {
		from adr.Status
		to   adr.Status
		want adr.EventType
	}{
		{adr.StatusProposed, adr.StatusAccepted, adr.EvtADRAccepted},
		{adr.StatusProposed, adr.StatusRejected, adr.EvtADRRejected},
		{adr.StatusAccepted, adr.StatusSuperseded, adr.EvtADRSuperseded},
		{adr.StatusAccepted, adr.StatusDeprecated, adr.EvtADRDeprecated},
	}
	for _, tc := range valid {
		et, ok := adr.EventTypeForTransition(tc.from, tc.to)
		if !ok {
			t.Errorf("EventTypeForTransition(%q, %q): ok=false, want true", tc.from, tc.to)
			continue
		}
		if et != tc.want {
			t.Errorf("EventTypeForTransition(%q, %q): got %q, want %q", tc.from, tc.to, et, tc.want)
		}
	}

	invalid := [][2]adr.Status{
		{adr.StatusAccepted, adr.StatusProposed},
		{adr.StatusRejected, adr.StatusAccepted},
		{adr.StatusSuperseded, adr.StatusDeprecated},
		{adr.StatusDeprecated, adr.StatusSuperseded},
		{adr.StatusProposed, adr.StatusSuperseded},
		{adr.StatusProposed, adr.StatusDeprecated},
	}
	for _, pair := range invalid {
		_, ok := adr.EventTypeForTransition(pair[0], pair[1])
		if ok {
			t.Errorf("EventTypeForTransition(%q, %q): ok=true, want false (invalid transition)", pair[0], pair[1])
		}
	}
}
