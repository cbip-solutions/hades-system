package scheduler_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

// fakeSession is a deterministic stand-in for Phase C's
// `tmuxlife.Session`. It satisfies `scheduler.LoopBinding` (the local
// interface declared in loop.go) and exposes a Kill() method that
// closes the Done channel — the canonical "session died" trigger that
// Loop.Bind listens for.
//
// Why a fake (not a real tmuxlife.Session): Phase D's loop.go MUST NOT
// import internal/tmuxlife/ (interface segregation; inv-zen-031 spirit
// — keep scheduler decoupled from process orchestration). Tests
// therefore use an in-package fake, which both proves the abstraction
// is sound (any LoopBinding works) and removes the test from the tmux
// integration surface (no os.Exec, no goroutine race against tmux).
type fakeSession struct {
	id   string
	done chan struct{}
	mu   sync.Mutex
	dead bool
}

func newFakeSession(id string) *fakeSession {
	return &fakeSession{id: id, done: make(chan struct{})}
}

func (f *fakeSession) ID() string { return f.id }

func (f *fakeSession) Done() <-chan struct{} { return f.done }

// Kill closes the Done channel idempotently — calling Kill twice MUST
// NOT panic, mirroring the contract a real `tmuxlife.Session` would
// honour (its Done() is closed exactly once on session teardown, but
// callers may race with their own context cancellation).
func (f *fakeSession) Kill() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return
	}
	f.dead = true
	close(f.done)
}

func validLoopParams() scheduler.LoopParams {
	return scheduler.LoopParams{
		ID:           "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		ProjectAlias: "internal-platform-x",
		Action:       "watch-inbox",
		Interval:     5 * time.Minute,
	}
}

func TestNewLoop_HappyPath(t *testing.T) {
	p := validLoopParams()
	loop, err := scheduler.NewLoop(p)
	if err != nil {
		t.Fatalf("NewLoop: %v", err)
	}
	if loop == nil {
		t.Fatal("NewLoop returned nil *Loop with nil error")
	}
	if got := loop.Interval(); got != 5*time.Minute {
		t.Errorf("Interval = %v, want 5min", got)
	}
	if got := loop.SessionID(); got != "" {
		t.Errorf("SessionID before Bind = %q, want empty", got)
	}

	if got := loop.ID(); got != p.ID {
		t.Errorf("ID = %q, want %q", got, p.ID)
	}
	if got := loop.ProjectAlias(); got != p.ProjectAlias {
		t.Errorf("ProjectAlias = %q, want %q", got, p.ProjectAlias)
	}
	if got := loop.Action(); got != p.Action {
		t.Errorf("Action = %q, want %q", got, p.Action)
	}

	select {
	case <-loop.Done():
		t.Errorf("Done() closed before Bind/Stop/session-death")
	default:

	}
}

func TestLoop_BindsToSession(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if got := loop.SessionID(); got != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", got)
	}
}

func TestLoop_DiesWithSession(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatal(err)
	}

	sess.Kill()
	select {
	case <-loop.Done():

	case <-time.After(time.Second):
		t.Errorf("loop.Done() did not fire after session.Kill(); the watcher goroutine is wedged")
	}
}

func TestLoop_StopCancels(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatal(err)
	}
	loop.Stop()
	select {
	case <-loop.Done():

	case <-time.After(time.Second):
		t.Errorf("loop.Done() did not fire after Stop(); the watcher goroutine is wedged")
	}
}

func TestLoop_ParentCtxCancelDies(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-loop.Done():

	case <-time.After(time.Second):
		t.Errorf("loop.Done() did not fire after parent ctx cancel")
	}
}

// TestLoop_SetInterval verifies the dynamic-interval contract: the
// operator may raise/lower the polling interval without restarting the
// loop (per spec line 2995). The setter MUST land atomically so a
// concurrent reader sees either the old or new value, never a torn
// duration.
func TestLoop_SetInterval(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.SetInterval(10 * time.Minute); err != nil {
		t.Fatalf("SetInterval: %v", err)
	}
	if got := loop.Interval(); got != 10*time.Minute {
		t.Errorf("Interval after SetInterval = %v, want 10min", got)
	}

	if err := loop.SetInterval(2 * time.Minute); err != nil {
		t.Fatalf("SetInterval(lower): %v", err)
	}
	if got := loop.Interval(); got != 2*time.Minute {
		t.Errorf("Interval after second SetInterval = %v, want 2min", got)
	}
}

func TestLoop_SetIntervalRejectsSubMinute(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	err = loop.SetInterval(30 * time.Second)
	if err == nil {
		t.Fatal("SetInterval(30s) = nil error, want floor violation")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("SetInterval(30s) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}

	if got := loop.Interval(); got != 5*time.Minute {
		t.Errorf("Interval after rejected SetInterval = %v, want 5min (unchanged)", got)
	}
}

func TestNewLoop_RejectsSubMinuteInterval(t *testing.T) {
	cases := []struct {
		name     string
		interval time.Duration
	}{
		{"30s", 30 * time.Second},
		{"1s", 1 * time.Second},
		{"zero", 0},
		{"negative", -time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validLoopParams()
			p.Interval = tc.interval
			_, err := scheduler.NewLoop(p)
			if err == nil {
				t.Fatalf("NewLoop(%v) = nil error, want floor violation", tc.interval)
			}
			if !errors.Is(err, scheduler.ErrInvalidSchedule) {
				t.Errorf("NewLoop(%v) error = %v, want errors.Is(ErrInvalidSchedule)",
					tc.interval, err)
			}
		})
	}
}

func TestNewLoop_RejectsEmptyFields(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(p *scheduler.LoopParams)
		match string
	}{
		{"empty ID", func(p *scheduler.LoopParams) { p.ID = "" }, "ID"},
		{"empty ProjectAlias", func(p *scheduler.LoopParams) { p.ProjectAlias = "" }, "ProjectAlias"},
		{"empty Action", func(p *scheduler.LoopParams) { p.Action = "" }, "Action"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validLoopParams()
			tc.mut(&p)
			_, err := scheduler.NewLoop(p)
			if err == nil {
				t.Fatalf("NewLoop(%v) = nil error, want ErrInvalidSchedule", tc.name)
			}
			if !errors.Is(err, scheduler.ErrInvalidSchedule) {
				t.Errorf("NewLoop(%v) error = %v, want errors.Is(ErrInvalidSchedule)",
					tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Errorf("NewLoop(%v) error = %q, want substring %q",
					tc.name, err.Error(), tc.match)
			}
		})
	}
}

func TestLoop_BindRejectsNil(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	err = loop.Bind(context.Background(), nil)
	if err == nil {
		t.Fatal("Bind(nil) = nil error, want ErrInvalidSchedule")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Bind(nil) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}
}

func TestLoop_BindRejectsDoubleBind(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess1 := newFakeSession("sess-1")
	sess2 := newFakeSession("sess-2")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess1); err != nil {
		t.Fatalf("Bind(1): %v", err)
	}
	err = loop.Bind(ctx, sess2)
	if err == nil {
		t.Fatal("Bind(2) = nil error, want already-bound rejection")
	}

	if got := loop.SessionID(); got != "sess-1" {
		t.Errorf("SessionID after rejected re-bind = %q, want sess-1 (unchanged)", got)
	}
}

func TestLoop_StopBeforeBind(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop() before Bind panicked: %v", r)
		}
	}()
	loop.Stop()
}

func TestLoop_StopIdempotent(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatal(err)
	}
	loop.Stop()
	loop.Stop()
	loop.Stop()
	select {
	case <-loop.Done():

	case <-time.After(time.Second):
		t.Errorf("Done() did not fire after Stop()")
	}
}

func TestLoop_DoubleSessionDeath_NoPanic(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	sess := newFakeSession("sess-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop.Bind(ctx, sess); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("racing cancel paths panicked: %v", r)
		}
	}()

	go sess.Kill()
	go loop.Stop()
	select {
	case <-loop.Done():

	case <-time.After(2 * time.Second):
		t.Errorf("Done() did not fire after racing cancellations")
	}
}

func TestLoop_ConcurrentSetIntervalAndRead(t *testing.T) {
	loop, err := scheduler.NewLoop(validLoopParams())
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 8
	const iters = 500
	var wg sync.WaitGroup
	wg.Add(goroutines)
	stop := make(chan struct{})

	for i := 0; i < goroutines/2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = loop.SetInterval(time.Duration(2+j%30) * time.Minute)
			}
		}()
	}
	for i := 0; i < goroutines/2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				select {
				case <-stop:
					return
				default:
				}
				d := loop.Interval()
				if d < time.Minute {
					t.Errorf("Interval() = %v < 1min floor (torn read?)", d)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
}

var _ scheduler.LoopBinding = (*fakeSession)(nil)
