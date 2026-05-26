package rrf

import (
	"fmt"
	"math"
	"sort"
	"testing"
)

func floatNear(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestFuse_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := Fuse(nil, 0, 10); got != nil {
		t.Errorf("Fuse(nil) = %v; want nil", got)
	}
	if got := Fuse([]TopK{}, 0, 10); got != nil {
		t.Errorf("Fuse([]TopK{}) = %v; want nil", got)
	}
}

func TestFuse_AllSourcesEmpty(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: nil},
		{Source: "vec", Results: []QueryResult{}},
	}
	if got := Fuse(per, 0, 10); got != nil {
		t.Errorf("Fuse(all empty sources) = %v; want nil", got)
	}
}

func TestFuse_SingleSourceOrdering(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts", Title: "T1"},
			{NoteID: "n2", Source: "fts", Title: "T2"},
			{NoteID: "n3", Source: "fts", Title: "T3"},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 3 {
		t.Fatalf("len=%d; want 3", len(got))
	}
	if got[0].NoteID != "n1" {
		t.Errorf("top noteID=%q; want n1", got[0].NoteID)
	}

	want := 1.0 / float64(60+1)
	if !floatNear(got[0].Score, want, 1e-12) {
		t.Errorf("top score=%v; want %v", got[0].Score, want)
	}
}

func TestFuse_CrossSourceSum(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n2", Source: "fts"},
			{NoteID: "n1", Source: "fts"},
		}},
		{Source: "vec", Results: []QueryResult{
			{NoteID: "n1", Source: "vec"},
			{NoteID: "n2", Source: "vec"},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}

	want := 1.0/float64(60+2) + 1.0/float64(60+1)
	for _, r := range got {
		if !floatNear(r.Score, want, 1e-12) {
			t.Errorf("noteID %q score=%v; want %v", r.NoteID, r.Score, want)
		}
	}
}

func TestFuse_PinBoost(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts"},
		}},
		{Source: "pin", Results: []QueryResult{
			{NoteID: "n2", Source: "pin"},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}

	if got[0].NoteID != "n2" {
		t.Errorf("top=%q; want n2 (pin-boosted)", got[0].NoteID)
	}
}

func TestFuse_MetadataSelectedByPriority(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "vec", Results: []QueryResult{
			{NoteID: "n1", Source: "vec", Title: "vec-title", Snippet: "v"},
		}},
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts", Title: "fts-title", Snippet: "f"},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 1 {
		t.Fatalf("len=%d; want 1", len(got))
	}
	if got[0].Title != "fts-title" {
		t.Errorf("title=%q; want fts-title (fts has lower priorityRank=1)", got[0].Title)
	}
	if got[0].Source != "fts" {
		t.Errorf("source=%q; want fts", got[0].Source)
	}
}

func TestFuse_UnknownSourceFallsToMaxPriority(t *testing.T) {
	t.Parallel()

	per := []TopK{
		{Source: "weird", Results: []QueryResult{
			{NoteID: "shared", Source: "weird", Title: "weird-title"},
			{NoteID: "n1", Source: "weird"},
		}},
		{Source: "fts", Results: []QueryResult{
			{NoteID: "shared", Source: "fts", Title: "fts-title"},
			{NoteID: "n2", Source: "fts"},
		}},
	}
	got := Fuse(per, 0, 10)

	for _, r := range got {
		if r.NoteID == "shared" && r.Title != "fts-title" {
			t.Errorf("shared title=%q; want fts-title", r.Title)
		}
	}
}

func TestFuse_DefaultK(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts"},
		}},
	}
	got := Fuse(per, -1, 10)
	want := 1.0 / float64(DefaultK+1)
	if !floatNear(got[0].Score, want, 1e-12) {
		t.Errorf("score=%v; want %v (DefaultK applied)", got[0].Score, want)
	}
}

func TestFuse_DefaultLimit(t *testing.T) {
	t.Parallel()

	results := make([]QueryResult, 30)
	for i := range results {
		results[i] = QueryResult{NoteID: fmt.Sprintf("n%d", i), Source: "fts"}
	}
	per := []TopK{{Source: "fts", Results: results}}
	got := Fuse(per, 0, -1)
	if len(got) != DefaultLimit {
		t.Errorf("len=%d; want %d", len(got), DefaultLimit)
	}
}

func TestFuse_ExplicitLimit(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts"},
			{NoteID: "n2", Source: "fts"},
			{NoteID: "n3", Source: "fts"},
		}},
	}
	got := Fuse(per, 0, 2)
	if len(got) != 2 {
		t.Errorf("len=%d; want 2", len(got))
	}
}

func TestFuse_TieBreakByNoteID(t *testing.T) {
	t.Parallel()

	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "zz", Source: "fts"},
		}},
		{Source: "vec", Results: []QueryResult{
			{NoteID: "aa", Source: "vec"},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}

	if got[0].NoteID != "zz" {
		t.Errorf("top=%q; want zz (fts priority)", got[0].NoteID)
	}
}

func TestFuse_TieBreakByNoteIDWhenPriorityEqual(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "zz", Source: "fts"},
			{NoteID: "aa", Source: "fts"},
		}},
	}

	got := Fuse(per, 0, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}

	if got[0].NoteID != "zz" {
		t.Errorf("top=%q; want zz", got[0].NoteID)
	}

	per = []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "zz", Source: "fts"}}},
		{Source: "fts", Results: []QueryResult{{NoteID: "aa", Source: "fts"}}},
	}
	got = Fuse(per, 0, 10)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}

	if got[0].NoteID != "aa" {
		t.Errorf("top=%q; want aa (NoteID alphabetical tie-break)", got[0].NoteID)
	}
}

func TestFuse_PriorityRankAllKnownSources(t *testing.T) {
	t.Parallel()

	if priorityRank("fts") != 1 {
		t.Errorf("priorityRank(fts) = %d; want 1", priorityRank("fts"))
	}
	if priorityRank("vec") != 2 {
		t.Errorf("priorityRank(vec) = %d; want 2", priorityRank("vec"))
	}
	if priorityRank("graph") != 3 {
		t.Errorf("priorityRank(graph) = %d; want 3", priorityRank("graph"))
	}
	if priorityRank("pin") != 4 {
		t.Errorf("priorityRank(pin) = %d; want 4", priorityRank("pin"))
	}
	if priorityRank("nonsense") != MaxPriority {
		t.Errorf("priorityRank(nonsense) = %d; want %d", priorityRank("nonsense"), MaxPriority)
	}
}

func TestFuse_StableSortByInsertionOrder(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "b", Source: "fts"},
			{NoteID: "a", Source: "fts"},
		}},
	}
	got := Fuse(per, 0, 10)

	sorted := append([]QueryResult{}, got...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}
}

func TestFuse_MetadataPreservation(t *testing.T) {
	t.Parallel()
	per := []TopK{
		{Source: "fts", Results: []QueryResult{
			{
				NoteID:           "n1",
				Source:           "fts",
				Title:            "Title",
				Snippet:          "Snippet",
				ProjectID:        "internal-platform-x",
				AuditChainAnchor: "2026_05:evt-1:abc",
			},
		}},
	}
	got := Fuse(per, 0, 10)
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	r := got[0]
	if r.Title != "Title" || r.Snippet != "Snippet" ||
		r.ProjectID != "internal-platform-x" || r.AuditChainAnchor != "2026_05:evt-1:abc" {
		t.Errorf("metadata not preserved: %+v", r)
	}
}
