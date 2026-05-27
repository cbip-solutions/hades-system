package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

func TestRecoverySchedulerProbesOpenTiers(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Millisecond,
	})
	cb.RecordFailure("bypass")

	prober := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("still down")}
	cb.AttemptRecovery(context.Background(), prober)

	if cb.Permit("bypass") {
		t.Fatalf("setup: tier should be open initially")
	}

	time.Sleep(30 * time.Millisecond)

	prober.mu.Lock()
	prober.probe = nil
	prober.mu.Unlock()

	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{prober}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = scheduler.Run(ctx)

	time.Sleep(100 * time.Millisecond)

	if !cb.Permit("bypass") {
		t.Errorf("scheduler should have healed tier via background probe")
	}
}

func TestRecoveryScheduler_DefaultInterval(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})

	s := orchestrator.NewRecoveryScheduler(cb, nil, 0)
	if s == nil {
		t.Fatal("NewRecoveryScheduler(0 interval) returned nil")
	}
}

func TestRecoveryScheduler_NegativeIntervalUsesDefault(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	s := orchestrator.NewRecoveryScheduler(cb, nil, -1*time.Second)
	if s == nil {
		t.Fatal("NewRecoveryScheduler(negative interval) returned nil")
	}
}

func TestRecoveryScheduler_GracefulShutdown(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	scheduler := orchestrator.NewRecoveryScheduler(cb, nil, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Run(ctx)

	cancel()

	select {
	case <-done:

	case <-time.After(1 * time.Second):
		t.Error("done channel did not close within 1s after ctx cancellation")
	}
}

func TestRecoveryScheduler_NoBackends(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})
	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{}, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Run(ctx)

	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case <-done:

	case <-time.After(1 * time.Second):
		t.Error("done channel did not close within 1s after ctx cancellation")
	}
}

// TestRecoveryScheduler_ClosedProvidersSkipped 2 backends both in StateClosed;
// Probe MUST NOT be called within N ticks. Verifies the State check before
// AttemptRecovery skips already-healthy providers.
func TestRecoveryScheduler_ClosedProvidersSkipped(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})

	p1 := &fakeProber{tier: providers.TierInHouse, name: "bypass"}
	p2 := &fakeProber{tier: providers.TierOpenClaude, name: "openclaude"}

	if cb.State("bypass") != orchestrator.StateClosed {
		t.Fatal("setup: bypass should start Closed")
	}
	if cb.State("openclaude") != orchestrator.StateClosed {
		t.Fatal("setup: openclaude should start Closed")
	}

	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{p1, p2}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Run(ctx)

	time.Sleep(70 * time.Millisecond)
	cancel()
	<-done

	if p1.callCount() != 0 {
		t.Errorf("bypass (Closed): Probe called %d times; expected 0", p1.callCount())
	}
	if p2.callCount() != 0 {
		t.Errorf("openclaude (Closed): Probe called %d times; expected 0", p2.callCount())
	}
}

func TestRecoveryScheduler_MultipleProvidersIndependentRecovery(t *testing.T) {

	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         5 * time.Millisecond,
	})

	cb.RecordFailure("bypass")
	cb.RecordFailure("openclaude")

	p1 := &fakeProber{tier: providers.TierInHouse, name: "bypass", probe: errors.New("down")}
	p2 := &fakeProber{tier: providers.TierOpenClaude, name: "openclaude", probe: errors.New("down")}
	cb.AttemptRecovery(context.Background(), p1)
	cb.AttemptRecovery(context.Background(), p2)

	time.Sleep(20 * time.Millisecond)

	p1.mu.Lock()
	p1.probe = nil
	p1.mu.Unlock()

	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{p1, p2}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Run(ctx)

	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	if !cb.Permit("bypass") {
		t.Errorf("bypass: expected healed (StateClosed), got state %v", cb.State("bypass"))
	}

	if cb.Permit("openclaude") {
		t.Errorf("openclaude: expected still open, got StateClosed")
	}
}

func TestRecoveryScheduler_MultipleTicksUntilHealed(t *testing.T) {

	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         5 * time.Millisecond,
	})

	cb.RecordFailure("bypass")

	var callMu = &fakeProber{tier: providers.TierInHouse, name: "bypass"}
	callCount := 0
	callMu.probeFn = func(ctx context.Context) error {
		callMu.mu.Lock()
		callCount++
		n := callCount
		callMu.mu.Unlock()
		if n <= 2 {
			return errors.New("not yet")
		}
		return nil
	}

	cb.AttemptRecovery(context.Background(), callMu)
	if cb.State("bypass") != orchestrator.StateOpen {
		t.Fatalf("setup: expected StateOpen after failing probe, got %v", cb.State("bypass"))
	}

	callMu.mu.Lock()
	callCount = 1
	callMu.mu.Unlock()

	time.Sleep(20 * time.Millisecond)

	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{callMu}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := scheduler.Run(ctx)

	deadline := time.After(500 * time.Millisecond)
	for {
		if cb.Permit("bypass") {
			break
		}
		select {
		case <-deadline:
			t.Error("tier not healed within 500ms; scheduler appears to have given up")
			cancel()
			<-done
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	cancel()
	<-done

	if !cb.Permit("bypass") {
		t.Errorf("expected bypass healed (StateClosed), got %v", cb.State("bypass"))
	}
}

func TestRecoveryScheduler_ProbesSuspectByName(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Hour,
	})

	be := &fakeProber{name: "prov-a", tier: providers.TierOllama, probe: nil}
	cb.RecordFailure("prov-a")

	rs := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{be}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := rs.Run(ctx)

	deadline := time.After(2 * time.Second)
	for cb.State("prov-a") != orchestrator.StateClosed {
		select {
		case <-deadline:
			t.Fatal("scheduler did not heal prov-a within 2s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	if be.callCount() == 0 {
		t.Error("scheduler never probed the suspect backend")
	}
}

// TestRecoveryScheduler_WritesHealthSamplePerProbe pins the
// Task 13 behaviour: the RecoveryScheduler MUST write one
// tier_health_samples row per probe via the installed TierHealthSink. The
// recorded row carries Provider=Backend.Name(), Tier=Backend.Tier().String(),
// Success matching the probe outcome (true for a healing probe), and a
// non-negative LatencyMS captured around AttemptRecovery.
//
// Reuses fakeProber (defined in circuit_breaker_test.go, same package) and
// fakeTierHealthSink (defined in tier_health_sink_test.go, same package).
func TestRecoveryScheduler_WritesHealthSamplePerProbe(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Cooldown:         time.Hour,
	})
	be := &fakeProber{name: "prov-a", tier: providers.TierOllama, probe: nil}
	cb.RecordFailure("prov-a")

	sink := &fakeTierHealthSink{}
	rs := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{be}, 10*time.Millisecond)
	rs.SetHealthSink(sink)
	ctx, cancel := context.WithCancel(context.Background())
	done := rs.Run(ctx)

	deadline := time.After(2 * time.Second)
	for sink.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("scheduler wrote no health sample within 2s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	<-done

	sink.mu.Lock()
	defer sink.mu.Unlock()
	s := sink.samples[0]
	if s.Provider != "prov-a" {
		t.Errorf("sample.Provider = %q, want prov-a", s.Provider)
	}
	if !s.Success {
		t.Error("probe succeeded — sample.Success should be true")
	}
	if s.Tier != providers.TierOllama.String() {
		t.Errorf("sample.Tier = %q, want %q", s.Tier, providers.TierOllama.String())
	}
	if s.LatencyMS < 0 {
		t.Errorf("sample.LatencyMS = %d, want >= 0", s.LatencyMS)
	}
	if s.ErrorPattern != "" {
		t.Errorf("sample.ErrorPattern = %q, want empty (success)", s.ErrorPattern)
	}
}

func TestRecoveryScheduler_SkipsRateLimited(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{})

	cb.RecordRateLimited("bypass", time.Hour)
	if got := cb.State("bypass"); got != orchestrator.StateRateLimited {
		t.Fatalf("setup: expected StateRateLimited, got %v", got)
	}

	probed := &fakeProber{name: "bypass", tier: providers.TierInHouse}

	rs := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{probed}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := rs.Run(ctx)

	time.Sleep(80 * time.Millisecond)
	cancel()
	<-done

	if probed.callCount() != 0 {
		t.Fatalf("rate-limited tier must not be probed by the scheduler; got %d probe calls", probed.callCount())
	}
}
