package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatcherCoalescesRapidEvents(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}

	var dispatchCount int64
	var seenPaths sync.Map
	w, err := newWatcherWithClock(
		newCountingIndexer(&dispatchCount, &seenPaths),
		50*time.Millisecond,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()

	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	p := filepath.Join(memDir, "rapid.md")
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(p, []byte("v"), 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(250 * time.Millisecond)

	got := atomic.LoadInt64(&dispatchCount)
	if got == 0 {
		t.Errorf("expected ≥ 1 dispatch after rapid writes, got 0")
	}

	if got > 2 {
		t.Errorf("dispatch count = %d, expected ≤ 2 (coalesced)", got)
	}
}

func TestWatcherThrottlesOnHighCPU(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}

	var dispatchCount int64
	var seen sync.Map
	w, err := newWatcherWithClock(
		newCountingIndexer(&dispatchCount, &seen),
		50*time.Millisecond,
		fakeCPUSampler{usage: 0.99},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(runDone)
	}()

	if err := os.WriteFile(filepath.Join(memDir, "x.md"), []byte("v"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	if got := atomic.LoadInt64(&dispatchCount); got != 0 {
		t.Errorf("expected 0 dispatches under high CPU (throttle), got %d", got)
	}
}

func TestWatcherThrottleRecovers(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}

	var dispatchCount int64
	var seen sync.Map
	sampler := &mutableCPUSampler{usage: 0.99}
	w, err := newWatcherWithClock(
		newCountingIndexer(&dispatchCount, &seen),
		40*time.Millisecond,
		sampler,
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	if err := os.WriteFile(filepath.Join(memDir, "throttled.md"), []byte("v"), 0o644); err != nil {
		t.Fatalf("write 1: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if got := atomic.LoadInt64(&dispatchCount); got != 0 {
		t.Fatalf("during throttle, dispatch=%d, expected 0", got)
	}

	sampler.set(0.0)
	if err := os.WriteFile(filepath.Join(memDir, "recovered.md"), []byte("v"), 0o644); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	if got := atomic.LoadInt64(&dispatchCount); got == 0 {
		t.Errorf("after CPU recovery, expected ≥ 1 dispatch, got 0")
	}
}

func TestWatcherDispatchesDelete(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}
	p := filepath.Join(memDir, "to-delete.md")
	if err := os.WriteFile(p, []byte("v"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	var reindexCount, deleteCount int64
	indexer := &countingIndexer{
		reindex: func(path string) { atomic.AddInt64(&reindexCount, 1) },
		del:     func(path string) { atomic.AddInt64(&deleteCount, 1) },
	}
	w, err := newWatcherWithClock(indexer, 40*time.Millisecond, fakeCPUSampler{usage: 0.0})
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt64(&deleteCount); got == 0 {
		t.Errorf("expected ≥ 1 delete dispatch after Remove, got 0")
	}
}

// TestWatcherIgnoresNonMarkdown asserts that *.txt or other non-.md files
// do not trigger indexer dispatch.
func TestWatcherIgnoresNonMarkdown(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir memDir: %v", err)
	}

	var dispatchCount int64
	var seen sync.Map
	w, err := newWatcherWithClock(
		newCountingIndexer(&dispatchCount, &seen),
		40*time.Millisecond,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	if err := os.WriteFile(filepath.Join(memDir, "data.txt"), []byte("v"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt64(&dispatchCount); got != 0 {
		t.Errorf("expected 0 dispatches for .txt, got %d", got)
	}
}

func TestWatcherRunReturnsOnContextCancel(t *testing.T) {
	tmp := t.TempDir()
	w, err := newWatcherWithClock(
		newCountingIndexer(nil, nil),
		40*time.Millisecond,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return within 2s of ctx cancel")
	}
}

func TestWatcherAddProjectIdempotent(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := newWatcherWithClock(
		newCountingIndexer(nil, nil),
		40*time.Millisecond,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()

	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject 1: %v", err)
	}
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject 2 (idempotent): %v", err)
	}
}

func TestNewWatcherDefaults(t *testing.T) {
	w, err := NewWatcher(newCountingIndexer(nil, nil))
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.fs.Close()
	if w.debounce != DefaultDebounce {
		t.Errorf("debounce = %v, want %v", w.debounce, DefaultDebounce)
	}
	if _, ok := w.cpu.(*realCPUSampler); !ok {
		t.Errorf("cpu sampler type = %T, want *realCPUSampler", w.cpu)
	}
}

func TestNewWatcherRejectsNilIndexer(t *testing.T) {
	w, err := NewWatcher(nil)
	if err == nil {
		t.Fatalf("NewWatcher(nil) returned no error; want non-nil")
	}
	if w != nil {
		t.Errorf("NewWatcher(nil) returned watcher %v; want nil", w)
	}
}

func TestWatcherDispatchDeleteSupersedesReindex(t *testing.T) {
	tmp := t.TempDir()
	memDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var reindexCount, deleteCount int64
	indexer := &countingIndexer{
		reindex: func(path string) { atomic.AddInt64(&reindexCount, 1) },
		del:     func(path string) { atomic.AddInt64(&deleteCount, 1) },
	}
	w, err := newWatcherWithClock(indexer, 60*time.Millisecond, fakeCPUSampler{usage: 0.0})
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()
	if err := w.AddProject(tmp); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	p := filepath.Join(memDir, "ephemeral.md")
	if err := os.WriteFile(p, []byte("v"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	time.Sleep(5 * time.Millisecond)
	if err := os.Remove(p); err != nil {
		t.Fatalf("remove: %v", err)
	}
	time.Sleep(250 * time.Millisecond)

	if got := atomic.LoadInt64(&reindexCount); got != 0 {
		t.Errorf("reindex count = %d under delete-supersedes; want 0", got)
	}
	if got := atomic.LoadInt64(&deleteCount); got == 0 {
		t.Errorf("delete count = 0; expected ≥ 1")
	}
}

// TestWatcherEnqueueChmodIgnored asserts that Chmod-only events do NOT
// arm the debounce timer. We synthesize the event directly via the
// (otherwise-internal) enqueue path so the test is deterministic — relying
// on the OS to emit Chmod is flaky across kernels.
func TestWatcherEnqueueChmodIgnored(t *testing.T) {
	w, err := newWatcherWithClock(
		newCountingIndexer(nil, nil),
		40*time.Millisecond,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()

	w.enqueue(fsnotify.Event{
		Name: "/some/path/file.md",
		Op:   fsnotify.Chmod,
	})

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		t.Errorf("Chmod-only event armed the debounce timer; should be ignored")
	}
	if len(w.pending) != 0 || len(w.deleted) != 0 {
		t.Errorf("Chmod-only event added to pending/deleted maps; should be ignored")
	}
}

func TestRealCPUSamplerSecondCallNonNegative(t *testing.T) {
	r := &realCPUSampler{}
	_ = r.Sample()
	burnCPU(5 * time.Millisecond)
	got := r.Sample()
	if got < 0 || got > 100 {
		t.Errorf("Sample = %v; want value in [0, 100] (sanity)", got)
	}
}

func TestWatcherEnqueueStopReset(t *testing.T) {
	w, err := newWatcherWithClock(
		newCountingIndexer(nil, nil),
		1*time.Hour,
		fakeCPUSampler{usage: 0.0},
	)
	if err != nil {
		t.Fatalf("newWatcherWithClock: %v", err)
	}
	defer w.fs.Close()

	w.enqueue(fsnotify.Event{Name: "/p/a.md", Op: fsnotify.Write})
	w.enqueue(fsnotify.Event{Name: "/p/b.md", Op: fsnotify.Write})

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) != 2 {
		t.Errorf("pending = %d; expected 2 (both paths queued under same timer)", len(w.pending))
	}
	if w.timer == nil {
		t.Errorf("timer is nil after two enqueues; expected armed")
	}
}

func TestWatcherRunReturnsNilOnFsClose(t *testing.T) {

	const iterations = 100
	for i := 0; i < iterations; i++ {
		w, err := newWatcherWithClock(
			newCountingIndexer(nil, nil),
			40*time.Millisecond,
			fakeCPUSampler{usage: 0.0},
		)
		if err != nil {
			t.Fatalf("iter %d: newWatcherWithClock: %v", i, err)
		}
		if err := w.fs.Close(); err != nil {
			t.Fatalf("iter %d: fs.Close pre-run: %v", i, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() { errCh <- w.Run(ctx) }()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("iter %d: Run returned %v; want nil or context.Canceled", i, err)
			}
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("iter %d: Run did not return within 2s after fs.Close", i)
		}
		cancel()
	}
}

func TestRealCPUSamplerFirstCallZero(t *testing.T) {
	r := &realCPUSampler{}
	first := r.Sample()
	if first != 0.0 {
		t.Errorf("first Sample() = %v, want 0.0 (no baseline yet)", first)
	}

	burnCPU(2 * time.Millisecond)
	second := r.Sample()
	if second < 0 {
		t.Errorf("second Sample() = %v, want ≥ 0", second)
	}
}

func burnCPU(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		runtime.Gosched()
	}
}

type countingIndexer struct {
	reindex func(path string)
	del     func(path string)
}

func (c *countingIndexer) Reindex(path string) error {
	if c.reindex != nil {
		c.reindex(path)
	}
	return nil
}

func (c *countingIndexer) Delete(path string) error {
	if c.del != nil {
		c.del(path)
	}
	return nil
}

func newCountingIndexer(counter *int64, seenPaths *sync.Map) *countingIndexer {
	return &countingIndexer{
		reindex: func(path string) {
			if counter != nil {
				atomic.AddInt64(counter, 1)
			}
			if seenPaths != nil {
				seenPaths.Store(path, struct{}{})
			}
		},
	}
}

type fakeCPUSampler struct{ usage float64 }

func (f fakeCPUSampler) Sample() float64 { return f.usage }

type mutableCPUSampler struct {
	mu    sync.Mutex
	usage float64
}

func (m *mutableCPUSampler) Sample() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

func (m *mutableCPUSampler) set(v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = v
}
