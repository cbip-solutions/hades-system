package worktreepool_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func TestPrewarm_ReachesFloorAtStartup(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       3,
		ElasticMax:  6,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if worktreepool.WarmCountForTest(p) >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("prewarm did not reach floor=3 within 2s; final warm count=%d",
		worktreepool.WarmCountForTest(p))
}

func TestPrewarm_RespectsCtxCancel(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       2,
		ElasticMax:  4,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

}

func TestPrewarm_DrainsWarmOnClose(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       3,
		ElasticMax:  6,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	p, err := worktreepool.NewPool(cfg, &fakeEmitter{}, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && worktreepool.WarmCountForTest(p) < 3 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := worktreepool.WarmCountForTest(p); got < 3 {
		t.Fatalf("prewarm did not reach floor before Close; warm=%d", got)
	}

	if err := p.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	calls := exec.callsSnapshot()
	removes := 0
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "worktree remove") {
			removes++
		}
	}
	if removes < 3 {
		t.Fatalf("drainWarmOnClose did not remove warm worktrees; saw %d remove calls in %d total", removes, len(calls))
	}
}

func TestPrewarm_SpawnFailureBacksOff(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       2,
		ElasticMax:  4,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree add",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	time.Sleep(800 * time.Millisecond)
	calls := exec.callsSnapshot()
	addCount := 0
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "worktree add") {
			addCount++
		}
	}
	if addCount > 20 {
		t.Errorf("prewarm did not back off; saw %d worktree-add attempts in 800ms", addCount)
	}
	if addCount < 1 {
		t.Errorf("prewarm did not attempt any spawn; saw %d", addCount)
	}

	saw := false
	for _, ev := range emitter.eventTypes() {
		if ev == eventlog.EvtWorktreePoolDegraded {
			saw = true
		}
	}
	if !saw {
		t.Errorf("EvtWorktreePoolDegraded not emitted on prewarm ENOSPC; saw: %v", emitter.eventTypes())
	}
}

func TestPrewarm_ResetsBackoffAfterSuccess(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       3,
		ElasticMax:  6,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	exec.setScenario("worktree add",
		[]byte("fatal: write error: No space left on device\n"),
		errors.New("exit status 128"))
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	time.Sleep(300 * time.Millisecond)

	exec.setScenario("worktree add", nil, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if worktreepool.WarmCountForTest(p) >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("prewarm did not refill to Floor=3 after success-flip; warm=%d",
		worktreepool.WarmCountForTest(p))
}

func TestPrewarm_RespectsElasticCeiling(t *testing.T) {
	cfg := worktreepool.PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       2,
		ElasticMax:  2,
		GCCadence:   1 * time.Second,
		Doctrine:    "default",
	}
	exec := newRecordingExec()
	emitter := &fakeEmitter{}
	p, err := worktreepool.NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && worktreepool.WarmCountForTest(p) < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := worktreepool.WarmCountForTest(p); got != 2 {
		t.Fatalf("prewarm filled to %d, want 2 (Floor==ElasticMax)", got)
	}

	w1, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 1: %v", err)
	}
	w2, err := p.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 2: %v", err)
	}

	// Give prewarm 500ms to do nothing (warm=0, total=ElasticMax → idle).
	time.Sleep(500 * time.Millisecond)
	if got := worktreepool.WarmCountForTest(p); got != 0 {
		t.Fatalf("prewarm spawned past ElasticMax; warm=%d", got)
	}

	calls := exec.callsSnapshot()
	addCount := 0
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "worktree add") {
			addCount++
		}
	}
	if addCount != 2 {
		t.Errorf("prewarm spawn count = %d, want 2 (Floor=ElasticMax=2)", addCount)
	}

	_ = p.Release(context.Background(), w1)
	_ = p.Release(context.Background(), w2)
}
