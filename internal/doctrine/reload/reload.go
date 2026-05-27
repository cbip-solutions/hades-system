// SPDX-License-Identifier: MIT
// Package reload implements doctrine-TOML hot-reload via fsnotify.
//
// The Watcher monitors per-doctrine TOML files (~/.config/zen-swarm/doctrines/*.toml
// for user defaults; <project>/.zen/doctrine-override.toml for per-project tighten
// overrides). On a write event, after a 2-second debounce window, the Watcher runs
// the validate-then-swap pipeline (parser.ParseStrict → schema.Validate →
// schema.ValidateTighten if per-project → atomic swap via active.Set*). Validation
// failures keep the previously-loaded schema active (last-good fallback) and emit
// DoctrineReloadFailed via the release eventlog. Repeated failures (5+ within a
// 60-second window) trigger a per-path 1-minute cooldown to protect the daemon
// from a wedged file.
//
// Reuses release Q16 + Qx-2 D file-watcher infrastructure (fsnotify wrapper +
// 25% CPU pool + debounce reset-on-event pattern); zero new infra.
//
// Boundary: zero imports of internal/store;
// reload package is downstream of parser + schema + active + errors only, plus
// fsnotify + eventlog interface.
//
// Atomicity: Watcher does NOT mutate active.Accessor's sync.Pointer
// directly; delegates to active.SetForProject + active.SetUserDefault, which own
// the Store call. Watcher concurrency contract: AddPath, NotifyForce,
// SubscribeReloadEvents safe for concurrent callers; Start runs in a single
// goroutine and is cancellable via ctx.Done().
package reload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

type EventlogClient interface {
	Emit(ctx context.Context, evt any) error
}

type ActiveAccessor interface {
	SetForProject(projectID string, schema *v1.Schema)
	SetUserDefault(schema *v1.Schema)
	ClearForProject(projectID string)
}

type ParserClient interface {
	ParseStrict(data []byte, source string, target *v1.Schema, opts parser.ParseOpts) error
}

// WatcherOpts carries Watcher constructor configuration. The three required
// clients (EventlogClient, ActiveAccessor, Parser) MUST be non-nil; the
// duration / threshold fields default to spec §1 Q10 values when zero.
type WatcherOpts struct {
	EventlogClient   EventlogClient
	ActiveAccessor   ActiveAccessor
	Parser           ParserClient
	BaselineProvider BaselineProvider
	Validator        Validator
	Clock            Clock
	DebounceWindow   time.Duration
	StormCooldown    time.Duration
	StormThreshold   int
	StormWindow      time.Duration
	StallTimeout     time.Duration
}

type Watcher struct {
	fsWatcher        *fsnotify.Watcher
	fsWatcherMu      sync.Mutex
	eventlog         EventlogClient
	active           ActiveAccessor
	parser           ParserClient
	baselineProvider BaselineProvider
	validator        Validator
	clock            Clock
	debounceWindow   time.Duration
	stormCooldown    time.Duration
	stormThreshold   int
	stormWindow      time.Duration
	stallTimeout     time.Duration

	debouncer *Debouncer

	perProjectMap sync.Map

	failureCounter sync.Map

	forcedSource sync.Map

	reloadEventMu   sync.Mutex
	reloadEventSubs []chan DoctrineReloaded

	reloadFailedEventMu   sync.Mutex
	reloadFailedEventSubs []chan DoctrineReloadFailed

	pathsCount atomic.Int32

	lastEventAtMu sync.Mutex
	lastEventAt   time.Time

	restartNeeded chan struct{}

	closeMu sync.Mutex
	closed  bool
}

func New(opts WatcherOpts) (*Watcher, error) {
	if opts.EventlogClient == nil {
		return nil, errors.New("reload: EventlogClient is required")
	}
	if opts.ActiveAccessor == nil {
		return nil, errors.New("reload: ActiveAccessor is required")
	}
	if opts.Parser == nil {
		return nil, errors.New("reload: Parser is required")
	}
	if opts.DebounceWindow == 0 {
		opts.DebounceWindow = 2 * time.Second
	}
	if opts.StormCooldown == 0 {
		opts.StormCooldown = 1 * time.Minute
	}
	if opts.StormThreshold == 0 {
		opts.StormThreshold = 5
	}
	if opts.StormWindow == 0 {
		opts.StormWindow = 60 * time.Second
	}
	if opts.StallTimeout == 0 {
		opts.StallTimeout = 5 * time.Minute
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Validator == nil {
		opts.Validator = schemaBoundValidator{}
	}
	w := &Watcher{
		eventlog:         opts.EventlogClient,
		active:           opts.ActiveAccessor,
		parser:           opts.Parser,
		baselineProvider: opts.BaselineProvider,
		validator:        opts.Validator,
		clock:            opts.Clock,
		debounceWindow:   opts.DebounceWindow,
		stormCooldown:    opts.StormCooldown,
		stormThreshold:   opts.StormThreshold,
		stormWindow:      opts.StormWindow,
		stallTimeout:     opts.StallTimeout,
		restartNeeded:    make(chan struct{}, 1),
	}
	w.debouncer = NewDebouncer(opts.DebounceWindow, w.runReloadAction)
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("reload: fsnotify.NewWatcher: %w", err)
	}
	w.fsWatcher = fsw
	return w, nil
}

func validatePath(path string) error {
	if path == "" {
		return errors.New("reload: AddPath: empty path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("reload: AddPath(%q): path must be absolute", path)
	}

	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return fmt.Errorf("reload: AddPath(%q): path contains traversal sequence", path)
		}
	}
	if cleaned := filepath.Clean(path); cleaned != path {
		return fmt.Errorf("reload: AddPath(%q): not in canonical form (cleaned %q)", path, cleaned)
	}
	return nil
}

func (w *Watcher) AddPath(path string, projectID string) error {
	if err := validatePath(path); err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("reload: AddPath(%q): %w", path, err)
	}
	w.fsWatcherMu.Lock()
	err := w.fsWatcher.Add(path)
	w.fsWatcherMu.Unlock()
	if err != nil {
		return fmt.Errorf("reload: fsnotify.Add(%q): %w", path, err)
	}
	if _, loaded := w.perProjectMap.Swap(path, projectID); !loaded {
		w.pathsCount.Add(1)
	}
	return nil
}

func (w *Watcher) PathsCount() int { return int(w.pathsCount.Load()) }

func (w *Watcher) DebounceWindow() time.Duration { return w.debounceWindow }

func (w *Watcher) StormCooldown() time.Duration { return w.stormCooldown }

func (w *Watcher) StormThreshold() int { return w.stormThreshold }

func (w *Watcher) Start(ctx context.Context) error {
	defer w.Close()
	go w.runHealthMonitor(ctx)
	for {
		w.fsWatcherMu.Lock()
		fsw := w.fsWatcher
		w.fsWatcherMu.Unlock()
		if fsw == nil {

			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fsw.Events:
			if !ok {
				return errors.New("reload: fsnotify Events channel closed")
			}
			w.recordEventTime()
			w.onEvent(ctx, ev)
		case err, ok := <-fsw.Errors:
			if !ok {
				return errors.New("reload: fsnotify Errors channel closed")
			}
			if isOverflowError(err) {
				w.handleOverflow(ctx, err)
				continue
			}
			return fmt.Errorf("reload: fsnotify error: %w", err)
		case <-w.restartNeeded:
			if err := w.performRestart(ctx, "stall"); err != nil {
				return fmt.Errorf("reload: restart on stall: %w", err)
			}
		}
	}
}

func (w *Watcher) Close() {
	w.closeMu.Lock()
	if w.closed {
		w.closeMu.Unlock()
		return
	}
	w.closed = true
	w.closeMu.Unlock()
	if w.debouncer != nil {
		w.debouncer.Close()
	}

	w.reloadEventMu.Lock()
	for _, s := range w.reloadEventSubs {
		close(s)
	}
	w.reloadEventSubs = nil
	w.reloadEventMu.Unlock()

	w.reloadFailedEventMu.Lock()
	for _, s := range w.reloadFailedEventSubs {
		close(s)
	}
	w.reloadFailedEventSubs = nil
	w.reloadFailedEventMu.Unlock()

	w.fsWatcherMu.Lock()
	if w.fsWatcher != nil {
		_ = w.fsWatcher.Close()
		w.fsWatcher = nil
	}
	w.fsWatcherMu.Unlock()
}

func (w *Watcher) onEvent(ctx context.Context, ev fsnotify.Event) {

	if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
		return
	}
	w.debouncer.Trigger(ctx, ev.Name)
}

func (w *Watcher) recordEventTime() {
	w.lastEventAtMu.Lock()
	defer w.lastEventAtMu.Unlock()
	w.lastEventAt = w.clock.Now()
}

func (w *Watcher) RunReloadActionForTest(ctx context.Context, path string) {
	w.runReloadAction(ctx, path)
}

// TriggerDebounceForTest arms the per-path debounce timer the same way an
// fsnotify event would (via onEvent → debouncer.Trigger). Used in tests
// that need to verify NotifyForce cancels a pending debounce timer
// deterministically — without this seam, drivers would have to drive a
// real os.WriteFile + Start loop, where event-coalescing on macOS
// fsnotify can fire 0..N times depending on FS quirks. Production paths
// MUST go through Start's event-channel branch.
func (w *Watcher) TriggerDebounceForTest(ctx context.Context, path string) {
	if w.debouncer != nil {
		w.debouncer.Trigger(ctx, path)
	}
}

func (w *Watcher) PendingDebounceForTest(path string) bool {
	if w.debouncer == nil {
		return false
	}
	w.debouncer.mu.Lock()
	defer w.debouncer.mu.Unlock()
	_, ok := w.debouncer.timers[path]
	return ok
}
