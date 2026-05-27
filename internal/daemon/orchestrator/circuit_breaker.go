// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/circuit_breaker.go
//
// CircuitBreaker per-provider state machine: closed → suspect → open → suspect.
// Driven by TierHealth (consecutive failures over rolling window) + cooldown
// timer + recovery probes.
//
// == Backend.Name()), NOT by providers.Tier. Two backends of the same
// tier (e.g. deepseek-direct and siliconflow-deepseek, both
// TierGenericOpenAICompat) therefore carry independent breaker state —
// a failover between them is observable, which a per-Tier key could not
// express.
//
// State transitions:
// - closed (default): provider permitted; outcomes feed the rolling-window
// TierHealth. On N consecutive failures (FailureThreshold) the breaker
// transitions to suspect.
// - suspect: provider denied; AttemptRecovery may be called by the recovery
// scheduler to invoke backend.Probe. Probe success → closed
// (and the underlying TierHealth is reset so the next outcome stream
// starts fresh). Probe failure → open with openAt = time.Now().
// - open: provider denied; AttemptRecovery is a no-op until time.Since(openAt)
// >= Cooldown, after which the same suspect → probe path runs.
// - ANY → closed on RecordSuccess (a successful real-traffic call is the
// strongest signal of recovery).
//
// Concurrency model:
// - One sync.Mutex (cb.mu) protects the cb.breakers map and every tierState.
// - AttemptRecovery uses a two-phase lock pattern: it acquires the lock
// to read state and decide whether to probe, releases the lock so the
// probe (potentially slow network round-trip) does not block other
// providers, then re-acquires the lock to commit the post-probe state.
// The tierState pointer is re-fetched after re-locking; in practice
// this returns the same pointer (entries are never deleted) but the
// re-fetch is a defensive guard against future map-replacement changes.
// - All other methods (Permit, RecordSuccess, RecordFailure, State)
// are simple Lock/defer-Unlock — fast, no I/O.
//
// Boundary (inv-hades-031): this file imports stdlib + internal/providers
// only. The orchestrator package MUST NOT import internal/store directly;
// boundary crossings flow through internal/daemon/dispatcheradapter.

package orchestrator

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

type State int

const (
	StateClosed State = iota

	StateSuspect

	StateOpen

	StateRateLimited
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateSuspect:
		return "suspect"
	case StateOpen:
		return "open"
	case StateRateLimited:
		return "rate_limited"
	default:
		return "unknown"
	}
}

type CircuitBreakerConfig struct {
	FailureThreshold int

	Window time.Duration

	Cooldown time.Duration
}

type CircuitBreaker struct {
	config   CircuitBreakerConfig
	mu       sync.Mutex
	breakers map[string]*tierState
}

type tierState struct {
	health        *TierHealth
	state         State
	openAt        time.Time
	cooldownUntil time.Time
	rlAttempt     int
}

func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold < 1 {
		cfg.FailureThreshold = 3
	}
	if cfg.Window <= 0 {
		cfg.Window = 5 * time.Minute
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 10 * time.Minute
	}
	return &CircuitBreaker{
		config:   cfg,
		breakers: map[string]*tierState{},
	}
}

// getOrCreateLocked returns the tierState for the provider name, creating
// it lazily on first access. Caller MUST hold cb.mu.
func (cb *CircuitBreaker) getOrCreateLocked(name string) *tierState {
	ts, ok := cb.breakers[name]
	if !ok {
		ts = &tierState{
			health: NewTierHealth(cb.config.Window),
			state:  StateClosed,
		}
		cb.breakers[name] = ts
	}
	return ts
}

func (cb *CircuitBreaker) Permit(name string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ts := cb.getOrCreateLocked(name)
	if ts.state == StateRateLimited {
		if time.Now().Before(ts.cooldownUntil) {
			return false
		}

		ts.state = StateClosed
	}
	return ts.state == StateClosed
}

func (cb *CircuitBreaker) RecordSuccess(name string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ts := cb.getOrCreateLocked(name)
	ts.health.RecordSuccess()
	ts.state = StateClosed
	ts.rlAttempt = 0
}

func (cb *CircuitBreaker) RecordFailure(name string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ts := cb.getOrCreateLocked(name)
	ts.health.RecordFailure()

	if ts.state == StateClosed && ts.health.ConsecutiveFailures() >= cb.config.FailureThreshold {
		ts.state = StateSuspect
	}
}

func (cb *CircuitBreaker) RecordRateLimited(name string, retryAfter time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ts := cb.getOrCreateLocked(name)
	ts.rlAttempt++
	ts.state = StateRateLimited
	ts.cooldownUntil = time.Now().Add(rateLimitCooldown(retryAfter, ts.rlAttempt))
}

// AttemptRecovery is called by the recovery scheduler or
// on-demand to attempt to heal a Suspect or post-cooldown Open provider.
// Returns true iff the provider is StateClosed after this call (heal).
//
// Behaviour by current state:
// - Closed: returns true immediately (already healthy).
// - Suspect: invokes backend.Probe. Success → Closed (true), TierHealth
// reset. Failure → Open (false), openAt = time.Now().
// - Open with cooldown not elapsed: no probe; returns false.
// - Open with cooldown elapsed: transitions to Suspect, then runs the
// suspect probe path above.
//
// Concurrency the probe runs OUTSIDE the breaker's lock. A slow probe
// MUST NOT block other providers' RecordSuccess / RecordFailure / Permit
// calls. The two-phase lock pattern is load-bearing.
func (cb *CircuitBreaker) AttemptRecovery(ctx context.Context, backend providers.TierBackend) bool {
	name := backend.Name()

	cb.mu.Lock()
	ts := cb.getOrCreateLocked(name)

	switch ts.state {
	case StateClosed:

		cb.mu.Unlock()
		return true
	case StateRateLimited:

		cb.mu.Unlock()
		return false
	case StateOpen:
		if time.Since(ts.openAt) < cb.config.Cooldown {

			cb.mu.Unlock()
			return false
		}

		ts.state = StateSuspect
	case StateSuspect:

	}
	cb.mu.Unlock()

	probeErr := backend.Probe(ctx)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	ts = cb.getOrCreateLocked(name)
	if probeErr == nil {

		ts.state = StateClosed
		ts.health = NewTierHealth(cb.config.Window)
		return true
	}

	ts.state = StateOpen
	ts.openAt = time.Now()
	return false
}

func (cb *CircuitBreaker) State(name string) State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	ts := cb.getOrCreateLocked(name)
	return ts.state
}

const rateLimitCooldownCap = 300 * time.Second

func rateLimitCooldown(retryAfter time.Duration, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := time.Second << uint(min(attempt-1, 9))
	if base > rateLimitCooldownCap {
		base = rateLimitCooldownCap
	}

	jittered := time.Duration(randInt63n(int64(base)) + 1)

	if jittered < retryAfter {
		jittered = retryAfter
	}

	if jittered > rateLimitCooldownCap && retryAfter <= rateLimitCooldownCap {
		jittered = rateLimitCooldownCap
	}
	return jittered
}

var randInt63n = randInt63nImpl

func randInt63nImpl(n int64) int64 {
	return rand.Int63n(n)
}

func RateLimitCooldownTestable(retryAfter time.Duration, attempt int) time.Duration {
	return rateLimitCooldown(retryAfter, attempt)
}
