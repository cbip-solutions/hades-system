package aggregator

import (
	"fmt"
	"math"
	"sort"
	"testing"
)

func makeResult(noteID, title, snippet, projectID, auditAnchor string, score float64) QueryResult {
	return QueryResult{
		NoteID:           noteID,
		Score:            score,
		Title:            title,
		Snippet:          snippet,
		ProjectID:        projectID,
		AuditChainAnchor: auditAnchor,
	}
}

func floatNear(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestRRFBasicSingleSource(t *testing.T) {
	ftsResults := []QueryResult{
		makeResult("n1", "Note 1", "snippet1", "proj-a", "", 10.0),
		makeResult("n2", "Note 2", "snippet2", "proj-a", "", 9.0),
		makeResult("n3", "Note 3", "snippet3", "proj-a", "", 8.0),
	}
	ftsResults[0].Source = "fts"
	ftsResults[1].Source = "fts"
	ftsResults[2].Source = "fts"

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)

	if len(got) != 3 {
		t.Fatalf("expected 3 results, got %d", len(got))
	}
	if got[0].NoteID != "n1" {
		t.Errorf("expected n1 first, got %s", got[0].NoteID)
	}

	expectedTop := 1.0 / float64(60+1)
	if !floatNear(got[0].Score, expectedTop, 1e-12) {
		t.Errorf("expected score %.15f, got %.15f", expectedTop, got[0].Score)
	}
}

func TestRRFCrossSourceSum(t *testing.T) {

	ftsResults := []QueryResult{
		{NoteID: "n2", Source: "fts", Title: "Note 2", Score: 10.0, ProjectID: "proj-a"},
		{NoteID: "n1", Source: "fts", Title: "Note 1", Score: 9.0, ProjectID: "proj-a"},
	}

	vecResults := []QueryResult{
		{NoteID: "n1", Source: "vec", Title: "Note 1 Vec", Score: 0.95, ProjectID: "proj-a"},
		{NoteID: "n2", Source: "vec", Title: "Note 2 Vec", Score: 0.85, ProjectID: "proj-a"},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "vec", Results: vecResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)

	if len(got) == 0 {
		t.Fatal("expected results, got none")
	}

	var n1Score float64
	var n2Score float64
	for _, r := range got {
		switch r.NoteID {
		case "n1":
			n1Score = r.Score
		case "n2":
			n2Score = r.Score
		}
	}

	expectedN1 := 1.0/float64(60+2) + 1.0/float64(60+1)

	expectedN2 := 1.0/float64(60+1) + 1.0/float64(60+2)

	if !floatNear(n1Score, expectedN1, 1e-12) {
		t.Errorf("n1 score: expected %.15f, got %.15f", expectedN1, n1Score)
	}
	if !floatNear(n2Score, expectedN2, 1e-12) {
		t.Errorf("n2 score: expected %.15f, got %.15f", expectedN2, n2Score)
	}

	if got[0].NoteID != "n1" {
		t.Errorf("tie-break: expected n1 first (NoteID alphabetical), got %s", got[0].NoteID)
	}
}

func TestRRFPinBoost(t *testing.T) {
	ftsResults := []QueryResult{
		{NoteID: "n1", Source: "fts", Title: "Note 1", Score: 10.0, ProjectID: "proj-a"},
	}
	pinResults := []QueryResult{
		{NoteID: "n2", Source: "pin", Title: "Note 2 Pinned", Score: 1.0, ProjectID: "proj-a"},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "pin", Results: pinResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)

	if len(got) < 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}

	if got[0].NoteID != "n2" {
		t.Errorf("expected n2 (pin boost) first, got %s", got[0].NoteID)
	}

	expectedN1 := 1.0 / float64(60+1)
	expectedN2 := 1.5 / float64(60+1)

	var n1Score, n2Score float64
	for _, r := range got {
		switch r.NoteID {
		case "n1":
			n1Score = r.Score
		case "n2":
			n2Score = r.Score
		}
	}

	if !floatNear(n1Score, expectedN1, 1e-12) {
		t.Errorf("n1 score: expected %.15f, got %.15f", expectedN1, n1Score)
	}
	if !floatNear(n2Score, expectedN2, 1e-12) {
		t.Errorf("n2 score: expected %.15f, got %.15f", expectedN2, n2Score)
	}
}

func TestRRFEmptyInputReturnsEmpty(t *testing.T) {
	got := Fuse(nil, 0, 10)
	if len(got) != 0 {
		t.Errorf("nil input: expected empty, got %d results", len(got))
	}

	got2 := Fuse([]TopK{}, 0, 10)
	if len(got2) != 0 {
		t.Errorf("empty slice input: expected empty, got %d results", len(got2))
	}
}

func TestRRFDefaultK(t *testing.T) {
	results := []QueryResult{
		{NoteID: "n1", Source: "fts", Title: "Note 1", Score: 10.0, ProjectID: "proj-a"},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: results},
	}

	got := Fuse(perSourceTopKs, 0, 10)
	if len(got) == 0 {
		t.Fatal("expected results, got none")
	}

	got2 := Fuse(perSourceTopKs, -5, 10)
	if len(got2) == 0 {
		t.Fatal("expected results with negative k, got none")
	}

	expected := 1.0 / float64(60+1)
	if !floatNear(got[0].Score, expected, 1e-12) {
		t.Errorf("k=0: expected score %.15f, got %.15f", expected, got[0].Score)
	}
	if !floatNear(got2[0].Score, expected, 1e-12) {
		t.Errorf("k=-5: expected score %.15f, got %.15f", expected, got2[0].Score)
	}
}

func TestRRFLimitCap(t *testing.T) {
	results := make([]QueryResult, 50)
	for i := range results {
		results[i] = QueryResult{
			NoteID:    fmt.Sprintf("note-%03d", i),
			Source:    "fts",
			Title:     fmt.Sprintf("Note %d", i),
			ProjectID: "proj-a",
			Score:     float64(50 - i),
		}
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: results},
	}

	got := Fuse(perSourceTopKs, 0, 10)
	if len(got) != 10 {
		t.Errorf("expected 10 results (limit cap), got %d", len(got))
	}
}

func TestRRFLimitZeroUsesDefault(t *testing.T) {
	results := make([]QueryResult, 50)
	for i := range results {
		results[i] = QueryResult{
			NoteID:    fmt.Sprintf("note-%03d", i),
			Source:    "fts",
			Title:     fmt.Sprintf("Note %d", i),
			ProjectID: "proj-a",
			Score:     float64(50 - i),
		}
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: results},
	}

	got := Fuse(perSourceTopKs, 0, 0)
	if len(got) != defaultQueryLimit {
		t.Errorf("limit=0: expected %d (defaultQueryLimit), got %d", defaultQueryLimit, len(got))
	}
}

func TestRRFTieBreakingDeterministic(t *testing.T) {
	ftsResults := []QueryResult{
		{NoteID: "n1", Source: "fts", Title: "Note 1", Score: 10.0, ProjectID: "proj-a"},
	}
	vecResults := []QueryResult{
		{NoteID: "n2", Source: "vec", Title: "Note 2", Score: 0.9, ProjectID: "proj-a"},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "vec", Results: vecResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)

	if len(got) < 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}

	if got[0].NoteID != "n1" {
		t.Errorf("tie-break: expected n1 (fts priority=1) first, got %s", got[0].NoteID)
	}
	if got[1].NoteID != "n2" {
		t.Errorf("tie-break: expected n2 second, got %s", got[1].NoteID)
	}

	if !floatNear(got[0].Score, got[1].Score, 1e-15) {
		t.Errorf("scores should be equal for tie-break test: %.15f vs %.15f", got[0].Score, got[1].Score)
	}
}

func TestRRFNumericalStabilityLargeK(t *testing.T) {
	results := []QueryResult{
		{NoteID: "n1", Source: "fts", Title: "Note 1", Score: 10.0, ProjectID: "proj-a"},
		{NoteID: "n2", Source: "fts", Title: "Note 2", Score: 9.0, ProjectID: "proj-a"},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: results},
	}

	const bigK = 1_000_000
	got := Fuse(perSourceTopKs, bigK, 10)

	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}

	for _, r := range got {
		if r.Score <= 0 {
			t.Errorf("note %s: score should be positive, got %v", r.NoteID, r.Score)
		}
		if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
			t.Errorf("note %s: score is not finite: %v", r.NoteID, r.Score)
		}
	}

	if got[0].NoteID != "n1" {
		t.Errorf("expected n1 first with large k, got %s", got[0].NoteID)
	}

	expected := 1.0 / float64(bigK+1)
	if !floatNear(got[0].Score, expected, 1e-20) {
		t.Errorf("large k score: expected %.20e, got %.20e", expected, got[0].Score)
	}
}

func TestRRFPreservesMetadataFromHighestRankSource(t *testing.T) {
	ftsResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "fts",
			Title:            "FTS Title",
			Snippet:          "fts snippet",
			ProjectID:        "proj-fts",
			AuditChainAnchor: "chain-fts",
			Score:            10.0,
		},
	}
	vecResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "vec",
			Title:            "Vec Title",
			Snippet:          "vec snippet",
			ProjectID:        "proj-vec",
			AuditChainAnchor: "chain-vec",
			Score:            0.9,
		},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "vec", Results: vecResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)

	if len(got) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(got))
	}

	r := got[0]

	if r.Title != "FTS Title" {
		t.Errorf("Title: expected 'FTS Title' (from fts), got %q", r.Title)
	}
	if r.Snippet != "fts snippet" {
		t.Errorf("Snippet: expected 'fts snippet', got %q", r.Snippet)
	}
	if r.ProjectID != "proj-fts" {
		t.Errorf("ProjectID: expected 'proj-fts', got %q", r.ProjectID)
	}
	if r.AuditChainAnchor != "chain-fts" {
		t.Errorf("AuditChainAnchor: expected 'chain-fts', got %q", r.AuditChainAnchor)
	}

	expectedScore := 1.0/float64(60+1) + 1.0/float64(60+1)
	if !floatNear(r.Score, expectedScore, 1e-12) {
		t.Errorf("Score: expected %.15f, got %.15f", expectedScore, r.Score)
	}
}

func TestRRFUnknownSourcePriorityRank(t *testing.T) {
	got := priorityRank("unknown-source-xyz")
	if got != maxPriority {
		t.Errorf("unknown source: expected maxPriority=%d, got %d", maxPriority, got)
	}

	cases := map[string]int{
		"fts":   1,
		"vec":   2,
		"graph": 3,
		"pin":   4,
	}
	for src, expected := range cases {
		if got := priorityRank(src); got != expected {
			t.Errorf("priorityRank(%q): expected %d, got %d", src, expected, got)
		}
	}
}

func TestRRFEmptyResultsInTopKReturnsNil(t *testing.T) {
	perSourceTopKs := []TopK{
		{Source: "fts", Results: []QueryResult{}},
		{Source: "vec", Results: []QueryResult{}},
	}
	got := Fuse(perSourceTopKs, 0, 10)
	if len(got) != 0 {
		t.Errorf("all-empty Results: expected nil/empty, got %d results", len(got))
	}
}

func TestRRFHigherPrioritySourceOverridesMetadataWhenSeenLater(t *testing.T) {

	vecResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "vec",
			Title:            "Vec Title",
			Snippet:          "vec snippet",
			ProjectID:        "proj-vec",
			AuditChainAnchor: "chain-vec",
			Score:            0.9,
		},
	}
	ftsResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "fts",
			Title:            "FTS Title",
			Snippet:          "fts snippet",
			ProjectID:        "proj-fts",
			AuditChainAnchor: "chain-fts",
			Score:            10.0,
		},
	}

	perSourceTopKs := []TopK{
		{Source: "vec", Results: vecResults},
		{Source: "fts", Results: ftsResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(got))
	}

	r := got[0]

	if r.Title != "FTS Title" {
		t.Errorf("Title: expected 'FTS Title' (fts overrides vec), got %q", r.Title)
	}
	if r.Snippet != "fts snippet" {
		t.Errorf("Snippet: expected 'fts snippet', got %q", r.Snippet)
	}
	if r.ProjectID != "proj-fts" {
		t.Errorf("ProjectID: expected 'proj-fts', got %q", r.ProjectID)
	}
	if r.AuditChainAnchor != "chain-fts" {
		t.Errorf("AuditChainAnchor: expected 'chain-fts', got %q", r.AuditChainAnchor)
	}
	if r.Source != "fts" {
		t.Errorf("Source: expected 'fts', got %q", r.Source)
	}

	expectedScore := 1.0/float64(60+1) + 1.0/float64(60+1)
	if !floatNear(r.Score, expectedScore, 1e-12) {
		t.Errorf("Score: expected %.15f, got %.15f", expectedScore, r.Score)
	}
}

func TestRRFLowerPrioritySourceDoesNotOverrideMetadata(t *testing.T) {

	ftsResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "fts",
			Title:            "FTS Title",
			Snippet:          "fts snippet",
			ProjectID:        "proj-fts",
			AuditChainAnchor: "chain-fts",
			Score:            10.0,
		},
	}
	graphResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "graph",
			Title:            "Graph Title",
			Snippet:          "graph snippet",
			ProjectID:        "proj-graph",
			AuditChainAnchor: "chain-graph",
			Score:            5.0,
		},
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "graph", Results: graphResults},
	}

	got := Fuse(perSourceTopKs, 0, 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(got))
	}

	r := got[0]

	if r.Title != "FTS Title" {
		t.Errorf("Title: expected 'FTS Title', got %q", r.Title)
	}
	if r.ProjectID != "proj-fts" {
		t.Errorf("ProjectID: expected 'proj-fts', got %q", r.ProjectID)
	}
	if r.AuditChainAnchor != "chain-fts" {
		t.Errorf("AuditChainAnchor: expected 'chain-fts', got %q", r.AuditChainAnchor)
	}

	expectedScore := 1.0/float64(60+1) + 1.0/float64(60+1)
	if !floatNear(r.Score, expectedScore, 1e-12) {
		t.Errorf("Score: expected %.15f, got %.15f", expectedScore, r.Score)
	}
}

func TestRRFOutputSortedDescending(t *testing.T) {
	ftsResults := make([]QueryResult, 20)
	for i := range ftsResults {
		ftsResults[i] = QueryResult{
			NoteID:    fmt.Sprintf("note-%02d", i),
			Source:    "fts",
			Title:     fmt.Sprintf("Note %d", i),
			ProjectID: "proj-a",
			Score:     float64(20 - i),
		}
	}
	vecResults := make([]QueryResult, 15)
	for i := range vecResults {
		vecResults[i] = QueryResult{
			NoteID:    fmt.Sprintf("vec-note-%02d", i),
			Source:    "vec",
			Title:     fmt.Sprintf("Vec Note %d", i),
			ProjectID: "proj-b",
			Score:     float64(15-i) * 0.7,
		}
	}

	perSourceTopKs := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "vec", Results: vecResults},
	}

	got := Fuse(perSourceTopKs, 0, 50)

	if !sort.SliceIsSorted(got, func(i, j int) bool {
		if got[i].Score != got[j].Score {
			return got[i].Score > got[j].Score
		}
		pi := priorityRank(got[i].Source)
		pj := priorityRank(got[j].Source)
		if pi != pj {
			return pi < pj
		}
		return got[i].NoteID < got[j].NoteID
	}) {
		t.Error("output is not sorted descending by score (then priority, then NoteID)")
		for idx, r := range got {
			t.Logf("[%d] NoteID=%s Score=%.6f Source=%s", idx, r.NoteID, r.Score, r.Source)
		}
	}
}
