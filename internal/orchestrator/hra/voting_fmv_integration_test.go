// go:build integration
package hra_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

// realApply touches a sentinel file inside the worktree at apply time
// so we can verify the FMV pipeline reached the apply step in a real
// worktree (and verify worktreepool.Release's git-reset + git-clean
// wipes the sentinel before returning the worktree to warm). The
// implementation deliberately does NOT use git plumbing — we want a
// concrete filesystem mutation that worktreepool.Release MUST clean
// up via `git clean -fdx` (since the file is untracked).
type realApply struct {
	calls atomic.Int32
}

func (r *realApply) ApplyFix(_ context.Context, dir string, c hra.FixProposal) error {
	r.calls.Add(1)

	path := filepath.Join(dir, "fmv-applied-"+c.ID+".tmp")
	return os.WriteFile(path, []byte("fmv integration sentinel"), 0o600)
}

type scriptedTestRunner struct {
	pass []int
	fail []int
	idx  atomic.Int32
}

func (r *scriptedTestRunner) Run(_ context.Context, _ string) (int, int, error) {
	i := r.idx.Add(1) - 1
	return r.pass[i], r.fail[i], nil
}

type integrationAppender struct {
	log *eventlog.Log
}

func (a *integrationAppender) Append(ctx context.Context, ev eventlog.Event) (int64, error) {
	return a.log.Append(ctx, ev)
}

func initRepo(t *testing.T) (repo string) {
	t.Helper()
	repo = t.TempDir()
	exec := worktreepool.NewOSExecutor()
	mustRun := func(args ...string) {
		t.Helper()
		if _, err := exec.Run(context.Background(), args[0], args[1:]...); err != nil {
			t.Fatalf("integration init: %v failed: %v", args, err)
		}
	}
	mustRun("git", "init", "-b", "main", repo)
	mustRun("git", "-C", repo, "config", "user.email", "ci@zen-swarm")
	mustRun("git", "-C", repo, "config", "user.name", "ci")
	mustRun("git", "-C", repo, "commit", "--allow-empty", "-m", "init")
	return repo
}

func TestFMV_Integration_RealWorktreePool(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	repo := initRepo(t)
	wtDir := t.TempDir()

	cfg := worktreepool.PoolConfig{
		RepoRoot:    repo,
		WorktreeDir: wtDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "fmv-int",
		Clock:       clock.Real{},
	}
	log := eventlog.NewMemory(clock.Real{})
	pool, err := worktreepool.NewPool(cfg, log, worktreepool.NewOSExecutor())
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := pool.Close(ctx); err != nil {
			t.Errorf("pool.Close: %v", err)
		}
	}()

	apply := &realApply{}
	runner := &scriptedTestRunner{pass: []int{10, 5}, fail: []int{0, 5}}
	fmv := hra.NewFMV(hra.FMVDeps{
		Pool:       hra.AdaptPool(pool),
		Apply:      apply,
		TestRunner: runner,
		EventLog:   &integrationAppender{log: log},
		Clock:      clock.Real{},
		SessionID:  "sess-int",
		ProjectID:  "proj-int",
	})

	candidates := []hra.FixProposal{
		{ID: "alpha", SupportingReviewers: 2},
		{ID: "beta", SupportingReviewers: 1},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := fmv.Run(ctx, candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v", err)
	}
	if res.Winner.ID != "alpha" {
		t.Fatalf("winner = %q, want alpha", res.Winner.ID)
	}
	if got, want := apply.calls.Load(), int32(len(candidates)); got != want {
		t.Fatalf("apply.calls = %d, want %d", got, want)
	}

	for _, row := range res.Trace {
		if row.ApplyErr != nil {
			t.Errorf("row %s: ApplyErr = %v, want nil", row.Candidate.ID, row.ApplyErr)
		}
		if row.RunErr != nil {
			t.Errorf("row %s: RunErr = %v (release should succeed against real pool)", row.Candidate.ID, row.RunErr)
		}
	}

	report, err := pool.PruneOrphans(ctx)
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}
	if report.GitPruned != 0 || report.AdminOnlyCleared != 0 || report.FilesystemSwept != 0 {
		t.Fatalf("PruneOrphans report shows leaks: %+v", report)
	}
}

// TestFMV_Integration_AdapterPropagatesNonExhaustedErrors confirms
// that a non-ErrPoolExhausted lease error from worktreepool (here:
// ErrPoolClosed after Close) propagates through AdaptPool unchanged
// — only ErrPoolExhausted is mapped to the FMV-package sentinel; all
// other lease errors (substrate diagnostics) MUST round-trip
// untouched so callers can errors.Is against the original
// worktreepool sentinel.
func TestFMV_Integration_AdapterPropagatesNonExhaustedErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	repo := initRepo(t)
	wtDir := t.TempDir()

	cfg := worktreepool.PoolConfig{
		RepoRoot:    repo,
		WorktreeDir: wtDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "fmv-int-closed",
		Clock:       clock.Real{},
	}
	log := eventlog.NewMemory(clock.Real{})
	pool, err := worktreepool.NewPool(cfg, log, worktreepool.NewOSExecutor())
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	if err := pool.Close(closeCtx); err != nil {
		t.Fatalf("pool.Close: %v", err)
	}

	adapter := hra.AdaptPool(pool)
	if adapter == nil {
		t.Fatal("AdaptPool returned nil for non-nil pool")
	}
	_, err = adapter.Lease(context.Background())
	if err == nil {
		t.Fatal("Lease against closed pool: err = nil, want non-nil")
	}
	if !errors.Is(err, worktreepool.ErrPoolClosed) {
		t.Fatalf("err = %v, want errors.Is(worktreepool.ErrPoolClosed)=true", err)
	}
	if errors.Is(err, hra.ErrPoolExhausted) {
		t.Fatalf("err = %v, must NOT match hra.ErrPoolExhausted (substrate error, not exhaustion)", err)
	}
}

func TestFMV_Integration_PoolExhaustedSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	repo := initRepo(t)
	wtDir := t.TempDir()

	cfg := worktreepool.PoolConfig{
		RepoRoot:    repo,
		WorktreeDir: wtDir,
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
		PoolID:      "fmv-int-exh",
		Clock:       clock.Real{},
	}
	log := eventlog.NewMemory(clock.Real{})
	pool, err := worktreepool.NewPool(cfg, log, worktreepool.NewOSExecutor())
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := pool.Close(ctx); err != nil {
			t.Errorf("pool.Close: %v", err)
		}
	}()

	heldCtx, heldCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer heldCancel()
	held, err := pool.Lease(heldCtx)
	if err != nil {
		t.Fatalf("pre-lease: %v", err)
	}
	defer func() {
		_ = pool.Release(context.Background(), held)
	}()

	apply := &realApply{}
	runner := &scriptedTestRunner{pass: []int{10}, fail: []int{0}}
	fmv := hra.NewFMV(hra.FMVDeps{
		Pool:       hra.AdaptPool(pool),
		Apply:      apply,
		TestRunner: runner,
		EventLog:   &integrationAppender{log: log},
		Clock:      clock.Real{},
		SessionID:  "sess-int-exh",
		ProjectID:  "proj-int-exh",
	})
	candidates := []hra.FixProposal{
		{ID: "alpha", SupportingReviewers: 1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	res, err := fmv.Run(ctx, candidates, hra.FMVOptions{})
	if err != nil {
		t.Fatalf("FMV.Run: %v (I-5: pool exhaustion degrades, does not error)", err)
	}
	if !res.Degraded {
		t.Fatal("res.Degraded = false, want true")
	}
	if res.Reason != "pool_exhausted" {
		t.Fatalf("res.Reason = %q, want pool_exhausted", res.Reason)
	}

	if res.Winner.ID != "alpha" {
		t.Fatalf("winner = %q, want alpha", res.Winner.ID)
	}
	if len(res.Trace) != 0 {
		t.Fatalf("trace len = %d, want 0 (lease never succeeded)", len(res.Trace))
	}
}
