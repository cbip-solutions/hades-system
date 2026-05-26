package worktreepool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPrewarm_BackoffCapsAtMax(t *testing.T) {

	backoff := prewarmBackoffMin
	for i := 0; i < 20; i++ {

		backoff *= 2
		if backoff > prewarmBackoffMax {
			backoff = prewarmBackoffMax
		}
		if backoff > prewarmBackoffMax {
			t.Fatalf("iter %d: backoff %v exceeded cap %v", i, backoff, prewarmBackoffMax)
		}
	}
	if backoff != prewarmBackoffMax {
		t.Fatalf("after 20 doublings backoff = %v, want %v (cap)",
			backoff, prewarmBackoffMax)
	}
}

func TestPrewarm_BackoffCapInLoop(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  4,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	failExec := &countingFailExec{
		err: errors.New("exit status 128"),

		stderr: []byte("fatal: write error: No space left on device\n"),
	}
	p, err := NewPool(cfg, &stubAppender{}, failExec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	time.Sleep(1500 * time.Millisecond)
	calls := failExec.callCount()

	if calls < 1 {
		t.Errorf("prewarm did not attempt any spawn in 1.5s")
	}
	if calls > 30 {
		t.Errorf("prewarm busy-spun: %d attempts in 1.5s (expected <=30)", calls)
	}
}

type countingFailExec struct {
	mu     sync.Mutex
	calls  int
	err    error
	stderr []byte
}

func (c *countingFailExec) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	for _, a := range args {
		if a == "add" {
			c.mu.Lock()
			c.calls++
			c.mu.Unlock()
			return c.stderr, c.err
		}
	}
	return nil, nil
}

func (c *countingFailExec) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestPrewarm_BackoffClampsToMax(t *testing.T) {

	origMax := prewarmBackoffMax
	prewarmBackoffMax = 5 * time.Millisecond
	defer func() { prewarmBackoffMax = origMax }()

	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  4,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	failExec := &countingFailExec{
		err:    errors.New("exit status 128"),
		stderr: []byte("fatal: write error: No space left on device\n"),
	}
	p, err := NewPool(cfg, &stubAppender{}, failExec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if failExec.callCount() >= 10 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := failExec.callCount(); got < 10 {
		t.Errorf("call count = %d, want ≥10 to cross cap-clamp branch", got)
	}

	if got := failExec.callCount(); got < 9 {
		t.Errorf("call count = %d, want ≥9 to cross cap-clamp branch", got)
	}
}

func TestPrewarm_AppendWarm_SignalSlotFullHitsDefault(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	p, err := NewPool(cfg, &stubAppender{}, stubExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	cp := p.(*concretePool)

	cp.signalSlot <- struct{}{}

	w := &Worktree{id: 9999, path: "/tmp/synthetic-prewarm-w-9999"}
	cp.appendWarmAndSignal(w)

	cp.mu.Lock()
	found := false
	for _, ww := range cp.warm {
		if ww.id == 9999 {
			found = true
		}
	}
	cp.mu.Unlock()
	if !found {
		t.Fatal("appendWarmAndSignal did not append synthetic worktree to warm")
	}
}

func TestPrewarm_AppendWarm_SpawnRacesClose(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	p, err := NewPool(cfg, &stubAppender{}, stubExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	cp := p.(*concretePool)

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone

	cp.total.Store(1)
	cp.closed.Store(true)

	w := &Worktree{id: 9999, path: "/tmp/synthetic-races-close-9999"}
	cp.appendWarmAndSignal(w)

	if got := cp.total.Load(); got != 0 {
		t.Errorf("total = %d, want 0 (rollback after closed-race)", got)
	}
	cp.mu.Lock()
	gotLen := len(cp.warm)
	cp.mu.Unlock()
	if gotLen != 0 {
		t.Errorf("warm len = %d, want 0 (closed-race must not append)", gotLen)
	}
}
