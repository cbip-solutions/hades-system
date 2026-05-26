package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type fakeProber struct {
	mu         sync.Mutex
	tier       providers.Tier
	name       string
	probe      error
	probeFn    func(ctx context.Context) error
	probeCalls int
}

func (f *fakeProber) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	return nil, nil
}

func (f *fakeProber) Probe(ctx context.Context) error {
	f.mu.Lock()
	f.probeCalls++
	fn := f.probeFn
	err := f.probe
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return err
}

func (f *fakeProber) Close() error { return nil }
func (f *fakeProber) Name() string {
	if f.name == "" {
		return "fakeProber"
	}
	return f.name
}
func (f *fakeProber) Capabilities() providers.TierCapabilities { return providers.TierCapabilities{} }
func (f *fakeProber) Tier() providers.Tier                     { return f.tier }

func (f *fakeProber) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.probeCalls
}

func TestCircuitBreakerInitiallyClosed(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	if !cb.Permit("bypass") {
		t.Error("initial state should permit")
	}
}

func TestCircuitBreakerOpensAfterThresholdFailures(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")

	if cb.Permit("bypass") {
		t.Error("3 consecutive failures should move to suspect → reject permit")
	}
}

func TestCircuitBreakerSuspectProbeRecovery(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")

	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: nil}
	healed := cb.AttemptRecovery(context.Background(), prober)
	if !healed {
		t.Error("healthy probe should heal suspect → closed")
	}
	if !cb.Permit("bypass") {
		t.Error("post-recovery should permit")
	}
}

func TestCircuitBreakerSuspectProbeFailsToOpen(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("probe failed")}
	healed := cb.AttemptRecovery(context.Background(), prober)
	if healed {
		t.Error("failing probe should keep tier rejected")
	}
	if cb.Permit("bypass") {
		t.Error("open circuit should deny permit")
	}
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Errorf("state after failed probe should be Open, got %v", cb.State("bypass"))
	}
}

func TestCircuitBreakerCooldownThenRetry(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         50 * time.Millisecond,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("first probe fail")}
	cb.AttemptRecovery(context.Background(), prober)
	if cb.Permit("bypass") {
		t.Error("post-failed-probe should be open (deny)")
	}

	time.Sleep(100 * time.Millisecond)
	prober.mu.Lock()
	prober.probe = nil
	prober.mu.Unlock()
	cb.AttemptRecovery(context.Background(), prober)
	if !cb.Permit("bypass") {
		t.Error("post-cooldown + healthy probe should permit again")
	}
}

func TestCircuitBreakerSuccessResetsState(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	cb.RecordSuccess("bypass")
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	if !cb.Permit("bypass") {
		t.Error("intervening success should have reset failure counter")
	}
}

func TestCircuitBreakerRecoveryResetsFailureStreak(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: nil}
	if !cb.AttemptRecovery(context.Background(), prober) {
		t.Fatal("expected successful recovery")
	}

	cb.RecordFailure("bypass")
	if !cb.Permit("bypass") {
		t.Error("one failure post-recovery should not trip breaker (streak reset)")
	}
	cb.RecordFailure("bypass")
	if cb.Permit("bypass") {
		t.Error("second failure post-recovery should trip breaker")
	}
}

func TestCircuitBreakerNewWithDefaults(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})

	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	if !cb.Permit("bypass") {
		t.Error("2 failures with default threshold (3) should still permit")
	}
	cb.RecordFailure("bypass")
	if cb.Permit("bypass") {
		t.Error("3 failures with default threshold (3) should trip → suspect")
	}
}

func TestCircuitBreakerPerNameIsolation(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	if cb.Permit("bypass") {
		t.Error("bypass should be tripped")
	}

	if !cb.Permit("openclaude") {
		t.Error("openclaude must not be affected by bypass failures")
	}
	if cb.State("openclaude") != orchestrator.StateClosed {
		t.Errorf("openclaude state = %v, want StateClosed", cb.State("openclaude"))
	}
}

func TestCircuitBreakerStateAccessor(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	if cb.State("bypass") != orchestrator.StateClosed {
		t.Errorf("initial state = %v, want StateClosed", cb.State("bypass"))
	}
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	if cb.State("bypass") != orchestrator.StateSuspect {
		t.Errorf("post-2-failures state = %v, want StateSuspect", cb.State("bypass"))
	}
	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("nope")}
	cb.AttemptRecovery(context.Background(), prober)
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Errorf("post-failed-probe state = %v, want StateOpen", cb.State("bypass"))
	}

	prober.mu.Lock()
	prober.probe = nil
	prober.mu.Unlock()

	cb.RecordSuccess("bypass")
	if cb.State("bypass") != orchestrator.StateClosed {
		t.Errorf("post-RecordSuccess state = %v, want StateClosed", cb.State("bypass"))
	}
}

func TestCircuitBreakerOpenCooldownNotElapsedDeniesRecovery(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	failProbe := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("fail-1")}
	if cb.AttemptRecovery(context.Background(), failProbe) {
		t.Fatal("first probe must fail")
	}

	healthy := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: nil}
	if cb.AttemptRecovery(context.Background(), healthy) {
		t.Error("cooldown-not-elapsed must deny recovery")
	}
	if healthy.callCount() != 0 {
		t.Errorf("Probe must not be invoked while in cooldown; got %d calls", healthy.callCount())
	}
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Errorf("state must remain StateOpen during cooldown; got %v", cb.State("bypass"))
	}
}

func TestCircuitBreakerOpenCooldownElapsedNoAttemptRecoveryStaysOpen(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         50 * time.Millisecond,
	})
	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")
	failProbe := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("probe-fail")}
	cb.AttemptRecovery(context.Background(), failProbe)
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Fatalf("expected StateOpen after failed probe, got %v", cb.State("bypass"))
	}

	time.Sleep(100 * time.Millisecond)

	if cb.Permit("bypass") {
		t.Error("Permit must not auto-recover after cooldown elapse")
	}
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Errorf("state must remain StateOpen until AttemptRecovery is called; got %v", cb.State("bypass"))
	}
}

func TestCircuitBreakerConcurrentRecordRaceClean(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 5,
		Window:           1 * time.Minute,
		Cooldown:         10 * time.Millisecond,
	})
	names := []string{"bypass", "openclaude"}
	const goroutines = 8
	const iters = 100

	probers := map[string]*fakeProber{
		"bypass":     {tier: providers.TierInHouse, name: "bypass", probe: nil},
		"openclaude": {tier: providers.TierOpenClaude, name: "openclaude", probe: nil},
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := time.Now()
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				name := names[(g+i)%len(names)]
				switch (g + i) % 5 {
				case 0:
					cb.RecordSuccess(name)
				case 1:
					cb.RecordFailure(name)
				case 2:
					_ = cb.Permit(name)
				case 3:
					_ = cb.State(name)
				case 4:
					_ = cb.AttemptRecovery(context.Background(), probers[name])
				}
			}
		}(g)
	}
	wg.Wait()
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("8x100 concurrent ops took %v (threshold 5s) — possible lock contention", elapsed)
	}
}

func TestStateString(t *testing.T) {
	cases := []struct {
		s    orchestrator.State
		want string
	}{
		{orchestrator.StateClosed, "closed"},
		{orchestrator.StateSuspect, "suspect"},
		{orchestrator.StateOpen, "open"},
		{orchestrator.State(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", int(c.s), got, c.want)
		}
	}
}

func TestCircuitBreakerHealthResetOnHeal(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	cb.RecordFailure("bypass")
	cb.RecordFailure("bypass")

	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: nil}
	if !cb.AttemptRecovery(context.Background(), prober) {
		t.Fatal("expected successful recovery")
	}
	if cb.State("bypass") != orchestrator.StateClosed {
		t.Fatalf("post-heal state = %v, want StateClosed", cb.State("bypass"))
	}

	cb.RecordFailure("bypass")
	if cb.State("bypass") != orchestrator.StateClosed {
		t.Errorf("one failure post-heal should not trip breaker; got state %v", cb.State("bypass"))
	}
	if !cb.Permit("bypass") {
		t.Error("one failure post-heal should still permit")
	}
}

func TestCircuitBreaker_PerNameIndependence(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{FailureThreshold: 2})

	cb.RecordFailure("deepseek-direct")
	cb.RecordFailure("deepseek-direct")
	if cb.Permit("deepseek-direct") {
		t.Error("deepseek-direct should be denied after 2 failures")
	}
	if !cb.Permit("siliconflow-deepseek") {
		t.Error("siliconflow-deepseek must stay permitted — breaker keys on Name, not Tier")
	}
	if got := cb.State("siliconflow-deepseek"); got != orchestrator.StateClosed {
		t.Errorf("siliconflow-deepseek State() = %v, want StateClosed", got)
	}
}

func TestCircuitBreakerRecordFailureOnOpenIsNoOpStateWise(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	cb.RecordFailure("prov-a")
	cb.RecordFailure("prov-a")

	failProbe := &fakeProber{tier: providers.TierInHouse, name: "prov-a", probe: errors.New("fail")}
	cb.AttemptRecovery(context.Background(), failProbe)
	if cb.State("prov-a") != orchestrator.StateOpen {
		t.Fatalf("expected StateOpen, got %v", cb.State("prov-a"))
	}

	cb.RecordFailure("prov-a")
	cb.RecordFailure("prov-a")
	if cb.State("prov-a") != orchestrator.StateOpen {
		t.Errorf("RecordFailure on Open provider should not change state; got %v", cb.State("prov-a"))
	}
	if cb.Permit("prov-a") {
		t.Error("Open provider must deny Permit after further failures")
	}
}

func TestCircuitBreakerAttemptRecoveryOnClosedReturnsTrue(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	prober := &fakeProber{tier: providers.TierInHouse, name: "prov-b", probe: errors.New("should-not-be-called")}
	result := cb.AttemptRecovery(context.Background(), prober)
	if !result {
		t.Error("AttemptRecovery on StateClosed should return true")
	}
	if prober.callCount() != 0 {
		t.Errorf("Probe must not be invoked on StateClosed provider; got %d calls", prober.callCount())
	}
}

func TestCircuitBreaker_RateLimited_DeniesUntilCooldownThenPermits(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	cb.RecordRateLimited("bypass", 50*time.Millisecond)

	if cb.Permit("bypass") {
		t.Fatal("rate-limited tier must be denied during cool-down")
	}
	if cb.State("bypass") != orchestrator.StateRateLimited {
		t.Fatalf("want StateRateLimited, got %v", cb.State("bypass"))
	}

	deadline := time.Now().Add(2 * time.Second)
	for !cb.Permit("bypass") {
		if time.Now().After(deadline) {
			t.Fatal("Permit never re-opened after cool-down")
		}
		time.Sleep(time.Millisecond)
	}

	cb.RecordSuccess("bypass")
	if cb.State("bypass") != orchestrator.StateClosed {
		t.Fatalf("RecordSuccess must clear to Closed, got %v", cb.State("bypass"))
	}
}

func TestCircuitBreaker_429DoesNotTripFailurePath(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{FailureThreshold: 3})
	for i := 0; i < 5; i++ {
		cb.RecordRateLimited("bypass", time.Millisecond)
	}

	if s := cb.State("bypass"); s == orchestrator.StateSuspect || s == orchestrator.StateOpen {
		t.Fatalf("429 must not reach the failure path; got %v", s)
	}
}

func TestRateLimitCooldown_RespectsRetryAfterFloorAndCaps(t *testing.T) {

	if d := orchestrator.RateLimitCooldownTestable(120*time.Second, 1); d < 120*time.Second {
		t.Fatalf("Retry-After must be a hard floor: got %v", d)
	}

	for attempt := 1; attempt <= 12; attempt++ {
		d := orchestrator.RateLimitCooldownTestable(0, attempt)
		if d <= 0 || d > 300*time.Second {
			t.Fatalf("attempt %d: cooldown %v out of (0,300s]", attempt, d)
		}
	}
}

func TestCircuitBreaker_AttemptRecovery_SkipsRateLimited(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	cb.RecordRateLimited("bypass", 10*time.Second)

	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: nil}
	result := cb.AttemptRecovery(context.Background(), prober)
	if result {
		t.Error("AttemptRecovery on StateRateLimited must return false (no probe)")
	}
	if prober.callCount() != 0 {
		t.Errorf("Probe must not be invoked on rate-limited provider; got %d calls", prober.callCount())
	}
	if cb.State("bypass") != orchestrator.StateRateLimited {
		t.Errorf("state must remain StateRateLimited; got %v", cb.State("bypass"))
	}
}
