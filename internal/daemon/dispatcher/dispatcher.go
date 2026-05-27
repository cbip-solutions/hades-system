// SPDX-License-Identifier: MIT
// internal/daemon/dispatcher/dispatcher.go
//
// Dispatcher is the single-egress-point for LLM traffic from hades-system-side
// callers. Every
// LLM request flows through Forward, which:
//
// 1. Consults BreakerState for which named providers are currently permitted.
// 2. Forward resolves req.Profile to an ordered cascade of provider names via
// the ProfileResolver, then iterates the cascade — for each name it looks
// up the backend in the BackendRegistry, consults BreakerState by name, and
// attempts the call; the first success returns, otherwise it continues to
// the next name; an exhausted cascade returns ErrAllTiersUnavailable.
// 3. Emits one CostEvent per attempt (success or failure) for the cost
// ledger via the CostEmitter interface (real impl lands in
// cost_emit.go and cost ledger; tests use a recording stub).
// 4. Returns the upstream response unchanged on success, or
// ErrAllTiersUnavailable when no provider in the cascade could serve.
//
// Concurrency Forward is safe for concurrent invocation as long as the
// underlying TierBackend impls are (they document this themselves) and the
// supplied BreakerState + CostEmitter are. The Dispatcher itself holds no
// mutable state.
//
// Boundary (inv-hades-031): this package MUST NOT import internal/store. The
// dispatcheradapter package bridges Dispatcher to the store via the
// CostEmitter interface so this package stays decoupled from persistence.

package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/quota"
)

type CostEvent struct {
	Timestamp time.Time

	Project string

	SessionID string

	Profile string

	// Provider is the registry name of the backend that handled (or
	// attempted) this request — Backend.Name(). Added in release
	// (frozen contract C8): the dispatcher cascade iterates named
	// providers and the circuit breaker decides at Name granularity, so
	// cost MUST be attributable per-provider. Always populated, even on
	// failure / contract violation. Persisted in cost_ledger.provider
	// (migration 064, lands Task 9).
	Provider string

	Tier providers.Tier

	Model string

	InputTokens int

	OutputTokens int

	Status int

	LatencyMS int64

	Err string
}

// CostEmitter receives one CostEvent per Forward attempt. The real impl
// writes to cost_ledger; tests use an in-memory
// recorder. Emitter errors are intentionally swallowed by the dispatcher:
// a downstream-ledger blip MUST NOT shadow a successful LLM response, and
// runs a periodic audit (cost_ledger drift check) to catch any
// gaps. inv-hades-031: the emitter itself talks to internal/store, never the
// dispatcher.
type CostEmitter interface {
	Emit(ctx context.Context, evt CostEvent) error
}

// BreakerState is consulted before every backend attempt and updated
// after every outcome. The orchestrator's CircuitBreaker
// (circuit_breaker.go) implements the full state machine; the dispatcher
// consumes the read+write side here.
//
// Backend.Name()), NOT providers.Tier — the cascade iterates named
// providers, and two providers of one tier carry independent breaker
// state.
//
// Permit returns true when the named provider is currently eligible.
// Returning false MUST NOT generate a CostEvent (no upstream call
// happened).
//
// returns a *providers.RateLimitedError (HTTP 429). This enters a
// rate-limit cool-down instead of the fault/failure path — the provider
// is healthy (upstream said "slow down"), not broken.
type BreakerState interface {
	Permit(name string) bool
	RecordSuccess(name string)
	RecordFailure(name string)
	// RecordRateLimited records an upstream HTTP 429 response for the named
	// provider. retryAfter is the parsed Retry-After header value (0 if
	// absent). Implementations MUST NOT increment the failure counter;
	// 429 is a rate-limit signal, not a health degradation.
	RecordRateLimited(name string, retryAfter time.Duration)
}

var ErrAllTiersUnavailable = errors.New("dispatcher: all tiers unavailable")

type BackendRegistry interface {
	Get(name string) (providers.TierBackend, error)
}

type ProfileResolver interface {
	Resolve(profile, project string) ([]string, error)
}

type Dispatcher struct {
	registry BackendRegistry
	resolver ProfileResolver
	emitter  CostEmitter
	breaker  BreakerState

	seam atomic.Pointer[QuotaSeam]
}

type PreFlightFunc func(ctx context.Context, alias string, d doctrine.Name, deps quota.PreFlightDeps) (quota.PreFlightDecision, error)

type QuotaSeam struct {
	PreFlight PreFlightFunc

	OverrideStore quota.OverrideStore

	Wfq *quota.WfqQueue
}

func New(registry BackendRegistry, resolver ProfileResolver, emitter CostEmitter, breaker BreakerState) *Dispatcher {
	if registry == nil || resolver == nil || emitter == nil || breaker == nil {
		panic("dispatcher.New: all dependencies (registry, resolver, emitter, breaker) are required")
	}
	return &Dispatcher{
		registry: registry,
		resolver: resolver,
		emitter:  emitter,
		breaker:  breaker,
	}
}

// Forward resolves req.Profile to an ordered provider-name cascade and
// iterates it. Returns the upstream response on the first success, or
// ErrAllTiersUnavailable when no provider in the cascade could serve.
//
// Algorithm:
//
// 1. resolver.Resolve(req.Profile, req.Project) -> []name. A resolver
// error is returned to the caller verbatim (a misconfigured profile
// is a fail-fast condition, not a degraded path).
//
// 2. For each name in cascade order:
// a. registry.Get(name). On error (provider not registered — e.g.
// a Keychain-disabled backend) skip it: no CostEvent (no call
// happened), continue to the next name.
// b. breaker.Permit(name) == false -> skip: no CostEvent, continue.
// c. attempt(ctx, backend, name, req). On success return resp, nil.
// d. On failure the breaker outcome + CostEvent are already
// recorded by attempt(); continue to the next name.
//
// 3. After any failed attempt, if the caller's ctx is done, return
// ctx.Err() immediately and do NOT attempt the next provider
// (the caller has given up; another upstream RTT burns budget for a
// response that will be discarded).
//
// 4. Cascade exhausted with no success -> ErrAllTiersUnavailable
// (callers map this to 503 + Retry-After).
//
// Context handling: a ctx that is already done when Forward is entered
// surfaces as ctx.Err() before any provider is attempted. A ctx that
// becomes done mid-cascade surfaces after the in-flight attempt resolves.
// CostEvents for failed attempts are STILL recorded on cancel (
// drift-audit fidelity); only the further upstream call is suppressed.
//
// inv-hades-088 single-egress: every LLM dispatch from a hades-system caller
// flows through this method. The TierRequest is forwarded to backends
// unchanged; header injection / canonical body encoding / credential
// unwrapping are concerns of headers.go and the backends. inv-hades-068:
// the dispatcher never inspects credential values.
func (d *Dispatcher) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	cascade, err := d.resolver.Resolve(req.Profile, req.Project)
	if err != nil {
		return nil, fmt.Errorf("dispatcher: resolve profile %q: %w", req.Profile, err)
	}

	for _, name := range cascade {
		backend, getErr := d.registry.Get(name)
		if getErr != nil {

			continue
		}
		if !d.breaker.Permit(name) {

			continue
		}
		if resp, attemptErr := d.attempt(ctx, backend, name, req); attemptErr == nil {
			return resp, nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return nil, ErrAllTiersUnavailable
}

func (d *Dispatcher) attempt(
	ctx context.Context,
	backend providers.TierBackend,
	name string,
	req providers.TierRequest,
) (*providers.TierResponse, error) {
	tier := backend.Tier()
	start := time.Now()

	resp, err := backend.Forward(ctx, req)
	latency := time.Since(start).Milliseconds()

	if err == nil && resp == nil {
		violation := fmt.Errorf("dispatcher: backend %q returned nil response with nil error (contract violation)", name)
		d.breaker.RecordFailure(name)
		_ = d.emitter.Emit(ctx, CostEvent{
			Timestamp: start,
			Project:   req.Project,
			SessionID: req.SessionID,
			Profile:   req.Profile,
			Provider:  name,
			Tier:      tier,
			Model:     req.Model,
			LatencyMS: latency,
			Err:       "backend contract violation: nil response with nil error",
		})
		return nil, violation
	}

	if err != nil {

		var rl *providers.RateLimitedError
		switch {
		case errors.Is(err, providers.ErrToolsUnsupported):

		case errors.As(err, &rl):
			d.breaker.RecordRateLimited(name, rl.RetryAfter)
		default:
			d.breaker.RecordFailure(name)
		}
		_ = d.emitter.Emit(ctx, CostEvent{
			Timestamp: start,
			Project:   req.Project,
			SessionID: req.SessionID,
			Profile:   req.Profile,
			Provider:  name,
			Tier:      tier,
			Model:     req.Model,
			LatencyMS: latency,
			Err:       err.Error(),
		})
		return nil, err
	}

	d.breaker.RecordSuccess(name)
	_ = d.emitter.Emit(ctx, CostEvent{
		Timestamp:    start,
		Project:      req.Project,
		SessionID:    req.SessionID,
		Profile:      req.Profile,
		Provider:     name,
		Tier:         tier,
		Model:        req.Model,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		Status:       resp.Status,
		LatencyMS:    latency,
	})
	return resp, nil
}

func (d *Dispatcher) SetQuotaSeam(s QuotaSeam) {
	d.seam.Store(&s)
}

func (d *Dispatcher) OverrideStore() quota.OverrideStore {
	s := d.seam.Load()
	if s == nil {
		return nil
	}
	return s.OverrideStore
}

func (d *Dispatcher) Wfq() *quota.WfqQueue {
	s := d.seam.Load()
	if s == nil {
		return nil
	}
	return s.Wfq
}

// PreFlightCheck delegates to the configured PreFlight function (or
// quota.PreFlight if none injected). Returns the decision verbatim so
// the caller can branch on
// Allowed + SoftWarn + Reason without re-classifying.
//
// Empty alias is rejected at the seam — every dispatch must originate
// from a known project context. This is defense-in-depth on top of
// quota.PreFlight's own check: returning the dispatcher-flavoured
// error here gives operators a stack frame that points at the
// integration site (not just the leaf-level quota package).
//
// Defensive a Dispatcher constructed via New() (without SetQuotaSeam)
// reaches this method with seam == nil. The implementation falls
// through to quota.PreFlight directly so callers that build the
// dispatcher before wiring the seam still get correct behaviour. The
// PreFlight delegate inside the seam is also nil-checked for the same
// reason.
//
// inv-hades-080 invariant: this method is decision-only. It MUST NOT
// invoke a provider backend (tier1 / tier2.Forward), MUST NOT enqueue
// into the WFQ, MUST NOT emit a CostEvent. The accompanying tests
// assert these post-conditions explicitly (TestPreFlightCheckNeverCallsProviders,
// TestPreFlightCheckDoesNotMutateWfqDepth).
func (d *Dispatcher) PreFlightCheck(ctx context.Context, alias string, dn doctrine.Name, deps quota.PreFlightDeps) (quota.PreFlightDecision, error) {
	if alias == "" {
		return quota.PreFlightDecision{}, fmt.Errorf("dispatcher.PreFlightCheck: alias is empty")
	}
	if s := d.seam.Load(); s != nil && s.PreFlight != nil {
		return s.PreFlight(ctx, alias, dn, deps)
	}
	return quota.PreFlight(ctx, alias, dn, deps)
}
