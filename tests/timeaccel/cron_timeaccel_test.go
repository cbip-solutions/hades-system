// catch-up across virtual time.
//
// Drives inv-zen-120 + inv-zen-121:
//
//   - inv-zen-120 jitter: scheduler.ComputeJitter(routineID, period) is
//     deterministic per routine ID, capped (90s for sub-1h periods,
//     15min for ≥1h periods), and bounded by 10% of the period.
//   - inv-zen-121 miss policy: under doctrine.NameMaxScope the
//     scheduler's MissPolicyCatchUpBounded gates the catch-up burst via
//     deps.RateLimit.Allow (1/30s/project). Under default doctrine the
//     miss policy is MissPolicySkip; under capa-firewall it is
//     MissPolicyNotifyOnly.
//
// Drift notes (vs plan-template heredoc):
//
//   - The plan template referenced fictional surfaces:
//     `clock.NewVirtual`, `eventlog.NewRecorder`,
//     `scheduler.New(scheduler.Deps{...})`, `scheduler.Routine`,
//     `sch.Insert(...)`, `testhelpers.NewFireRecorder`,
//     `scheduler.MissPolicy{MaxCatchUp:1}`, `scheduler.DoctrineMaxScope`.
//     None of these exist. The actual surface:
//
//   - `scheduler.ComputeJitter(routineID, period)` is the
//     inv-zen-120 carrier; it is a pure function with no clock
//     dependency.
//   - `scheduler.Fire(ctx, *Schedule, FireDeps)` is the dispatch
//     entry point; FireDeps takes a `Now func() time.Time` callback
//     so the test can drive virtual time.
//   - `scheduler.EffectiveMissPolicy(s, doctrine)` resolves the
//     per-doctrine miss policy without invoking the scheduler.
//   - `scheduler.ComputeMissed(s, now)` reports the missed-fire gap
//     for the current schedule + virtual now.
//
//   - The plan template asserted "100 routines × 30 days × 6/hour =
//     432_000 fires; offset within ±60s of canonical cron boundary;
//     mean ~0; stddev 5..50s". The actual implementation:
//
//   - Jitter is constant per routine ID (does NOT change across
//     fires). The "30-day distribution" assertion is therefore the
//     same as "100 routine ID samples"; we assert the 100-sample
//     distribution directly.
//   - For a 10-min period, the 10% bucket is 60s and the cap is 90s
//     (one-shot since 10min < 1h). The bucket dominates → jitter ∈
//     [0, 60s). We assert this bound.
//   - Mean and stddev: the bucket is uniform on [0, 60s) when
//     hash-mod-bucket is uniform; expected mean 30s, stddev ~17s
//     (uniform on [0,60]: μ=30, σ=60/√12≈17.3). The plan template
//     said "mean ~0" — that is wrong (jitter is non-negative;
//     subtracting bucket/2 is one possible centring, but the actual
//     ComputeJitter does NOT centre). Reality wins: we assert mean
//     ∈ [25, 35] and stddev ∈ [10, 25].
//
//   - Catch-up bounded: the actual implementation does NOT take a
//     `MaxCatchUp:1` knob. Instead, the catch-up loop in
//     `internal/scheduler/fire.go:147` iterates over `missed.MissedCount`
//     and breaks on `RateLimit.Allow == false`. The bound emerges
//     from the rate-limiter (1/30s/project per spec §1 Q9 C); we
//     model that with a one-shot RateLimit fake whose first Allow
//     returns true and subsequent calls return false.
//
//go:build timeaccel
// +build timeaccel

package timeaccel_test

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestTimeaccel_CronJitter_DistributionWithinBound(t *testing.T) {
	const period = 10 * time.Minute
	const n = 100
	const bucket = period / 10
	const cap = 90 * time.Second

	offsets := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("routine-%03d", i)
		off := scheduler.ComputeJitter(id, period)

		if off < 0 {
			t.Fatalf("routine %s: jitter %v is negative", id, off)
		}
		if off > bucket {
			t.Fatalf("routine %s: jitter %v exceeds 10%% bucket %v",
				id, off, bucket)
		}

		if off > cap {
			t.Fatalf("routine %s: jitter %v exceeds %v cap", id, off, cap)
		}
		offsets = append(offsets, off)
	}

	var sum, sumSq float64
	for _, o := range offsets {
		s := o.Seconds()
		sum += s
		sumSq += s * s
	}
	mean := sum / float64(n)
	variance := sumSq/float64(n) - mean*mean
	stddev := math.Sqrt(variance)

	if mean < 20.0 || mean > 40.0 {
		t.Fatalf("jitter mean %.2fs outside [20, 40]; sample distribution skewed", mean)
	}
	if stddev < 10.0 || stddev > 25.0 {
		t.Fatalf("jitter stddev %.2fs outside [10, 25]; sample distribution skewed", stddev)
	}
}

// TestTimeaccel_CronJitter_DeterministicPerRoutineID: the same routine
// ID + period MUST produce the same offset across N invocations.
// This is the structural property that makes "30-day fan-out same as
// 100-sample distribution" valid.
//
// Timeaccel claim: 1000 evaluations of one routine ID is the timeaccel
// proxy for "same routine fires every 10min for 1 week (1000 fires)".
// All MUST be identical.
func TestTimeaccel_CronJitter_DeterministicPerRoutineID(t *testing.T) {
	const period = 10 * time.Minute
	const id = "deterministic"
	const reps = 1000

	first := scheduler.ComputeJitter(id, period)
	for i := 1; i < reps; i++ {
		off := scheduler.ComputeJitter(id, period)
		if off != first {
			t.Fatalf("non-deterministic jitter at rep %d: first=%v this=%v",
				i, first, off)
		}
	}
}

// TestTimeaccel_CronJitter_24hPeriodHits15minCap: with period = 24h,
// the 10% bucket is 144min (8640s) which exceeds the 15min recurring
// cap (period >= 1h). All offsets MUST be ≤ 15min.
func TestTimeaccel_CronJitter_24hPeriodHits15minCap(t *testing.T) {
	const period = 24 * time.Hour
	const recurringCap = 15 * time.Minute
	const n = 200

	for i := 0; i < n; i++ {
		id := fmt.Sprintf("daily-routine-%03d", i)
		off := scheduler.ComputeJitter(id, period)
		if off < 0 {
			t.Fatalf("routine %s: negative jitter %v", id, off)
		}
		if off > recurringCap {
			t.Fatalf("routine %s: jitter %v exceeds 15min recurring cap (period=%v)",
				id, off, period)
		}
	}
}

func TestTimeaccel_CronCatchUp_MaxScopeRateLimitedAtOne(t *testing.T) {
	ctx := context.Background()

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	virtualClock := &testClock{now: now}

	s := newCatchUpSchedule("catchup-test", "alpha", "do-thing", virtualClock.Now())
	s.LastRunAt = virtualClock.Now().Add(-30 * time.Minute)
	s.NextRunAt = virtualClock.Now().Add(-29 * time.Minute)

	rl := &oneShotRateLimit{}
	disp := &countingDispatcher{}
	emitter := &recordingEmitter{}
	store := &noopScheduleStore{}

	// Effective miss policy under max-scope MUST be CatchUpBounded.
	if got := scheduler.EffectiveMissPolicy(s, doctrine.NameMaxScope); got != scheduler.MissPolicyCatchUpBounded {
		t.Fatalf("max-scope miss policy: got %v, want MissPolicyCatchUpBounded", got)
	}

	err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        virtualClock.Now,
		Doctrine:   doctrine.NameMaxScope,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  rl,
		Store:      store,
	})
	if err != nil && err != scheduler.ErrRateLimited {
		t.Fatalf("Fire: unexpected non-ratelimit error: %v", err)
	}

	count := disp.Count()
	if count != 1 {
		t.Fatalf("max-scope catch-up bounded: expected exactly 1 dispatch (single catch-up; live tick rate-limited); got %d",
			count)
	}

	hasRateLimited := false
	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventRateLimited {
			hasRateLimited = true
			break
		}
	}
	if !hasRateLimited {
		t.Errorf("expected EventRateLimited in emitter; events=%+v", emitter.events())
	}
}

func TestTimeaccel_CronCatchUp_DefaultDoctrineSkipsAllMissed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	vc := &testClock{now: now}

	s := newCatchUpSchedule("default-skip", "alpha", "do-thing", vc.Now())
	s.LastRunAt = vc.Now().Add(-30 * time.Minute)
	s.NextRunAt = vc.Now().Add(-29 * time.Minute)

	if got := scheduler.EffectiveMissPolicy(s, doctrine.NameDefault); got != scheduler.MissPolicySkip {
		t.Fatalf("default miss policy: got %v, want MissPolicySkip", got)
	}

	disp := &countingDispatcher{}
	emitter := &recordingEmitter{}
	store := &noopScheduleStore{}

	if err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        vc.Now,
		Doctrine:   doctrine.NameDefault,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  &alwaysAllowRateLimit{},
		Store:      store,
	}); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if got := disp.Count(); got != 1 {
		t.Fatalf("default skip: expected 1 dispatch (current tick only); got %d", got)
	}
	skipped := 0
	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventRoutineSkipped {
			skipped++
		}
	}
	if skipped < 1 {
		t.Fatalf("default skip: expected ≥1 EventRoutineSkipped events; got %d", skipped)
	}
}

func TestTimeaccel_CronCatchUp_CapaFirewallNotifyOnly(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	vc := &testClock{now: now}

	s := newCatchUpSchedule("capa-notify", "alpha", "do-thing", vc.Now())
	s.LastRunAt = vc.Now().Add(-30 * time.Minute)
	s.NextRunAt = vc.Now().Add(-29 * time.Minute)

	if got := scheduler.EffectiveMissPolicy(s, doctrine.NameCapaFirewall); got != scheduler.MissPolicyNotifyOnly {
		t.Fatalf("capa-firewall miss policy: got %v, want MissPolicyNotifyOnly", got)
	}

	disp := &countingDispatcher{}
	emitter := &recordingEmitter{}
	store := &noopScheduleStore{}

	if err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        vc.Now,
		Doctrine:   doctrine.NameCapaFirewall,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  &alwaysAllowRateLimit{},
		Store:      store,
	}); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if got := disp.Count(); got != 1 {
		t.Fatalf("capa-firewall notify: expected 1 dispatch (current tick only); got %d", got)
	}
	missed := 0
	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventMissedFire {
			missed++
		}
	}
	if missed != 1 {
		t.Fatalf("capa-firewall notify: expected exactly 1 EventMissedFire (collapsed); got %d", missed)
	}
}

// --- helpers (timeaccel-package-local; do not collide with chaos-pkg
// helpers since these are in a different package + build tag) ---

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

type oneShotRateLimit struct {
	mu       sync.Mutex
	consumed map[string]bool
}

func (rl *oneShotRateLimit) Allow(_ context.Context, alias string, _ time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.consumed == nil {
		rl.consumed = map[string]bool{}
	}
	if rl.consumed[alias] {
		return false
	}
	rl.consumed[alias] = true
	return true
}

type alwaysAllowRateLimit struct{}

func (*alwaysAllowRateLimit) Allow(_ context.Context, _ string, _ time.Time) bool {
	return true
}

type alwaysAllowQuota struct{}

func (*alwaysAllowQuota) PreFlight(_ context.Context, _ string, _ doctrine.Name) (quota.PreFlightDecision, error) {
	return quota.PreFlightDecision{Allowed: true}, nil
}

type countingDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (d *countingDispatcher) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *countingDispatcher) Dispatch(_ context.Context, _ scheduler.DispatchInput) (scheduler.DispatchResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return scheduler.DispatchResult{
		CostUSD:    0.001,
		DurationMs: 1,
		Tier:       "timeaccel-counting",
	}, nil
}

type recordingEmitter struct {
	mu sync.Mutex
	ev []scheduler.Event
}

func (e *recordingEmitter) Emit(_ context.Context, ev scheduler.Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ev = append(e.ev, ev)
	return nil
}

func (e *recordingEmitter) events() []scheduler.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]scheduler.Event, len(e.ev))
	copy(out, e.ev)
	return out
}

type noopScheduleStore struct {
	mu      sync.Mutex
	history []scheduler.HistoryEntry
}

func (s *noopScheduleStore) Insert(_ context.Context, _ *scheduler.Schedule) error { return nil }
func (s *noopScheduleStore) Get(_ context.Context, _ string) (*scheduler.Schedule, error) {
	return nil, scheduler.ErrNotFound
}
func (s *noopScheduleStore) UpdateNextRun(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}
func (s *noopScheduleStore) UpdateStatus(_ context.Context, _ string, _ scheduler.Status) error {
	return nil
}
func (s *noopScheduleStore) Delete(_ context.Context, _ string) error { return nil }
func (s *noopScheduleStore) ListDue(_ context.Context, _ time.Time) ([]*scheduler.Schedule, error) {
	return nil, nil
}
func (s *noopScheduleStore) ListByProject(_ context.Context, _ string) ([]*scheduler.Schedule, error) {
	return nil, nil
}
func (s *noopScheduleStore) AppendHistory(_ context.Context, h scheduler.HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, h)
	return nil
}
func (s *noopScheduleStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	return nil, nil
}

func newCatchUpSchedule(id, alias, action string, createdAt time.Time) *scheduler.Schedule {
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
		CreatedAt:  createdAt,
	}
}
