// internal/orchestrator/merge/candidate_flake_internal_test.go
//
// White-box (package merge) tests for applyFlakeRerun branches that are
// unreachable via the public Run() API given the current Mode taxonomy +
// Run pipeline shape. These cover defensive / future-proof branches that
// guard against:
//
//   - len(failed) == 0 with TestFailCount > 0 (invariant violation upstream)
//   - smoke-tier mode reaching the rerun loop (cfg with budget>0 +
//     TestTier=Smoke, which the canonical taxonomy doesn't pair but the
//     impl supports cleanly for future Mode extensions)
//   - "baseline_breaker_post_flake" reason assignment when prior reason
//     was empty or "smoke_failed*" AND post-flake still misses baseline
//
// These branches MUST stay covered for the inv-zen-106 compliance pass.
// White-box construction: build a concreteCandidate directly and invoke
// applyFlakeRerun with a constructed ModeConfig — applyFlakeRerun accepts
// cfg as a parameter precisely so internal tests can drive any cfg shape
// (see candidate_runner.go applyFlakeRerun docstring).
package merge

import (
	"context"
	"testing"
)

type flakeFakeExecutor struct {
	scripts []RunResult
	errs    []error
	calls   int
	lastCmd []string
}

func (f *flakeFakeExecutor) Run(_ context.Context, _ string, cmd []string, _ []string) (RunResult, error) {
	f.calls++
	f.lastCmd = cmd
	if len(f.scripts) == 0 {
		return RunResult{}, nil
	}
	r := f.scripts[0]
	f.scripts = f.scripts[1:]
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return r, err
}

type flakeFakeEmitter struct {
	events []Event
}

func (f *flakeFakeEmitter) Append(_ context.Context, e Event) error {
	f.events = append(f.events, e)
	return nil
}

func makeFlakeCC(exec TestExecutor, em EventEmitter) *concreteCandidate {
	return &concreteCandidate{
		deps: CandidateDeps{Executor: exec, Emitter: em},
		cfg:  CandidateConfig{StderrCapBytes: 512},
	}
}

func TestApplyFlakeRerunBudgetZeroEarlyReturn(t *testing.T) {
	exec := &flakeFakeExecutor{}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 1,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 0}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if exec.calls != 0 {
		t.Errorf("executor.calls = %d want 0 (budget=0 short-circuit)", exec.calls)
	}
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d want 0 (no rerun on budget=0)", out.FlakeCount)
	}
}

func TestApplyFlakeRerunTestFailCountZeroEarlyReturn(t *testing.T) {
	exec := &flakeFakeExecutor{}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 0,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a"}
	suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 2}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if exec.calls != 0 {
		t.Errorf("executor.calls = %d want 0 (TestFailCount=0 short-circuit)", exec.calls)
	}
}

func TestApplyFlakeRerunFailedListEmptyEarlyReturn(t *testing.T) {
	exec := &flakeFakeExecutor{}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a", "test_b"},
		TestFailCount: 1,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 2}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if exec.calls != 0 {
		t.Errorf("executor.calls = %d want 0 (empty failed-set defensive return)", exec.calls)
	}
	if out.FlakeCount != 0 {
		t.Errorf("FlakeCount = %d want 0", out.FlakeCount)
	}

}

func TestApplyFlakeRerunSmokeTierUsesSmokeCmd(t *testing.T) {
	exec := &flakeFakeExecutor{
		scripts: []RunResult{
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 1,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke-cmd"}, Full: []string{"full-cmd"}}

	cfg := ModeConfig{TestTier: TestTierSmoke, FlakeRerunBudget: 1}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if exec.calls != 1 {
		t.Errorf("executor.calls = %d want 1 (one rerun)", exec.calls)
	}
	if len(exec.lastCmd) != 1 || exec.lastCmd[0] != "smoke-cmd" {
		t.Errorf("lastCmd = %v want [smoke-cmd] (smoke-tier rerunCmd)", exec.lastCmd)
	}
	if out.FlakeCount != 1 {
		t.Errorf("FlakeCount = %d want 1 (test_b recovered)", out.FlakeCount)
	}
}

func TestApplyFlakeRerunSmokeFailFastTierUsesSmokeCmd(t *testing.T) {
	exec := &flakeFakeExecutor{
		scripts: []RunResult{
			{Stdout: "", ExitCode: 1},
		},
	}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 1,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke-cmd"}, Full: []string{"full-cmd"}}
	cfg := ModeConfig{TestTier: TestTierSmokeFailFast, FlakeRerunBudget: 1}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if len(exec.lastCmd) != 1 || exec.lastCmd[0] != "smoke-cmd" {
		t.Errorf("lastCmd = %v want [smoke-cmd] (smokeFailFast-tier rerunCmd)", exec.lastCmd)
	}
}

func TestApplyFlakeRerunPostFlakeReasonBaselineBreakerPostFlake(t *testing.T) {
	cases := []struct {
		name        string
		priorReason string
	}{
		{"empty_reason", ""},
		{"smoke_failed_prefix", "smoke_failed: exit=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &flakeFakeExecutor{
				scripts: []RunResult{
					{Stdout: "", ExitCode: 1},
					{Stdout: "", ExitCode: 1},
				},
			}
			em := &flakeFakeEmitter{}
			cc := makeFlakeCC(exec, em)

			out := &CandidateOutcome{
				PassingSet:    PassingSet{"test_a"},
				TestFailCount: 1,
				Reason:        tc.priorReason,
			}
			cand := MergeCandidate{HeadSHA: "h1"}
			baseline := PassingSet{"test_a", "test_b"}
			suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
			cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 2}

			cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

			if !out.HardRejected {
				t.Error("HardRejected = false; want true (test_b still missing post-flake)")
			}
			if out.Reason != "baseline_breaker_post_flake" {
				t.Errorf("Reason = %q want baseline_breaker_post_flake", out.Reason)
			}
		})
	}
}

func TestApplyFlakeRerunPostFlakeReasonPreservedWhenAlreadyBaselineBreaker(t *testing.T) {
	exec := &flakeFakeExecutor{
		scripts: []RunResult{
			{Stdout: "", ExitCode: 1},
			{Stdout: "", ExitCode: 1},
		},
	}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 1,
		Reason:        "baseline_breaker",
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 2}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if !out.HardRejected {
		t.Error("HardRejected = false; want true")
	}
	if out.Reason != "baseline_breaker" {
		t.Errorf("Reason = %q want baseline_breaker (prefix branch must NOT fire)", out.Reason)
	}
}

func TestApplyFlakeRerunPostFlakeNoReasonResetWhenNotBaselineBreaker(t *testing.T) {
	exec := &flakeFakeExecutor{
		scripts: []RunResult{
			{Stdout: "test_b\n", ExitCode: 0},
		},
	}
	em := &flakeFakeEmitter{}
	cc := makeFlakeCC(exec, em)

	out := &CandidateOutcome{
		PassingSet:    PassingSet{"test_a"},
		TestFailCount: 1,
		Reason:        "some_other_reason",
		HardRejected:  true,
	}
	cand := MergeCandidate{HeadSHA: "h1"}
	baseline := PassingSet{"test_a", "test_b"}
	suite := TestSuite{Smoke: []string{"smoke"}, Full: []string{"full"}}
	cfg := ModeConfig{TestTier: TestTierFull, FlakeRerunBudget: 2}

	cc.applyFlakeRerun(context.Background(), out, context.Background(), "/wt", cand, baseline, suite, 0, cfg)

	if out.Reason != "some_other_reason" {
		t.Errorf("Reason = %q want some_other_reason (no reset for non-baseline_breaker reasons)", out.Reason)
	}

	if !out.HardRejected {
		t.Error("HardRejected = false; expected unchanged true")
	}
}
