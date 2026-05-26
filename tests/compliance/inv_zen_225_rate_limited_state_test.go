// tests/compliance/inv_zen_225_rate_limited_state_test.go
//
// inv-zen-225 (v0.17.7 / B-3) — a 429 response never yields StateSuspect or StateOpen.
//
// Root cause guarded: before v0.17.7, RecordRateLimited did not exist and a
// 429 was routed to RecordFailure, which drove the failure counter and could
// transition the tier into StateSuspect → StateOpen (the hard-failure path).
// A throttling upstream is not a broken upstream; treating it as a failure
// burns probe budget and can starve the tier indefinitely.
//
// inv-zen-225 pins the correct behaviour: calling RecordRateLimited on a
// CircuitBreaker MUST result in StateRateLimited, never StateSuspect or
// StateOpen, regardless of how many consecutive 429s are recorded.
package compliance

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
)

func TestInvZen225_RateLimitedNeverYieldsSuspectOrOpen(t *testing.T) {
	const name = "test-backend"

	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	cb.RecordRateLimited(name, 5*time.Second)

	st := cb.State(name)
	if st == orchestrator.StateSuspect || st == orchestrator.StateOpen {
		t.Errorf("inv-zen-225 VIOLATED: single RecordRateLimited yielded %s (want rate_limited)", st)
	}
	if st != orchestrator.StateRateLimited {
		t.Errorf("inv-zen-225: single RecordRateLimited yielded %s (want rate_limited)", st)
	}

	cb2 := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})
	for i := 0; i < 5; i++ {
		cb2.RecordRateLimited(name, 1*time.Second)
		st2 := cb2.State(name)
		if st2 == orchestrator.StateSuspect || st2 == orchestrator.StateOpen {
			t.Errorf("inv-zen-225 VIOLATED: after %d RecordRateLimited calls, state = %s (want rate_limited)", i+1, st2)
		}
	}

	cb3 := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 3,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	for i := 0; i < 3; i++ {
		cb3.RecordFailure(name)
	}
	if cb3.State(name) != orchestrator.StateSuspect {
		t.Fatalf("inv-zen-225 pre-cond: 3 RecordFailure calls did not reach StateSuspect (got %s)", cb3.State(name))
	}

	cb3.RecordSuccess(name)
	cb3.RecordRateLimited(name, 2*time.Second)
	st3 := cb3.State(name)
	if st3 == orchestrator.StateSuspect || st3 == orchestrator.StateOpen {
		t.Errorf("inv-zen-225 VIOLATED: RecordRateLimited after RecordSuccess yielded %s (want rate_limited)", st3)
	}
}

func TestInvZen225_RateLimitedDoesNotIncrementFailureCounter(t *testing.T) {
	const name = "sensitive"
	cb := orchestrator.NewCircuitBreaker(orchestrator.CircuitBreakerConfig{
		FailureThreshold: 1,
		Window:           5 * time.Minute,
		Cooldown:         10 * time.Minute,
	})

	cb.RecordRateLimited(name, 1*time.Second)

	st := cb.State(name)
	if st == orchestrator.StateSuspect {
		t.Errorf("inv-zen-225 VIOLATED: RecordRateLimited incremented the failure counter (threshold=1 yielded StateSuspect)")
	}
	if st == orchestrator.StateOpen {
		t.Errorf("inv-zen-225 VIOLATED: RecordRateLimited drove the breaker to StateOpen")
	}
}
