// Copyright 2026 hades-system contributors. SPDX-License-Identifier: MIT

package citation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

type AuditEventRow struct {
	ID        string
	ProjectID string
	Type      string
	Doctrine  string
	Payload   string
	EmittedAt int64
}

func EnvelopeFromAuditEvent(row AuditEventRow) (Envelope, error) {
	if row.ID == "" || row.ProjectID == "" {
		return Envelope{}, errors.New("EnvelopeFromAuditEvent: ID + ProjectID required")
	}

	citIDBody := sanitiseForCitationID(row.ID)
	if citIDBody == "" {

		citIDBody = "0000"
	}
	if len(citIDBody) < 2 {
		citIDBody = citIDBody + "00"
	}
	citID := "c-" + citIDBody
	env := Envelope{
		ID:           CitationID(citID),
		Type:         CitationTypeAuditEvent,
		Source:       SourceManualOverride,
		Lane:         LaneSemantic,
		AuditEventID: row.ID,
		Confidence:   1.0,
		RRFScore:     0,
		RRFRank:      -1,
		ProjectID:    row.ProjectID,
		Payload:      formatAuditPayload(row),
	}
	if err := env.Validate(); err != nil {
		return Envelope{}, fmt.Errorf("EnvelopeFromAuditEvent: %w", err)
	}
	return env, nil
}

func sanitiseForCitationID(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
		default:

			continue
		}
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return string(out)
}

func formatAuditPayload(row AuditEventRow) string {
	return fmt.Sprintf("[%s] %s @ %s (doctrine=%s)\n%s",
		row.Type, row.ID, row.ProjectID, row.Doctrine, row.Payload)
}

func (e *Envelope) ValidateStrict() error {
	if math.IsNaN(e.Confidence) || math.IsInf(e.Confidence, 0) {
		return fmt.Errorf("envelope.Confidence not finite: %v", e.Confidence)
	}
	if math.IsNaN(e.RRFScore) || math.IsInf(e.RRFScore, 0) {
		return fmt.Errorf("envelope.RRFScore not finite: %v", e.RRFScore)
	}
	return e.Validate()
}

func (e *Envelope) canonicalJSON() ([]byte, error) {

	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("canonicalJSON marshal: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("canonicalJSON re-unmarshal: %w", err)
	}
	return marshalSortedMap(m)
}

func marshalSortedMap(m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, _ := json.Marshal(k)
		buf = append(buf, kb...)
		buf = append(buf, ':')
		v := m[k]
		var vb []byte
		var err error
		if sub, ok := v.(map[string]any); ok {
			vb, err = marshalSortedMap(sub)
		} else {
			vb, err = json.Marshal(v)
		}
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf, nil
}

// Hash returns the deterministic 64-bit content hash of the envelope.
// Used for cache-lookup + dedup; NOT a security primitive. Truncated
// SHA-256 over canonical JSON.
//
// invariant corollary: two envelopes with identical fields produce
// identical hashes; one field difference produces different hash with
// near-100% probability (collision space 64 bits ≈ 1.8e19).
func (e *Envelope) Hash() uint64 {
	raw, err := e.canonicalJSON()
	if err != nil {

		raw, _ = json.Marshal(e)
	}
	sum := sha256.Sum256(raw)
	return binary.BigEndian.Uint64(sum[:8])
}
