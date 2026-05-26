//go:build chaos

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

func TestChaos_ClockDrift_JitterDeterministicAcrossDriftWindows(t *testing.T) {
	const period = time.Minute
	const id = "ten-min-routine"

	preDrift := scheduler.ComputeJitter(id, period)

	// "Inject" 5min drift — but jitter doesn't observe the clock, so
	// recomputing it MUST produce the same value.
	postDrift := scheduler.ComputeJitter(id, period)
	if preDrift != postDrift {
		t.Fatalf("inv-zen-120 violated: jitter changed across simulated drift; pre=%v post=%v",
			preDrift, postDrift)
	}

	// Different routine IDs MUST produce different jitter (otherwise
	// the per-routine offset is useless for thundering-herd avoidance).
	other := scheduler.ComputeJitter(id+"-other", period)
	if preDrift == other {
		t.Fatalf("inv-zen-120 violated: jitter identical across distinct IDs (collision)")
	}

	if got := scheduler.ComputeJitter(id, 0); got != 0 {
		t.Fatalf("ComputeJitter(_, 0): got %v, want 0", got)
	}
	if got := scheduler.ComputeJitter(id, -time.Second); got != 0 {
		t.Fatalf("ComputeJitter(_, -1s): got %v, want 0", got)
	}
}

func TestChaos_ClockDrift_MissPolicyResolvedPerDoctrine(t *testing.T) {
	cases := []struct {
		doctrine doctrine.Name
		want     scheduler.MissPolicy
	}{
		{doctrine.NameDefault, scheduler.MissPolicySkip},
		{doctrine.NameMaxScope, scheduler.MissPolicyCatchUpBounded},
		{doctrine.NameCapaFirewall, scheduler.MissPolicyNotifyOnly},
	}
	for _, tc := range cases {
		t.Run(string(tc.doctrine), func(t *testing.T) {
			s := newCronSchedule("drift-test-"+string(tc.doctrine), "alpha", "do-thing")
			got := scheduler.EffectiveMissPolicy(s, tc.doctrine)
			if got != tc.want {
				t.Fatalf("doctrine %v: EffectiveMissPolicy got %v, want %v",
					tc.doctrine, got, tc.want)
			}
		})
	}

	// DoctrineMissPolicy (the doctrine-only variant, no Schedule
	// override) MUST produce the same answer when Schedule has no
	// per-row override.
	for _, tc := range cases {
		got := scheduler.DoctrineMissPolicy(tc.doctrine)
		if got != tc.want {
			t.Fatalf("doctrine %v: DoctrineMissPolicy got %v, want %v",
				tc.doctrine, got, tc.want)
		}
	}
}

func TestChaos_ClockDrift_5MinForwardJumpFireSucceeds(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}

	disp := &counterDispatcher{}
	emitter := newRecordingEmitter()
	store := newRecordingScheduleStore()

	s := newCronSchedule("drift-fire-test", "alpha", "do-thing")

	s.LastRunAt = clk.Now().Add(-5 * time.Minute)

	if err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        clk.Now,
		Doctrine:   doctrine.NameDefault,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  &alwaysAllowRateLimit{},
		Store:      store,
	}); err != nil {
		t.Fatalf("pre-drift Fire: %v", err)
	}
	preCount := disp.Count()

	clk.advance(5 * time.Minute)

	if err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        clk.Now,
		Doctrine:   doctrine.NameDefault,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  &alwaysAllowRateLimit{},
		Store:      store,
	}); err != nil {
		t.Fatalf("post-drift Fire: %v", err)
	}
	postCount := disp.Count()
	if postCount-preCount < 1 {
		t.Fatalf("expected post-drift dispatch; pre=%d post=%d", preCount, postCount)
	}

	emitter.assertHasKind(t, scheduler.EventRoutineFired)
}

func TestChaos_ClockDrift_DoctrineCapaFirewallSurfaceAtMostNotifyOnly(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}

	disp := &counterDispatcher{}
	emitter := newRecordingEmitter()
	store := newRecordingScheduleStore()

	s := newCronSchedule("capa-drift-test", "alpha", "do-thing")
	s.LastRunAt = clk.Now().Add(-10 * time.Minute)

	s.NextRunAt = clk.Now().Add(-1 * time.Minute)

	if got := scheduler.EffectiveMissPolicy(s, doctrine.NameCapaFirewall); got != scheduler.MissPolicyNotifyOnly {
		t.Fatalf("expected MissPolicyNotifyOnly under capa-firewall, got %v", got)
	}

	if err := scheduler.Fire(ctx, s, scheduler.FireDeps{
		Now:        clk.Now,
		Doctrine:   doctrine.NameCapaFirewall,
		Quota:      &alwaysAllowQuota{},
		Dispatcher: disp,
		Eventlog:   emitter,
		RateLimit:  &alwaysAllowRateLimit{},
		Store:      store,
	}); err != nil {
		t.Fatalf("Fire under capa-firewall: %v", err)
	}

	hasMissed := false
	for _, ev := range emitter.events() {
		if ev.Kind == scheduler.EventMissedFire {
			hasMissed = true
			break
		}
	}
	if !hasMissed {
		t.Fatalf("expected at least one EventMissedFire event under capa-firewall doctrine drift")
	}
}

func TestChaos_ClockDrift_DispatchInvariantStableAcrossDoctrines(t *testing.T) {
	run := func(d doctrine.Name) int {
		ctx := context.Background()
		now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
		clk := &fakeClock{now: now}

		disp := &counterDispatcher{}
		emitter := newRecordingEmitter()
		store := newRecordingScheduleStore()

		s := newCronSchedule("stability-"+string(d), "alpha", "do-thing")
		s.LastRunAt = clk.Now().Add(-3 * time.Minute)
		s.NextRunAt = clk.Now().Add(-30 * time.Second)

		_ = scheduler.Fire(ctx, s, scheduler.FireDeps{
			Now:        clk.Now,
			Doctrine:   d,
			Quota:      &alwaysAllowQuota{},
			Dispatcher: disp,
			Eventlog:   emitter,
			RateLimit:  &alwaysAllowRateLimit{},
			Store:      store,
		})
		return disp.Count()
	}

	for _, d := range []doctrine.Name{doctrine.NameDefault, doctrine.NameMaxScope, doctrine.NameCapaFirewall} {
		first := run(d)
		second := run(d)
		if first != second {
			t.Fatalf("non-deterministic dispatch under doctrine %v: first=%d second=%d",
				d, first, second)
		}
	}
}

type counterDispatcher struct {
	mu    sync.Mutex
	calls int
}

func (c *counterDispatcher) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *counterDispatcher) Dispatch(_ context.Context, _ scheduler.DispatchInput) (scheduler.DispatchResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return scheduler.DispatchResult{
		CostUSD:    0.001,
		DurationMs: 1,
		Tier:       "chaos-counter",
	}, nil
}

var _ = quota.PreFlightDecision{}
