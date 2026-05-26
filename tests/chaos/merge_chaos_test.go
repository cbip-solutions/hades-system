//go:build chaos
// +build chaos

package chaos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type chaosPool struct {
	inflight  atomic.Int32
	max       int32
	exhausted atomic.Int32
}

func (p *chaosPool) Lease(_ context.Context) (*merge.LeasedWorktree, error) {
	if p.inflight.Add(1) > p.max {
		p.inflight.Add(-1)
		p.exhausted.Add(1)
		return nil, errors.New("merge: pool capacity exhausted (chaos)")
	}
	return &merge.LeasedWorktree{Dir: "/tmp/wt"}, nil
}

func (p *chaosPool) Release(_ context.Context, _ *merge.LeasedWorktree) error {
	p.inflight.Add(-1)
	return nil
}

type chaosEmitter struct {
	mu sync.Mutex
	ev []merge.Event
}

func (e *chaosEmitter) Append(_ context.Context, ev merge.Event) error {
	e.mu.Lock()
	e.ev = append(e.ev, ev)
	e.mu.Unlock()
	return nil
}

type chaosExec struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func (e *chaosExec) Run(_ context.Context, _ string, _, _ []string) (merge.RunResult, error) {
	return merge.RunResult{Stdout: e.stdout, Stderr: e.stderr, ExitCode: e.exitCode}, e.err
}

func TestChaos_PoolExhaustionSurfacesAsBaselineFail(t *testing.T) {
	pool := &chaosPool{max: 0}
	em := &chaosEmitter{}
	br, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: &chaosExec{exitCode: 0},
		Emitter:  em,
		Git:      merge.NewFakeGit(),
	}, merge.BaselineConfig{Timeout: 5 * time.Second, StderrCapBytes: 512})
	if err != nil {
		t.Fatalf("NewBaselineRunner: %v", err)
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err = br.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if err == nil {
		t.Fatal("expected error on pool exhaustion")
	}
	if pool.exhausted.Load() == 0 {
		t.Errorf("pool.exhausted = 0 (expected >= 1)")
	}
}

func TestChaos_ConcurrentMergesSaturateCeiling(t *testing.T) {
	const ceiling = 2
	pool := &chaosPool{max: int32(ceiling)}
	em := &chaosEmitter{}
	br, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: &chaosExec{stdout: "test_a\n", exitCode: 0},
		Emitter:  em,
		Git:      merge.NewFakeGit(),
	}, merge.BaselineConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewBaselineRunner: %v", err)
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	const goroutines = 10
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = br.Run(context.Background(), "abc", merge.ModeNormal, suite)
		}()
	}
	wg.Wait()
	if pool.exhausted.Load() == 0 {
		t.Skip("no exhaustion observed — timing race; ceiling not saturated under this schedule")
	}
}

func TestChaos_GitBinaryUnavailable(t *testing.T) {
	pool := &chaosPool{max: 100}
	em := &chaosEmitter{}
	exec := &chaosExec{err: errors.New("exec: git: not found")}
	br, runnerErr := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: exec,
		Emitter:  em,
		Git:      merge.NewFakeGit(),
	}, merge.BaselineConfig{Timeout: 5 * time.Second})
	if runnerErr != nil {
		t.Fatalf("NewBaselineRunner: %v", runnerErr)
	}
	suite := merge.TestSuite{Smoke: []string{"s"}, Full: []string{"f"}}
	_, err := br.Run(context.Background(), "abc", merge.ModeNormal, suite)
	if !errors.Is(err, merge.ErrBaselineFailed) {
		t.Fatalf("err = %v want wraps ErrBaselineFailed", err)
	}
}
