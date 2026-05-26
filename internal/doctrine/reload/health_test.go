package reload_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/parser"
	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
	v1 "github.com/cbip-solutions/hades-system/internal/doctrine/schema/v1"
)

func waitForEvent(t *testing.T, evlog *fakeEventlog, want func(any) bool, deadline time.Duration) int {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		count := 0
		for _, ev := range evlog.Snapshot() {
			if want(ev) {
				count++
			}
		}
		if count > 0 {
			return count
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0
}

type stallControlClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStallControlClock(t time.Time) *stallControlClock {
	return &stallControlClock{now: t}
}

func (c *stallControlClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stallControlClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestHealth_StallDetectionEmitsEvent(t *testing.T) {
	clk := newStallControlClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
		StallTimeout:   100 * time.Millisecond,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(150 * time.Millisecond)
	clk.Advance(200 * time.Millisecond)

	got := waitForEvent(t, evlog, func(ev any) bool {
		_, ok := ev.(reload.DoctrineWatcherStalled)
		return ok
	}, 1*time.Second)
	if got == 0 {
		t.Errorf("expected at least one DoctrineWatcherStalled emit; got 0")
	}
}

func TestHealth_StallTriggersRestart(t *testing.T) {
	clk := newStallControlClock(time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	path := filepath.Join(dir, "max-scope.toml")
	if err := os.WriteFile(path, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser:         &fakeParser{},
		StallTimeout:   100 * time.Millisecond,
		Clock:          clk,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(path, ""); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()

	time.Sleep(150 * time.Millisecond)
	clk.Advance(200 * time.Millisecond)

	stalled := waitForEvent(t, evlog, func(ev any) bool {
		_, ok := ev.(reload.DoctrineWatcherStalled)
		return ok
	}, 1*time.Second)
	restarted := waitForEvent(t, evlog, func(ev any) bool {
		_, ok := ev.(reload.DoctrineWatcherRestarted)
		return ok
	}, 1*time.Second)
	if stalled == 0 || restarted == 0 {
		t.Errorf("stalled=%d restarted=%d; want both >0 (events: %#v)",
			stalled, restarted, evlog.Snapshot())
	}
}

func TestHealth_OverflowEmitsForceReload(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.toml")
	pathB := filepath.Join(dir, "b.toml")
	for _, p := range []string{pathA, pathB} {
		if err := os.WriteFile(p, []byte(`x`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	evlog := &fakeEventlog{}
	parsedMu := sync.Mutex{}
	parsed := map[string]int{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser: &fakeParser{parseFn: func(_ []byte, source string, _ *v1.Schema, _ parser.ParseOpts) error {
			parsedMu.Lock()
			parsed[source]++
			parsedMu.Unlock()
			return errors.New("synthetic parse fail in overflow test")
		}},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(pathA, ""); err != nil {
		t.Fatal(err)
	}
	if err := w.AddPath(pathB, ""); err != nil {
		t.Fatal(err)
	}
	w.HandleOverflowForTest(context.Background(), errors.New("EWATCHQUEUEOVERFLOW"))

	parsedMu.Lock()
	gotA := parsed[pathA]
	gotB := parsed[pathB]
	parsedMu.Unlock()
	if gotA != 1 || gotB != 1 {
		t.Errorf("parses A=%d B=%d; want 1 each (force-reload-all)", gotA, gotB)
	}
	overflow := 0
	for _, ev := range evlog.Snapshot() {
		if _, ok := ev.(reload.DoctrineWatcherOverflow); ok {
			overflow++
		}
	}
	if overflow != 1 {
		t.Errorf("DoctrineWatcherOverflow count = %d; want 1", overflow)
	}
}

func TestHealth_OverflowSignalsRestartNeeded(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.toml")
	if err := os.WriteFile(pathA, []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	evlog := &fakeEventlog{}
	w, err := reload.New(reload.WatcherOpts{
		EventlogClient: evlog,
		ActiveAccessor: &recordingActive{},
		Parser: &fakeParser{parseFn: func(_ []byte, _ string, _ *v1.Schema, _ parser.ParseOpts) error {
			return errors.New("synthetic parse fail in restart-signal test")
		}},
		Validator:        &fakeValidator{},
		BaselineProvider: fakeBaselineProvider{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	if err := w.AddPath(pathA, ""); err != nil {
		t.Fatal(err)
	}

	if w.RestartNeededSignalForTest() {
		t.Fatal("pre-condition violated: restartNeeded already signaled before handleOverflow")
	}

	w.HandleOverflowForTest(context.Background(), errors.New("EWATCHQUEUEOVERFLOW"))

	// Post-condition: restartNeeded MUST be signaled so the Start loop
	// performs performRestart, swapping the fsnotify watcher (and thus
	// the kernel queue) for a fresh one — closing the recurrence vector
	// where the saturated queue would immediately re-overflow.
	if !w.RestartNeededSignalForTest() {
		t.Errorf("inv-zen-291 violated: handleOverflow did not signal restartNeeded; the fsnotify watcher stays bound to the saturated kernel queue and a follow-on burst will immediately re-overflow in a tight loop")
	}
}
