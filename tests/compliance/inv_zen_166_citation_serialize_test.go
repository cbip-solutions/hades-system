// Copyright 2026 zen-swarm contributors. SPDX-License-Identifier: MIT
//
// tests/compliance/inv_zen_166_citation_serialize_test.go — D-8.
//
// Compliance test for invariant: citation envelope structured
// serialization preserves to Tessera audit chain natively
// (no flattening, no field loss).
//
// Anchored at compile-time via internal/citation/sentinel.go's
// envelopeJSONSchemaSentinel; runtime via property test
// (internal/citation/property_test.go); compliance test below
// extends with Tessera-leaf round-trip assertion.
package compliance

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type SyntheticTesseraLeaf struct {
	LeafID       string          `json:"leaf_id"`
	EventType    string          `json:"event_type"`
	EmittedAt    int64           `json:"emitted_at"`
	Payload      json.RawMessage `json:"payload"`
	PrevLeafHash string          `json:"prev_leaf_hash"`
}

func TestInvZen166CitationEnvelopeSerializePreserves(t *testing.T) {
	t.Parallel()

	cases := []citation.Envelope{

		{
			ID:           "c-kgnode01abcdef",
			Type:         citation.CitationTypeKGNode,
			Source:       citation.SourceCaronteQuery,
			Lane:         citation.LaneSemantic,
			AuditEventID: "evt-2026-05-10-0001",
			Confidence:   0.94,
			RRFScore:     0.01627,
			RRFRank:      0,
			ProjectID:    "internal-platform-x",
			Payload:      "MergeEngine.Score(candidates []Candidate) (Winner, error)",
		},

		{
			ID:           "c-expires0001ab",
			Type:         citation.CitationTypeCommunitySummary,
			Source:       citation.SourceCaronteContext,
			Lane:         citation.LaneGraph,
			AuditEventID: "evt-2026-05-10-0002",
			Confidence:   0.78,
			RRFScore:     0.01234,
			RRFRank:      3,
			Expiration:   time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
			ProjectID:    "internal-platform-x",
			Payload:      "Community summary: orchestration cluster (12 nodes, 47 edges)",
			PlatformRenders: citation.PlatformRenders{
				"ink": citation.PlatformRender{
					"clickable": json.RawMessage(`true`),
					"preview":   json.RawMessage(`"hover preview text"`),
				},
				"telegram": citation.PlatformRender{
					"inline_button_text": json.RawMessage(`"View community"`),
					"callback_data":      json.RawMessage(`"cmt-0001"`),
				},
			},
		},

		{
			ID:           "c-manual0001abcd",
			Type:         citation.CitationTypeFileSlice,
			Source:       citation.SourceManualOverride,
			Lane:         citation.LaneSemantic,
			AuditEventID: "evt-2026-05-10-0003",
			Confidence:   1.0,
			RRFScore:     0,
			RRFRank:      -1,
			ProjectID:    "internal-platform-x",
			Payload:      "manual: spec §1 Q9 lines 196-209",
		},
	}

	for i, env := range cases {
		t.Run(string(env.ID), func(t *testing.T) {
			if err := env.Validate(); err != nil {
				t.Fatalf("case %d: invalid envelope: %v", i, err)
			}

			envJSON, err := json.Marshal(&env)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			leaf := SyntheticTesseraLeaf{
				LeafID:       "leaf-" + string(env.ID),
				EventType:    "AugmentationCompleted",
				EmittedAt:    time.Now().Unix(),
				Payload:      envJSON,
				PrevLeafHash: "prev-hash-stub",
			}
			leafJSON, err := json.Marshal(&leaf)
			if err != nil {
				t.Fatalf("marshal leaf: %v", err)
			}

			var gotLeaf SyntheticTesseraLeaf
			if err := json.Unmarshal(leafJSON, &gotLeaf); err != nil {
				t.Fatalf("unmarshal leaf: %v", err)
			}

			var gotEnv citation.Envelope
			if err := json.Unmarshal(gotLeaf.Payload, &gotEnv); err != nil {
				t.Fatalf("unmarshal envelope from leaf: %v", err)
			}

			if gotEnv.ID != env.ID {
				t.Errorf("ID drift: want %s got %s", env.ID, gotEnv.ID)
			}
			if gotEnv.Type != env.Type || gotEnv.Source != env.Source || gotEnv.Lane != env.Lane {
				t.Errorf("enum drift")
			}
			if gotEnv.Confidence != env.Confidence || gotEnv.RRFScore != env.RRFScore || gotEnv.RRFRank != env.RRFRank {
				t.Errorf("score drift")
			}
			if !gotEnv.Expiration.Equal(env.Expiration) {
				t.Errorf("Expiration drift")
			}
			if gotEnv.ProjectID != env.ProjectID || gotEnv.Payload != env.Payload {
				t.Errorf("metadata drift")
			}
			if !reflect.DeepEqual(env.PlatformRenders, gotEnv.PlatformRenders) {
				t.Errorf("PlatformRenders drift: want %+v got %+v", env.PlatformRenders, gotEnv.PlatformRenders)
			}
		})
	}
}

// TestInvZen166RetiredGitnexusSourceAliasResolves is the
// back-compat gate: the code-graph citation sources were renamed
// gitnexus_* -> caronte_* in the cutover, but the Tessera audit chain has
// HISTORICAL leaves whose envelope payload carries the OLD "gitnexus_query" /
// "gitnexus_context" wire values. `zen audit` inspection of those pre-cutover
// rows MUST still resolve them — citation.ParseCitationSource keeps the old
// values as read-side aliases mapping to the current caronte_* enum. A
// regression here would make pre- audit events un-inspectable (a silent
// history-loss bug), so this test bite-checks the alias by deserializing a
// synthetic leaf carrying the retired value.
func TestInvZen166RetiredGitnexusSourceAliasResolves(t *testing.T) {
	t.Parallel()

	cases := []struct {
		retiredWire string
		want        citation.CitationSource
	}{
		{"gitnexus_query", citation.SourceCaronteQuery},
		{"gitnexus_context", citation.SourceCaronteContext},
	}
	for _, tc := range cases {
		t.Run(tc.retiredWire, func(t *testing.T) {

			got, ok := citation.ParseCitationSource(tc.retiredWire)
			if !ok {
				t.Fatalf("ParseCitationSource(%q) = (_, false); retired alias must still resolve (inv-zen-166)", tc.retiredWire)
			}
			if got != tc.want {
				t.Errorf("ParseCitationSource(%q) = %q; want %q (retired alias maps to current caronte enum)", tc.retiredWire, got, tc.want)
			}

			leafPayload := []byte(`{"id":"c-precut0001ab","type":"kg_node","source":"` + tc.retiredWire +
				`","retrieval_lane":"semantic","audit_event_id":"evt-precut","confidence":0.9,` +
				`"rrf_score":0.01,"rrf_rank":0,"project_id":"internal-platform-x","payload":"pre-cutover row"}`)
			leaf := SyntheticTesseraLeaf{
				LeafID:       "leaf-precut-" + tc.retiredWire,
				EventType:    "AugmentationCompleted",
				EmittedAt:    time.Now().Unix(),
				Payload:      leafPayload,
				PrevLeafHash: "prev-hash-stub",
			}
			leafJSON, err := json.Marshal(&leaf)
			if err != nil {
				t.Fatalf("marshal leaf: %v", err)
			}
			var gotLeaf SyntheticTesseraLeaf
			if err := json.Unmarshal(leafJSON, &gotLeaf); err != nil {
				t.Fatalf("unmarshal leaf: %v", err)
			}
			var gotEnv citation.Envelope
			if err := json.Unmarshal(gotLeaf.Payload, &gotEnv); err != nil {
				t.Fatalf("unmarshal envelope from pre-cutover leaf: %v", err)
			}

			if string(gotEnv.Source) != tc.retiredWire {
				t.Errorf("pre-cutover envelope Source = %q; want verbatim %q", gotEnv.Source, tc.retiredWire)
			}
			resolved, ok := citation.ParseCitationSource(string(gotEnv.Source))
			if !ok || resolved != tc.want {
				t.Errorf("resolve(pre-cutover Source %q) = (%q, %v); want (%q, true)", gotEnv.Source, resolved, ok, tc.want)
			}
		})
	}
}

func TestInvZen166HashStabilityAcrossLeaves(t *testing.T) {
	t.Parallel()

	env := citation.Envelope{
		ID: "c-stable01abcdef", Type: citation.CitationTypeKGNode,
		Source: citation.SourceCaronteQuery, Lane: citation.LaneSemantic,
		AuditEventID: "evt-stable", Confidence: 0.5, RRFScore: 0.01, RRFRank: 0,
		ProjectID: "p", Payload: "stable",
	}
	h1 := env.Hash()
	h2 := env.Hash()
	if h1 != h2 {
		t.Fatalf("Hash() not stable: %x vs %x", h1, h2)
	}

	raw, _ := json.Marshal(&env)
	var got citation.Envelope
	_ = json.Unmarshal(raw, &got)
	if got.Hash() != h1 {
		t.Errorf("Hash() differs after round-trip: %x vs %x", got.Hash(), h1)
	}
}
