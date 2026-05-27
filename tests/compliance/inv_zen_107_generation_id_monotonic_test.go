// tests/compliance/inv_zen_107_generation_id_monotonic_test.go
//
// Compliance gate for invariant: GenerationID strict monotonicity across
// sequential Merge() invocations.
//
// merge.GenerationCounter (events.go:220) that produces a strictly
// monotonic, concurrency-safe int64 sequence used to tag every Event
// emitted within a single Merge() invocation.
// amendment.proposer relies on the monotonic property when
// reconstructing causal chains across replay (Q9 D vocabulary): two
// merges executed back-to-back MUST carry distinct, strictly-increasing
// GenerationIDs in their EvtMergeStartedWithMode events so the replay
// tier can reconstruct the wall-clock ordering even if event timestamps
// race on the same nanosecond boundary.
//
// This compliance gate is the runtime expression of that contract: drive
// 10 sequential Merge() calls through the production engine surface and
// assert each EvtMergeStartedWithMode event carries a strictly larger
// GenerationID than the previous one. The cache lookup short-circuit is
// avoided by giving each request a different TargetBranch so cache keys
// stay distinct (a cache hit emits EvtMergeCacheHit, NOT
// EvtMergeStartedWithMode — so cache-hit short-circuiting would silently
// reduce the assertion's sample size).
//
// Single-candidate requests (N=1) are used so Validate's MergeBase check
// does not fire (Validate.go:81 — `if n >= 2`); FakeGit thus needs only
// two outputs per merge: RevParse(refs/heads/<TargetBranch>) and the
// Step-7 update-ref. 10 merges × 2 outputs = 20 pre-loaded FakeOutputs.
//
// Reference: internal design record §8.3 invariant
//
// Drift adaptation per task instructions: package compliance (not
// _test) to match the predominant tests/compliance convention (matches
// inv_zen_105/106/109 sibling pattern). Local fakes are c107-prefixed
// to avoid name collisions with sibling files (b7Pool / b7Emitter /
// complianceEmitter etc.). The shared b7Emitter declared in
// inv_zen_105_replay_determinism_test.go is reused — Go same-package
// rules make this a single declaration shared across compliance files.
package compliance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type pool107 struct{}

func (pool107) Lease(_ context.Context) (*merge.LeasedWorktree, error) {
	return &merge.LeasedWorktree{Dir: "/tmp/wt"}, nil
}

func (pool107) Release(_ context.Context, _ *merge.LeasedWorktree) error {
	return nil
}

// complianceBaseline107 is a minimal merge.BaselineRunner fake. Returns
// an empty PassingSet + nil error so the engine reaches Step 5 (Runner)
// on every call. The PassingSet contents do not affect invariant: this
// invariant is about per-Merge() generation IDs, not baseline state.
type complianceBaseline107 struct{}

func (complianceBaseline107) Run(_ context.Context, _ string, _ merge.Mode, _ merge.TestSuite) (merge.PassingSet, error) {
	return merge.PassingSet{}, nil
}

type complianceRunner107 struct{}

func (complianceRunner107) RunCandidates(_ context.Context, cands []merge.MergeCandidate, _ string, _ merge.PassingSet, _ merge.Mode, _ merge.TestSuite) ([]merge.CandidateOutcome, error) {
	out := make([]merge.CandidateOutcome, len(cands))
	for i, c := range cands {
		out[i] = merge.CandidateOutcome{Candidate: c, TestPassCount: 1, TestFailCount: 0}
	}
	return out, nil
}

type complianceScorer107 struct{}

func (complianceScorer107) Rank(_ context.Context, outcomes []merge.CandidateOutcome, _ map[string]int, _ merge.ScoringConfig) (merge.ScoringResult, error) {
	if len(outcomes) == 0 {

		return merge.ScoringResult{}, nil
	}
	winner := outcomes[0].Candidate.HeadSHA
	return merge.ScoringResult{
		WinnerID:  winner,
		AllScores: map[string]float64{winner: 1.0},
	}, nil
}

type complianceCache107 struct{}

func (complianceCache107) Lookup(_ merge.MergeRequest) (merge.MergeOutcome, bool) {
	return merge.MergeOutcome{}, false
}

func (complianceCache107) Store(_ merge.MergeRequest, _ merge.MergeOutcome) {}

// TestInvZen107GenerationIDMonotonic asserts that 10 sequential Merge()
// calls produce distinct, strictly-increasing GenerationIDs in their
// EvtMergeStartedWithMode events.
//
// The test drives the production realEngine through 10 merges with
// distinct TargetBranches (so cache keys stay distinct and the cache
// lookup never short-circuits, even though complianceCache107 already
// misses unconditionally). Each merge consumes 2 FakeGit outputs:
// RevParse(refs/heads/<TargetBranch>) for Validate's live-state check,
// and update-ref for Step 7's fast-forward. 10 merges × 2 outputs = 20
// FakeOutputs pre-loaded into NewFakeGit.
//
// Assertions:
//
// - At least 10 EvtMergeStartedWithMode events emitted (one per merge).
// - Each event's MergeStartedWithModePayload.GenerationID is strictly
// larger than the previous one.
// - GenerationIDs are non-zero
// starts at 1, reserving 0 as "unassigned" — engine.go:286 calls
// gen := e.gen.Next() before any emit, so the first merge's
// GenerationID MUST be ≥1).
//
// Reference: internal design record §8.3 invariant
func TestInvZen107GenerationIDMonotonic(t *testing.T) {
	const numMerges = 10

	em := &b7Emitter{}

	gitOutputs := make([]merge.FakeOutput, 0, numMerges*3)
	for i := 0; i < numMerges; i++ {

		gitOutputs = append(gitOutputs, merge.FakeOutput{
			Stdout: "feedface00000000000000000000000000000000\n",
		})

		gitOutputs = append(gitOutputs, merge.FakeOutput{
			Stdout: "cafef00d00000000000000000000000000000000\n",
		})

		gitOutputs = append(gitOutputs, merge.FakeOutput{})
	}
	gitFake := merge.NewFakeGit(gitOutputs...)

	deps := merge.Deps{
		Pool:     pool107{},
		Emitter:  em,
		Clock:    nil,
		Baseline: complianceBaseline107{},
		Runner:   complianceRunner107{},
		Scorer:   complianceScorer107{},
		Cache:    complianceCache107{},
		Anomaly:  nil,
		Git:      gitFake,
		Config: merge.EngineConfig{
			Scoring:       merge.ScoringConfig{},
			EngineVersion: "compliance-inv-zen-107",
			PoolCapacity:  5,
		},
	}

	e, err := merge.NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	branches := []string{"main-a", "main-b", "main-c", "main-d", "main-e", "main-f", "main-g", "main-h", "main-i", "main-j"}
	if len(branches) != numMerges {
		t.Fatalf("setup: expected %d branches, got %d", numMerges, len(branches))
	}

	for i, branch := range branches {
		req := merge.MergeRequest{
			TargetBranch:  branch,
			BaseSHA:       "deadbeef0000000000000000000000000000beef",
			Mode:          merge.ModeNormal,
			EngineVersion: "compliance-inv-zen-107",
			Candidates: []merge.MergeCandidate{
				{
					Branch:      "feat-" + branch,
					HeadSHA:     "1111111111111111111111111111111111111111",
					SubmittedAt: time.Unix(0, 0),
				},
			},
			TestSuite: merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}},
		}
		if _, mErr := e.Merge(context.Background(), req); mErr != nil {
			t.Fatalf("Merge[%d] target=%q: %v", i, branch, mErr)
		}
	}

	// Collect all EvtMergeStartedWithMode events and decode their
	// generation IDs. There MUST be at least numMerges events (one per
	// successful merge — cache misses unconditionally so no
	// EvtMergeCacheHit short-circuits).
	var started []startedEvent107
	for idx, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeStartedWithMode {
			continue
		}
		var pl merge.MergeStartedWithModePayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("event[%d] payload unmarshal: %v", idx, err)
		}
		started = append(started, startedEvent107{idx: idx, genID: ev.GenerationID, payload: pl})
	}

	if len(started) < numMerges {
		t.Fatalf("EvtMergeStartedWithMode count = %d, want >= %d", len(started), numMerges)
	}

	// GenerationCounter starts at 1 (events.go:226 — Next()
	// reserves 0 as "unassigned"). The first merge's gen MUST be ≥1.
	if started[0].genID < 1 {
		t.Errorf("first EvtMergeStartedWithMode GenerationID = %d, want >= 1 (zero is reserved as 'unassigned')", started[0].genID)
	}

	for i := 1; i < len(started); i++ {
		prev := started[i-1].genID
		curr := started[i].genID
		if curr <= prev {
			t.Errorf("GenerationID NOT strictly increasing at index %d: prev=%d curr=%d (full sequence: %v)",
				i, prev, curr, genIDSequence107(started))
		}
	}

	for i, s := range started {
		if s.payload.GenerationID != s.genID {
			t.Errorf("started[%d]: payload.GenerationID=%d != event.GenerationID=%d (must match)",
				i, s.payload.GenerationID, s.genID)
		}
	}
}

type startedEvent107 struct {
	idx     int
	genID   int64
	payload merge.MergeStartedWithModePayload
}

func genIDSequence107(events []startedEvent107) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.genID
	}
	return out
}
