// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/baseline.go
package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type LeasedWorktree struct {
	Dir string
}

type WorktreePool interface {
	Lease(ctx context.Context) (*LeasedWorktree, error)
	Release(ctx context.Context, w *LeasedWorktree) error
}

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type TestExecutor interface {
	Run(ctx context.Context, workingDir string, cmd []string, env []string) (RunResult, error)
}

type BaselineRunner interface {
	Run(ctx context.Context, baseSHA string, mode Mode, suite TestSuite) (PassingSet, error)
}

type BaselineDeps struct {
	Pool     WorktreePool
	Executor TestExecutor
	Emitter  EventEmitter
	Git      GitClient
	GenCtr   *GenerationCounter
}

type BaselineConfig struct {
	Timeout        time.Duration
	StderrCapBytes int
}

type concreteBaseline struct {
	deps BaselineDeps
	cfg  BaselineConfig
}

func NewBaselineRunner(deps BaselineDeps, cfg BaselineConfig) (BaselineRunner, error) {
	if deps.Pool == nil || deps.Executor == nil || deps.Emitter == nil || deps.Git == nil {
		return nil, fmt.Errorf("merge.NewBaselineRunner: missing required dep (Pool/Executor/Emitter/Git)")
	}
	if cfg.StderrCapBytes < 0 {
		return nil, fmt.Errorf("merge.NewBaselineRunner: StderrCapBytes negative")
	}
	return &concreteBaseline{deps: deps, cfg: cfg}, nil
}

type BaselineStartedPayload struct {
	BaseSHA string `json:"base_sha"`
	Mode    string `json:"mode"`
	Tier    string `json:"tier"`
}

type BaselineCompletePayload struct {
	BaseSHA        string `json:"base_sha"`
	PassingSetHash string `json:"passing_set_hash"`
	TestCount      int    `json:"test_count"`
	DurationMs     int64  `json:"duration_ms"`
}

type BaselineFailedPayload struct {
	BaseSHA  string `json:"base_sha"`
	Reason   string `json:"reason"`
	ExitCode int    `json:"exit_code"`
	Stderr   string `json:"stderr"`
}

// Run executes the baseline test suite for baseSHA under the selected
// mode/suite. Releases the leased worktree via defer (panic-safe). Emits
// EvtBaselineStarted before test invocation; EvtBaselineComplete on success;
// EvtBaselineFailed on any error path.
//
// Mode → suite selection:
//
//	ModeNormal, ModeDegraded60       → suite.Full
//	ModeDegraded80, ModeEmergencyOnly → suite.Smoke
//
// SmokeFailFast tier (EmergencyOnly mode) sets the env var
// ZEN_MERGE_FAIL_FAST=1 so test runners can opt into fail-fast.
//
// Returns wrapped ErrBaselineFailed on:
//   - Non-zero exit code
//   - TestExecutor returns an error
//   - Test stdout malformed / unparseable
//   - Context cancellation mid-run
//
// inv-zen-106 atomicity: callers (Phase D engine.go) MUST NOT proceed to
// candidate execution after this method returns a wrapped ErrBaselineFailed;
// the engine state-machine guard surfaces the constraint as a runtime panic
// in development builds.
func (b *concreteBaseline) Run(ctx context.Context, baseSHA string, mode Mode, suite TestSuite) (PassingSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: ctx cancelled before lease: %v", ErrBaselineFailed, err)
	}
	cfg := ModeFor(mode)

	wt, err := b.deps.Pool.Lease(ctx)
	if err != nil {
		return nil, fmt.Errorf("merge.Baseline.Run: pool.Lease: %w", err)
	}
	defer func() {
		_ = b.deps.Pool.Release(context.Background(), wt)
	}()

	if _, _, gerr := b.deps.Git.Run(ctx, wt.Dir, "", "checkout", baseSHA); gerr != nil {
		b.emitFailed(ctx, baseSHA, "git_checkout_failed", -1, gerr.Error())
		return nil, fmt.Errorf("%w: git checkout %s: %v", ErrBaselineFailed, baseSHA, gerr)
	}

	cmd, env := b.selectCommand(cfg.TestTier, suite)
	if len(cmd) == 0 {
		b.emitFailed(ctx, baseSHA, "empty_command", -1, "")
		return nil, fmt.Errorf("%w: empty test command for tier %v", ErrBaselineFailed, cfg.TestTier)
	}

	startPayload, _ := json.Marshal(BaselineStartedPayload{
		BaseSHA: baseSHA,
		Mode:    mode.String(),
		Tier:    cfg.TestTier.String(),
	})
	gen := b.genID()
	_ = b.deps.Emitter.Append(ctx, Event{
		Type:         EvtBaselineStarted,
		GenerationID: gen,
		RequestHash:  "",
		Payload:      startPayload,
		Timestamp:    time.Now(),
	})

	runCtx := ctx
	var cancel context.CancelFunc
	if b.cfg.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, b.cfg.Timeout)
		defer cancel()
	}

	start := time.Now()
	res, runErr := b.deps.Executor.Run(runCtx, wt.Dir, cmd, env)
	durationMs := time.Since(start).Milliseconds()

	if runErr != nil {
		b.emitFailed(ctx, baseSHA, "executor_error: "+runErr.Error(), res.ExitCode, b.truncStderr(res.Stderr))
		return nil, fmt.Errorf("%w: executor error: %v", ErrBaselineFailed, runErr)
	}
	if res.ExitCode != 0 {
		b.emitFailed(ctx, baseSHA, "non_zero_exit", res.ExitCode, b.truncStderr(res.Stderr))
		return nil, fmt.Errorf("%w: exit code %d", ErrBaselineFailed, res.ExitCode)
	}

	ids := parseTestIDs(res.Stdout)
	pset := PassingSet(ids)

	completePayload, _ := json.Marshal(BaselineCompletePayload{
		BaseSHA:        baseSHA,
		PassingSetHash: pset.Hash(),
		TestCount:      len(ids),
		DurationMs:     durationMs,
	})
	_ = b.deps.Emitter.Append(ctx, Event{
		Type:         EvtBaselineComplete,
		GenerationID: gen,
		Payload:      completePayload,
		Timestamp:    time.Now(),
	})

	return pset, nil
}

func (b *concreteBaseline) selectCommand(tier TestTier, suite TestSuite) (cmd []string, env []string) {
	switch tier {
	case TestTierFull:
		return suite.Full, nil
	case TestTierSmoke:
		return suite.Smoke, nil
	case TestTierSmokeFailFast:
		return suite.Smoke, []string{"ZEN_MERGE_FAIL_FAST=1"}
	default:
		return nil, nil
	}
}

func (b *concreteBaseline) emitFailed(ctx context.Context, baseSHA, reason string, exitCode int, stderr string) {
	payload, _ := json.Marshal(BaselineFailedPayload{
		BaseSHA:  baseSHA,
		Reason:   reason,
		ExitCode: exitCode,
		Stderr:   b.truncStderr(stderr),
	})
	_ = b.deps.Emitter.Append(ctx, Event{
		Type:         EvtBaselineFailed,
		GenerationID: b.genID(),
		Payload:      payload,
		Timestamp:    time.Now(),
	})
}

func (b *concreteBaseline) truncStderr(s string) string {
	limit := b.cfg.StderrCapBytes
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit]
}

func (b *concreteBaseline) genID() int64 {
	if b.deps.GenCtr == nil {
		return 0
	}
	return b.deps.GenCtr.Current()
}

func parseTestIDs(stdout string) []string {
	lines := strings.Split(stdout, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line[0] {
		case '-', '=', '#', '+':
			continue
		}
		out = append(out, line)
	}
	return out
}

var _ BaselineRunner = (*concreteBaseline)(nil)
