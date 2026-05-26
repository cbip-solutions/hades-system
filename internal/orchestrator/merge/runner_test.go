package merge_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type fakeCandidateRunner struct {
	mu       sync.Mutex
	calls    []fakeCandRunCall
	delay    time.Duration
	outcome  func(c merge.MergeCandidate) merge.CandidateOutcome
	err      func(c merge.MergeCandidate) error
	panicFor func(c merge.MergeCandidate) any
	hangFor  map[string]time.Duration

	ignoreCtxHang bool
}

type fakeCandRunCall struct {
	HeadSHA string
	BaseSHA string
	Mode    merge.Mode
}

func (f *fakeCandidateRunner) Run(ctx context.Context, c merge.MergeCandidate, baseSHA string, _ merge.PassingSet, mode merge.Mode, _ merge.TestSuite) (merge.CandidateOutcome, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCandRunCall{HeadSHA: c.HeadSHA, BaseSHA: baseSHA, Mode: mode})
	hang := f.hangFor[c.HeadSHA]
	d := f.delay
	getOut := f.outcome
	getErr := f.err
	getPanic := f.panicFor
	ignoreCtx := f.ignoreCtxHang
	f.mu.Unlock()

	totalDelay := d
	if hang > 0 {
		totalDelay = hang
	}
	if totalDelay > 0 {
		if ignoreCtx {

			time.Sleep(totalDelay)
		} else {
			select {
			case <-ctx.Done():
				return merge.CandidateOutcome{Candidate: c, HardRejected: true, Reason: "ctx_cancelled"}, ctx.Err()
			case <-time.After(totalDelay):
			}
		}
	}

	if getPanic != nil {
		if v := getPanic(c); v != nil {
			panic(v)
		}
	}

	var out merge.CandidateOutcome
	if getOut != nil {
		out = getOut(c)
	} else {
		out = merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
	}
	var err error
	if getErr != nil {
		err = getErr(c)
	}
	return out, err
}

func (f *fakeCandidateRunner) Calls() []fakeCandRunCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeCandRunCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func makeRunner(t *testing.T, cr merge.CandidateRunner, em merge.EventEmitter) merge.Runner {
	t.Helper()
	gc := &merge.GenerationCounter{}
	r, err := merge.NewRunner(merge.RunnerDeps{
		Candidate: cr,
		Emitter:   em,
		GenCtr:    gc,
	}, merge.RunnerConfig{StragglerKillGracePeriod: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return r
}

func TestNewRunnerRejectsMissingDeps(t *testing.T) {
	cases := []merge.RunnerDeps{
		{},
		{Candidate: &fakeCandidateRunner{}},
		{Emitter: &recordingEmitter{}},
		{Candidate: &fakeCandidateRunner{}, Emitter: nil},
	}
	for i, deps := range cases {
		_, err := merge.NewRunner(deps, merge.RunnerConfig{})
		if err == nil {
			t.Errorf("case %d: NewRunner accepted incomplete deps %+v", i, deps)
		}
	}
}

func TestNewRunnerRejectsNegativeGrace(t *testing.T) {
	deps := merge.RunnerDeps{
		Candidate: &fakeCandidateRunner{},
		Emitter:   &recordingEmitter{},
	}
	_, err := merge.NewRunner(deps, merge.RunnerConfig{StragglerKillGracePeriod: -time.Second})
	if err == nil {
		t.Error("NewRunner accepted negative grace period")
	}
}

func TestNewRunnerDefaultsGracePeriod(t *testing.T) {
	deps := merge.RunnerDeps{
		Candidate: &fakeCandidateRunner{},
		Emitter:   &recordingEmitter{},
	}
	r, err := merge.NewRunner(deps, merge.RunnerConfig{})
	if err != nil {
		t.Fatalf("NewRunner with zero grace returned err: %v", err)
	}
	if r == nil {
		t.Fatal("NewRunner returned nil with zero grace + nil err")
	}
}

func TestNewRunnerDefaultsClock(t *testing.T) {
	deps := merge.RunnerDeps{
		Candidate: &fakeCandidateRunner{},
		Emitter:   &recordingEmitter{},
	}
	r, err := merge.NewRunner(deps, merge.RunnerConfig{StragglerKillGracePeriod: time.Second})
	if err != nil {
		t.Fatalf("NewRunner with nil Clock returned err: %v", err)
	}
	if r == nil {
		t.Fatal("NewRunner returned nil with nil Clock + nil err")
	}
}

func TestErrAllCandidatesFailedIsSentinel(t *testing.T) {
	if !errors.Is(merge.ErrAllCandidatesFailed, merge.ErrAllCandidatesFailed) {
		t.Error("ErrAllCandidatesFailed not matchable via errors.Is on itself")
	}
}

func TestDefaultStragglerKillGracePeriodIs30s(t *testing.T) {
	if merge.DefaultStragglerKillGracePeriod != 30*time.Second {
		t.Errorf("DefaultStragglerKillGracePeriod = %v want 30s", merge.DefaultStragglerKillGracePeriod)
	}
}

func TestMakeRunnerHelperCompiles(t *testing.T) {
	cr := &fakeCandidateRunner{}
	em := &recordingEmitter{}
	r := makeRunner(t, cr, em)
	if r == nil {
		t.Fatal("makeRunner returned nil")
	}
}

// TestRunCandidatesParallelFanout proves the goroutine fanout is parallel,
// not sequential. With 3 candidates each delaying 50ms, total elapsed must
// stay well below the 150ms sequential floor (130ms threshold accommodates
// goroutine spawn + scheduler jitter + WaitGroup sync overhead).
//
// Doctrine — sibling isolation contract (Q8 D): the runner MUST NOT serialize
// candidate work. A regression that drops the goroutine fanout would make
// this elapsed-time check fail loudly rather than silently degrade.
func TestRunCandidatesParallelFanout(t *testing.T) {
	cr := &fakeCandidateRunner{
		delay: 50 * time.Millisecond,
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	em := &recordingEmitter{}
	r := makeRunner(t, cr, em)
	cands := []merge.MergeCandidate{
		{Branch: "feat-A", HeadSHA: "h1"},
		{Branch: "feat-B", HeadSHA: "h2"},
		{Branch: "feat-C", HeadSHA: "h3"},
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	start := time.Now()
	outs, err := r.RunCandidates(context.Background(), cands, "abc", merge.PassingSet{}, merge.ModeNormal, suite)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunCandidates: %v", err)
	}
	if len(outs) != 3 {
		t.Errorf("len(outs) = %d want 3", len(outs))
	}
	if elapsed > 130*time.Millisecond {
		t.Errorf("elapsed = %v want < 130ms (parallel fanout)", elapsed)
	}
}

func TestRunCandidatesPreservesInputOrder(t *testing.T) {
	cr := &fakeCandidateRunner{
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	r := makeRunner(t, cr, &recordingEmitter{})
	cands := []merge.MergeCandidate{
		{Branch: "a", HeadSHA: "h1"},
		{Branch: "b", HeadSHA: "h2"},
		{Branch: "c", HeadSHA: "h3"},
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	outs, _ := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	for i, o := range outs {
		if o.Candidate.HeadSHA != cands[i].HeadSHA {
			t.Errorf("outs[%d].Candidate.HeadSHA = %s want %s (order drift)", i, o.Candidate.HeadSHA, cands[i].HeadSHA)
		}
	}
}

func TestRunCandidatesSiblingIsolationOnSinglePanic(t *testing.T) {
	cr := &fakeCandidateRunner{
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			if c.HeadSHA == "h2" {
				return merge.CandidateOutcome{Candidate: c, HardRejected: true, Reason: "panic"}
			}
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	r := makeRunner(t, cr, &recordingEmitter{})
	cands := []merge.MergeCandidate{
		{Branch: "a", HeadSHA: "h1"},
		{Branch: "b", HeadSHA: "h2"},
		{Branch: "c", HeadSHA: "h3"},
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	outs, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("RunCandidates: %v (sibling fail should not surface)", err)
	}
	survivors := 0
	for _, o := range outs {
		if !o.HardRejected {
			survivors++
		}
	}
	if survivors != 2 {
		t.Errorf("survivors = %d want 2 (sibling isolation)", survivors)
	}
}

func TestRunCandidatesAllFailReturnsErrAllCandidatesFailed(t *testing.T) {
	cr := &fakeCandidateRunner{
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, HardRejected: true, Reason: "test failed"}
		},
	}
	em := &recordingEmitter{}
	r := makeRunner(t, cr, em)
	cands := []merge.MergeCandidate{
		{Branch: "a", HeadSHA: "h1"},
		{Branch: "b", HeadSHA: "h2"},
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrAllCandidatesFailed) {
		t.Fatalf("err = %v want wraps ErrAllCandidatesFailed", err)
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAllCandidatesFailed {
			saw = true
			var p merge.MergeAllCandidatesFailedPayload
			if uerr := json.Unmarshal(e.Payload, &p); uerr != nil {
				t.Errorf("unmarshal MergeAllCandidatesFailedPayload: %v", uerr)
			}
			if len(p.CandidateFailures) != 2 {
				t.Errorf("CandidateFailures len = %d want 2", len(p.CandidateFailures))
			}
		}
	}
	if !saw {
		t.Error("EvtMergeAllCandidatesFailed not emitted")
	}
}

func TestRunCandidatesEmptyInput(t *testing.T) {
	cr := &fakeCandidateRunner{}
	r := makeRunner(t, cr, &recordingEmitter{})
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	outs, err := r.RunCandidates(context.Background(), nil, "abc", nil, merge.ModeNormal, suite)
	if err == nil {
		t.Error("RunCandidates(nil) accepted (should reject — caller bug)")
	}
	if len(outs) != 0 {
		t.Errorf("len(outs) = %d want 0 on nil input", len(outs))
	}
}

// TestRunCandidatesGoroutinePanicRecovered exercises the runner's
// `defer recover()` path: a real panic inside CandidateRunner.Run for
// one goroutine MUST surface as a HardRejected outcome with Reason
// formatted as "runner_panic: <recovered>" — siblings continue.
//
// Distinct from TestRunCandidatesSiblingIsolationOnSinglePanic (which
// emulates the post-recover state by returning HardRejected directly):
// this test triggers a real Go-runtime panic so the defer/recover path
// in runner.go is statement-covered. Closes the doctrine-floor coverage
// gap on the recover branch (no-defer doctrine: untested documented
// behaviour is tech debt regardless of the "Minor" tag).
func TestRunCandidatesGoroutinePanicRecovered(t *testing.T) {
	cr := &fakeCandidateRunner{
		panicFor: func(c merge.MergeCandidate) any {
			if c.HeadSHA == "h2" {
				return "synthetic explosion"
			}
			return nil
		},
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	r := makeRunner(t, cr, &recordingEmitter{})
	cands := []merge.MergeCandidate{
		{Branch: "a", HeadSHA: "h1"},
		{Branch: "b", HeadSHA: "h2"},
		{Branch: "c", HeadSHA: "h3"},
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	outs, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("RunCandidates: %v (single-goroutine panic must NOT surface)", err)
	}
	if len(outs) != 3 {
		t.Fatalf("len(outs) = %d want 3", len(outs))
	}
	if !outs[1].HardRejected {
		t.Errorf("outs[1].HardRejected = false want true (panicked goroutine should be flagged)")
	}
	if outs[1].Reason == "" || outs[1].Reason[:13] != "runner_panic:" {
		t.Errorf("outs[1].Reason = %q want prefix 'runner_panic:'", outs[1].Reason)
	}

	if outs[1].Candidate.HeadSHA != "h2" {
		t.Errorf("outs[1].Candidate.HeadSHA = %q want 'h2'", outs[1].Candidate.HeadSHA)
	}
	survivors := 0
	for _, o := range outs {
		if !o.HardRejected {
			survivors++
		}
	}
	if survivors != 2 {
		t.Errorf("survivors = %d want 2 (sibling isolation around real panic)", survivors)
	}
}

// TestRunCandidatesErrInjectionFromCandidateRunner exercises the err-path
// from CandidateRunner.Run: the runner MUST flag HardRejected, populate
// Reason with the "runner_err: <err>" prefix when the upstream Reason is
// empty, AND respect a non-empty Reason set by the candidate runner
// (CandidateRunner contract: "may populate Reason for diagnostic clarity
// even on the err path"). Closes the doctrine-floor coverage gap on the
// `err != nil` branch in runner.go.
func TestRunCandidatesErrInjectionFromCandidateRunner(t *testing.T) {
	t.Run("err with empty out.Reason → runner injects prefix", func(t *testing.T) {
		cr := &fakeCandidateRunner{
			outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {

				return merge.CandidateOutcome{}
			},
			err: func(c merge.MergeCandidate) error {
				return errors.New("boom")
			},
		}
		em := &recordingEmitter{}
		r := makeRunner(t, cr, em)
		cands := []merge.MergeCandidate{{Branch: "a", HeadSHA: "h1"}}
		suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
		outs, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
		if !errors.Is(err, merge.ErrAllCandidatesFailed) {
			t.Fatalf("err = %v want wraps ErrAllCandidatesFailed (single-cand all-fail)", err)
		}
		if !outs[0].HardRejected {
			t.Errorf("outs[0].HardRejected = false want true (err must flag)")
		}
		if outs[0].Reason != "runner_err: boom" {
			t.Errorf("outs[0].Reason = %q want 'runner_err: boom'", outs[0].Reason)
		}

		if outs[0].Candidate.HeadSHA != "h1" {
			t.Errorf("outs[0].Candidate.HeadSHA = %q want 'h1' (defensive rehydration)", outs[0].Candidate.HeadSHA)
		}
	})

	t.Run("err with pre-populated Reason → runner preserves Reason", func(t *testing.T) {
		cr := &fakeCandidateRunner{
			outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
				return merge.CandidateOutcome{Candidate: c, Reason: "pre-existing reason"}
			},
			err: func(c merge.MergeCandidate) error {
				return errors.New("ignored at Reason level")
			},
		}
		em := &recordingEmitter{}
		r := makeRunner(t, cr, em)
		cands := []merge.MergeCandidate{{Branch: "a", HeadSHA: "h1"}}
		suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
		outs, _ := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
		if outs[0].Reason != "pre-existing reason" {
			t.Errorf("outs[0].Reason = %q want 'pre-existing reason' (preserve)", outs[0].Reason)
		}
		if !outs[0].HardRejected {
			t.Errorf("outs[0].HardRejected = false want true (err always flags HardRejected)")
		}
	})
}

// TestStragglerEmitsAfterGrace exercises inv-zen-108: when a per-candidate
// ctx fires (deadline or cancel) before Candidate.Run returns, the
// armStragglerSupervisor goroutine MUST arm a grace timer and, if the
// candidate has not signalled `done` by the time the grace elapses,
// emit EvtMergeStragglerKilled with MergeStragglerKilledPayload carrying
// the candidate id, "SIGKILL" canonical signal name, and the configured
// grace period in milliseconds.
//
// Setup uses tight timings so the test stays fast: ctx timeout 50ms,
// grace 30ms, candidate hang 500ms with ignoreCtxHang=true (emulates a
// real subprocess that ignores SIGTERM and only stops on SIGKILL —
// exactly inv-zen-108's production scenario). Because the candidate
// hangs PAST ctx+grace, the supervisor's first-select catches
// cctx.Done() at t=50ms (parent-ctx propagation), arms AfterFunc(30ms),
// and emits at t=80ms — well before Run unblocks at t=500ms.
//
// Total deadline budget: ctx timeout (50ms) + grace (30ms) + emit slack
// (≤120ms scheduler jitter) = 200ms ceiling. RunCandidates returns at
// candidate hang ≈ 500ms+ε; we observe the emission by polling the
// emitter snapshot WHILE the candidate is still running.
func TestStragglerEmitsAfterGrace(t *testing.T) {
	cr := &fakeCandidateRunner{
		hangFor:       map[string]time.Duration{"h1": 500 * time.Millisecond},
		ignoreCtxHang: true,
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	em := &recordingEmitter{}
	gc := &merge.GenerationCounter{}
	r, ferr := merge.NewRunner(merge.RunnerDeps{
		Candidate: cr,
		Emitter:   em,
		GenCtr:    gc,
	}, merge.RunnerConfig{StragglerKillGracePeriod: 30 * time.Millisecond})
	if ferr != nil {
		t.Fatalf("NewRunner: %v", ferr)
	}
	cands := []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h1"}}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rcDone := make(chan struct{})
	go func() {
		defer close(rcDone)
		_, _ = r.RunCandidates(ctx, cands, "abc", nil, merge.ModeNormal, suite)
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		for _, e := range em.Snapshot() {
			if e.Type == merge.EvtMergeStragglerKilled {
				var p merge.MergeStragglerKilledPayload
				if uerr := json.Unmarshal(e.Payload, &p); uerr != nil {
					t.Fatalf("unmarshal MergeStragglerKilledPayload: %v", uerr)
				}
				if p.CandidateID != "h1" {
					t.Errorf("payload.CandidateID = %q want 'h1'", p.CandidateID)
				}
				if p.Signal != "SIGKILL" {
					t.Errorf("payload.Signal = %q want 'SIGKILL'", p.Signal)
				}
				if p.GraceMs != 30 {
					t.Errorf("payload.GraceMs = %d want 30", p.GraceMs)
				}

				<-rcDone
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	<-rcDone
	t.Fatal("EvtMergeStragglerKilled not emitted within deadline")
}

func TestStragglerSupervisorExitsOnNormalCompletion(t *testing.T) {
	cr := &fakeCandidateRunner{
		delay: 10 * time.Millisecond,
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, TestPassCount: 10}
		},
	}
	em := &recordingEmitter{}
	r := makeRunner(t, cr, em)
	cands := []merge.MergeCandidate{{Branch: "feat-A", HeadSHA: "h1"}}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	if err != nil {
		t.Fatalf("RunCandidates: %v", err)
	}

	time.Sleep(120 * time.Millisecond)
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeStragglerKilled {
			t.Fatalf("EvtMergeStragglerKilled emitted on healthy candidate (supervisor mis-stopped)")
		}
	}
}

// TestRunCandidatesGenIDFallbackOnNilGenCtr exercises the runner's
// genID() helper when RunnerDeps.GenCtr is nil: the emitted all-fail
// event MUST carry GenerationID=0 (per BaselineRunner symmetry —
// "0 means unassigned" is detectable by inv-zen-107 compliance tests).
// Closes the doctrine-floor coverage gap on the genID-nil branch.
func TestRunCandidatesGenIDFallbackOnNilGenCtr(t *testing.T) {
	cr := &fakeCandidateRunner{
		outcome: func(c merge.MergeCandidate) merge.CandidateOutcome {
			return merge.CandidateOutcome{Candidate: c, HardRejected: true, Reason: "test"}
		},
	}
	em := &recordingEmitter{}

	r, ferr := merge.NewRunner(merge.RunnerDeps{
		Candidate: cr,
		Emitter:   em,
	}, merge.RunnerConfig{StragglerKillGracePeriod: 100 * time.Millisecond})
	if ferr != nil {
		t.Fatalf("NewRunner: %v", ferr)
	}
	cands := []merge.MergeCandidate{{Branch: "a", HeadSHA: "h1"}}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := r.RunCandidates(context.Background(), cands, "abc", nil, merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrAllCandidatesFailed) {
		t.Fatalf("err = %v want wraps ErrAllCandidatesFailed", err)
	}
	saw := false
	for _, e := range em.Snapshot() {
		if e.Type == merge.EvtMergeAllCandidatesFailed {
			saw = true
			if e.GenerationID != 0 {
				t.Errorf("EvtMergeAllCandidatesFailed.GenerationID = %d want 0 (nil GenCtr fallback)", e.GenerationID)
			}
		}
	}
	if !saw {
		t.Error("EvtMergeAllCandidatesFailed not emitted (single-cand all-fail path)")
	}
}
