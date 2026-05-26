package aggregator

import (
	"fmt"
	"math"
	"sort"
	"testing"
)

func TestFuseWeighted_EqualWeights_ReducesToUnweighted(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}, {NoteID: "b", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "b", Source: "vec"}, {NoteID: "c", Source: "vec"}}},
	}
	weights := map[string]float64{"fts": 1.0, "vec": 1.0}
	weighted := FuseWeighted(src, weights, 60, 10)
	unweighted := Fuse(src, 60, 10)
	if len(weighted) != len(unweighted) {
		t.Fatalf("len mismatch: weighted=%d unweighted=%d", len(weighted), len(unweighted))
	}
	for i := range weighted {
		if weighted[i].NoteID != unweighted[i].NoteID {
			t.Errorf("position %d: weighted=%s unweighted=%s", i, weighted[i].NoteID, unweighted[i].NoteID)
		}
		if math.Abs(weighted[i].Score-unweighted[i].Score) > 1e-9 {
			t.Errorf("position %d score mismatch: weighted=%v unweighted=%v", i, weighted[i].Score, unweighted[i].Score)
		}
	}
}

func TestFuseWeighted_HigherWeightFavoursThatSource(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "x", Source: "fts"}, {NoteID: "y", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "y", Source: "vec"}, {NoteID: "x", Source: "vec"}}},
	}

	weights := map[string]float64{"fts": 0.8, "vec": 0.2}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) < 1 {
		t.Fatalf("expected results; got %d", len(out))
	}
	if out[0].NoteID != "x" {
		t.Errorf("x should win under fts-weighted top-rank; got %s (full=%+v)", out[0].NoteID, out)
	}
}

func TestFuseWeighted_AbsoluteWeightMagnitude(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "b", Source: "vec"}}},
	}
	weights := map[string]float64{"fts": 5.0, "vec": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results; got %d", len(out))
	}
	if out[0].NoteID != "a" {
		t.Errorf("higher absolute weight should produce higher score; got %s", out[0].NoteID)
	}

	var aScore, bScore float64
	for _, r := range out {
		if r.NoteID == "a" {
			aScore = r.Score
		}
		if r.NoteID == "b" {
			bScore = r.Score
		}
	}
	expectedRatio := 5.0
	actualRatio := aScore / bScore
	if math.Abs(actualRatio-expectedRatio) > 1e-9 {
		t.Errorf("score ratio: expected %.6f, got %.6f (aScore=%v bScore=%v)", expectedRatio, actualRatio, aScore, bScore)
	}
}

func TestFuseWeighted_MissingWeight_DefaultsToZero(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "b", Source: "vec"}}},
	}
	weights := map[string]float64{"fts": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	var foundA, foundB bool
	for _, r := range out {
		if r.NoteID == "a" {
			foundA = true
			if r.Score <= 0 {
				t.Errorf("a should have positive score; got %v", r.Score)
			}
		}
		if r.NoteID == "b" {
			foundB = true
			if r.Score != 0 {
				t.Errorf("b should have zero score (missing weight); got %v", r.Score)
			}
		}
	}
	if !foundA || !foundB {
		t.Errorf("both notes should appear; foundA=%v foundB=%v", foundA, foundB)
	}

	if out[0].NoteID != "a" {
		t.Errorf("a should rank first (positive > zero); got %s", out[0].NoteID)
	}
}

func TestFuseWeighted_DeterministicTiebreak(t *testing.T) {
	t.Parallel()

	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "n2", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "n1", Source: "vec"}}},
	}
	weights := map[string]float64{"fts": 1.0, "vec": 1.0}
	a := FuseWeighted(src, weights, 60, 10)
	b := FuseWeighted(src, weights, 60, 10)
	if len(a) != len(b) {
		t.Fatalf("non-determinism in result count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].NoteID != b[i].NoteID || a[i].Score != b[i].Score {
			t.Errorf("non-determinism at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	if len(a) < 2 {
		t.Fatalf("expected 2 results; got %d", len(a))
	}

	if a[0].Source != "fts" {
		t.Errorf("tie-break: expected fts source first (priority 1); got %s", a[0].Source)
	}
	if a[0].NoteID != "n2" {
		t.Errorf("tie-break: expected n2 (from fts) first; got %s", a[0].NoteID)
	}
}

func TestFuseWeighted_TiebreakAlphabeticalSameSource(t *testing.T) {
	t.Parallel()

	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "b", Source: "fts"}, {NoteID: "a", Source: "fts"}}},
		{Source: "vec", Results: []QueryResult{{NoteID: "a", Source: "vec"}, {NoteID: "b", Source: "vec"}}},
	}
	weights := map[string]float64{"fts": 1.0, "vec": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results; got %d", len(out))
	}

	if math.Abs(out[0].Score-out[1].Score) > 1e-12 {
		t.Errorf("expected equal scores for tie-break test; got %.15f vs %.15f", out[0].Score, out[1].Score)
	}
	if out[0].NoteID != "a" {
		t.Errorf("tie-break alphabetical: expected a first; got %s", out[0].NoteID)
	}
}

func TestFuseWeighted_EmptyInputs(t *testing.T) {
	t.Parallel()
	out := FuseWeighted(nil, nil, 60, 10)
	if out != nil {
		t.Errorf("nil input must yield nil output; got %v", out)
	}
	out = FuseWeighted([]TopK{}, map[string]float64{}, 60, 10)
	if len(out) != 0 {
		t.Errorf("empty input must yield empty output; got %d", len(out))
	}

	src := []TopK{{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}}}
	out = FuseWeighted(src, nil, 60, 10)
	if len(out) != 1 {
		t.Errorf("nil weights with non-nil source: expected doc returned with 0 score; got %d results", len(out))
	}
	if len(out) == 1 && out[0].Score != 0 {
		t.Errorf("nil weights: expected zero score; got %v", out[0].Score)
	}
}

func TestFuseWeighted_ZeroWeight_DocStillReturned(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
	}
	weights := map[string]float64{"fts": 0.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Errorf("zero weight: doc still returned with 0 score; got %d results", len(out))
	}
	if len(out) == 1 && out[0].Score != 0 {
		t.Errorf("zero weight: score must be 0; got %v", out[0].Score)
	}
}

func TestFuseWeighted_LimitTruncation(t *testing.T) {
	t.Parallel()
	var results []QueryResult
	for i := 0; i < 20; i++ {
		results = append(results, QueryResult{
			NoteID: fmt.Sprintf("note-%02d", i),
			Source: "fts",
		})
	}
	src := []TopK{{Source: "fts", Results: results}}
	out := FuseWeighted(src, map[string]float64{"fts": 1.0}, 60, 5)
	if len(out) != 5 {
		t.Errorf("limit=5 must truncate; got %d", len(out))
	}
	if !sort.SliceIsSorted(out, func(i, j int) bool { return out[i].Score > out[j].Score }) {
		t.Errorf("output not sorted descending by score: %+v", out)
	}
}

func TestFuseWeighted_DefaultK(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "n1", Source: "fts"}}},
	}
	weights := map[string]float64{"fts": 1.0}

	got := FuseWeighted(src, weights, 0, 10)
	if len(got) == 0 {
		t.Fatal("expected results; got none")
	}
	expected := 1.0 / float64(60+1)
	if math.Abs(got[0].Score-expected) > 1e-12 {
		t.Errorf("k=0: expected score %.15f, got %.15f", expected, got[0].Score)
	}

	got2 := FuseWeighted(src, weights, -5, 10)
	if len(got2) == 0 {
		t.Fatal("expected results with negative k; got none")
	}
	if math.Abs(got2[0].Score-expected) > 1e-12 {
		t.Errorf("k=-5: expected score %.15f, got %.15f", expected, got2[0].Score)
	}
}

func TestFuseWeighted_DefaultLimit(t *testing.T) {
	t.Parallel()
	results := make([]QueryResult, 50)
	for i := range results {
		results[i] = QueryResult{
			NoteID: fmt.Sprintf("note-%03d", i),
			Source: "fts",
		}
	}
	src := []TopK{{Source: "fts", Results: results}}
	weights := map[string]float64{"fts": 1.0}
	out := FuseWeighted(src, weights, 60, 0)
	if len(out) != defaultQueryLimit {
		t.Errorf("limit=0: expected %d (defaultQueryLimit), got %d", defaultQueryLimit, len(out))
	}
	out2 := FuseWeighted(src, weights, 60, -1)
	if len(out2) != defaultQueryLimit {
		t.Errorf("limit=-1: expected %d (defaultQueryLimit), got %d", defaultQueryLimit, len(out2))
	}
}

func TestFuseWeighted_MetadataMergeBestSource(t *testing.T) {
	t.Parallel()

	vecResults := []QueryResult{
		{
			NoteID:           "n1",
			Source:           "vec",
			Title:            "Vec Title",
			Snippet:          "vec snippet",
			ProjectID:        "proj-vec",
			AuditChainAnchor: "chain-vec",
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
		},
	}
	src := []TopK{
		{Source: "vec", Results: vecResults},
		{Source: "fts", Results: ftsResults},
	}

	weights := map[string]float64{"vec": 5.0, "fts": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged result; got %d", len(out))
	}
	r := out[0]
	if r.Source != "fts" {
		t.Errorf("Source: expected fts (priority 1 < vec priority 2); got %s", r.Source)
	}
	if r.Title != "FTS Title" {
		t.Errorf("Title: expected 'FTS Title' (from fts); got %q", r.Title)
	}
	if r.Snippet != "fts snippet" {
		t.Errorf("Snippet: expected 'fts snippet'; got %q", r.Snippet)
	}
	if r.ProjectID != "proj-fts" {
		t.Errorf("ProjectID: expected 'proj-fts'; got %q", r.ProjectID)
	}
	if r.AuditChainAnchor != "chain-fts" {
		t.Errorf("AuditChainAnchor: expected 'chain-fts'; got %q", r.AuditChainAnchor)
	}

	expectedScore := 5.0/float64(60+1) + 1.0/float64(60+1)
	if math.Abs(r.Score-expectedScore) > 1e-12 {
		t.Errorf("Score: expected %.15f; got %.15f", expectedScore, r.Score)
	}
}

func TestFuseWeighted_NoMetadataOverrideFromLowerPriority(t *testing.T) {
	t.Parallel()
	ftsResults := []QueryResult{
		{
			NoteID:    "n1",
			Source:    "fts",
			Title:     "FTS Title",
			Snippet:   "fts snippet",
			ProjectID: "proj-fts",
		},
	}
	graphResults := []QueryResult{
		{
			NoteID:    "n1",
			Source:    "graph",
			Title:     "Graph Title",
			Snippet:   "graph snippet",
			ProjectID: "proj-graph",
		},
	}
	src := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "graph", Results: graphResults},
	}
	weights := map[string]float64{"fts": 1.0, "graph": 2.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Fatalf("expected 1 result; got %d", len(out))
	}
	r := out[0]
	if r.Source != "fts" {
		t.Errorf("Source: expected fts (priority 1 < graph priority 3); got %s", r.Source)
	}
	if r.Title != "FTS Title" {
		t.Errorf("Title: expected 'FTS Title' (priority 1 wins over priority 3); got %q", r.Title)
	}
}

func TestFuseWeighted_EmptyResultsInTopK_HasAnyFalse(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{}},
		{Source: "vec", Results: []QueryResult{}},
	}
	weights := map[string]float64{"fts": 1.0, "vec": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 0 {
		t.Errorf("all-empty TopK Results: expected empty output; got %d", len(out))
	}
}

func TestFuseWeighted_ExtraWeightForUnknownSource(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
	}
	weights := map[string]float64{"fts": 1.0, "phantom-eco": 99.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Fatalf("expected 1 result; got %d", len(out))
	}
	expected := 1.0 / float64(60+1)
	if math.Abs(out[0].Score-expected) > 1e-12 {
		t.Errorf("phantom weight must not affect scoring; expected %.15f, got %.15f", expected, out[0].Score)
	}
}

// TestFuseWeighted_NegativeWeightProducesNegativeScore — formula is
// arithmetic; we do not clamp. A negative weight subtracts from the sum.
// Tests we don't silently coerce to 0 / abs / clamp — the doc contract is
// "ratios matter, no internal normalisation".
func TestFuseWeighted_NegativeWeightProducesNegativeScore(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
	}
	weights := map[string]float64{"fts": -1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Fatalf("expected 1 result; got %d", len(out))
	}
	expected := -1.0 / float64(60+1)
	if math.Abs(out[0].Score-expected) > 1e-12 {
		t.Errorf("negative weight: expected %.15f, got %.15f", expected, out[0].Score)
	}
}

func TestFuseWeighted_LargeKNumericalStability(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "fts", Results: []QueryResult{
			{NoteID: "n1", Source: "fts"},
			{NoteID: "n2", Source: "fts"},
		}},
	}
	weights := map[string]float64{"fts": 1.0}
	const bigK = 1_000_000
	out := FuseWeighted(src, weights, bigK, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results; got %d", len(out))
	}
	for _, r := range out {
		if r.Score <= 0 || math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
			t.Errorf("note %s: score not positive+finite; got %v", r.NoteID, r.Score)
		}
	}
	if out[0].NoteID != "n1" {
		t.Errorf("expected n1 first under large k; got %s", out[0].NoteID)
	}
	expectedTop := 1.0 / float64(bigK+1)
	if math.Abs(out[0].Score-expectedTop) > 1e-20 {
		t.Errorf("large k: expected %.20e, got %.20e", expectedTop, out[0].Score)
	}
}

func TestFuseWeighted_PinSourceBoostStacks(t *testing.T) {
	t.Parallel()

	src := []TopK{
		{Source: "fts", Results: []QueryResult{{NoteID: "a", Source: "fts"}}},
		{Source: "pin", Results: []QueryResult{{NoteID: "b", Source: "pin"}}},
	}
	weights := map[string]float64{"fts": 1.0, "pin": 1.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 2 {
		t.Fatalf("expected 2 results; got %d", len(out))
	}
	if out[0].NoteID != "b" {
		t.Errorf("pin boost: expected b (pin) first; got %s (full=%+v)", out[0].NoteID, out)
	}

	var aScore, bScore float64
	for _, r := range out {
		if r.NoteID == "a" {
			aScore = r.Score
		}
		if r.NoteID == "b" {
			bScore = r.Score
		}
	}
	expectedA := 1.0 / float64(60+1)
	expectedB := 1.5 / float64(60+1)
	if math.Abs(aScore-expectedA) > 1e-12 {
		t.Errorf("a score: expected %.15f, got %.15f", expectedA, aScore)
	}
	if math.Abs(bScore-expectedB) > 1e-12 {
		t.Errorf("b score (pin boost): expected %.15f, got %.15f", expectedB, bScore)
	}
}

func TestFuseWeighted_PinBoostWithWeight(t *testing.T) {
	t.Parallel()
	src := []TopK{
		{Source: "pin", Results: []QueryResult{{NoteID: "p", Source: "pin"}}},
	}
	weights := map[string]float64{"pin": 2.0}
	out := FuseWeighted(src, weights, 60, 10)
	if len(out) != 1 {
		t.Fatalf("expected 1 result; got %d", len(out))
	}

	expected := 2.0 * 1.5 / float64(60+1)
	if math.Abs(out[0].Score-expected) > 1e-12 {
		t.Errorf("pin weight + boost: expected %.15f, got %.15f", expected, out[0].Score)
	}
}

func TestFuseWeighted_OutputOrderingFullSpec(t *testing.T) {
	t.Parallel()
	ftsResults := make([]QueryResult, 5)
	for i := range ftsResults {
		ftsResults[i] = QueryResult{
			NoteID: fmt.Sprintf("fts-%d", i),
			Source: "fts",
		}
	}
	vecResults := make([]QueryResult, 5)
	for i := range vecResults {
		vecResults[i] = QueryResult{
			NoteID: fmt.Sprintf("vec-%d", i),
			Source: "vec",
		}
	}
	src := []TopK{
		{Source: "fts", Results: ftsResults},
		{Source: "vec", Results: vecResults},
	}
	weights := map[string]float64{"fts": 1.0, "vec": 0.5}
	out := FuseWeighted(src, weights, 60, 20)
	if !sort.SliceIsSorted(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		pi := priorityRank(out[i].Source)
		pj := priorityRank(out[j].Source)
		if pi != pj {
			return pi < pj
		}
		return out[i].NoteID < out[j].NoteID
	}) {
		for idx, r := range out {
			t.Logf("[%d] NoteID=%s Score=%.6f Source=%s", idx, r.NoteID, r.Score, r.Source)
		}
		t.Error("output not sorted by (score desc, source priority asc, NoteID asc)")
	}
}
