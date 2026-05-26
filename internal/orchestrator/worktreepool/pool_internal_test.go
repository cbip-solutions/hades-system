package worktreepool

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

type stubAppender struct {
	mu sync.Mutex
}

func (s *stubAppender) Append(_ context.Context, _ eventlog.Event) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return 0, nil
}

type stubExecutor struct{}

func (stubExecutor) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return nil, errors.New("stub: not wired in B-1 internal tests")
}

// TestDegradedReason_DefaultUnknownBranch covers the default arm of
// degradedReason — exercised when a future class joins the
// ErrPoolDegraded surface without a reason mapping. The classifier
// today attaches ErrPoolDegraded only to ENOSPC + WorktreeLocked +
// Network + Signal, so the default branch is unreachable through the
// public API; we exercise it directly with a synthesised error to keep
// 100% coverage AND pin the defensive contract (no crash, surface
// "Unknown" as a stable bucket so dashboards do not break).
func TestDegradedReason_DefaultUnknownBranch(t *testing.T) {
	if got := degradedReason(ErrPoolDegraded); got != "Unknown" {
		t.Fatalf("degradedReason(bare ErrPoolDegraded) = %q, want \"Unknown\"", got)
	}

	bare := errors.New("hypothetical future class")
	wrapped := fmt.Errorf("%w: %w", bare, ErrPoolDegraded)
	if got := degradedReason(wrapped); got != "Unknown" {
		t.Fatalf("degradedReason(wrapped no-class) = %q, want \"Unknown\"", got)
	}
}

func TestClose_CtxDeadlineFiresOnPrewarmWait(t *testing.T) {
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

	cp.prewarmDone = make(chan struct{})
	cp.gcDone = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = cp.Close(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Close: err = %v, want context.Canceled", err)
	}
	if !cp.closed.Load() {
		t.Fatal("pool not marked closed after ctx-canceled Close")
	}

	if err := cp.Close(context.Background()); err != nil {
		t.Fatalf("idempotent re-Close: %v", err)
	}
}

// TestLease_UnderMuClosedRecheck covers the tight race window where Lease
// has passed the outer closed.Load() fast-path hint (observed false), is
// blocked on mu.Lock, and Close fires before Lease takes mu. The under-mu
// re-check MUST observe closed=true and return ErrPoolClosed without
// touching warm/leased (Close has nilled both maps under the same mu).
//
// This branch is unreachable from the external _test package because the
// outer closed.Load and inner re-check always observe the same value once
// Close has returned. White-box test forces the interleaving.
func TestLease_UnderMuClosedRecheck(t *testing.T) {
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

	cp.mu.Lock()

	leaseDone := make(chan error, 1)
	go func() {
		_, err := cp.Lease(context.Background())
		leaseDone <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cp.closed.Store(true)
	cp.mu.Unlock()

	select {
	case err := <-leaseDone:
		if !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("Lease under-mu re-check: err = %v, want ErrPoolClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lease did not return within 2s after under-mu Close")
	}

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone
}

func TestRelease_UnderMuClosedRecheck_AtLeasedGate(t *testing.T) {
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

	w := &Worktree{id: 9999, path: "/tmp/synthetic-pool-w-9999"}

	cp.mu.Lock()

	releaseDone := make(chan error, 1)
	go func() {
		releaseDone <- cp.Release(context.Background(), w)
	}()

	time.Sleep(20 * time.Millisecond)
	cp.closed.Store(true)
	cp.mu.Unlock()

	select {
	case err := <-releaseDone:
		if !errors.Is(err, ErrPoolClosed) {
			t.Fatalf("Release under-mu re-check (leased gate): err = %v, want ErrPoolClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Release did not return within 2s after under-mu Close")
	}

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone
}

func TestRelease_UnderMuClosedRecheck_AtWarmGate(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  2,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	gated := &gatedExec{}
	emitter := &stubAppender{}
	p, err := NewPool(cfg, emitter, gated)
	if err != nil {
		t.Fatal(err)
	}
	cp := p.(*concretePool)
	gated.onClean = func() {

		cp.closed.Store(true)
		cp.mu.Lock()
		cp.warm = nil
		cp.leased = nil
		cp.signalSlot = nil
		cp.mu.Unlock()
	}

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = cp.Release(context.Background(), w)
	if !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Release under-mu re-check (warm gate): err = %v, want ErrPoolClosed", err)
	}

	cp.prewarmCancel()
	cp.gcCancel()
	<-cp.prewarmDone
	<-cp.gcDone
}

func TestRelease_SignalSlotFull_SuccessPathHitsDefaultBranch(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
	}
	p, err := NewPool(cfg, &stubAppender{}, &gatedExec{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	cp := p.(*concretePool)

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cp.signalSlot <- struct{}{}

	if err := cp.Release(context.Background(), w); err != nil {
		t.Fatalf("Release with full signalSlot: %v", err)
	}
}

func TestRelease_SignalSlotFull_DestroyPathHitsDefaultBranch(t *testing.T) {
	cfg := PoolConfig{
		RepoRoot:    t.TempDir(),
		WorktreeDir: t.TempDir(),
		BranchBase:  "main",
		Floor:       1,
		ElasticMax:  1,
		GCCadence:   1 * time.Hour,
		Doctrine:    "default",
		PoolID:      "p",
	}
	gated := &gatedExec{resetErr: errors.New("exit status 1")}
	p, err := NewPool(cfg, &stubAppender{}, gated)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	cp := p.(*concretePool)

	w, err := p.Lease(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	cp.signalSlot <- struct{}{}

	if err := cp.Release(context.Background(), w); err != nil {
		t.Fatalf("Release destroy with full signalSlot: %v", err)
	}
}

type gatedExec struct {
	onClean  func()
	resetErr error
}

func (g *gatedExec) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	for i, a := range args {
		if a == "reset" && i+1 < len(args) && args[i+1] == "--hard" {
			if g.resetErr != nil {
				return nil, g.resetErr
			}
			return nil, nil
		}
		if a == "clean" {
			if g.onClean != nil {
				g.onClean()
			}
			return nil, nil
		}
	}
	_ = name
	return nil, nil
}

func TestClose_CtxDeadlineFiresOnGCWait(t *testing.T) {
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

	closedCh := make(chan struct{})
	close(closedCh)
	cp.prewarmDone = closedCh
	cp.gcDone = make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = cp.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close: err = %v, want context.DeadlineExceeded", err)
	}
}
