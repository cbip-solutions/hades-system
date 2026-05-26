package parser

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

type recordingSink struct {
	mu       sync.Mutex
	reindex  []string
	deleted  []string
	reindexN int
}

func (r *recordingSink) Reindex(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reindex = append(r.reindex, path)
	r.reindexN++
	return nil
}
func (r *recordingSink) Delete(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, path)
	return nil
}
func (r *recordingSink) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reindex), len(r.deleted)
}

type fixedSampler float64

func (f fixedSampler) Sample() float64 { return float64(f) }

type fakeDoctrine struct {
	budget  float64
	cadence time.Duration
}

func (f fakeDoctrine) WatcherCPUBudget(string) float64     { return f.budget }
func (f fakeDoctrine) WatcherCadence(string) time.Duration { return f.cadence }

func TestWatcherDefaultsWithNilDoctrine(t *testing.T) {
	sink := &recordingSink{}
	w, err := NewWatcher(sink, nil, "proj")
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.closeForTest()
	if w.cpuBudgetPct() != MaxCPUBudgetPct {
		t.Errorf("nil doctrine budget = %v; want default %v", w.cpuBudgetPct(), MaxCPUBudgetPct)
	}
	if w.debounceDur() != DefaultDebounce {
		t.Errorf("nil doctrine debounce = %v; want default %v", w.debounceDur(), DefaultDebounce)
	}
}

func TestWatcherReadsDoctrine(t *testing.T) {
	sink := &recordingSink{}
	doc := fakeDoctrine{budget: 0.10, cadence: 500 * time.Millisecond}
	w, err := NewWatcher(sink, doc, "proj")
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.closeForTest()
	if w.cpuBudgetPct() != 10.0 {
		t.Errorf("doctrine budget = %v; want 10.0 (0.10 of a core → 10%%)", w.cpuBudgetPct())
	}
	if w.debounceDur() != 500*time.Millisecond {
		t.Errorf("doctrine debounce = %v; want 500ms", w.debounceDur())
	}
}

func TestWatcherEnqueueFiltersGoFiles(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()

	w.enqueueWrite("a.go")
	w.enqueueWrite("README.md")
	w.enqueueWrite("b.go")

	pending, _ := w.queueLensForTest()
	if pending != 2 {
		t.Errorf("pending = %d; want 2 (only .go files queued)", pending)
	}
}

func TestWatcherDispatchReindexesPending(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()
	w.enqueueWrite("a.go")
	w.enqueueWrite("b.go")
	w.dispatchForTest()
	ri, _ := sink.counts()
	if ri != 2 {
		t.Errorf("Reindex calls = %d; want 2", ri)
	}
	pending, _ := w.queueLensForTest()
	if pending != 0 {
		t.Errorf("pending after dispatch = %d; want 0 (drained)", pending)
	}
}

func TestWatcherThrottleLeavesQueueIntact(t *testing.T) {
	sink := &recordingSink{}

	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.50))
	defer w.closeForTest()
	w.enqueueWrite("a.go")
	w.dispatchForTest()
	ri, _ := sink.counts()
	if ri != 0 {
		t.Errorf("throttled dispatch made %d Reindex calls; want 0", ri)
	}
	pending, _ := w.queueLensForTest()
	if pending != 1 {
		t.Errorf("throttled dispatch dropped the queue (pending=%d); want 1 preserved", pending)
	}
}

func TestWatcherDispatchDeletesRemoved(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()
	w.enqueueDelete("gone.go")
	w.dispatchForTest()
	_, del := sink.counts()
	if del != 1 {
		t.Errorf("Delete calls = %d; want 1", del)
	}
}

func TestWatcherRunCancels(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Run returned nil on cancel; want ctx.Err()")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancel")
	}
}

func TestRealCPUSamplerFirstCallZero(t *testing.T) {
	s := &realCPUSampler{}
	if got := s.Sample(); got != 0.0 {
		t.Errorf("first Sample() = %v; want 0.0", got)
	}
}

func TestRealCPUSamplerSecondCallReturnsNonNegative(t *testing.T) {
	s := &realCPUSampler{}
	_ = s.Sample()
	got := s.Sample()
	if got < 0 {
		t.Errorf("second Sample() = %v; want >= 0 (CPU fraction)", got)
	}
}

func TestNewWatcherNilIndexerErrors(t *testing.T) {
	_, err := NewWatcher(nil, nil, "proj")
	if err == nil {
		t.Error("NewWatcher(nil sink) returned nil error; want error")
	}
}

func TestWatcherEnqueueWriteViaFsnotify(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()

	w.enqueue(fsnotify.Event{Name: "foo.go", Op: fsnotify.Write})
	w.enqueue(fsnotify.Event{Name: "bar.go", Op: fsnotify.Create})
	w.enqueue(fsnotify.Event{Name: "ignore.md", Op: fsnotify.Write})
	w.enqueue(fsnotify.Event{Name: "ignored.go", Op: fsnotify.Chmod})
	pending, deleted := w.queueLensForTest()
	if pending != 2 {
		t.Errorf("pending = %d after Write+Create for .go files; want 2", pending)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d; want 0", deleted)
	}

	w.enqueue(fsnotify.Event{Name: "foo.go", Op: fsnotify.Remove})
	pending2, deleted2 := w.queueLensForTest()
	if pending2 != 1 {
		t.Errorf("pending = %d after Remove of pending path; want 1", pending2)
	}
	if deleted2 != 1 {
		t.Errorf("deleted = %d after Remove of pending path; want 1", deleted2)
	}

	w.enqueue(fsnotify.Event{Name: "new.go", Op: fsnotify.Rename})
	_, deleted3 := w.queueLensForTest()
	if deleted3 != 2 {
		t.Errorf("deleted = %d after Rename of new path; want 2", deleted3)
	}
}

func TestWatcherEnqueueDeleteNonGoIgnored(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()
	w.enqueueDelete("README.md")
	_, del := w.queueLensForTest()
	if del != 0 {
		t.Errorf("deleted = %d after enqueueDelete(.md); want 0", del)
	}
}

func TestWatcherTickNilWhenNoTimer(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()
	if ch := w.tick(); ch != nil {
		t.Error("tick() on an idle watcher returned non-nil channel; want nil")
	}
}

func TestWatcherThrottleWithCustomBudget(t *testing.T) {
	sink := &recordingSink{}

	doc := fakeDoctrine{budget: 0.10, cadence: 10 * time.Millisecond}
	w, _ := newWatcherForTest(sink, doc, "proj", 10*time.Millisecond, fixedSampler(0.05))
	defer w.closeForTest()
	w.enqueueWrite("a.go")
	w.dispatchForTest()
	ri, _ := sink.counts()
	if ri != 1 {
		t.Errorf("Reindex calls = %d with 5%% CPU vs 10%% budget; want 1 (not throttled)", ri)
	}
}

func TestWatcherConflictPendingBecomesDelete(t *testing.T) {
	sink := &recordingSink{}
	w, _ := newWatcherForTest(sink, nil, "proj", 10*time.Millisecond, fixedSampler(0.0))
	defer w.closeForTest()
	w.enqueueWrite("conflict.go")
	w.enqueueDelete("conflict.go")
	pending, deleted := w.queueLensForTest()
	if pending != 0 {
		t.Errorf("pending = %d after delete supersedes write; want 0", pending)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d after delete supersedes write; want 1", deleted)
	}
}
