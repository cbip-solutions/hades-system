package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestBootstrapWiresMergeEngine(t *testing.T) {
	t.Skip("integration test; runs against real daemon — see tests/integration/bootstrap_merge_integration_test.go (Plan 6 F-4 follow-on)")
}

type bootstrapTestPool struct {
	mu  sync.Mutex
	dir string
}

func (p *bootstrapTestPool) Lease(_ context.Context) (*merge.LeasedWorktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &merge.LeasedWorktree{Dir: p.dir}, nil
}

func (p *bootstrapTestPool) Release(_ context.Context, _ *merge.LeasedWorktree) error {
	return nil
}

type bootstrapTestEmitter struct{}

func (bootstrapTestEmitter) Append(_ context.Context, _ merge.Event) error { return nil }

type bootstrapTestExecutor struct{}

func (bootstrapTestExecutor) Run(_ context.Context, _ string, _ []string, _ []string) (merge.RunResult, error) {
	return merge.RunResult{}, nil
}

type bootstrapTestClock struct{}

func (bootstrapTestClock) Now() time.Time { return time.Unix(0, 0) }

func TestBootstrapMergeEngineFactoryBuilds(t *testing.T) {
	cfg := MergeBootstrapConfig{
		EngineVersion:     "v0.6.0-test",
		PoolCapacity:      12,
		ScoringConfig:     merge.ScoringConfig{AlphaReviewerWeight: 1.0, GammaFlakePenalty: 2.0},
		AnomalyThresholds: merge.DefaultAnomalyThresholds(),
		BaselineConfig:    merge.BaselineConfig{Timeout: 30 * time.Second, StderrCapBytes: 4096},
		CandidateConfig:   merge.CandidateConfig{Timeout: 60 * time.Second, StderrCapBytes: 4096},
		RunnerConfig:      merge.RunnerConfig{StragglerKillGracePeriod: 30 * time.Second},
	}

	pool := &bootstrapTestPool{dir: t.TempDir()}
	emitter := bootstrapTestEmitter{}
	clk := bootstrapTestClock{}
	gitClient := merge.NewFakeGit()
	executor := bootstrapTestExecutor{}

	engine, err := NewMergeEngineFromConfig(cfg, pool, emitter, clk, gitClient, executor)
	if err != nil {
		t.Fatalf("NewMergeEngineFromConfig: unexpected error: %v", err)
	}
	if engine == nil {
		t.Fatal("NewMergeEngineFromConfig returned nil engine without an error")
	}
}

func TestBootstrapMergeEngineFactoryRejectsNilDeps(t *testing.T) {
	cfg := MergeBootstrapConfig{
		EngineVersion:     "v0.6.0-test",
		PoolCapacity:      12,
		ScoringConfig:     merge.ScoringConfig{AlphaReviewerWeight: 1.0, GammaFlakePenalty: 2.0},
		AnomalyThresholds: merge.DefaultAnomalyThresholds(),
	}

	pool := &bootstrapTestPool{dir: t.TempDir()}
	emitter := bootstrapTestEmitter{}
	clk := bootstrapTestClock{}
	executor := bootstrapTestExecutor{}

	engine, err := NewMergeEngineFromConfig(cfg, pool, emitter, clk, nil, executor)
	if err == nil {
		t.Fatal("expected non-nil error when Git is nil")
	}
	if engine != nil {
		t.Errorf("expected nil engine on validation error, got %v", engine)
	}
	if !strings.Contains(err.Error(), "merge bootstrap: baseline") {
		t.Errorf("error must be wrapped with subsystem context, got: %q", err.Error())
	}
}
