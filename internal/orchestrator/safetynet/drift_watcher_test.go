package safetynet

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

type watcherValidator struct {
	mu      sync.Mutex
	calls   int
	windows []int
	errs    []error

	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func newWatcherValidator() *watcherValidator {
	return &watcherValidator{
		started: make(chan struct{}, 16),
		release: make(chan struct{}),
		done:    make(chan struct{}, 16),
	}
}

func (v *watcherValidator) Validate(ctx context.Context, n int) (Report, error) {
	v.mu.Lock()
	v.calls++
	v.windows = append(v.windows, n)
	call := v.calls
	var err error
	if len(v.errs) >= call {
		err = v.errs[call-1]
	}
	v.mu.Unlock()

	v.started <- struct{}{}
	if v.release != nil {
		select {
		case <-ctx.Done():
		case <-v.release:
		}
	}
	v.done <- struct{}{}
	return Report{}, err
}

func (v *watcherValidator) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

func (v *watcherValidator) windowAt(i int) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.windows[i]
}

func TestDriftWatcher_Defaults(t *testing.T) {
	t.Parallel()
	v := newWatcherValidator()
	w, err := NewDriftWatcher(DriftWatcherConfig{Validator: v})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	if w.interval != DefaultDriftWatcherInterval {
		t.Fatalf("interval = %v want %v", w.interval, DefaultDriftWatcherInterval)
	}
	if w.window != DefaultDriftWatcherWindow {
		t.Fatalf("window = %d want %d", w.window, DefaultDriftWatcherWindow)
	}
	if _, ok := w.clk.(clock.Real); !ok {
		t.Fatalf("clock = %T want clock.Real", w.clk)
	}
}

func TestDriftWatcher_RejectsMissingValidator(t *testing.T) {
	t.Parallel()
	_, err := NewDriftWatcher(DriftWatcherConfig{})
	if !errors.Is(err, ErrDriftWatcherInvalidConfig) {
		t.Fatalf("NewDriftWatcher err = %v want ErrDriftWatcherInvalidConfig", err)
	}
}

func TestDriftWatcher_TickRunsValidate(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	v := newWatcherValidator()
	close(v.release)
	w, err := NewDriftWatcher(DriftWatcherConfig{
		Validator: v,
		Clock:     clk,
		Interval:  time.Second,
		Window:    7,
	})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runWatcher(t, w, ctx)

	advanceUntil(t, clk, time.Second, func() bool { return v.callCount() >= 1 }, "first validate call")
	waitSignal(t, v.started, "validate start")
	waitSignal(t, v.done, "validate done")
	if got := v.callCount(); got != 1 {
		t.Fatalf("Validate calls = %d want 1", got)
	}
	if got := v.windowAt(0); got != 7 {
		t.Fatalf("Validate window = %d want 7", got)
	}
	cancel()
	waitSignal(t, runDone, "watcher stop")
}

func TestDriftWatcher_EmitsFindingsThroughExistingDrift(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	em := newWatcherEmitter()
	drift := NewDrift(&fakeCommitSource{commits: []Commit{{
		SHA:     "bad",
		Subject: "not conventional",
	}}}, em)
	w, err := NewDriftWatcher(DriftWatcherConfig{
		Validator: drift,
		Clock:     clk,
		Interval:  time.Second,
		Window:    1,
	})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runWatcher(t, w, ctx)

	advanceUntil(t, clk, time.Second, func() bool { return em.count() == 1 }, "drift event emitted")
	events := em.snapshot()
	if events[0].Type != EventSubstrateDriftDetected {
		t.Fatalf("event type = %s want %s", events[0].Type, EventSubstrateDriftDetected)
	}
	cancel()
	waitSignal(t, runDone, "watcher stop")
}

func TestDriftWatcher_ValidateErrorDoesNotExit(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	v := newWatcherValidator()
	v.errs = []error{errors.New("git log unavailable")}
	close(v.release)
	w, err := NewDriftWatcher(DriftWatcherConfig{
		Validator: v,
		Clock:     clk,
		Interval:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runWatcher(t, w, ctx)

	advanceUntil(t, clk, time.Second, func() bool { return v.callCount() >= 1 }, "first validate call")
	waitSignal(t, v.started, "first validate start")
	waitSignal(t, v.done, "first validate done")
	advanceUntil(t, clk, time.Second, func() bool { return v.callCount() >= 2 }, "second validate call")
	waitSignal(t, v.started, "second validate start")
	waitSignal(t, v.done, "second validate done")
	if got := v.callCount(); got != 2 {
		t.Fatalf("Validate calls = %d want 2", got)
	}
	cancel()
	waitSignal(t, runDone, "watcher stop")
}

func TestDriftWatcher_SkipsOverlappingValidation(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	v := newWatcherValidator()
	w, err := NewDriftWatcher(DriftWatcherConfig{
		Validator: v,
		Clock:     clk,
		Interval:  time.Second,
	})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runWatcher(t, w, ctx)

	advanceUntil(t, clk, time.Second, func() bool { return v.callCount() >= 1 }, "first validate call")
	waitSignal(t, v.started, "first validate start")
	clk.Advance(5 * time.Second)
	if got := v.callCount(); got != 1 {
		t.Fatalf("Validate calls during overlap = %d want 1", got)
	}
	close(v.release)
	waitSignal(t, v.done, "first validate done")
	advanceUntil(t, clk, time.Second, func() bool { return v.callCount() >= 2 }, "second validate call")
	waitSignal(t, v.started, "second validate start")
	waitSignal(t, v.done, "second validate done")
	if got := v.callCount(); got != 2 {
		t.Fatalf("Validate calls after release = %d want 2", got)
	}
	cancel()
	waitSignal(t, runDone, "watcher stop")
}

func TestDriftWatcher_CancelStopsWithinOneTick(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Unix(0, 0))
	v := newWatcherValidator()
	close(v.release)
	w, err := NewDriftWatcher(DriftWatcherConfig{
		Validator: v,
		Clock:     clk,
		Interval:  time.Minute,
	})
	if err != nil {
		t.Fatalf("NewDriftWatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := runWatcher(t, w, ctx)

	cancel()
	waitSignal(t, runDone, "watcher stop")
	if got := v.callCount(); got != 0 {
		t.Fatalf("Validate calls after immediate cancel = %d want 0", got)
	}
}

func runWatcher(t *testing.T, w *DriftWatcher, ctx context.Context) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()
	return done
}

type watcherEmitter struct {
	mu     sync.Mutex
	events []Event
}

func newWatcherEmitter() *watcherEmitter { return &watcherEmitter{} }

func (e *watcherEmitter) Emit(_ context.Context, ev Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

func (e *watcherEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

func (e *watcherEmitter) snapshot() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Event, len(e.events))
	copy(out, e.events)
	return out
}

func advanceUntil(t *testing.T, clk *clock.Fake, step time.Duration, pred func() bool, label string) {
	t.Helper()
	deadline := time.After(time.Second)
	for !pred() {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for %s", label)
		default:
			clk.Advance(step)
			clk.BlockUntilCondition(pred, 10*time.Millisecond)
		}
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s", label)
	}
}
