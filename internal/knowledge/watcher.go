// SPDX-License-Identifier: MIT
package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const DefaultDebounce = 3 * time.Second

const MaxCPUBudgetPct = 25.0

// IndexerSink is the dispatch contract for the watcher: re-index a file
// or delete its index row. The daemon's indexer wrapper (G-9 ColdRebuild +
// G-10 IncrementalUpdate) implements this; tests inject a mock counter.
//
// Both methods MUST be safe to call from a single goroutine (the watcher
// dispatch loop). Implementations that need parallelism own the goroutine
// fan-out internally.
type IndexerSink interface {
	Reindex(path string) error
	Delete(path string) error
}

type CPUSampler interface {
	Sample() float64
}

type realCPUSampler struct {
	mu       sync.Mutex
	hasPrev  bool
	prevUser time.Duration
	prevSys  time.Duration
	prevWall time.Time
}

func (r *realCPUSampler) Sample() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0.0
	}
	now := time.Now()
	user := time.Duration(ru.Utime.Sec)*time.Second +
		time.Duration(ru.Utime.Usec)*time.Microsecond
	sys := time.Duration(ru.Stime.Sec)*time.Second +
		time.Duration(ru.Stime.Usec)*time.Microsecond

	if !r.hasPrev {

		r.hasPrev = true
		r.prevUser = user
		r.prevSys = sys
		r.prevWall = now
		return 0.0
	}

	wallDelta := now.Sub(r.prevWall)
	if wallDelta <= 0 {

		return 0.0
	}
	cpuDelta := (user - r.prevUser) + (sys - r.prevSys)

	r.prevUser = user
	r.prevSys = sys
	r.prevWall = now

	if cpuDelta < 0 {

		return 0.0
	}
	return float64(cpuDelta) / float64(wallDelta)
}

// Watcher wraps fsnotify with a debounce + CPU-budget throttle. Owns
// the fsnotify.Watcher, a pending-events map, and the debounce timer.
//
// Concurrency model: Run owns all fsnotify event reads + timer ticks +
// dispatches in a single goroutine. enqueue / dispatch / tick all take
// the mutex briefly because callers (Run + AddProject) may execute
// concurrently with respect to the field state. AddProject is the only
// public method that mutates state outside Run.
//
// Lifecycle NewWatcher → AddProject(...) → Run(ctx) (blocks). Cancel
// ctx to stop. Watcher is single-shot; do NOT call Run twice.
type Watcher struct {
	fs       *fsnotify.Watcher
	indexer  IndexerSink
	debounce time.Duration
	cpu      CPUSampler

	mu      sync.Mutex
	pending map[string]struct{}
	deleted map[string]struct{}
	timer   *time.Timer
}

// NewWatcher constructs a Watcher with DefaultDebounce (3s) and the real
// syscall.Getrusage-based CPU sampler. Returns an error if fsnotify
// initialization fails (typically when the OS limit on inotify watchers
// has been hit on Linux).
//
// The caller MUST call AddProject for at least one project before Run,
// otherwise the watcher will block forever with no events to process.
func NewWatcher(indexer IndexerSink) (*Watcher, error) {
	return newWatcherWithClock(indexer, DefaultDebounce, &realCPUSampler{})
}

func newWatcherWithClock(indexer IndexerSink, debounce time.Duration, cpu CPUSampler) (*Watcher, error) {
	if indexer == nil {
		return nil, fmt.Errorf("knowledge: watcher requires non-nil IndexerSink")
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("knowledge: fsnotify: %w", err)
	}
	return &Watcher{
		fs:       fs,
		indexer:  indexer,
		debounce: debounce,
		cpu:      cpu,
		pending:  make(map[string]struct{}),
		deleted:  make(map[string]struct{}),
	}, nil
}

func (w *Watcher) AddProject(projectRoot string) error {
	dirs := []string{
		filepath.Join(projectRoot, "memory"),
		filepath.Join(projectRoot, "docs", "decisions"),
		filepath.Join(projectRoot, "docs", "superpowers", "specs"),
		filepath.Join(projectRoot, "docs", "superpowers", "plans"),
		projectRoot,
	}
	for _, d := range dirs {

		_ = w.fs.Add(d)
	}
	return nil
}

func (w *Watcher) Run(ctx context.Context) error {
	defer func() { _ = w.fs.Close() }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.fs.Events:
			if !ok {
				return nil
			}
			w.enqueue(ev)
		case <-w.tick():
			w.dispatch()
		case err, ok := <-w.fs.Errors:
			if !ok {
				return nil
			}

			fmt.Fprintf(os.Stderr, "knowledge watcher: %v\n", err)
		}
	}
}

func (w *Watcher) enqueue(ev fsnotify.Event) {
	if !isMarkdown(ev.Name) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case ev.Op&fsnotify.Write == fsnotify.Write,
		ev.Op&fsnotify.Create == fsnotify.Create:
		w.pending[ev.Name] = struct{}{}
		delete(w.deleted, ev.Name)
	case ev.Op&fsnotify.Remove == fsnotify.Remove,
		ev.Op&fsnotify.Rename == fsnotify.Rename:
		w.deleted[ev.Name] = struct{}{}
		delete(w.pending, ev.Name)
	default:

		return
	}
	if w.timer == nil {
		w.timer = time.NewTimer(w.debounce)
		return
	}

	w.timer.Stop()
	w.timer.Reset(w.debounce)
}

func (w *Watcher) tick() <-chan time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		return nil
	}
	return w.timer.C
}

// dispatch applies the pending (Reindex) and deleted (Delete) sets,
// subject to the CPU-budget throttle.
//
// Throttled path: if Sample() exceeds 25%, the queues are LEFT INTACT
// and the timer is RE-ARMED so the next debounce tick re-checks. This
// is the "next cycle in 3s the file is still in the queue, so re-checked
// then" guarantee from the spec context.
//
// Non-throttled path: snapshot pending+deleted under lock, drop the
// timer (next event arms a fresh one), then dispatch outside the lock
// (Reindex/Delete may take milliseconds; we MUST NOT hold the watcher
// mutex during that — operator-typed-then-saved-then-re-saved would
// stall the next enqueue).
//
// Conflict resolution invariant: a path is NEVER in both pending and
// deleted simultaneously — enqueue maintains this by deleting the
// counterpart entry whenever it adds. The dispatch loop therefore
// processes the two sets independently. If you change enqueue to
// allow simultaneous membership, dispatch's loops MUST be updated to
// resolve the conflict (delete-wins).
func (w *Watcher) dispatch() {
	if w.cpu.Sample()*100.0 > MaxCPUBudgetPct {

		w.mu.Lock()
		if w.timer != nil {

			w.timer = time.NewTimer(w.debounce)
		}
		w.mu.Unlock()
		return
	}

	w.mu.Lock()
	pending := w.pending
	deleted := w.deleted
	w.pending = make(map[string]struct{})
	w.deleted = make(map[string]struct{})
	w.timer = nil
	w.mu.Unlock()

	for path := range pending {
		_ = w.indexer.Reindex(path)
	}
	for path := range deleted {
		_ = w.indexer.Delete(path)
	}
}

func isMarkdown(p string) bool { return filepath.Ext(p) == ".md" }
