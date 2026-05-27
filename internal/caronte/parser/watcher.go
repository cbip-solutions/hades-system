// SPDX-License-Identifier: MIT
package parser

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
	user := time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	sys := time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	if !r.hasPrev {
		r.hasPrev = true
		r.prevUser, r.prevSys, r.prevWall = user, sys, now
		return 0.0
	}
	wallDelta := now.Sub(r.prevWall)
	if wallDelta <= 0 {
		return 0.0
	}
	cpuDelta := (user - r.prevUser) + (sys - r.prevSys)
	r.prevUser, r.prevSys, r.prevWall = user, sys, now
	if cpuDelta < 0 {
		return 0.0
	}
	return float64(cpuDelta) / float64(wallDelta)
}

// Watcher wraps fsnotify with a debounce + CPU-budget throttle, re-indexing Go
// files on save through an IndexerSink. It mirrors internal/knowledge.Watcher
// (same concurrency model: Run owns all event reads + timer ticks +
// dispatches in one goroutine; enqueue/tick/dispatch take the mutex briefly)
// with two Caronte deltas: the file filter is.go (not.md), and the debounce
// + CPU budget are read per-dispatch from the DoctrineAccessor seam
// ([doctrine.caronte.watcher]).
//
// Lifecycle NewWatcher → AddProject(...) → Run(ctx) (blocks). Cancel ctx to
// stop. Single-shot; do NOT call Run twice.
type Watcher struct {
	fs        *fsnotify.Watcher
	indexer   IndexerSink
	doctrine  DoctrineAccessor
	projectID string

	mu      sync.Mutex
	pending map[string]struct{}
	deleted map[string]struct{}
	timer   *time.Timer

	cpu CPUSampler
}

// NewWatcher constructs a Caronte file watcher over the given sink, doctrine
// accessor (nil → defaults), and project id. Returns an error if fsnotify init
// fails (e.g. Linux inotify-watch limit). The caller MUST AddProject at least
// one project before Run.
func NewWatcher(indexer IndexerSink, doctrine DoctrineAccessor, projectID string) (*Watcher, error) {
	if indexer == nil {
		return nil, fmt.Errorf("caronte/parser: watcher requires non-nil IndexerSink")
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("caronte/parser: fsnotify: %w", err)
	}
	return &Watcher{
		fs:        fs,
		indexer:   indexer,
		doctrine:  doctrine,
		projectID: projectID,
		pending:   make(map[string]struct{}),
		deleted:   make(map[string]struct{}),
		cpu:       &realCPUSampler{},
	}, nil
}

func newWatcherForTest(indexer IndexerSink, doctrine DoctrineAccessor, projectID string, _ time.Duration, cpu CPUSampler) (*Watcher, error) {
	return &Watcher{
		indexer:   indexer,
		doctrine:  doctrine,
		projectID: projectID,
		pending:   make(map[string]struct{}),
		deleted:   make(map[string]struct{}),
		cpu:       cpu,
	}, nil
}

func (w *Watcher) cpuBudgetPct() float64 {
	if w.doctrine != nil {
		if b := w.doctrine.WatcherCPUBudget(w.projectID); b > 0 {
			return b * 100.0
		}
	}
	return MaxCPUBudgetPct
}

func (w *Watcher) debounceDur() time.Duration {
	if w.doctrine != nil {
		if d := w.doctrine.WatcherCadence(w.projectID); d > 0 {
			return d
		}
	}
	return DefaultDebounce
}

func (w *Watcher) AddProject(projectRoot string) error {
	_ = filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		base := d.Name()
		if base == ".git" || base == "vendor" || base == "node_modules" || base == ".zen" {
			return filepath.SkipDir
		}
		_ = w.fs.Add(path)
		return nil
	})
	return nil
}

func (w *Watcher) Run(ctx context.Context) error {
	if w.fs != nil {
		defer func() { _ = w.fs.Close() }()
	}
	for {
		var events <-chan fsnotify.Event
		var errs <-chan error
		if w.fs != nil {
			events = w.fs.Events
			errs = w.fs.Errors
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			w.enqueue(ev)
		case <-w.tick():
			w.dispatch()
		case err, ok := <-errs:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "caronte watcher: %v\n", err)
		}
	}
}

func (w *Watcher) enqueue(ev fsnotify.Event) {
	if !isGoFile(ev.Name) {
		return
	}
	switch {
	case ev.Op&fsnotify.Write == fsnotify.Write, ev.Op&fsnotify.Create == fsnotify.Create:
		w.enqueueWrite(ev.Name)
	case ev.Op&fsnotify.Remove == fsnotify.Remove, ev.Op&fsnotify.Rename == fsnotify.Rename:
		w.enqueueDelete(ev.Name)
	}
}

func (w *Watcher) enqueueWrite(path string) {
	if !isGoFile(path) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending[path] = struct{}{}
	delete(w.deleted, path)
	w.armTimerLocked()
}

func (w *Watcher) enqueueDelete(path string) {
	if !isGoFile(path) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deleted[path] = struct{}{}
	delete(w.pending, path)
	w.armTimerLocked()
}

func (w *Watcher) armTimerLocked() {
	d := w.debounceDur()
	if w.timer == nil {
		w.timer = time.NewTimer(d)
		return
	}

	w.timer.Stop()
	w.timer.Reset(d)
}

func (w *Watcher) tick() <-chan time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		return nil
	}
	return w.timer.C
}

func (w *Watcher) dispatch() {
	if w.cpu.Sample()*100.0 > w.cpuBudgetPct() {

		w.mu.Lock()
		if w.timer != nil {

			w.timer = time.NewTimer(w.debounceDur())
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

func isGoFile(p string) bool { return filepath.Ext(p) == ".go" }

func (w *Watcher) dispatchForTest() { w.dispatch() }

func (w *Watcher) queueLensForTest() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.pending), len(w.deleted)
}

func (w *Watcher) closeForTest() {
	if w.fs != nil {
		_ = w.fs.Close()
	}
}
