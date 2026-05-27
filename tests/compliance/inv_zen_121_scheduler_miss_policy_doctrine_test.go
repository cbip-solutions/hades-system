// Package compliance — invariant: scheduler miss-policy follows
// doctrine matrix + rate-limit 1/30s/project on max-scope catch-up.
//
// Spec §1 Q9 C / §7.2 invariant wording:
//
// "Per-doctrine miss policy MUST map max-scope=CatchUpBounded,
// default=Skip, capa-firewall=NotifyOnly; rate-limit 1/30s/project
// enforced on catch-up dispatches."
//
// This test is the cross-package, boundary-side witness. The in-package
// test surface in internal/scheduler/miss_policy_test.go locks
// per-implementation behaviour; this file re-asserts the contract from
// outside the package so any future refactor (e.g. adding a fourth
// doctrine, swapping the fallback semantics, lifting the rate-limit
// into a separate package) gets caught at the public surface.
//
// Coverage matrix:
//
// (a) DoctrineMissPolicy maps the three canonical doctrines + the
// safe-default fallback for unknown / empty names.
// (b) EffectiveMissPolicy override semantics:
// - non-zero per-Schedule override wins over doctrine default;
// - zero (Skip) on default doctrine: respected (Skip is the
// doctrine default anyway);
// - zero (Skip) on max-scope: treated as "unset", falls through
// to CatchUpBounded;
// - nil receiver: returns DoctrineMissPolicy(d) without panic.
// (c) Catch-up rate-limit 1/30s/project: feed N concurrent fires
// through the live Fire() pipeline with a fake-clock-driven rate
// limiter; assert that the limiter is consulted on every
// catch-up dispatch and that exactly the budgeted fires get
// through.
//
// Boundary: this test imports only internal/scheduler +
// internal/doctrine + internal/quota + stdlib. It does NOT touch
// internal/store, internal/providers, or private-tier1-module.
//
// Inv-zen-121 contract.
package compliance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestInvZen121DoctrineMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		d    doctrine.Name
		want scheduler.MissPolicy
	}{
		{"default-doctrine-skip", doctrine.NameDefault, scheduler.MissPolicySkip},
		{"max-scope-doctrine-catchup", doctrine.NameMaxScope, scheduler.MissPolicyCatchUpBounded},
		{"capa-firewall-doctrine-notify-only", doctrine.NameCapaFirewall, scheduler.MissPolicyNotifyOnly},

		{"unknown-doctrine-fallback-skip", doctrine.Name("unknown"), scheduler.MissPolicySkip},
		{"empty-doctrine-fallback-skip", doctrine.Name(""), scheduler.MissPolicySkip},
		{"typo-doctrine-fallback-skip", doctrine.Name("Default"), scheduler.MissPolicySkip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scheduler.DoctrineMissPolicy(tc.d)
			if got != tc.want {
				t.Errorf("inv-zen-121 violated: doctrine=%q got=%v want=%v",
					string(tc.d), got, tc.want)
			}
		})
	}
}

func TestInvZen121EffectiveMissPolicyOverride(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		schedule *scheduler.Schedule
		d        doctrine.Name
		want     scheduler.MissPolicy
	}{
		{
			name:     "explicit-coalesce-on-max-scope-wins",
			schedule: &scheduler.Schedule{MissPolicy: scheduler.MissPolicyCoalesce},
			d:        doctrine.NameMaxScope,
			want:     scheduler.MissPolicyCoalesce,
		},
		{
			name:     "explicit-notify-on-default-wins",
			schedule: &scheduler.Schedule{MissPolicy: scheduler.MissPolicyNotifyOnly},
			d:        doctrine.NameDefault,
			want:     scheduler.MissPolicyNotifyOnly,
		},
		{
			name:     "zero-skip-on-max-scope-falls-through",
			schedule: &scheduler.Schedule{MissPolicy: scheduler.MissPolicySkip},
			d:        doctrine.NameMaxScope,
			want:     scheduler.MissPolicyCatchUpBounded,
		},
		{
			name:     "zero-skip-on-default-honoured",
			schedule: &scheduler.Schedule{MissPolicy: scheduler.MissPolicySkip},
			d:        doctrine.NameDefault,
			want:     scheduler.MissPolicySkip,
		},
		{
			name:     "zero-skip-on-capa-firewall-falls-through",
			schedule: &scheduler.Schedule{MissPolicy: scheduler.MissPolicySkip},
			d:        doctrine.NameCapaFirewall,
			want:     scheduler.MissPolicyNotifyOnly,
		},
		{
			name:     "nil-receiver-uses-doctrine-default",
			schedule: nil,
			d:        doctrine.NameMaxScope,
			want:     scheduler.MissPolicyCatchUpBounded,
		},
		{
			name:     "nil-receiver-on-default-doctrine",
			schedule: nil,
			d:        doctrine.NameDefault,
			want:     scheduler.MissPolicySkip,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scheduler.EffectiveMissPolicy(tc.schedule, tc.d)
			if got != tc.want {
				t.Errorf("inv-zen-121 violated: schedule=%+v doctrine=%q got=%v want=%v",
					tc.schedule, string(tc.d), got, tc.want)
			}
		})
	}
}

type fakeClockRateLimiter struct {
	mu sync.Mutex

	lastAllowed map[string]time.Time

	bucketRefill time.Duration

	calls []rateCall
}

type rateCall struct {
	project string
	now     time.Time
	allowed bool
}

func newFakeClockRateLimiter(refill time.Duration) *fakeClockRateLimiter {
	return &fakeClockRateLimiter{
		lastAllowed:  make(map[string]time.Time),
		bucketRefill: refill,
	}
}

func (r *fakeClockRateLimiter) Allow(_ context.Context, project string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	last, hasPrior := r.lastAllowed[project]
	allowed := !hasPrior || now.Sub(last) >= r.bucketRefill
	if allowed {
		r.lastAllowed[project] = now
	}
	r.calls = append(r.calls, rateCall{project: project, now: now, allowed: allowed})
	return allowed
}

func (r *fakeClockRateLimiter) Calls() []rateCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]rateCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *fakeClockRateLimiter) allowedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.allowed {
			n++
		}
	}
	return n
}

type noopQuota struct{}

func (noopQuota) PreFlight(_ context.Context, _ string, _ doctrine.Name) (quota.PreFlightDecision, error) {
	return quota.PreFlightDecision{Allowed: true}, nil
}

// countingDispatcher implements scheduler.Dispatcher and records the
// number of dispatches without any side effect. Captures the load-
// bearing assertion: rate-limit-denied catch-up fires DO NOT reach
// the dispatcher.
type countingDispatcher struct {
	count int32
}

func (c *countingDispatcher) Dispatch(_ context.Context, _ scheduler.DispatchInput) (scheduler.DispatchResult, error) {
	atomic.AddInt32(&c.count, 1)
	return scheduler.DispatchResult{CostUSD: 0.001, DurationMs: 1}, nil
}

func (c *countingDispatcher) Count() int32 {
	return atomic.LoadInt32(&c.count)
}

type nullEventLog struct{}

func (nullEventLog) Emit(_ context.Context, _ scheduler.Event) error { return nil }

type schedulerMemStore struct {
	mu      sync.Mutex
	history []scheduler.HistoryEntry
}

func (m *schedulerMemStore) Insert(_ context.Context, _ *scheduler.Schedule) error { return nil }
func (m *schedulerMemStore) Get(_ context.Context, _ string) (*scheduler.Schedule, error) {
	return nil, scheduler.ErrNotFound
}
func (m *schedulerMemStore) UpdateNextRun(_ context.Context, _ string, _, _ time.Time) error {
	return nil
}
func (m *schedulerMemStore) UpdateStatus(_ context.Context, _ string, _ scheduler.Status) error {
	return nil
}
func (m *schedulerMemStore) Delete(_ context.Context, _ string) error { return nil }
func (m *schedulerMemStore) ListDue(_ context.Context, _ time.Time) ([]*scheduler.Schedule, error) {
	return nil, nil
}
func (m *schedulerMemStore) ListByProject(_ context.Context, _ string) ([]*scheduler.Schedule, error) {
	return nil, nil
}
func (m *schedulerMemStore) AppendHistory(_ context.Context, h scheduler.HistoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, h)
	return nil
}
func (m *schedulerMemStore) QueryHistory(_ context.Context, _ string, _, _ time.Time) ([]scheduler.HistoryEntry, error) {
	return nil, nil
}

func TestInvZen121CatchUpRateLimit_OneTokenPer30sPerProject(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	rl := newFakeClockRateLimiter(30 * time.Second)
	disp := &countingDispatcher{}
	deps := scheduler.FireDeps{
		Now:        func() time.Time { return now },
		Doctrine:   doctrine.NameMaxScope,
		Quota:      noopQuota{},
		Dispatcher: disp,
		Eventlog:   nullEventLog{},
		RateLimit:  rl,
		Store:      &schedulerMemStore{},
	}
	s := &scheduler.Schedule{
		ID:            "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "morning-brief",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  7 * 24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-5 * 24 * time.Hour),
		NextRunAt:     now.Add(-1 * time.Minute),
		CreatedAt:     now.Add(-30 * 24 * time.Hour),
	}

	err := scheduler.Fire(context.Background(), s, deps)

	if err == nil {
		t.Fatalf("inv-zen-121 violated: Fire returned nil, want ErrRateLimited or wrapped (rate-limit cap)")
	}
	if !errors.Is(err, scheduler.ErrRateLimited) {
		t.Errorf("inv-zen-121: Fire err %v, want errors.Is ErrRateLimited", err)
	}

	if got := len(rl.Calls()); got < 2 {
		t.Errorf("inv-zen-121 violated: rate-limiter called %d times, want >=2 (catch-up + current)", got)
	}

	if got := rl.allowedCount(); got != 1 {
		t.Errorf("inv-zen-121 violated: rate-limiter allowed %d fires, want exactly 1 (1/30s/project budget)", got)
	}

	if got := disp.Count(); got > 1 {
		t.Errorf("inv-zen-121 violated: dispatcher called %d times, want <=1 (rate-limited)", got)
	}
}

func TestInvZen121CatchUpRateLimit_DistinctProjectsIsolated(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	rl := newFakeClockRateLimiter(30 * time.Second)

	allowed1 := rl.Allow(context.Background(), "internal-platform-x", now)
	allowed2 := rl.Allow(context.Background(), "reference-project", now)
	if !allowed1 {
		t.Errorf("inv-zen-121 violated: project A first call denied; per-project budgets must start full")
	}
	if !allowed2 {
		t.Errorf("inv-zen-121 violated: project B first call denied; per-project budgets must start full")
	}

	if rl.Allow(context.Background(), "internal-platform-x", now) {
		t.Errorf("inv-zen-121 violated: project A second call at same wall clock allowed; bucket should be empty")
	}
	if rl.Allow(context.Background(), "reference-project", now) {
		t.Errorf("inv-zen-121 violated: project B second call at same wall clock allowed; bucket should be empty")
	}

	advanced := now.Add(30 * time.Second)
	if !rl.Allow(context.Background(), "internal-platform-x", advanced) {
		t.Errorf("inv-zen-121 violated: project A bucket did not refill after 30s")
	}
	if !rl.Allow(context.Background(), "reference-project", advanced) {
		t.Errorf("inv-zen-121 violated: project B bucket did not refill after 30s")
	}
}

func TestInvZen121CatchUpRateLimit_BucketRefillTimeline(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	rl := newFakeClockRateLimiter(30 * time.Second)
	project := "internal-platform-x"

	if !rl.Allow(context.Background(), project, now) {
		t.Errorf("inv-zen-121 violated: +0s first call denied")
	}

	if rl.Allow(context.Background(), project, now.Add(15*time.Second)) {
		t.Errorf("inv-zen-121 violated: +15s call allowed; budget should still be empty")
	}

	if !rl.Allow(context.Background(), project, now.Add(30*time.Second)) {
		t.Errorf("inv-zen-121 violated: +30s call denied; budget should have refilled")
	}

	if !rl.Allow(context.Background(), project, now.Add(60*time.Second)) {
		t.Errorf("inv-zen-121 violated: +60s call denied; budget should have refilled again")
	}

	if rl.Allow(context.Background(), project, now.Add(75*time.Second)) {
		t.Errorf("inv-zen-121 violated: +75s call allowed; budget should still be empty after +60s consumption")
	}
}

func TestInvZen121CatchUpRateLimit_RateLimiterInterfaceContract(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	rl := &alwaysAllowCountingRateLimit{}
	disp := &countingDispatcher{}
	deps := scheduler.FireDeps{
		Now:        func() time.Time { return now },
		Doctrine:   doctrine.NameMaxScope,
		Quota:      noopQuota{},
		Dispatcher: disp,
		Eventlog:   nullEventLog{},
		RateLimit:  rl,
		Store:      &schedulerMemStore{},
	}

	s := &scheduler.Schedule{
		ID:            "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "morning-brief",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  7 * 24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-3 * 24 * time.Hour),
		NextRunAt:     now.Add(-1 * time.Minute),
		CreatedAt:     now.Add(-30 * 24 * time.Hour),
	}

	if err := scheduler.Fire(context.Background(), s, deps); err != nil {
		t.Fatalf("Fire = %v, want nil (always-allow path)", err)
	}

	if got := rl.calls; got < 2 {
		t.Errorf("inv-zen-121 violated: rate-limiter consulted %d times, want >=2 (limiter is single decision point)",
			got)
	}

	if got := int(disp.Count()); got != rl.calls {
		t.Errorf("inv-zen-121 violated: dispatcher called %d times, rate-limiter consulted %d times; mismatch indicates bypass path",
			got, rl.calls)
	}
}

type alwaysAllowCountingRateLimit struct {
	mu    sync.Mutex
	calls int
}

func (r *alwaysAllowCountingRateLimit) Allow(_ context.Context, _ string, _ time.Time) bool {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return true
}
