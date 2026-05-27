// go:build integration

package p11_citation_test

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/citation"
)

type candidate struct {
	ID          string
	TestPass    bool
	BlastRadius float64
	Envelope    citation.Envelope
}

func TestMergeWinner_TiebreakOnBlastRadius(t *testing.T) {
	mkEnv := func(id, evtID string) citation.Envelope {
		return citation.Envelope{
			ID:           citation.CitationID("c-" + id),
			Type:         citation.CitationTypeCommunitySummary,
			Source:       citation.SourceCaronteContext,
			Lane:         citation.LaneGraph,
			AuditEventID: evtID,
			Confidence:   0.8,
			ProjectID:    "p-merge",
		}
	}

	candidates := []candidate{
		{ID: "C1", TestPass: true, BlastRadius: 0.85, Envelope: mkEnv("c1", "evt-c1")},
		{ID: "C2", TestPass: true, BlastRadius: 0.30, Envelope: mkEnv("c2", "evt-c2")},
		{ID: "C3", TestPass: false, BlastRadius: 0.10, Envelope: mkEnv("c3", "evt-c3")},
	}

	winner := selectWinner(candidates)
	if winner.ID != "C2" {
		t.Errorf("winner = %q, want C2 (test_pass + min blast_radius among tied)", winner.ID)
	}

	if winner.Envelope.AuditEventID != "evt-c2" {
		t.Errorf("winner.AuditEventID = %q, want evt-c2", winner.Envelope.AuditEventID)
	}
}

func TestMergeWinner_EnvelopeJSONRoundtrip(t *testing.T) {

	env := citation.Envelope{
		ID:           "c-merge001",
		Type:         citation.CitationTypeKGNode,
		Source:       citation.SourceCaronteContext,
		Lane:         citation.LaneGraph,
		AuditEventID: "evt-merge-001",
		Confidence:   0.72,
		RRFScore:     0.0167,
		RRFRank:      3,
		ProjectID:    "p-merge",
		Payload:      "Engine.Run",
	}

	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var recovered citation.Envelope
	if err := json.Unmarshal(b, &recovered); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if recovered.ID != env.ID {
		t.Errorf("ID drifted: got %q want %q", recovered.ID, env.ID)
	}
	if recovered.AuditEventID != env.AuditEventID {
		t.Errorf("AuditEventID drifted: got %q want %q", recovered.AuditEventID, env.AuditEventID)
	}
	if recovered.RRFScore != env.RRFScore {
		t.Errorf("RRFScore drifted: got %v want %v", recovered.RRFScore, env.RRFScore)
	}
}

func TestMergeWinner_DeterministicOrdering(t *testing.T) {
	candidates := []candidate{
		{ID: "A", TestPass: true, BlastRadius: 0.5},
		{ID: "B", TestPass: true, BlastRadius: 0.5},
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID > candidates[j].ID })

	w1 := selectWinner(candidates)
	w2 := selectWinner(candidates)
	if w1.ID != w2.ID {
		t.Errorf("non-deterministic ordering: w1=%q w2=%q", w1.ID, w2.ID)
	}
}

func selectWinner(cs []candidate) candidate {
	var passing []candidate
	for _, c := range cs {
		if c.TestPass {
			passing = append(passing, c)
		}
	}
	pool := passing
	if len(pool) == 0 {
		pool = cs
	}
	winner := pool[0]
	for _, c := range pool[1:] {
		if c.BlastRadius < winner.BlastRadius {
			winner = c
			continue
		}
		if c.BlastRadius == winner.BlastRadius && c.ID < winner.ID {
			winner = c
		}
	}
	return winner
}
