package worktreepool

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

func TestLeaseSlowPath_SignalSlotWakeupRetriesAndSpawns(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	exec := newFakeExec()
	emitter := &recordingAppender{}
	p, err := NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	cp := p.(*concretePool)

	w1, err := cp.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 1: %v", err)
	}

	leaseDone := make(chan struct {
		w   *Worktree
		err error
	}, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		w, err := cp.Lease(ctx)
		leaseDone <- struct {
			w   *Worktree
			err error
		}{w: w, err: err}
	}()

	time.Sleep(50 * time.Millisecond)

	cp.mu.Lock()
	delete(cp.leased, w1.ID())
	cp.total.Add(-1)
	cp.mu.Unlock()

	cp.signalSlot <- struct{}{}

	select {
	case res := <-leaseDone:
		if res.err != nil {
			t.Fatalf("Lease 2 (post-wakeup): %v", res.err)
		}
		if res.w == nil {
			t.Fatal("Lease 2 (post-wakeup): nil worktree")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Lease did not return within 3s after signalSlot send")
	}
}

func TestLeaseSlowPath_WakeupFindsWarmAndPopsIt(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	exec := newFakeExec()
	emitter := &recordingAppender{}
	p, err := NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	cp := p.(*concretePool)

	w1, err := cp.Lease(context.Background())
	if err != nil {
		t.Fatalf("Lease 1: %v", err)
	}

	leaseDone := make(chan struct {
		w   *Worktree
		err error
	}, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		w, err := cp.Lease(ctx)
		leaseDone <- struct {
			w   *Worktree
			err error
		}{w: w, err: err}
	}()

	time.Sleep(50 * time.Millisecond)

	cp.mu.Lock()
	delete(cp.leased, w1.ID())
	cp.warm = append(cp.warm, w1)
	cp.mu.Unlock()

	cp.signalSlot <- struct{}{}

	select {
	case res := <-leaseDone:
		if res.err != nil {
			t.Fatalf("Lease 2: %v", res.err)
		}
		if res.w == nil {
			t.Fatal("Lease 2: nil worktree")
		}
		if res.w.ID() != w1.ID() {
			t.Errorf("Lease 2 returned id=%d, want re-popped warm w1=%d", res.w.ID(), w1.ID())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Lease did not return within 3s")
	}
}

func TestLeaseSlowPath_WakeupObservesClosedAndReturns(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	exec := newFakeExec()
	emitter := &recordingAppender{}
	p, err := NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	cp := p.(*concretePool)

	if _, err := cp.Lease(context.Background()); err != nil {
		t.Fatalf("Lease 1: %v", err)
	}

	leaseDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := cp.Lease(ctx)
		leaseDone <- err
	}()

	time.Sleep(50 * time.Millisecond)

	cp.closed.Store(true)
	cp.signalSlot <- struct{}{}

	select {
	case err := <-leaseDone:
		if !errors.Is(err, ErrPoolClosed) {
			t.Errorf("err=%v, want ErrPoolClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Lease did not return within 3s")
	}

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone
}

func TestLeaseSlowPath_SpawnSuccessRacesClose(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	exec := newBlockingExec()
	emitter := &recordingAppender{}
	p, err := NewPool(cfg, emitter, exec)
	if err != nil {
		t.Fatal(err)
	}
	cp := p.(*concretePool)

	leaseDone := make(chan error, 1)
	go func() {
		_, err := cp.Lease(context.Background())
		leaseDone <- err
	}()

	select {
	case <-exec.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not enter Run within 2s")
	}

	cp.closed.Store(true)

	close(exec.release)

	select {
	case err := <-leaseDone:
		if !errors.Is(err, ErrPoolClosed) {
			t.Errorf("err=%v, want ErrPoolClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Lease did not return within 3s")
	}

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone
}

type recordingAppender struct {
	mu     sync.Mutex
	events []eventlog.Event
}

func (a *recordingAppender) Append(_ context.Context, ev eventlog.Event) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
	return int64(len(a.events)), nil
}

type blockingExec struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingExec() *blockingExec {
	return &blockingExec{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (b *blockingExec) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
