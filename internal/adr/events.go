// SPDX-License-Identifier: MIT
package adr

import (
	"sync"
	"time"
)

type EventType string

const (
	EvtADRProposed EventType = "adr.proposed"

	EvtADRAccepted EventType = "adr.accepted"

	EvtADRRejected EventType = "adr.rejected"

	EvtADRSuperseded EventType = "adr.superseded"

	EvtADRDeprecated EventType = "adr.deprecated"
)

type EventPayload struct {
	ADRID string `json:"adr_id"`

	StatusFrom Status `json:"status_from"`

	StatusTo Status `json:"status_to"`

	OperatorID string `json:"operator_id,omitempty"`

	Reason string `json:"reason,omitempty"`

	// Timestamp is the wall-clock time at which the transition was
	// recorded. Callers MUST set this to time.Now().UTC() at emit time;
	// zero-value is rejected by the audit-chain adapter as a schema
	// violation.
	Timestamp time.Time `json:"timestamp"`
}

type EventSink interface {
	Emit(t EventType, p EventPayload) error
}

type RecordedEvent struct {
	Type    EventType
	Payload EventPayload
}

type RecordingEventSink struct {
	mu       sync.Mutex
	Recorded []RecordedEvent
}

func (s *RecordingEventSink) Emit(t EventType, p EventPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Recorded = append(s.Recorded, RecordedEvent{Type: t, Payload: p})
	return nil
}

func (s *RecordingEventSink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Recorded = s.Recorded[:0]
}

type NoopEventSink struct{}

func (NoopEventSink) Emit(_ EventType, _ EventPayload) error { return nil }

func EventTypeForTransition(from, to Status) (EventType, bool) {
	switch {
	case from == StatusProposed && to == StatusAccepted:
		return EvtADRAccepted, true
	case from == StatusProposed && to == StatusRejected:
		return EvtADRRejected, true
	case from == StatusAccepted && to == StatusSuperseded:
		return EvtADRSuperseded, true
	case from == StatusAccepted && to == StatusDeprecated:
		return EvtADRDeprecated, true
	default:
		return "", false
	}
}
