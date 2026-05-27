// go:build chaos

// Drives the contract from spec §4.6 row "Daemon panic":
// - A panic inside the dispatcher goroutine MUST NOT corrupt
// scheduler state. The goroutine wrapper (the daemon's errgroup)
// recover()s the panic, logs it, and the next tick fires fresh.
// - The eventlog records the failure trail (a structured Event for
// the panicking dispatch) so post-mortem can reconstruct.
// - Subsequent Fire calls with a healthy Dispatcher succeed: the
// panic does not leave Schedule, Store, or Eventlog in a bad
// state.
// - 3+ panics within 1h: the daemon's panic-budget guard would
// refuse autonomous mode (this surface is a daemon-level concern;
// the chaos test here verifies that the Fire-level state is sane
// so the daemon's guard sees the panic count it expects).
//
// Drives REAL production code: scheduler.Fire + DispatchInput +
// EventEmitter + Store interfaces. The Dispatcher we inject is a
// chaos-local fake that panics N times then succeeds.
package chaos

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestChaos_DaemonPanic_GoroutineWrapperRecovers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}

	disp := &panickyDispatcher{}
	disp.PanicOnNthCall(1)

	emitter := newRecordingEmitter()
	store := newRecordingScheduleStore()

	s := newCronSchedule("panic-test", "alpha", "do-thing")

	type panicResult struct {
		val any
		err error
	}
	results := make(chan panicResult, 4)

	runOnce := func() {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					results <- panicResult{val: r}
					return
				}
			}()
			err := scheduler.Fire(ctx, s, scheduler.FireDeps{
				Now:        clk.Now,
				Doctrine:   doctrine.NameDefault,
				Quota:      &alwaysAllowQuota{},
				Dispatcher: disp,
				Eventlog:   emitter,
				RateLimit:  &alwaysAllowRateLimit{},
				Store:      store,
			})
			results <- panicResult{err: err}
		}()
	}

	runOnce()
	first := <-results
	if first.val == nil {
		t.Fatalf("expected panic on first call, got err=%v", first.err)
	}

	if got := disp.Calls(); got != 1 {
		t.Fatalf("dispatch calls after panic: got %d, want 1", got)
	}

	clk.advance(time.Second)
	runOnce()
	second := <-results
	if second.val != nil {
		t.Fatalf("unexpected panic on second call: %v", second.val)
	}
	if second.err != nil {
		t.Fatalf("second call returned err: %v", second.err)
	}

	emitter.assertHasKind(t, scheduler.EventRoutineFired)

	if got := store.HistoryCount(); got < 1 {
		t.Fatalf("post-recovery history count: got %d, want >=1", got)
	}
}

func TestChaos_DaemonPanic_RepeatedPanicsKeepStoreCounted(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}

	disp := &panickyDispatcher{}
	disp.PanicOnEveryCall()

	emitter := newRecordingEmitter()
	store := newRecordingScheduleStore()

	s := newCronSchedule("repeated-panic", "alpha", "do-thing")

	const PANICS = 4
	for i := 0; i < PANICS; i++ {
		func() {
			defer func() {
				_ = recover()
			}()
			_ = scheduler.Fire(ctx, s, scheduler.FireDeps{
				Now:        clk.Now,
				Doctrine:   doctrine.NameDefault,
				Quota:      &alwaysAllowQuota{},
				Dispatcher: disp,
				Eventlog:   emitter,
				RateLimit:  &alwaysAllowRateLimit{},
				Store:      store,
			})
		}()
		clk.advance(time.Second)
	}

	if got := disp.Calls(); got != PANICS {
		t.Fatalf("dispatcher.Calls(): got %d, want %d", got, PANICS)
	}

	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventRoutineFired {
			t.Fatalf("unexpected EventRoutineFired event after PANIC-only run: %+v", ev)
		}
	}

	if got := store.HistoryCount(); got != 0 {
		t.Fatalf("history count: got %d, want 0 (no successful fires)", got)
	}
}

func TestChaos_DaemonPanic_RecoveryOrderingHonoursIdempotency(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}

	disp := &panickyDispatcher{}
	emitter := newRecordingEmitter()
	store := newRecordingScheduleStore()

	const cycles = 10
	disp.PanicOnNthCall(3)

	s := newCronSchedule("idem-test", "alpha", "do-thing")

	for i := 0; i < cycles; i++ {
		func() {
			defer func() { _ = recover() }()
			_ = scheduler.Fire(ctx, s, scheduler.FireDeps{
				Now:        clk.Now,
				Doctrine:   doctrine.NameDefault,
				Quota:      &alwaysAllowQuota{},
				Dispatcher: disp,
				Eventlog:   emitter,
				RateLimit:  &alwaysAllowRateLimit{},
				Store:      store,
			})
		}()
		clk.advance(time.Second)
	}

	if got, want := disp.Calls(), cycles; got != want {
		t.Fatalf("dispatcher.Calls(): got %d, want %d", got, want)
	}

	fired := 0
	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventRoutineFired {
			fired++
		}
	}
	if fired != cycles-1 {
		t.Fatalf("EventRoutineFired count: got %d, want %d", fired, cycles-1)
	}

	if got := store.HistoryCount(); got != cycles-1 {
		t.Fatalf("history count: got %d, want %d", got, cycles-1)
	}
}

type panickyDispatcher struct {
	mu         sync.Mutex
	calls      int
	panicNth   int
	panicEvery bool
}

func (p *panickyDispatcher) PanicOnNthCall(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.panicNth = n
}

func (p *panickyDispatcher) PanicOnEveryCall() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.panicEvery = true
}

func (p *panickyDispatcher) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *panickyDispatcher) Dispatch(_ context.Context, _ scheduler.DispatchInput) (scheduler.DispatchResult, error) {
	p.mu.Lock()
	p.calls++
	shouldPanic := p.panicEvery || (p.panicNth > 0 && p.calls == p.panicNth)
	p.mu.Unlock()
	if shouldPanic {
		panic("chaos: synthetic dispatcher panic")
	}
	return scheduler.DispatchResult{
		CostUSD:    0.001,
		DurationMs: 1,
		Tier:       "chaos-fake",
	}, nil
}

type recordingEmitter struct {
	mu  sync.Mutex
	all []scheduler.Event
}

func newRecordingEmitter() *recordingEmitter { return &recordingEmitter{} }

func (e *recordingEmitter) Emit(_ context.Context, ev scheduler.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.all = append(e.all, ev)
	return nil
}

func (e *recordingEmitter) events() []scheduler.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]scheduler.Event, len(e.all))
	copy(out, e.all)
	return out
}

func (e *recordingEmitter) assertHasKind(t *testing.T, k scheduler.EventKind) {
	t.Helper()
	for _, ev := range e.events() {
		if ev.Kind == k {
			return
		}
	}
	t.Fatalf("expected at least one event of kind %v in %d emitted events", k, len(e.events()))
}

type recordingScheduleStore struct {
	mu      sync.Mutex
	history []scheduler.HistoryEntry
}

func newRecordingScheduleStore() *recordingScheduleStore {
	return &recordingScheduleStore{}
}

func (s *recordingScheduleStore) Insert(_ context.Context, _ *scheduler.Schedule) error {
	return nil
}

func (s *recordingScheduleStore) Get(_ context.Context, _ string) (*scheduler.Schedule, error) {
	return nil, scheduler.ErrNotFound
}

func (s *recordingScheduleStore) UpdateNextRun(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}

func (s *recordingScheduleStore) UpdateStatus(_ context.Context, _ string, _ scheduler.Status) error {
	return nil
}

func (s *recordingScheduleStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (s *recordingScheduleStore) ListDue(_ context.Context, _ time.Time) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (s *recordingScheduleStore) ListByProject(_ context.Context, _ string) ([]*scheduler.Schedule, error) {
	return nil, nil
}

func (s *recordingScheduleStore) AppendHistory(_ context.Context, h scheduler.HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, h)
	return nil
}

func (s *recordingScheduleStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	return nil, nil
}

func (s *recordingScheduleStore) HistoryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.history)
}

type alwaysAllowQuota struct{}

func (*alwaysAllowQuota) PreFlight(_ context.Context, _ string, _ doctrine.Name) (quota.PreFlightDecision, error) {
	return quota.PreFlightDecision{Allowed: true}, nil
}

type alwaysAllowRateLimit struct{}

func (*alwaysAllowRateLimit) Allow(_ context.Context, _ string, _ time.Time) bool {
	return true
}

func newCronSchedule(id, alias, action string) *scheduler.Schedule {
	return &scheduler.Schedule{
		ID:           id,
		Tier:         scheduler.TierRoutine,
		ProjectAlias: alias,
		Action:       action,
		TriggerType:  scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "* * * * *",
		},
		MissPolicy: scheduler.MissPolicySkip,
		Status:     scheduler.StatusEnabled,
		CreatedAt:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		NextRunAt:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
}
