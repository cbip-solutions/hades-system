// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/candidate_runner.go
package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type concreteCandidate struct {
	deps CandidateDeps
	cfg  CandidateConfig
}

func NewCandidateRunner(deps CandidateDeps, cfg CandidateConfig) (CandidateRunner, error) {
	if deps.Pool == nil {
		return nil, fmt.Errorf("merge.NewCandidateRunner: Pool nil")
	}
	if deps.Executor == nil {
		return nil, fmt.Errorf("merge.NewCandidateRunner: Executor nil")
	}
	if deps.Emitter == nil {
		return nil, fmt.Errorf("merge.NewCandidateRunner: Emitter nil")
	}
	if deps.Git == nil {
		return nil, fmt.Errorf("merge.NewCandidateRunner: Git nil")
	}
	if cfg.StderrCapBytes < 0 {
		return nil, fmt.Errorf("merge.NewCandidateRunner: StderrCapBytes negative")
	}
	return &concreteCandidate{deps: deps, cfg: cfg}, nil
}

func (cc *concreteCandidate) Run(ctx context.Context, cand MergeCandidate, baseSHA string, baseline PassingSet, mode Mode, suite TestSuite) (CandidateOutcome, error) {
	out := CandidateOutcome{
		Candidate:      cand,
		PatchSizeLines: PatchSizeLines(cand.Patch),
	}
	if err := ctx.Err(); err != nil {
		return out, fmt.Errorf("merge.Candidate.Run: ctx cancelled before lease: %w", err)
	}

	wt, err := cc.deps.Pool.Lease(ctx)
	if err != nil {
		return out, fmt.Errorf("merge.Candidate.Run: pool.Lease: %w", err)
	}
	defer func() {

		_ = cc.deps.Pool.Release(context.Background(), wt)
	}()

	startTime := time.Now()
	gen := cc.genID()

	if _, _, gerr := cc.deps.Git.Run(ctx, wt.Dir, "", "checkout", baseSHA); gerr != nil {
		cc.emitFailed(ctx, gen, cand, CandidateFailureGitTransient, "git checkout: "+gerr.Error(), -1, "")
		out.HardRejected = true
		out.Reason = "git_checkout: " + gerr.Error()
		out.Duration = time.Since(startTime)
		return out, nil
	}

	if aerr := ApplyPatch(ctx, cc.deps.Git, wt.Dir, cand.Patch, cc.cfg.StderrCapBytes); aerr != nil {
		cc.emitFailed(ctx, gen, cand, CandidateFailurePatchRejected, aerr.Error(), -1, "")
		out.HardRejected = true
		out.Reason = "patch_rejected: " + aerr.Error()
		out.Duration = time.Since(startTime)
		return out, nil
	}

	startPayload, _ := json.Marshal(CandidateStartedPayload{
		CandidateID:    cand.HeadSHA,
		Branch:         cand.Branch,
		Mode:           mode.String(),
		PatchSizeBytes: len(cand.Patch),
	})
	_ = cc.deps.Emitter.Append(ctx, Event{
		Type:         EvtCandidateStarted,
		GenerationID: gen,
		Payload:      startPayload,
		Timestamp:    time.Now(),
	})

	runCtx := ctx
	var cancelFn context.CancelFunc
	if cc.cfg.Timeout > 0 {
		runCtx, cancelFn = context.WithTimeout(ctx, cc.cfg.Timeout)
		defer cancelFn()
	}

	smokeRes, smokeErr := cc.deps.Executor.Run(runCtx, wt.Dir, suite.Smoke, nil)
	if smokeErr != nil {
		cc.emitFailed(ctx, gen, cand, classifyExecError(smokeErr, smokeRes.ExitCode), "smoke: "+smokeErr.Error(), smokeRes.ExitCode, cc.trunc(smokeRes.Stderr))
		out.HardRejected = true
		out.Reason = "smoke_executor_error: " + smokeErr.Error()
		out.Stderr = cc.trunc(smokeRes.Stderr)
		out.Duration = time.Since(startTime)
		return out, nil
	}
	if smokeRes.ExitCode != 0 {

		cc.emitFailed(ctx, gen, cand, CandidateFailureBaselineBreaker, "smoke_failed", smokeRes.ExitCode, cc.trunc(smokeRes.Stderr))
		out.HardRejected = true
		out.Reason = "smoke_failed"
		out.TestFailCount = 1
		out.Stderr = cc.trunc(smokeRes.Stderr)
		out.Duration = time.Since(startTime)
		return out, nil
	}

	tier := ModeFor(mode).TestTier
	var fullRes RunResult
	var fullErr error
	if tier == TestTierFull {
		fullRes, fullErr = cc.deps.Executor.Run(runCtx, wt.Dir, suite.Full, nil)
		if fullErr != nil {
			cc.emitFailed(ctx, gen, cand, classifyExecError(fullErr, fullRes.ExitCode), "full: "+fullErr.Error(), fullRes.ExitCode, cc.trunc(fullRes.Stderr))
			out.HardRejected = true
			out.Reason = "full_executor_error: " + fullErr.Error()
			out.Stderr = cc.trunc(fullRes.Stderr)
			out.Duration = time.Since(startTime)
			return out, nil
		}
	} else {
		fullRes = smokeRes
	}

	stdout := smokeRes.Stdout
	if tier == TestTierFull {
		stdout = fullRes.Stdout
	}
	candPassing := PassingSet(parseTestIDs(stdout))
	out.PassingSet = candPassing
	out.TestPassCount = len(candPassing)

	if baseline.HasAnyMissing(candPassing) {
		out.HardRejected = true
		out.Reason = "baseline_breaker"
	}
	if fullRes.ExitCode != 0 && tier == TestTierFull {

		missing := 0
		for _, id := range baseline {
			if !candPassing.Has(id) {
				missing++
			}
		}
		out.TestFailCount = missing
	}

	out.Duration = time.Since(startTime)

	cc.applyFlakeRerun(ctx, &out, runCtx, wt.Dir, cand, baseline, suite, gen, ModeFor(mode))

	completePayload, _ := json.Marshal(CandidateCompletePayload{
		CandidateID:    cand.HeadSHA,
		TestPassCount:  out.TestPassCount,
		TestFailCount:  out.TestFailCount,
		FlakeCount:     out.FlakeCount,
		HardRejected:   out.HardRejected,
		PatchSizeLines: out.PatchSizeLines,
		PassingSetHash: candPassing.Hash(),
		DurationMs:     out.Duration.Milliseconds(),
	})
	_ = cc.deps.Emitter.Append(ctx, Event{
		Type:         EvtCandidateComplete,
		GenerationID: gen,
		Payload:      completePayload,
		Timestamp:    time.Now(),
	})

	out.Stderr = cc.trunc(fullRes.Stderr)
	return out, nil
}

func (cc *concreteCandidate) emitFailed(ctx context.Context, gen int64, cand MergeCandidate, ftype CandidateFailureType, reason string, exitCode int, stderr string) {
	payload, _ := json.Marshal(CandidateFailedPayload{
		CandidateID: cand.HeadSHA,
		FailureType: ftype.String(),
		Reason:      reason,
		ExitCode:    exitCode,
		Stderr:      stderr,
	})
	_ = cc.deps.Emitter.Append(ctx, Event{
		Type:         EvtCandidateFailed,
		GenerationID: gen,
		Payload:      payload,
		Timestamp:    time.Now(),
	})
}

func (cc *concreteCandidate) trunc(s string) string {
	limit := cc.cfg.StderrCapBytes
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}

func (cc *concreteCandidate) genID() int64 {
	if cc.deps.GenCtr == nil {
		return 0
	}
	return cc.deps.GenCtr.Current()
}

func (cc *concreteCandidate) applyFlakeRerun(ctx context.Context, out *CandidateOutcome, runCtx context.Context, worktreeDir string, cand MergeCandidate, baseline PassingSet, suite TestSuite, gen int64, cfg ModeConfig) {
	if cfg.FlakeRerunBudget <= 0 {
		return
	}
	if out.TestFailCount == 0 {
		return
	}

	failed := make([]string, 0, len(baseline))
	for _, id := range baseline {
		if !out.PassingSet.Has(id) {
			failed = append(failed, id)
		}
	}
	if len(failed) == 0 {
		return
	}

	rerunCmd := suite.Full
	if cfg.TestTier != TestTierFull {
		rerunCmd = suite.Smoke
	}

	for retry := 1; retry <= cfg.FlakeRerunBudget && len(failed) > 0; retry++ {

		for _, tid := range failed {
			payload, _ := json.Marshal(FlakeRerunStartedPayload{
				CandidateID: cand.HeadSHA,
				RetryN:      retry,
				TestID:      tid,
			})
			_ = cc.deps.Emitter.Append(ctx, Event{
				Type:         EvtFlakeRerunStarted,
				GenerationID: gen,
				Payload:      payload,
				Timestamp:    time.Now(),
			})
		}

		env := []string{"HADES_MERGE_RERUN_TESTS=" + strings.Join(failed, ",")}
		res, err := cc.deps.Executor.Run(runCtx, worktreeDir, rerunCmd, env)
		if err != nil {

			return
		}

		newPass := PassingSet(parseTestIDs(res.Stdout))
		stillFailing := make([]string, 0, len(failed))
		for _, tid := range failed {
			if newPass.Has(tid) {
				out.FlakeCount++
				out.PassingSet = append(out.PassingSet, tid)
			} else {
				stillFailing = append(stillFailing, tid)
			}
		}
		failed = stillFailing
	}

	out.TestPassCount = len(out.PassingSet)
	out.TestFailCount = len(failed)

	if baseline.HasAnyMissing(out.PassingSet) {
		out.HardRejected = true

		if out.Reason == "" || strings.HasPrefix(out.Reason, "smoke_failed") {
			out.Reason = "baseline_breaker_post_flake"
		}
	} else {

		if out.Reason == "baseline_breaker" {
			out.HardRejected = false
			out.Reason = ""
		}
	}
}

func classifyExecError(err error, exitCode int) CandidateFailureType {
	if err == nil {
		return CandidateFailureUnknown
	}
	if errIsTimeout(err) {
		return CandidateFailureTimeout
	}
	if exitCode < 0 {
		return CandidateFailureCrash
	}
	return CandidateFailurePanic
}

func errIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	type timeoutI interface{ Timeout() bool }
	if t, ok := err.(timeoutI); ok && t.Timeout() {
		return true
	}
	return false
}

var _ CandidateRunner = (*concreteCandidate)(nil)
