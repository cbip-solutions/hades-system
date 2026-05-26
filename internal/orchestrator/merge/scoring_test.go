package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func makeScoreOutcome(headSHA string, testPass int, patchLines int, flake int, hardReject bool, branch string) merge.CandidateOutcome {
	return merge.CandidateOutcome{
		Candidate:      merge.MergeCandidate{Branch: branch, HeadSHA: headSHA},
		TestPassCount:  testPass,
		PatchSizeLines: patchLines,
		FlakeCount:     flake,
		HardRejected:   hardReject,
	}
}

func TestScorerInterfaceSatisfied(t *testing.T) {
	var _ merge.Scorer = merge.NewScorer()
}

func TestRankSelectsMaxTestPass(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 50, 0, false, "feat-A"),
		makeScoreOutcome("h2", 15, 80, 0, false, "feat-B"),
		makeScoreOutcome("h3", 12, 30, 0, false, "feat-C"),
	}
	res, err := s.Rank(context.Background(), out, nil, merge.ScoringConfig{
		AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0,
	})
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "h2" {
		t.Errorf("WinnerID = %s want h2 (max test_pass=15)", res.WinnerID)
	}
	if res.TiebreakApplied {
		t.Error("TiebreakApplied = true on clear winner")
	}
}

func TestRankFiltersHardRejected(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 100, 50, 0, true, "feat-A"),
		makeScoreOutcome("h2", 15, 80, 0, false, "feat-B"),
		makeScoreOutcome("h3", 12, 30, 0, false, "feat-C"),
	}
	res, err := s.Rank(context.Background(), out, nil, merge.ScoringConfig{
		AlphaReviewerWeight: 1.0,
	})
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "h2" {
		t.Errorf("WinnerID = %s want h2 (h1 hard-rejected)", res.WinnerID)
	}
	got := res.HardRejectedIDs
	if len(got) != 1 || got[0] != "h1" {
		t.Errorf("HardRejectedIDs = %v want [h1]", got)
	}
}

func TestRankTiebreakActivatesOnTies(t *testing.T) {
	s := merge.NewScorer()

	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 30, 1, false, "feat-A"),
		makeScoreOutcome("h2", 10, 80, 0, false, "feat-B"),
	}
	votes := map[string]int{"feat-A": 2, "feat-B": 1}
	cfg := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.0,
		GammaFlakePenalty:    2.0,
	}
	res, err := s.Rank(context.Background(), out, votes, cfg)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if !res.TiebreakApplied {
		t.Error("TiebreakApplied = false on tied test-pass-counts")
	}
	if len(res.AllScores) != 2 {
		t.Errorf("AllScores len = %d want 2 (tiebreak winner + tied candidate)", len(res.AllScores))
	}

	if res.WinnerID != "h2" {
		t.Errorf("WinnerID = %s want h2 (score 1 > 0)", res.WinnerID)
	}
}

func TestRankTiebreakWithBetaPatchPenalty(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 30, 0, false, "feat-A"),
		makeScoreOutcome("h2", 10, 80, 0, false, "feat-B"),
	}
	votes := map[string]int{"feat-A": 0, "feat-B": 0}
	cfg := merge.ScoringConfig{
		AlphaReviewerWeight:  0.0,
		BetaPatchSizePenalty: 0.1,
		GammaFlakePenalty:    0.0,
	}
	res, err := s.Rank(context.Background(), out, votes, cfg)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "h1" {
		t.Errorf("WinnerID = %s want h1 (smaller patch wins on β)", res.WinnerID)
	}
}

func TestRankTiebreakWithGammaFlakePenalty(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 50, 5, false, "feat-A"),
		makeScoreOutcome("h2", 10, 50, 0, false, "feat-B"),
	}
	cfg := merge.ScoringConfig{
		AlphaReviewerWeight:  0.0,
		BetaPatchSizePenalty: 0.0,
		GammaFlakePenalty:    2.0,
	}
	res, err := s.Rank(context.Background(), out, nil, cfg)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "h2" {
		t.Errorf("WinnerID = %s want h2 (lower flake wins)", res.WinnerID)
	}
}

func TestRankNoSurvivorsReturnsErr(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 100, 50, 0, true, "feat-A"),
		makeScoreOutcome("h2", 100, 50, 0, true, "feat-B"),
	}
	_, err := s.Rank(context.Background(), out, nil, merge.ScoringConfig{})
	if !errors.Is(err, merge.ErrNoSurvivors) {
		t.Fatalf("err = %v want ErrNoSurvivors", err)
	}
}

func TestRankEmptyOutcomesReturnsErr(t *testing.T) {
	s := merge.NewScorer()
	_, err := s.Rank(context.Background(), nil, nil, merge.ScoringConfig{})
	if !errors.Is(err, merge.ErrNoSurvivors) {
		t.Fatalf("err = %v want ErrNoSurvivors", err)
	}
}

func TestRankTiebreakIsDeterministic(t *testing.T) {

	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("zzz", 10, 50, 0, false, "feat-Z"),
		makeScoreOutcome("aaa", 10, 50, 0, false, "feat-A"),
	}
	cfg := merge.ScoringConfig{
		AlphaReviewerWeight: 0.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 0.0,
	}
	for i := 0; i < 10; i++ {
		res, err := s.Rank(context.Background(), out, nil, cfg)
		if err != nil {
			t.Fatalf("Rank: %v", err)
		}
		if res.WinnerID != "aaa" {
			t.Errorf("call %d: WinnerID = %s want aaa (lex-smaller)", i, res.WinnerID)
		}
	}
}

func TestRankSingleCandidateNoTiebreak(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 50, 0, false, "feat-A"),
	}
	res, err := s.Rank(context.Background(), out, nil, merge.ScoringConfig{})
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "h1" {
		t.Errorf("WinnerID = %s want h1", res.WinnerID)
	}
	if res.TiebreakApplied {
		t.Error("TiebreakApplied = true on single survivor")
	}
}

func TestRankRespectsContextCancel(t *testing.T) {
	s := merge.NewScorer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := []merge.CandidateOutcome{makeScoreOutcome("h1", 10, 50, 0, false, "feat-A")}
	_, err := s.Rank(ctx, out, nil, merge.ScoringConfig{})
	if err == nil {
		t.Fatal("expected error on pre-cancelled context")
	}
}

func TestScoringConfigZeroValueSafe(t *testing.T) {

	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 50, 0, false, "feat-A"),
		makeScoreOutcome("h2", 10, 80, 5, false, "feat-B"),
	}
	res, err := s.Rank(context.Background(), out, nil, merge.ScoringConfig{})
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if !res.TiebreakApplied {
		t.Error("TiebreakApplied = false on tied test-pass with zero config")
	}

	if res.WinnerID != "h1" {
		t.Errorf("WinnerID = %s want h1", res.WinnerID)
	}
}

func TestRankAllScoresContainsEverySurvivor(t *testing.T) {
	s := merge.NewScorer()
	out := []merge.CandidateOutcome{
		makeScoreOutcome("h1", 10, 50, 0, false, "feat-A"),
		makeScoreOutcome("h2", 10, 60, 0, false, "feat-B"),
		makeScoreOutcome("h3", 10, 70, 0, false, "feat-C"),
	}
	cfg := merge.ScoringConfig{BetaPatchSizePenalty: 0.1}
	res, _ := s.Rank(context.Background(), out, nil, cfg)
	if !res.TiebreakApplied {
		t.Fatal("expected tiebreak")
	}
	wantIDs := []string{"h1", "h2", "h3"}
	gotIDs := make([]string, 0, len(res.AllScores))
	for k := range res.AllScores {
		gotIDs = append(gotIDs, k)
	}
	sort.Strings(gotIDs)
	for i, w := range wantIDs {
		if gotIDs[i] != w {
			t.Errorf("AllScores key[%d] = %s want %s", i, gotIDs[i], w)
		}
	}
}

func TestMarshalScoringCompleteTiebreak(t *testing.T) {
	res := merge.ScoringResult{
		WinnerID:        "h1",
		AllScores:       map[string]float64{"h1": 1.5, "h2": 0.5},
		TiebreakApplied: true,
		HardRejectedIDs: []string{"h3"},
	}
	cfg := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.1,
		GammaFlakePenalty:    2.0,
	}

	raw := merge.MarshalScoringComplete(res, cfg)
	if len(raw) == 0 {
		t.Fatal("MarshalScoringComplete returned empty bytes")
	}

	var got merge.ScoringCompletePayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.WinnerID != "h1" {
		t.Errorf("WinnerID = %s want h1", got.WinnerID)
	}
	if !got.TiebreakApplied {
		t.Error("TiebreakApplied = false want true")
	}
	if len(got.AllScores) != 2 || got.AllScores["h1"] != 1.5 || got.AllScores["h2"] != 0.5 {
		t.Errorf("AllScores = %v want {h1:1.5, h2:0.5}", got.AllScores)
	}
	if len(got.HardRejectedIDs) != 1 || got.HardRejectedIDs[0] != "h3" {
		t.Errorf("HardRejectedIDs = %v want [h3]", got.HardRejectedIDs)
	}

	wantSubstrs := []string{"argmax(test_pass)", "α=1.00", "β=0.10", "γ=2.00"}
	for _, sub := range wantSubstrs {
		if !strings.Contains(got.Formula, sub) {
			t.Errorf("Formula missing %q: %s", sub, got.Formula)
		}
	}

	// Verify the omitempty pathway: if no tiebreak fired and no rejected ids,
	// AllScores + HardRejectedIDs MUST be omitted from the JSON.
	noTie := merge.MarshalScoringComplete(merge.ScoringResult{WinnerID: "h1"}, merge.ScoringConfig{})
	if strings.Contains(string(noTie), "all_scores") {
		t.Errorf("no-tiebreak payload should omit all_scores: %s", string(noTie))
	}
	if strings.Contains(string(noTie), "hard_rejected_ids") {
		t.Errorf("no-rejected payload should omit hard_rejected_ids: %s", string(noTie))
	}
	if !strings.Contains(string(noTie), `"tiebreak_applied":false`) {
		t.Errorf("no-tiebreak payload should encode tiebreak_applied:false: %s", string(noTie))
	}
}

func TestValidateTightenOnlyAcceptsEqual(t *testing.T) {
	base := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.0,
		GammaFlakePenalty:    2.0,
	}
	if err := merge.ValidateTightenOnly(base, base); err != nil {
		t.Errorf("ValidateTightenOnly(equal) = %v want nil", err)
	}
}

func TestValidateTightenOnlyAcceptsTightening(t *testing.T) {
	base := merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0}
	tighter := merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.1, GammaFlakePenalty: 5.0}
	if err := merge.ValidateTightenOnly(base, tighter); err != nil {
		t.Errorf("ValidateTightenOnly(tighter) = %v want nil", err)
	}
}

func TestValidateTightenOnlyRejectsBetaLoosen(t *testing.T) {
	base := merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.5, GammaFlakePenalty: 2.0}
	loose := merge.ScoringConfig{AlphaReviewerWeight: 1.0, BetaPatchSizePenalty: 0.0, GammaFlakePenalty: 2.0}
	err := merge.ValidateTightenOnly(base, loose)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}

	msg := err.Error()
	for _, want := range []string{"BetaPatchSizePenalty", "base=0.5000", "override=0.0000"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err message missing substring %q: %q", want, msg)
		}
	}
}

func TestValidateTightenOnlyRejectsGammaLoosen(t *testing.T) {
	base := merge.ScoringConfig{GammaFlakePenalty: 2.0}
	loose := merge.ScoringConfig{GammaFlakePenalty: 1.0}
	err := merge.ValidateTightenOnly(base, loose)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}

	msg := err.Error()
	for _, want := range []string{"GammaFlakePenalty", "base=2.0000", "override=1.0000"} {
		if !strings.Contains(msg, want) {
			t.Errorf("err message missing substring %q: %q", want, msg)
		}
	}
}

// TestValidateTightenOnlyEarlyExitOnBeta pins the short-circuit semantics:
// when BOTH Beta and Gamma loosen, the validator MUST return on the first
// violation (Beta) and never reach the Gamma check. A future refactor that
// batches violations or reorders checks would silently change error semantics
// downstream (CLI displays a different field; operator triage misled). This
// test fails on any such drift.
func TestValidateTightenOnlyEarlyExitOnBeta(t *testing.T) {
	base := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.5,
		GammaFlakePenalty:    2.0,
	}
	override := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.0,
		GammaFlakePenalty:    1.0,
	}
	err := merge.ValidateTightenOnly(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "BetaPatchSizePenalty") {
		t.Errorf("err missing BetaPatchSizePenalty (first violation): %q", msg)
	}
	if strings.Contains(msg, "GammaFlakePenalty") {
		t.Errorf("err must NOT contain GammaFlakePenalty (Beta short-circuits): %q", msg)
	}
}

func TestValidateTightenOnlyAlphaIsNeutral(t *testing.T) {
	base := merge.ScoringConfig{AlphaReviewerWeight: 1.0}
	for _, v := range []float64{0.0, 0.5, 1.0, 1.5, 5.0} {
		o := merge.ScoringConfig{AlphaReviewerWeight: v}
		if err := merge.ValidateTightenOnly(base, o); err != nil {
			t.Errorf("ValidateTightenOnly(α=%v) = %v want nil (α is neutral)", v, err)
		}
	}
}

func TestRankBlastRadiusTiebreak(t *testing.T) {
	s := merge.NewScorer()
	cfg := merge.ScoringConfig{DeltaBlastRadiusPenalty: 1.0}
	outcomes := []merge.CandidateOutcome{
		{Candidate: merge.MergeCandidate{HeadSHA: "aaa", Branch: "A"}, TestPassCount: 5, BlastRadius: 0.8},
		{Candidate: merge.MergeCandidate{HeadSHA: "bbb", Branch: "B"}, TestPassCount: 5, BlastRadius: 0.1},
	}
	res, err := s.Rank(context.Background(), outcomes, nil, cfg)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if res.WinnerID != "bbb" {
		t.Errorf("WinnerID = %q; want bbb (lower blast-radius wins the tiebreak)", res.WinnerID)
	}
	if !res.TiebreakApplied {
		t.Error("TiebreakApplied = false; want true (a tie was resolved)")
	}

	if res.AllScores["bbb"] <= res.AllScores["aaa"] {
		t.Errorf("scores: bbb=%v aaa=%v; want bbb > aaa", res.AllScores["bbb"], res.AllScores["aaa"])
	}
}

func TestRankBlastRadiusZeroWhenUnscored(t *testing.T) {
	s := merge.NewScorer()
	cfg := merge.ScoringConfig{DeltaBlastRadiusPenalty: 5.0}
	outcomes := []merge.CandidateOutcome{
		{Candidate: merge.MergeCandidate{HeadSHA: "bbb", Branch: "B"}, TestPassCount: 5},
		{Candidate: merge.MergeCandidate{HeadSHA: "aaa", Branch: "A"}, TestPassCount: 5},
	}
	res, err := s.Rank(context.Background(), outcomes, nil, cfg)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}

	if res.WinnerID != "aaa" {
		t.Errorf("WinnerID = %q; want aaa (lex tiebreak when blast contributes nothing)", res.WinnerID)
	}
}

func TestValidateTightenOnlyRejectsLooserDelta(t *testing.T) {
	base := merge.ScoringConfig{DeltaBlastRadiusPenalty: 2.0}
	override := merge.ScoringConfig{DeltaBlastRadiusPenalty: 1.0}
	if err := merge.ValidateTightenOnly(base, override); err == nil {
		t.Error("ValidateTightenOnly(δ 2.0 → 1.0) = nil; want ErrLooseAttemptRejected")
	}

	if err := merge.ValidateTightenOnly(base, merge.ScoringConfig{DeltaBlastRadiusPenalty: 3.0}); err != nil {
		t.Errorf("ValidateTightenOnly(δ 2.0 → 3.0) = %v; want nil (tighter OK)", err)
	}
}
