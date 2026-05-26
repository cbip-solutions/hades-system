// Package aggregator — value-type tests (PinNote, QueryRequest, WikilinkEdge).
//
// No CGO tag: all types in types.go compile under both build variants;
// these tests do not touch SQLite at all.
package aggregator

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPinNoteJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	original := PinNote{
		NoteID:           "internal-platform-x:notes/methodology",
		ProjectID:        "internal-platform-x",
		Title:            "Internal-Platform-X methodology",
		Content:          "...markdown body...",
		FrontmatterJSON:  `{"tags":["doctrine"]}`,
		PromotedAt:       now,
		PromotedBy:       "testuser",
		PromoteReason:    "canonical doctrine reference",
		AuditChainAnchor: "2026_05:evt-123:abcdef",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got PinNote
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.PromotedAt.Equal(original.PromotedAt) {
		t.Errorf("PromotedAt mismatch: %v vs %v", got.PromotedAt, original.PromotedAt)
	}
	if got.NoteID != original.NoteID || got.PromoteReason != original.PromoteReason {
		t.Errorf("round-trip mismatch: %#v vs %#v", got, original)
	}
}

func TestQueryRequestValidationRejectsEmptyTextOnGlobalScope(t *testing.T) {
	req := QueryRequest{Scope: ScopeGlobal, Text: "", Limit: 10}
	if err := req.Validate(); err == nil {
		t.Error("empty Text on global scope should fail validation")
	}
}

func TestQueryRequestValidationDefaultsLimit(t *testing.T) {
	req := QueryRequest{Scope: ScopeGlobal, Text: "hello"}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if req.Limit != defaultQueryLimit {
		t.Errorf("Limit not defaulted: got %d want %d", req.Limit, defaultQueryLimit)
	}
}

func TestQueryRequestValidationCapsLimit(t *testing.T) {
	req := QueryRequest{Scope: ScopeGlobal, Text: "hello", Limit: 100000}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if req.Limit != maxQueryLimit {
		t.Errorf("Limit not capped: got %d want %d", req.Limit, maxQueryLimit)
	}
}

func TestQueryRequestValidationRejectsUnknownScope(t *testing.T) {
	req := QueryRequest{Scope: "ecosystem-rag", Text: "hello"}
	if err := req.Validate(); err == nil {
		t.Error("unknown scope should fail validation (ecosystem-rag is Plan 14)")
	}
}

func TestQueryRequestValidationRejectsProjectScopeWithoutProjectID(t *testing.T) {
	req := QueryRequest{Scope: ScopeProject, Text: "hello", ProjectID: ""}
	if err := req.Validate(); err == nil {
		t.Error("project scope without ProjectID should fail validation")
	}
}

func TestQueryRequestValidationDefaultsWikilinkDepth(t *testing.T) {
	req := QueryRequest{Scope: ScopeGlobal, Text: "hello"}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if req.WikilinkDepth != defaultWikilinkDepth {
		t.Errorf("WikilinkDepth not defaulted: got %d want %d", req.WikilinkDepth, defaultWikilinkDepth)
	}
}

func TestQueryRequestValidationScopePinnedOnly(t *testing.T) {
	req := QueryRequest{Scope: ScopePinnedOnly, Text: "doctrine"}
	if err := req.Validate(); err != nil {
		t.Errorf("pinned-only scope with text should pass: %v", err)
	}
}

func TestQueryRequestValidationExplicitLimitKept(t *testing.T) {
	req := QueryRequest{Scope: ScopeGlobal, Text: "hello", Limit: 42}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if req.Limit != 42 {
		t.Errorf("explicit Limit 42 mutated: got %d", req.Limit)
	}
}

func TestWikilinkEdgeLinkTypeEnumExhaustive(t *testing.T) {
	allowed := map[string]bool{
		"wikilink": true,
		"backlink": true,
		"relates":  true,
	}
	if len(allowed) != 3 {
		t.Errorf("wikilink link types unexpected: %v", allowed)
	}

	for lt := range allowed {
		e := WikilinkEdge{SourceNoteID: "src", TargetNoteID: "dst", LinkType: lt}
		if e.LinkType != lt {
			t.Errorf("WikilinkEdge LinkType field not round-tripping: %s", lt)
		}
	}
}

func TestTopKStructHoldsResults(t *testing.T) {
	tk := TopK{
		Source: "fts",
		Results: []QueryResult{
			{NoteID: "n1", Score: 1.23, Source: "fts"},
		},
	}
	data, err := json.Marshal(tk)
	if err != nil {
		t.Fatalf("Marshal TopK: %v", err)
	}
	var got TopK
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal TopK: %v", err)
	}
	if got.Source != "fts" || len(got.Results) != 1 {
		t.Errorf("TopK round-trip failed: %+v", got)
	}
}
