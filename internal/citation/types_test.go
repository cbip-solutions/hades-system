// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// internal/citation/types_test.go — Task D-1.
//
// Round-trip + zero-value + minimal-valid tests for the citation envelope
// substrate types. Deeper property-based round-trip lives in
// envelope_test.go / property_test.go (D-2 / D-7).
package citation_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

func TestEnvelopeZeroValueValid(t *testing.T) {
	var env citation.Envelope
	if err := env.Validate(); err == nil {
		t.Fatal("zero-value Envelope must be invalid (missing required fields)")
	}
}

func TestEnvelopeMinimalValid(t *testing.T) {
	env := citation.Envelope{
		ID:           citation.CitationID("c-abcdef0123456789"),
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-0001-2026-05-10",
		Confidence:   0.94,
		RRFScore:     0.0162,
		RRFRank:      0,
		ProjectID:    "internal-platform-x",
		Payload:      "MergeEngine.Score()",
		Expiration:   time.Time{},
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("minimal valid Envelope rejected: %v", err)
	}
}

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	env := citation.Envelope{
		ID:           citation.CitationID("c-abcdef0123456789"),
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteQuery,
		Lane:         citation.LaneSemantic,
		AuditEventID: "evt-0001-2026-05-10",
		Confidence:   0.94,
		RRFScore:     0.0162,
		RRFRank:      0,
		Expiration:   time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		ProjectID:    "internal-platform-x",
		Payload:      "MergeEngine.Score()",
		PlatformRenders: citation.PlatformRenders{
			"ink": citation.PlatformRender{"clickable": json.RawMessage(`true`)},
		},
	}
	raw, err := json.Marshal(&env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got citation.Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != env.ID {
		t.Errorf("ID round-trip: want %s, got %s", env.ID, got.ID)
	}
	if got.Type != env.Type || got.Source != env.Source || got.Lane != env.Lane {
		t.Errorf("enum round-trip drift")
	}
	if got.Confidence != env.Confidence || got.RRFScore != env.RRFScore || got.RRFRank != env.RRFRank {
		t.Errorf("score round-trip drift")
	}
	if !got.Expiration.Equal(env.Expiration) {
		t.Errorf("Expiration round-trip drift: want %v, got %v", env.Expiration, got.Expiration)
	}
	if got.ProjectID != env.ProjectID || got.Payload != env.Payload {
		t.Errorf("metadata round-trip drift")
	}
	if got.PlatformRenders["ink"]["clickable"] == nil {
		t.Errorf("PlatformRenders not preserved")
	}
}

func TestCitationIDValidates(t *testing.T) {
	cases := []struct {
		id      citation.CitationID
		wantErr bool
	}{
		{"c-abc", false},
		{"c-abcdef0123456789", false},
		{"c-aa", false},
		{"c-a", true},
		{"abcdef", true},
		{"c-A", true},
		{"c-abc!", true},
		{"", true},
		{"c-abcdef01234567890123", true},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			err := tc.id.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate(%q): want err=%v, got err=%v", tc.id, tc.wantErr, err)
			}
		})
	}
}

func TestEnvelopeAuditEventURL(t *testing.T) {
	env := citation.Envelope{AuditEventID: "evt-0001-2026-05-10"}
	got := env.AuditEventURL()
	want := "zen://audit/evt-0001-2026-05-10"
	if got != want {
		t.Errorf("AuditEventURL: want %s got %s", want, got)
	}
}

func TestMarkdownFallbackPlatform(t *testing.T) {
	m := citation.NewMarkdownFallback(nil)
	if m.Platform() != "markdown" {
		t.Errorf("Platform: want 'markdown', got %s", m.Platform())
	}
}
