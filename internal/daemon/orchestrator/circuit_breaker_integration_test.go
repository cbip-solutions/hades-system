package orchestrator_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

type atomicErr struct{ err error }

type flakeProber struct {
	t   providers.Tier
	n   string
	val atomic.Value
}

func newFlakeProber(tier providers.Tier, name string, initErr error) *flakeProber {
	fp := &flakeProber{t: tier, n: name}
	fp.val.Store(&atomicErr{err: initErr})
	return fp
}

func (f *flakeProber) setErr(err error) { f.val.Store(&atomicErr{err: err}) }

func (f *flakeProber) Probe(_ context.Context) error {
	return f.val.Load().(*atomicErr).err
}

func (f *flakeProber) Tier() providers.Tier { return f.t }
func (f *flakeProber) Name() string {
	if f.n == "" {
		return "flakeProber"
	}
	return f.n
}
func (f *flakeProber) Forward(_ context.Context, _ providers.TierRequest) (*providers.TierResponse, error) {
	return nil, nil
}
func (f *flakeProber) Close() error                             { return nil }
func (f *flakeProber) Capabilities() providers.TierCapabilities { return providers.TierCapabilities{} }

func TestCircuitBreakerEndToEndRecoveryFlow(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           1 * time.Second,
		Cooldown:         50 * time.Millisecond,
	})

	cb.RecordFailure("openclaude")
	cb.RecordFailure("openclaude")
	cb.RecordFailure("openclaude")

	if cb.Permit("openclaude") {
		t.Fatal("after 3 failures: Permit must return false (Suspect)")
	}

	prober := newFlakeProber(providers.TierOpenClaude, "openclaude", errors.New("still down"))
	cb.AttemptRecovery(context.Background(), prober)

	if cb.Permit("openclaude") {
		t.Error("first probe failed: Permit must return false (Open)")
	}
	if cb.State("openclaude") != orchestrator.StateOpen {
		t.Errorf("state after failed probe should be Open, got %v", cb.State("openclaude"))
	}

	time.Sleep(100 * time.Millisecond)

	prober.setErr(nil)

	healed := cb.AttemptRecovery(context.Background(), prober)
	if !healed {
		t.Error("post-cooldown + healthy probe: AttemptRecovery should return true (healed)")
	}

	if !cb.Permit("openclaude") {
		t.Error("post-heal: Permit must return true (Closed)")
	}
	if cb.State("openclaude") != orchestrator.StateClosed {
		t.Errorf("post-heal state should be StateClosed, got %v", cb.State("openclaude"))
	}
}

func TestCircuitBreakerIntegration_FailingThenHealingTier(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         5 * time.Millisecond,
	})

	prober := newFlakeProber(providers.TierInHouse, "inhouse-heal", errors.New("tick-fail"))
	cb.RecordFailure("inhouse-heal")
	cb.AttemptRecovery(context.Background(), prober)
	if cb.State("inhouse-heal") != orchestrator.StateOpen {
		t.Fatalf("setup: expected StateOpen, got %v", cb.State("inhouse-heal"))
	}

	var callMu sync.Mutex
	schedulerCalls := 0

	healingProber := &fakeProber{
		tier: providers.TierInHouse,
		name: "inhouse-heal",
		probeFn: func(_ context.Context) error {
			callMu.Lock()
			schedulerCalls++
			n := schedulerCalls
			callMu.Unlock()
			if n <= 3 {
				return errors.New("not yet")
			}
			return nil
		},
	}

	time.Sleep(20 * time.Millisecond)

	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{healingProber}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = scheduler.Run(ctx)

	deadline := time.After(500 * time.Millisecond)
	for {
		if cb.Permit("inhouse-heal") {
			break
		}
		select {
		case <-deadline:
			t.Errorf("tier not healed within 500ms; state=%v, schedulerCalls=%d",
				cb.State("inhouse-heal"), schedulerCalls)
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if cb.State("inhouse-heal") != orchestrator.StateClosed {
		t.Errorf("final state should be StateClosed, got %v", cb.State("inhouse-heal"))
	}

	cb.RecordSuccess("inhouse-heal")
	if cb.State("inhouse-heal") != orchestrator.StateClosed {
		t.Errorf("RecordSuccess on healed tier: state should remain StateClosed, got %v",
			cb.State("inhouse-heal"))
	}
}

func TestCircuitBreakerIntegration_PermitConsistency(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 5,
		Window:           1 * time.Minute,
		Cooldown:         10 * time.Millisecond,
	})

	const readers = 8
	const iters = 200

	prober := newFlakeProber(providers.TierGemini, "gemini-flash", errors.New("down"))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			cb.RecordFailure("gemini-flash")
		}
		for i := 0; i < iters; i++ {
			cb.AttemptRecovery(context.Background(), prober)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	results := make([]bool, readers)
	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var last bool
			for i := 0; i < iters; i++ {
				last = cb.Permit("gemini-flash")
			}
			results[idx] = last
		}(g)
	}

	wg.Wait()

	if cb.Permit("gemini-flash") {
		t.Error("after constant failing probes: Permit should be false")
	}

	time.Sleep(20 * time.Millisecond)
	prober.setErr(nil)
	if !cb.AttemptRecovery(context.Background(), prober) {
		t.Error("healthy probe after cooldown should heal the tier")
	}
	if !cb.Permit("gemini-flash") {
		t.Error("post-heal: Permit must return true")
	}
}

func TestCircuitBreakerIntegration_RecoverySchedulerHealsOpenTierEnd2End(t *testing.T) {
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 2,
		Window:           5 * time.Minute,
		Cooldown:         15 * time.Millisecond,
	})

	cb.RecordFailure("ollama")
	cb.RecordFailure("ollama")
	if cb.State("ollama") != orchestrator.StateSuspect {
		t.Fatalf("setup: expected StateSuspect after 2 failures, got %v", cb.State("ollama"))
	}

	setupProber := &fakeProber{tier: providers.TierOllama, name: "ollama", probe: errors.New("ollama-down")}
	cb.AttemptRecovery(context.Background(), setupProber)
	if cb.State("ollama") != orchestrator.StateOpen {
		t.Fatalf("setup: expected StateOpen after failing probe, got %v", cb.State("ollama"))
	}

	if cb.Permit("ollama") {
		t.Fatal("setup: Open tier must not be permitted")
	}

	time.Sleep(30 * time.Millisecond)

	healthyProber := newFlakeProber(providers.TierOllama, "ollama", nil)
	scheduler := orchestrator.NewRecoveryScheduler(cb, []providers.TierBackend{healthyProber}, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := scheduler.Run(ctx)

	deadline := time.After(300 * time.Millisecond)
	for {
		if cb.Permit("ollama") {
			break
		}
		select {
		case <-deadline:
			t.Errorf("scheduler failed to heal tier within 300ms; state=%v",
				cb.State("ollama"))
			cancel()
			<-done
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	if cb.State("ollama") != orchestrator.StateClosed {
		t.Errorf("post-heal state should be StateClosed, got %v", cb.State("ollama"))
	}

	cancel()
	<-done

	cb.RecordFailure("ollama")
	if cb.State("ollama") != orchestrator.StateClosed {
		t.Errorf("one post-heal failure (threshold=2) should not re-trip; got %v",
			cb.State("ollama"))
	}
	if !cb.Permit("ollama") {
		t.Error("one post-heal failure should still permit (health reset)")
	}

	cb.RecordFailure("ollama")
	if cb.Permit("ollama") {
		t.Error("two post-heal failures (= threshold) should trip the breaker again")
	}
}
