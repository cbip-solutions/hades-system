package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type fakeBaselineRunnerEng struct {
	mu      sync.Mutex
	calls   []fakeBaselineCall
	passing merge.PassingSet
	err     error
}

type fakeBaselineCall struct {
	BaseSHA string
	Mode    merge.Mode
	Suite   merge.TestSuite
}

func (f *fakeBaselineRunnerEng) Run(_ context.Context, baseSHA string, mode merge.Mode, suite merge.TestSuite) (merge.PassingSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeBaselineCall{BaseSHA: baseSHA, Mode: mode, Suite: suite})
	return f.passing, f.err
}

func (f *fakeBaselineRunnerEng) Calls() []fakeBaselineCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeBaselineCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakeScorerEng struct {
	mu     sync.Mutex
	calls  []fakeScorerCall
	result merge.ScoringResult
	err    error
}

type fakeScorerCall struct {
	Outcomes []merge.CandidateOutcome
	Votes    map[string]int
	Cfg      merge.ScoringConfig
}

func (f *fakeScorerEng) Rank(_ context.Context, outcomes []merge.CandidateOutcome, votes map[string]int, cfg merge.ScoringConfig) (merge.ScoringResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]merge.CandidateOutcome, len(outcomes))
	copy(cp, outcomes)
	f.calls = append(f.calls, fakeScorerCall{Outcomes: cp, Votes: votes, Cfg: cfg})
	return f.result, f.err
}

func (f *fakeScorerEng) Calls() []fakeScorerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeScorerCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakeCacheEng struct {
	mu      sync.Mutex
	store   map[string]merge.MergeOutcome
	lookups []merge.MergeRequest
	stores  []fakeCacheStoreCall
}

type fakeCacheStoreCall struct {
	Req     merge.MergeRequest
	Outcome merge.MergeOutcome
}

func newFakeCacheEng() *fakeCacheEng {
	return &fakeCacheEng{store: make(map[string]merge.MergeOutcome)}
}

func (f *fakeCacheEng) Lookup(req merge.MergeRequest) (merge.MergeOutcome, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups = append(f.lookups, req)
	out, ok := f.store[merge.CacheKey(req)]
	return out, ok
}

func (f *fakeCacheEng) Store(req merge.MergeRequest, outcome merge.MergeOutcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stores = append(f.stores, fakeCacheStoreCall{Req: req, Outcome: outcome})
	f.store[merge.CacheKey(req)] = outcome
}

func (f *fakeCacheEng) Lookups() []merge.MergeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]merge.MergeRequest, len(f.lookups))
	copy(out, f.lookups)
	return out
}

func (f *fakeCacheEng) Stores() []fakeCacheStoreCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeCacheStoreCall, len(f.stores))
	copy(out, f.stores)
	return out
}

type fakeAnomalyEng struct {
	mu    sync.Mutex
	calls []merge.Event
	err   error
}

func (f *fakeAnomalyEng) OnEvent(_ context.Context, evt merge.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, evt)
	return f.err
}

func (f *fakeAnomalyEng) Calls() []merge.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]merge.Event, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakeRunnerEng struct {
	mu       sync.Mutex
	calls    []fakeRunnerCall
	outcomes []merge.CandidateOutcome
	err      error
}

type fakeRunnerCall struct {
	Candidates []merge.MergeCandidate
	BaseSHA    string
	PassingSet merge.PassingSet
	Mode       merge.Mode
	Suite      merge.TestSuite
}

func (f *fakeRunnerEng) RunCandidates(_ context.Context, cands []merge.MergeCandidate, baseSHA string, ps merge.PassingSet, mode merge.Mode, suite merge.TestSuite) ([]merge.CandidateOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]merge.MergeCandidate, len(cands))
	copy(cp, cands)
	f.calls = append(f.calls, fakeRunnerCall{
		Candidates: cp, BaseSHA: baseSHA, PassingSet: ps, Mode: mode, Suite: suite,
	})
	return f.outcomes, f.err
}

func (f *fakeRunnerEng) Calls() []fakeRunnerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeRunnerCall, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakePoolEng struct {
	mu       sync.Mutex
	leases   int
	releases int
	leaseErr error
	relErr   error
}

func (f *fakePoolEng) Lease(_ context.Context) (*merge.LeasedWorktree, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leases++
	if f.leaseErr != nil {
		return nil, f.leaseErr
	}
	return &merge.LeasedWorktree{Dir: "/tmp/wt"}, nil
}

func (f *fakePoolEng) Release(_ context.Context, _ *merge.LeasedWorktree) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return f.relErr
}

func (f *fakePoolEng) Leases() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leases
}

func (f *fakePoolEng) Releases() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases
}

func makeDeps(_ *testing.T) merge.Deps {
	return merge.Deps{
		Pool:     &fakePoolEng{},
		Emitter:  &recordingEmitter{},
		Clock:    nil,
		Baseline: &fakeBaselineRunnerEng{},
		Runner:   &fakeRunnerEng{},
		Scorer:   &fakeScorerEng{},
		Cache:    newFakeCacheEng(),
		Anomaly:  nil,
		Git:      merge.NewFakeGit(),
		Config: merge.EngineConfig{
			Scoring:       merge.ScoringConfig{},
			EngineVersion: "test-engine-v1",
			PoolCapacity:  5,
		},
	}
}

func TestNewEngineRejectsMissingDeps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(d *merge.Deps)
	}{
		{"empty deps", func(d *merge.Deps) { *d = merge.Deps{} }},
		{"missing Pool", func(d *merge.Deps) { d.Pool = nil }},
		{"missing Emitter", func(d *merge.Deps) { d.Emitter = nil }},
		{"missing Baseline", func(d *merge.Deps) { d.Baseline = nil }},
		{"missing Runner", func(d *merge.Deps) { d.Runner = nil }},
		{"missing Scorer", func(d *merge.Deps) { d.Scorer = nil }},
		{"missing Cache", func(d *merge.Deps) { d.Cache = nil }},
		{"missing Git", func(d *merge.Deps) { d.Git = nil }},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			deps := makeDeps(t)
			c.mutate(&deps)
			eng, err := merge.NewEngine(deps)
			if err == nil {
				t.Fatalf("NewEngine(%s) returned no error; want validation error", c.name)
			}
			if eng != nil {
				t.Errorf("NewEngine(%s) returned non-nil engine on error path: %v", c.name, eng)
			}
		})
	}
}

func TestNewEngineHappy(t *testing.T) {
	t.Parallel()

	deps := makeDeps(t)
	eng, err := merge.NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine happy path: %v", err)
	}
	if eng == nil {
		t.Fatal("NewEngine happy path returned nil engine, no error")
	}
}

func TestNewEngineDefaultsClock(t *testing.T) {
	t.Parallel()

	deps := makeDeps(t)
	deps.Clock = nil
	eng, err := merge.NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine with Clock=nil should succeed (defaults to realClock); got %v", err)
	}
	if eng == nil {
		t.Fatal("NewEngine with Clock=nil returned nil engine")
	}
}

func TestNewEngineAnomalyOptional(t *testing.T) {
	t.Parallel()

	depsNil := makeDeps(t)
	depsNil.Anomaly = nil
	if _, err := merge.NewEngine(depsNil); err != nil {
		t.Fatalf("NewEngine with Anomaly=nil: %v", err)
	}

	depsSet := makeDeps(t)
	depsSet.Anomaly = &fakeAnomalyEng{}
	if _, err := merge.NewEngine(depsSet); err != nil {
		t.Fatalf("NewEngine with Anomaly set: %v", err)
	}
}

func TestMergeEngineInterfaceSatisfied(t *testing.T) {
	t.Parallel()

	deps := makeDeps(t)
	eng, err := merge.NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	var _ merge.MergeEngine = eng
}

func makeReqEng() merge.MergeRequest {
	return merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "deadbeef0000000000000000000000000000beef",
		Mode:          merge.ModeNormal,
		EngineVersion: "v0.6.0",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-A", HeadSHA: "1111111111111111111111111111111111111111"},
		},
		ReviewerVotes: map[string]int{"feat-A": 1},
		TestSuite:     merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}},
	}
}

func TestMergeRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	d.Git = merge.NewFakeGit(merge.FakeOutput{Stderr: "fatal", Err: errors.New("exit 128")})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	req := makeReqEng()
	req.TargetBranch = ""

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for empty TargetBranch")
	}
	if !errors.Is(mergeErr, merge.ErrInvalidRequest) {
		t.Errorf("Merge err: not ErrInvalidRequest sentinel; got %v", mergeErr)
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeFailed {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on invalid-request rejection")
	}
}

func TestMergeCacheHitReturnsStoredOutcome(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	cache := d.Cache.(*fakeCacheEng)

	req := makeReqEng()
	cached := merge.MergeOutcome{
		Winner:         req.Candidates[0],
		IntegrationSHA: "cached-int",
		TestsPassed:    true,
	}
	cache.Store(req, cached)

	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge on cache hit: %v", mergeErr)
	}
	if out.IntegrationSHA != "cached-int" {
		t.Errorf("IntegrationSHA = %q, want %q", out.IntegrationSHA, "cached-int")
	}

	em := d.Emitter.(*recordingEmitter)
	sawHit := false
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeCacheHit {
			sawHit = true
		}
	}
	if !sawHit {
		t.Error("EvtMergeCacheHit not emitted on cache hit")
	}

	if calls := d.Baseline.(*fakeBaselineRunnerEng).Calls(); len(calls) != 0 {
		t.Errorf("baseline calls on cache hit = %d, want 0", len(calls))
	}
}

func TestMergeEmitsStartedOnFreshRequest(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, _ = e.Merge(context.Background(), makeReqEng())

	em := d.Emitter.(*recordingEmitter)
	sawStarted := false
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeStartedWithMode {
			sawStarted = true
		}
	}
	if !sawStarted {
		t.Error("EvtMergeStartedWithMode not emitted on fresh cache-miss request")
	}
}

// TestMergeBaselineFailAbortsAtomically — Step 4: BaselineRunner.Run returns
// err → Merge emits EvtMergeFailed{reason: "baseline_failed"} + returns the
// wrapped error verbatim. Critically, the Runner MUST NOT be invoked
// (invariant atomicity — a failed baseline aborts the pipeline before any
// per-candidate work begins; partial outcomes pollute scoring).
func TestMergeBaselineFailAbortsAtomically(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	baseErr := errors.New("baseline-blew-up")
	d.Baseline = &fakeBaselineRunnerEng{err: baseErr}

	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Baseline.Run returns err")
	}
	if !errors.Is(mergeErr, baseErr) {
		t.Errorf("Merge err: %v; want wrapping %v", mergeErr, baseErr)
	}

	// invariant atomicity: Runner MUST NOT be called when Baseline failed.
	if calls := d.Runner.(*fakeRunnerEng).Calls(); len(calls) != 0 {
		t.Errorf("Runner.RunCandidates calls on baseline failure = %d, want 0 (inv-zen-106)", len(calls))
	}

	if calls := d.Scorer.(*fakeScorerEng).Calls(); len(calls) != 0 {
		t.Errorf("Scorer.Rank calls on baseline failure = %d, want 0", len(calls))
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason != "baseline_failed" {
			t.Errorf("MergeFailed.Reason = %q, want %q", pl.Reason, "baseline_failed")
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on baseline failure")
	}
}

func TestMergeAllCandidatesFailReturnsErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	runErr := errors.New("everything-went-wrong")
	d.Runner = &fakeRunnerEng{err: runErr}
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Runner.RunCandidates returns err")
	}
	if !errors.Is(mergeErr, runErr) {
		t.Errorf("Merge err: %v; want wrapping %v", mergeErr, runErr)
	}

	if calls := d.Baseline.(*fakeBaselineRunnerEng).Calls(); len(calls) != 1 {
		t.Errorf("Baseline.Run calls = %d, want 1", len(calls))
	}

	if calls := d.Scorer.(*fakeScorerEng).Calls(); len(calls) != 0 {
		t.Errorf("Scorer.Rank calls on runner failure = %d, want 0", len(calls))
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason != "all_candidates_failed" {
			t.Errorf("MergeFailed.Reason = %q, want %q", pl.Reason, "all_candidates_failed")
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on runner failure")
	}
}

func TestMergeScoringEmitsScoringComplete(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}

	winnerScore := merge.ScoringResult{
		WinnerID:        cand.HeadSHA,
		AllScores:       map[string]float64{cand.HeadSHA: 1.0},
		TiebreakApplied: false,
	}
	d.Scorer = &fakeScorerEng{result: winnerScore}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, _ = e.Merge(context.Background(), req)

	if calls := d.Baseline.(*fakeBaselineRunnerEng).Calls(); len(calls) != 1 {
		t.Errorf("Baseline.Run calls = %d, want 1", len(calls))
	}
	if calls := d.Runner.(*fakeRunnerEng).Calls(); len(calls) != 1 {
		t.Errorf("Runner.RunCandidates calls = %d, want 1", len(calls))
	}
	scorerCalls := d.Scorer.(*fakeScorerEng).Calls()
	if len(scorerCalls) != 1 {
		t.Fatalf("Scorer.Rank calls = %d, want 1", len(scorerCalls))
	}

	if got, want := scorerCalls[0].Votes, req.ReviewerVotes; len(got) != len(want) {
		t.Errorf("Scorer.Rank votes len = %d, want %d", len(got), len(want))
	}

	em := d.Emitter.(*recordingEmitter)
	sawScored := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtScoringComplete {
			continue
		}
		var pl merge.ScoringCompletePayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("ScoringComplete payload unmarshal: %v", jerr)
		}
		if pl.WinnerID != cand.HeadSHA {
			t.Errorf("ScoringComplete.WinnerID = %q, want %q", pl.WinnerID, cand.HeadSHA)
		}
		if pl.Formula == "" {
			t.Error("ScoringComplete.Formula empty; MarshalScoringComplete should render the tiebreak formula")
		}
		sawScored = true
	}
	if !sawScored {
		t.Error("EvtScoringComplete not emitted on successful scoring")
	}
}

// TestMergeScoringNilVotesDefaultsToEmptyMap — Step 6 sub-contract: when
// req.ReviewerVotes is nil, the engine MUST forward an empty (non-nil) map
// to Scorer.Rank rather than nil. Scorer impls assume map indexing on a
// non-nil map; passing nil would panic on lookup.
func TestMergeScoringNilVotesDefaultsToEmptyMap(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	req := makeReqEng()
	req.ReviewerVotes = nil

	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{WinnerID: cand.HeadSHA}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, _ = e.Merge(context.Background(), req)

	scorerCalls := d.Scorer.(*fakeScorerEng).Calls()
	if len(scorerCalls) != 1 {
		t.Fatalf("Scorer.Rank calls = %d, want 1", len(scorerCalls))
	}
	if scorerCalls[0].Votes == nil {
		t.Error("Scorer.Rank received nil votes; engine must default to empty map (non-nil)")
	}
}

func TestMergeFullHappyPath(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{},
	)

	req := makeReqEng()
	cand := req.Candidates[0]

	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 3, TestFailCount: 0},
		},
	}
	winnerScores := map[string]float64{cand.HeadSHA: 1.0}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:        cand.HeadSHA,
		AllScores:       winnerScores,
		TiebreakApplied: false,
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge full happy-path: %v", mergeErr)
	}

	if out.Winner.HeadSHA != cand.HeadSHA {
		t.Errorf("out.Winner.HeadSHA = %q, want %q", out.Winner.HeadSHA, cand.HeadSHA)
	}
	if out.Winner.Branch != cand.Branch {
		t.Errorf("out.Winner.Branch = %q, want %q", out.Winner.Branch, cand.Branch)
	}
	if out.IntegrationSHA != cand.HeadSHA {
		t.Errorf("out.IntegrationSHA = %q, want %q (winner.HeadSHA)", out.IntegrationSHA, cand.HeadSHA)
	}
	if !out.TestsPassed {
		t.Error("out.TestsPassed = false, want true on happy-path")
	}
	if out.Reverted {
		t.Error("out.Reverted = true, want false on happy-path (no ctx-cancel)")
	}
	if got, want := out.AllScores[cand.HeadSHA], 1.0; got != want {
		t.Errorf("out.AllScores[%q] = %v, want %v", cand.HeadSHA, got, want)
	}

	if out.ReviewerSummary != "feat-A:+1" {
		t.Errorf("out.ReviewerSummary = %q, want %q", out.ReviewerSummary, "feat-A:+1")
	}

	cache := d.Cache.(*fakeCacheEng)
	stores := cache.Stores()
	if len(stores) != 1 {
		t.Fatalf("Cache.Store calls = %d, want 1", len(stores))
	}
	if stores[0].Outcome.IntegrationSHA != cand.HeadSHA {
		t.Errorf("Cache.Store outcome.IntegrationSHA = %q, want %q",
			stores[0].Outcome.IntegrationSHA, cand.HeadSHA)
	}

	calls := d.Git.(*merge.FakeGit).Calls()
	sawUpdateRef := false
	for _, c := range calls {
		if len(c.Args) >= 3 && c.Args[0] == "update-ref" {
			sawUpdateRef = true
			if c.Args[1] != "refs/heads/"+req.TargetBranch {
				t.Errorf("update-ref ref = %q, want %q", c.Args[1], "refs/heads/"+req.TargetBranch)
			}
			if c.Args[2] != cand.HeadSHA {
				t.Errorf("update-ref sha = %q, want %q (winner.HeadSHA)", c.Args[2], cand.HeadSHA)
			}
		}
	}
	if !sawUpdateRef {
		t.Errorf("Git.Run(update-ref ...) not invoked; calls=%v", calls)
	}

	em := d.Emitter.(*recordingEmitter)
	sawCompleted := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeCompleted {
			continue
		}
		var pl merge.MergeCompletedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeCompleted payload unmarshal: %v", jerr)
		}
		if pl.WinnerCandidateID != cand.HeadSHA {
			t.Errorf("MergeCompleted.WinnerCandidateID = %q, want %q",
				pl.WinnerCandidateID, cand.HeadSHA)
		}
		if pl.IntegrationSHA != cand.HeadSHA {
			t.Errorf("MergeCompleted.IntegrationSHA = %q, want %q",
				pl.IntegrationSHA, cand.HeadSHA)
		}
		if pl.RequestHash == "" {
			t.Error("MergeCompleted.RequestHash empty; engine must populate")
		}
		if pl.Outcome.IntegrationSHA != cand.HeadSHA {
			t.Errorf("MergeCompleted.Outcome.IntegrationSHA = %q, want %q",
				pl.Outcome.IntegrationSHA, cand.HeadSHA)
		}
		sawCompleted = true
	}
	if !sawCompleted {
		t.Error("EvtMergeCompleted not emitted on happy-path")
	}
}

// TestMergeFastForwardFailureSurfacesEvtMergeFailed — Step 7 sad path:
// Git.Run for update-ref returns err → Merge emits EvtMergeFailed{reason:
// integration_failed} + returns a wrapped error referencing the git failure.
// Cache.Store MUST NOT be invoked (atomicity: a failed FF means there is no
// integration SHA to cache).
func TestMergeFastForwardFailureSurfacesEvtMergeFailed(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	updateRefErr := errors.New("update-ref-blew-up")
	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{Stderr: "fatal: cannot lock ref", Err: updateRefErr},
	)

	req := makeReqEng()
	cand := req.Candidates[0]

	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when update-ref fails")
	}
	if !errors.Is(mergeErr, updateRefErr) {
		t.Errorf("Merge err: %v; want wrapping %v", mergeErr, updateRefErr)
	}

	// Cache.Store MUST NOT be invoked when FF fails.
	cache := d.Cache.(*fakeCacheEng)
	if stores := cache.Stores(); len(stores) != 0 {
		t.Errorf("Cache.Store calls on FF failure = %d, want 0 (atomicity)", len(stores))
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason != "integration_failed" {
			t.Errorf("MergeFailed.Reason = %q, want %q", pl.Reason, "integration_failed")
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on FF failure")
	}

	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeCompleted {
			t.Error("EvtMergeCompleted emitted on FF failure; must not be")
		}
	}
}

// TestMergeWinnerIDLookupFailureSurfacesEvtMergeFailed — Step 7 lookup-fail
// path: Scorer returns WinnerID that is NOT in req.Candidates → Merge emits
// EvtMergeFailed{reason: integration_failed} + returns a wrapped error
// referencing the missing winner. Git.Run for update-ref MUST NOT be invoked
// (no winner to fast-forward to). Cache.Store MUST NOT be invoked.
//
// This guards against Scorer drift — a future Scorer impl that returns a
// HeadSHA from a stale request would otherwise propagate uncaught into the
// FF step.
func TestMergeWinnerIDLookupFailureSurfacesEvtMergeFailed(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	req := makeReqEng()
	cand := req.Candidates[0]

	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}

	const orphanWinner = "ffffffffffffffffffffffffffffffffffffffff"
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  orphanWinner,
		AllScores: map[string]float64{orphanWinner: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when WinnerID not in candidates")
	}

	if !contains(mergeErr.Error(), orphanWinner) {
		t.Errorf("Merge err = %v; want message referencing %q for diagnosability",
			mergeErr, orphanWinner)
	}

	// update-ref MUST NOT be invoked (no winner candidate to FF to).
	calls := d.Git.(*merge.FakeGit).Calls()
	for _, c := range calls {
		if len(c.Args) >= 1 && c.Args[0] == "update-ref" {
			t.Errorf("update-ref invoked despite missing winner; calls=%v", calls)
		}
	}

	// Cache.Store MUST NOT be invoked.
	cache := d.Cache.(*fakeCacheEng)
	if stores := cache.Stores(); len(stores) != 0 {
		t.Errorf("Cache.Store calls on lookup failure = %d, want 0", len(stores))
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason != "integration_failed" {
			t.Errorf("MergeFailed.Reason = %q, want %q", pl.Reason, "integration_failed")
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on winner-lookup failure")
	}
}

func TestMergeMultipleReviewerVotesSortedByKey(t *testing.T) {
	t.Parallel()

	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "deadbeef0000000000000000000000000000beef",
		Mode:          merge.ModeNormal,
		EngineVersion: "v0.6.0",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat-Z", HeadSHA: "1111111111111111111111111111111111111111"},
			{Branch: "feat-A", HeadSHA: "2222222222222222222222222222222222222222"},
			{Branch: "feat-M", HeadSHA: "3333333333333333333333333333333333333333"},
		},

		ReviewerVotes: map[string]int{
			"feat-Z": -2,
			"feat-A": 3,
			"feat-M": 0,
		},
		TestSuite: merge.TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}},
	}

	d := makeDeps(t)

	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: req.BaseSHA + "\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{},
	)

	winnerCand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: winnerCand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  winnerCand.HeadSHA,
		AllScores: map[string]float64{winnerCand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge multi-vote happy-path: %v", mergeErr)
	}

	const want = "feat-A:+3, feat-M:+0, feat-Z:-2"
	if out.ReviewerSummary != want {
		t.Errorf("ReviewerSummary = %q, want %q (sort-by-key + signed format)",
			out.ReviewerSummary, want)
	}
}

// TestMergeDefaultsEngineVersionFromConfig — Step 2 sub-contract: when
// req.EngineVersion is empty, the engine MUST default it from
// EngineConfig.EngineVersion before computing CacheKey. Without this
// defaulting, callers that don't pin a per-call version would compute a
// CacheKey against EngineVersion="" — defeating the deploy-time cache
// invalidation contract (Q5 A).
//
// Verified by: pre-populating the cache against the request with
// EngineVersion=Config.EngineVersion ("test-engine-v1"), then invoking
// Merge with req.EngineVersion="". The cache lookup must hit (proving
// the engine substituted the config value before keying).
func TestMergeDefaultsEngineVersionFromConfig(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	cache := d.Cache.(*fakeCacheEng)

	canonicalReq := makeReqEng()
	canonicalReq.EngineVersion = "test-engine-v1"
	cached := merge.MergeOutcome{
		Winner:         canonicalReq.Candidates[0],
		IntegrationSHA: "cached-default-int",
		TestsPassed:    true,
	}
	cache.Store(canonicalReq, cached)

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	req := makeReqEng()
	req.EngineVersion = ""

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge with empty EngineVersion: %v", mergeErr)
	}
	if out.IntegrationSHA != "cached-default-int" {
		t.Errorf("IntegrationSHA = %q, want %q (cache hit on defaulted version)",
			out.IntegrationSHA, "cached-default-int")
	}
}

func TestMergeScorerFailureSurfacesAllCandidatesFailed(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	scoreErr := errors.New("scorer-blew-up")
	d.Scorer = &fakeScorerEng{err: scoreErr}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Scorer.Rank returns err")
	}
	if !errors.Is(mergeErr, scoreErr) {
		t.Errorf("Merge err: %v; want wrapping %v", mergeErr, scoreErr)
	}

	em := d.Emitter.(*recordingEmitter)
	sawFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason != "all_candidates_failed" {
			t.Errorf("MergeFailed.Reason = %q, want %q (spec §3)", pl.Reason, "all_candidates_failed")
		}
		sawFailed = true
	}
	if !sawFailed {
		t.Error("EvtMergeFailed not emitted on Scorer failure")
	}

	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtScoringComplete {
			t.Error("EvtScoringComplete emitted on Scorer failure; must not be")
		}
	}
}

type ctxCancellingBaselineEng struct {
	fakeBaselineRunnerEng
	cancel context.CancelFunc
}

func (f *ctxCancellingBaselineEng) Run(ctx context.Context, baseSHA string, mode merge.Mode, suite merge.TestSuite) (merge.PassingSet, error) {

	f.cancel()
	return f.fakeBaselineRunnerEng.Run(ctx, baseSHA, mode, suite)
}

type ctxCancellingRunnerEng struct {
	fakeRunnerEng
	cancel context.CancelFunc
}

func (f *ctxCancellingRunnerEng) RunCandidates(ctx context.Context, cands []merge.MergeCandidate, baseSHA string, ps merge.PassingSet, mode merge.Mode, suite merge.TestSuite) ([]merge.CandidateOutcome, error) {
	f.cancel()
	return f.fakeRunnerEng.RunCandidates(ctx, cands, baseSHA, ps, mode, suite)
}

type ctxCancellingScorerEng struct {
	fakeScorerEng
	cancel context.CancelFunc
}

func (f *ctxCancellingScorerEng) Rank(ctx context.Context, outcomes []merge.CandidateOutcome, votes map[string]int, cfg merge.ScoringConfig) (merge.ScoringResult, error) {
	f.cancel()
	return f.fakeScorerEng.Rank(ctx, outcomes, votes, cfg)
}

type emitterCancelOnStarted struct {
	inner    merge.EventEmitter
	cancel   context.CancelFunc
	once     sync.Once
	captured []merge.Event
	mu       sync.Mutex
}

func (e *emitterCancelOnStarted) Append(ctx context.Context, ev merge.Event) error {
	e.mu.Lock()
	e.captured = append(e.captured, ev)
	e.mu.Unlock()
	if ev.Type == merge.EvtMergeStartedWithMode {
		e.once.Do(func() { e.cancel() })
	}
	return e.inner.Append(ctx, ev)
}

func (e *emitterCancelOnStarted) Snapshot() []merge.Event {
	if r, ok := e.inner.(*recordingEmitter); ok {
		return r.Snapshot()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]merge.Event, len(e.captured))
	copy(out, e.captured)
	return out
}

type cancellingGitOnUpdateRef struct {
	inner    *merge.FakeGit
	cancel   context.CancelFunc
	once     sync.Once
	rollback []merge.FakeCall
	mu       sync.Mutex
}

func (g *cancellingGitOnUpdateRef) Run(ctx context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	if len(args) >= 1 && args[0] == "update-ref" {
		g.once.Do(func() {

			g.cancel()
		})
	}
	return g.inner.Run(ctx, repoDir, stdin, args...)
}

func TestMergeRevertedTrueOnCtxCancelPreValidate(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, mergeErr := e.Merge(ctx, makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for pre-cancelled ctx")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on ctx-cancel pre-validate; want true (spec §3.4)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on pre-validate ctx-cancel")
	}
}

func TestMergeRevertedTrueOnCtxCancelMidPipeline(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	ctx, cancel := context.WithCancel(context.Background())
	d.Baseline = &ctxCancellingBaselineEng{cancel: cancel}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for mid-pipeline ctx-cancel")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on mid-pipeline ctx-cancel; want true (spec §3.4)")
	}

	// invariant atomicity: Runner MUST NOT be invoked when the engine
	// catches ctx-cancel post-Baseline (mid-pipeline boundary).
	if calls := d.Runner.(*fakeRunnerEng).Calls(); len(calls) != 0 {
		t.Errorf("Runner.RunCandidates calls on mid-pipeline ctx-cancel = %d, want 0", len(calls))
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on mid-pipeline ctx-cancel")
	}
}

func TestMergeRevertedTrueOnCtxCancelInBaselineErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	d.Baseline = &fakeBaselineRunnerEng{err: context.Canceled}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Baseline returns context.Canceled")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false when Baseline returns ctx.Canceled; want true (spec §3.4)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	sawBaselineFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		switch pl.Reason {
		case "ctx_cancelled":
			sawCtxCancelled = true
		case "baseline_failed":
			sawBaselineFailed = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on Baseline ctx-cancel err")
	}
	if sawBaselineFailed {
		t.Error("EvtMergeFailed{reason: baseline_failed} emitted on ctx-cancel; spec §3.4 mandates ctx_cancelled discrimination")
	}
}

func TestMergeRevertedTrueOnCtxCancelInRunnerErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	d.Runner = &fakeRunnerEng{err: context.DeadlineExceeded}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Runner returns context.DeadlineExceeded")
	}
	if !errors.Is(mergeErr, context.DeadlineExceeded) {
		t.Errorf("Merge err: %v; want errors.Is context.DeadlineExceeded", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on Runner ctx.DeadlineExceeded; want true (spec §3.4)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on Runner ctx-cancel err")
	}
}

func TestMergeRevertedTrueOnCtxCancelInScorerErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{err: context.Canceled}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when Scorer returns context.Canceled")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on Scorer ctx.Canceled; want true (spec §3.4)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on Scorer ctx-cancel err")
	}
}

func TestMergeRevertedTrueOnCtxCancelPostFastForward(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	const preFFSHA = "preff000000000000000000000000000000ff00f"
	innerFG := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: preFFSHA + "\n"},
		merge.FakeOutput{},
		merge.FakeOutput{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	d.Git = &cancellingGitOnUpdateRef{inner: innerFG, cancel: cancel}

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when ctx cancelled post-FF")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on post-FF ctx-cancel; want true (spec §3.4)")
	}

	calls := innerFG.Calls()
	updateRefCalls := 0
	sawFFApply := false
	sawRollback := false
	for _, c := range calls {
		if len(c.Args) >= 1 && c.Args[0] != "update-ref" {
			continue
		}
		updateRefCalls++
		if len(c.Args) == 3 && c.Args[2] == cand.HeadSHA {
			sawFFApply = true
		}
		if len(c.Args) == 3 && c.Args[2] == preFFSHA {
			sawRollback = true
		}
	}
	if updateRefCalls < 2 {
		t.Errorf("update-ref calls = %d, want ≥2 (FF apply + rollback)", updateRefCalls)
	}
	if !sawFFApply {
		t.Errorf("FF apply update-ref(<winner>) not invoked; calls=%v", calls)
	}
	if !sawRollback {
		t.Errorf("Rollback update-ref(<preFFSHA=%q>) not invoked; calls=%v", preFFSHA, calls)
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type == merge.EvtMergeCompleted {
			t.Error("EvtMergeCompleted emitted on post-FF ctx-cancel; must not be (rollback path)")
		}
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on post-FF ctx-cancel")
	}

	// Cache.Store MUST NOT be invoked when the FF was rolled back.
	cache := d.Cache.(*fakeCacheEng)
	if stores := cache.Stores(); len(stores) != 0 {
		t.Errorf("Cache.Store calls on post-FF rollback = %d, want 0 (rollback invalidates outcome)", len(stores))
	}
}

// TestMergeRevertedTrueOnCtxCancelPostFFWithEmptyPreSHA — case (6) variant:
// pre-FF rev-parse fails (empty stdout, no err). The engine MUST still
// rollback by deleting refs/heads/<target> (update-ref -d). Verifies the
// helper's preSHA="" branch lands the deletion path.
func TestMergeRevertedTrueOnCtxCancelPostFFWithEmptyPreSHA(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	innerFG := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{},
		merge.FakeOutput{},
		merge.FakeOutput{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	d.Git = &cancellingGitOnUpdateRef{inner: innerFG, cancel: cancel}

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil")
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false; want true on post-FF ctx-cancel + empty preSHA")
	}

	calls := innerFG.Calls()
	sawDeleteRollback := false
	for _, c := range calls {
		if len(c.Args) >= 2 && c.Args[0] == "update-ref" && c.Args[1] == "-d" {
			sawDeleteRollback = true
		}
	}
	if !sawDeleteRollback {
		t.Errorf("Rollback update-ref -d not invoked when preFFSHA empty; calls=%v", calls)
	}
}

type cancellingGitOnUpdateRefRollbackErr struct {
	inner    *merge.FakeGit
	cancel   context.CancelFunc
	once     sync.Once
	rollback error
}

func (g *cancellingGitOnUpdateRefRollbackErr) Run(ctx context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	if len(args) >= 1 && args[0] == "update-ref" {
		first := false
		g.once.Do(func() {
			g.cancel()
			first = true
		})
		if !first {

			return "", "fatal: rollback denied", g.rollback
		}
	}
	return g.inner.Run(ctx, repoDir, stdin, args...)
}

func TestMergeRevertedTrueOnCtxCancelPostFFWithRollbackErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	const preFFSHA = "preff000000000000000000000000000000ff00f"
	innerFG := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: preFFSHA + "\n"},
		merge.FakeOutput{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	rollbackErr := errors.New("rollback-blew-up")
	d.Git = &cancellingGitOnUpdateRefRollbackErr{inner: innerFG, cancel: cancel, rollback: rollbackErr}

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil")
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on rollback-failure path; want true (best-effort rollback contract)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawDetailWithRollbackFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" && contains(pl.Detail, "rollback") {
			sawDetailWithRollbackFailed = true
		}
	}
	if !sawDetailWithRollbackFailed {
		t.Error("EvtMergeFailed.Detail did not include rollback failure information")
	}
}

func TestMergeRevertedFalseOnNonCtxBaselineErr(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})
	d.Baseline = &fakeBaselineRunnerEng{err: errors.New("merge: tests fail")}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil")
	}
	if out.Reverted {
		t.Error("MergeOutcome.Reverted = true on non-ctx baseline err; want false (zero value preserved)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawBaselineFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "baseline_failed" {
			sawBaselineFailed = true
		}
		if pl.Reason == "ctx_cancelled" {
			t.Errorf("EvtMergeFailed{reason=ctx_cancelled} on non-ctx err; ctx discrimination too eager")
		}
	}
	if !sawBaselineFailed {
		t.Error("EvtMergeFailed{reason: baseline_failed} not emitted on non-ctx baseline err")
	}
}

func TestMergeRevertedTrueOnCtxCancelBetweenStartedAndBaseline(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	ctx, cancel := context.WithCancel(context.Background())
	d.Emitter = &emitterCancelOnStarted{inner: &recordingEmitter{}, cancel: cancel}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, makeReqEng())
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for between-Started-and-Baseline ctx-cancel")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on between-Started-and-Baseline ctx-cancel; want true (spec §3.4)")
	}

	// Baseline MUST NOT be invoked — ctx-cancel boundary catches before Step 4.
	if calls := d.Baseline.(*fakeBaselineRunnerEng).Calls(); len(calls) != 0 {
		t.Errorf("Baseline.Run calls on between-Started-Baseline ctx-cancel = %d, want 0", len(calls))
	}

	em := d.Emitter.(*emitterCancelOnStarted).inner.(*recordingEmitter)
	sawCtxCancelled := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" {
			sawCtxCancelled = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on between-Started-Baseline ctx-cancel")
	}
}

func TestMergeRevertedTrueOnCtxCancelBetweenRunnerAndScorer(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	ctx, cancel := context.WithCancel(context.Background())
	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &ctxCancellingRunnerEng{
		fakeRunnerEng: fakeRunnerEng{
			outcomes: []merge.CandidateOutcome{
				{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
			},
		},
		cancel: cancel,
	}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for between-Runner-Scorer ctx-cancel")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on between-Runner-Scorer ctx-cancel; want true (spec §3.4)")
	}

	// Scorer MUST NOT be invoked.
	if calls := d.Scorer.(*fakeScorerEng).Calls(); len(calls) != 0 {
		t.Errorf("Scorer.Rank calls on between-Runner-Scorer ctx-cancel = %d, want 0", len(calls))
	}
}

func TestMergeRevertedTrueOnCtxCancelBetweenScorerAndFastForward(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"})

	ctx, cancel := context.WithCancel(context.Background())
	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &ctxCancellingScorerEng{
		fakeScorerEng: fakeScorerEng{result: merge.ScoringResult{
			WinnerID:  cand.HeadSHA,
			AllScores: map[string]float64{cand.HeadSHA: 1.0},
		}},
		cancel: cancel,
	}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(ctx, req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil for between-Scorer-FF ctx-cancel")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on between-Scorer-FF ctx-cancel; want true (spec §3.4)")
	}

	// update-ref MUST NOT be invoked — ctx-cancel boundary fires before Step 7 FF.
	calls := d.Git.(*merge.FakeGit).Calls()
	for _, c := range calls {
		if len(c.Args) >= 1 && c.Args[0] == "update-ref" {
			t.Errorf("update-ref invoked on between-Scorer-FF ctx-cancel; calls=%v", calls)
		}
	}
}

type cancellingGitFFErr struct {
	inner       *merge.FakeGit
	mu          sync.Mutex
	updateCount int
}

func (g *cancellingGitFFErr) Run(ctx context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	if len(args) >= 1 && args[0] == "update-ref" {
		g.mu.Lock()
		g.updateCount++
		count := g.updateCount
		g.mu.Unlock()
		if count == 1 {

			return "", "", context.Canceled
		}
	}
	return g.inner.Run(ctx, repoDir, stdin, args...)
}

func TestMergeRevertedTrueOnCtxCancelDuringFastForward(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	const preFFSHA = "preff000000000000000000000000000000ff00f"
	innerFG := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: preFFSHA + "\n"},

		merge.FakeOutput{},
	)
	d.Git = &cancellingGitFFErr{inner: innerFG}

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil when FF returns context.Canceled")
	}
	if !errors.Is(mergeErr, context.Canceled) {
		t.Errorf("Merge err: %v; want errors.Is context.Canceled", mergeErr)
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false when FF returns ctx.Canceled; want true (spec §3.4)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawCtxCancelled := false
	sawIntegrationFailed := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		switch pl.Reason {
		case "ctx_cancelled":
			sawCtxCancelled = true
		case "integration_failed":
			sawIntegrationFailed = true
		}
	}
	if !sawCtxCancelled {
		t.Error("EvtMergeFailed{reason: ctx_cancelled} not emitted on FF ctx-cancel err")
	}
	if sawIntegrationFailed {
		t.Error("EvtMergeFailed{reason: integration_failed} on ctx-cancel; spec §3.4 mandates ctx_cancelled")
	}
}

// TestMergeRevertedTrueOnCtxCancelDuringFastForwardRollbackFails — defense:
// FF returns ctx.Canceled AND the rollback also fails. Reverted=true MUST
// still be returned (best-effort rollback contract). Rollback failure
// surfaces in EvtMergeFailed.Detail.
type cancellingGitFFErrRollbackFails struct {
	inner       *merge.FakeGit
	mu          sync.Mutex
	updateCount int
	rollbackErr error
}

func (g *cancellingGitFFErrRollbackFails) Run(ctx context.Context, repoDir, stdin string, args ...string) (string, string, error) {
	if len(args) >= 1 && args[0] == "update-ref" {
		g.mu.Lock()
		g.updateCount++
		count := g.updateCount
		g.mu.Unlock()
		if count == 1 {
			return "", "", context.Canceled
		}

		return "", "fatal: rollback failed", g.rollbackErr
	}
	return g.inner.Run(ctx, repoDir, stdin, args...)
}

func TestMergeRevertedTrueOnCtxCancelDuringFastForwardRollbackFails(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	const preFFSHA = "preff000000000000000000000000000000ff00f"
	innerFG := merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: preFFSHA + "\n"},
	)
	d.Git = &cancellingGitFFErrRollbackFails{inner: innerFG, rollbackErr: errors.New("rollback-blew-up")}

	req := makeReqEng()
	cand := req.Candidates[0]
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 1, TestFailCount: 0},
		},
	}
	d.Scorer = &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	out, mergeErr := e.Merge(context.Background(), req)
	if mergeErr == nil {
		t.Fatal("Merge: err = nil, want non-nil")
	}
	if !out.Reverted {
		t.Error("MergeOutcome.Reverted = false on FF-ctx + rollback-fail; want true (best-effort)")
	}

	em := d.Emitter.(*recordingEmitter)
	sawDetailWithRollbackFailure := false
	for _, ev := range em.Snapshot() {
		if ev.Type != merge.EvtMergeFailed {
			continue
		}
		var pl merge.MergeFailedPayload
		if jerr := json.Unmarshal(ev.Payload, &pl); jerr != nil {
			t.Fatalf("MergeFailed payload unmarshal: %v", jerr)
		}
		if pl.Reason == "ctx_cancelled" && contains(pl.Detail, "rollback") {
			sawDetailWithRollbackFailure = true
		}
	}
	if !sawDetailWithRollbackFailure {
		t.Error("EvtMergeFailed.Detail did not surface rollback failure")
	}
}

type fakeBlastScorerEng struct {
	mu      sync.Mutex
	calls   []fakeBlastScorerCall
	verdict merge.Verdict
	err     error
}

type fakeBlastScorerCall struct {
	ProjectID      string
	ChangedSymbols []string
	ChangedFiles   []string
}

func (f *fakeBlastScorerEng) BlastRadius(_ context.Context, projectID string, syms, files []string) (merge.Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	symsCp := make([]string, len(syms))
	copy(symsCp, syms)
	filesCp := make([]string, len(files))
	copy(filesCp, files)
	f.calls = append(f.calls, fakeBlastScorerCall{ProjectID: projectID, ChangedSymbols: symsCp, ChangedFiles: filesCp})
	return f.verdict, f.err
}

func (f *fakeBlastScorerEng) Calls() []fakeBlastScorerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeBlastScorerCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestEngineBlastRadiusNilDepTolerated(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)

	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{},
	)

	req := makeReqEng()
	cand := req.Candidates[0]
	scorerCapture := &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 3, TestFailCount: 0},
		},
	}
	d.Scorer = scorerCapture

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine with nil BlastRadius: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge with nil BlastRadius: %v", mergeErr)
	}

	calls := scorerCapture.Calls()
	if len(calls) != 1 {
		t.Fatalf("Scorer.Rank calls = %d, want 1", len(calls))
	}
	for _, o := range calls[0].Outcomes {
		if o.BlastRadius != 0 {
			t.Errorf("outcome %q BlastRadius = %v; want 0 (nil scorer, Plan 6 preserved)", o.Candidate.HeadSHA, o.BlastRadius)
		}
	}
}

func TestEngineBlastRadiusPopulatedBeforeRank(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{},
	)

	req := makeReqEng()
	cand := req.Candidates[0]

	const wantBlastScore = 0.75
	blastScorer := &fakeBlastScorerEng{verdict: merge.Verdict{Level: "high", Score: wantBlastScore}}

	scorerCapture := &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 3, TestFailCount: 0},
		},
	}
	d.Scorer = scorerCapture
	d.BlastRadius = blastScorer

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine with BlastRadius scorer: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge with BlastRadius scorer: %v", mergeErr)
	}

	blastCalls := blastScorer.Calls()
	if len(blastCalls) != 1 {
		t.Fatalf("BlastRadius scorer calls = %d, want 1", len(blastCalls))
	}

	scorerCalls := scorerCapture.Calls()
	if len(scorerCalls) != 1 {
		t.Fatalf("Scorer.Rank calls = %d, want 1", len(scorerCalls))
	}
	for _, o := range scorerCalls[0].Outcomes {
		if o.BlastRadius != wantBlastScore {
			t.Errorf("outcome %q BlastRadius at Rank = %v; want %v (populate-before-Rank contract)",
				o.Candidate.HeadSHA, o.BlastRadius, wantBlastScore)
		}
	}
}

func TestEngineBlastRadiusScorerErrorDegrades(t *testing.T) {
	t.Parallel()

	d := makeDeps(t)
	d.Git = merge.NewFakeGit(
		merge.FakeOutput{Stdout: "feedface00000000000000000000000000000000\n"},
		merge.FakeOutput{Stdout: "cafef00d00000000000000000000000000000000\n"},
		merge.FakeOutput{},
	)

	req := makeReqEng()
	cand := req.Candidates[0]

	errBlast := &fakeBlastScorerEng{
		verdict: merge.Verdict{Score: 0.9},
		err:     errors.New("caronte-unavailable"),
	}

	scorerCapture := &fakeScorerEng{result: merge.ScoringResult{
		WinnerID:  cand.HeadSHA,
		AllScores: map[string]float64{cand.HeadSHA: 1.0},
	}}
	d.Runner = &fakeRunnerEng{
		outcomes: []merge.CandidateOutcome{
			{Candidate: cand, TestPassCount: 3, TestFailCount: 0},
		},
	}
	d.Scorer = scorerCapture
	d.BlastRadius = errBlast

	e, err := merge.NewEngine(d)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, mergeErr := e.Merge(context.Background(), req)
	if mergeErr != nil {
		t.Fatalf("Merge must succeed despite scorer error (best-effort degradation): %v", mergeErr)
	}

	scorerCalls := scorerCapture.Calls()
	if len(scorerCalls) != 1 {
		t.Fatalf("Scorer.Rank calls = %d, want 1", len(scorerCalls))
	}
	for _, o := range scorerCalls[0].Outcomes {
		if o.BlastRadius != 0 {
			t.Errorf("outcome BlastRadius = %v on scorer error; want 0 (degrade, not block)", o.BlastRadius)
		}
	}
}
