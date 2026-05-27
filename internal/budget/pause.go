// SPDX-License-Identifier: MIT
// auto-resume scheduler. The four legitimate scopes are
// project / doctrine / stage / worker_id (per spec §2.2). Any other
// scope name is rejected; the storage layer is free-text but the
// engine guards.
//
// State semantics:
// - Trigger writes a row; auto_resume_at = 0 means "indefinite,
// requires explicit Resume"; positive value is the unix-ms moment
// after which the scheduler clears the row automatically.
// - Resume clears the row.
// - IsPaused returns true iff a row exists AND (auto_resume_at == 0
// OR auto_resume_at > now).
// - StartScheduler runs a goroutine that polls every `cadence` and
// clears expired rows. Stops on ctx.Done().
//
// Persist-first ordering: Trigger calls
// PauseSet BEFORE returning success; IsPaused reflects the persisted
// state. There is no in-memory cache that can diverge from storage.
//
// Concurrency contract: methods are goroutine-safe. State lives entirely
// in the BudgetStore; no in-memory cache. The scheduler goroutine started
// by StartScheduler observes the same store other callers write to.
// Concurrent Trigger + Resume on the same (scope, scope_value) pair are
// serialised by the underlying store; the final state reflects the last
// write (typical SQLite UPSERT semantics).
//
// inv-hades-079 anchor lives in enforce.go (precedence sort); this file
// is the storage + state side of the same invariant.
package budget

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrUnknownPauseScope = errors.New("budget: pause scope must be project/doctrine/stage/worker_id")

type PauseRow struct {
	Scope          string
	ScopeValue     string
	Reason         string
	StartedAtMs    int64
	AutoResumeAtMs int64
}

func ValidPauseScopes() []string {
	return []string{"project", "doctrine", "stage", "worker_id"}
}

func isValidPauseScope(scope string) bool {
	for _, s := range ValidPauseScopes() {
		if s == scope {
			return true
		}
	}
	return false
}

type Pauser struct {
	store BudgetStore
	clock func() time.Time
}

func NewPauser(store BudgetStore) *Pauser {
	if store == nil {
		panic("NewPauser: store is nil")
	}
	return &Pauser{store: store, clock: time.Now}
}

// SetClock injects a custom clock for deterministic scheduler tests.
// nil panics.
//
// Concurrency contract (post-review I-3 fix): SetClock is NOT
// goroutine-safe with concurrent Trigger / IsPaused / RunSchedulerOnce
// calls. The clock function pointer is mutated without serialisation.
// Tests MUST call SetClock before launching any goroutine that reads
// from the Pauser; production code MUST NOT call SetClock at all
// (the constructor seeds time.Now and that is the only production
// clock). This matches the precedent set by
// Gate.SetRollupWindow.
func (p *Pauser) SetClock(clock func() time.Time) {
	if clock == nil {
		panic("SetClock: clock is nil")
	}
	p.clock = clock
}

func (p *Pauser) Trigger(ctx context.Context, scope, scopeValue, reason string, duration time.Duration) error {
	if !isValidPauseScope(scope) {
		return fmt.Errorf("%w: got %q", ErrUnknownPauseScope, scope)
	}
	if scopeValue == "" {
		return errors.New("Trigger: scopeValue is empty")
	}
	if reason == "" {
		reason = "unspecified"
	}
	now := p.clock()
	startedMs := now.UnixMilli()
	autoResumeMs := int64(0)
	if duration > 0 {
		autoResumeMs = now.Add(duration).UnixMilli()
	}
	if err := p.store.PauseSet(ctx, scope, scopeValue, reason, startedMs, autoResumeMs); err != nil {
		return fmt.Errorf("PauseSet(%q,%q): %w", scope, scopeValue, err)
	}
	return nil
}

func (p *Pauser) Resume(ctx context.Context, scope, scopeValue string) error {
	if !isValidPauseScope(scope) {
		return fmt.Errorf("%w: got %q", ErrUnknownPauseScope, scope)
	}
	if err := p.store.PauseClear(ctx, scope, scopeValue); err != nil {
		return fmt.Errorf("PauseClear(%q,%q): %w", scope, scopeValue, err)
	}
	return nil
}

func (p *Pauser) IsPaused(ctx context.Context, scope, scopeValue string) (bool, error) {
	active, autoResumeMs, err := p.store.PauseGet(ctx, scope, scopeValue)
	if err != nil {
		return false, fmt.Errorf("PauseGet(%q,%q): %w", scope, scopeValue, err)
	}
	if !active {
		return false, nil
	}
	if autoResumeMs == 0 {
		return true, nil
	}
	return autoResumeMs > p.clock().UnixMilli(), nil
}

func (p *Pauser) ListActive(ctx context.Context) ([]PauseRow, error) {
	rows, err := p.store.PauseListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("PauseListActive: %w", err)
	}
	return rows, nil
}

func (p *Pauser) RunSchedulerOnce(ctx context.Context) error {
	rows, err := p.store.PauseListActive(ctx)
	if err != nil {
		return fmt.Errorf("PauseListActive: %w", err)
	}
	nowMs := p.clock().UnixMilli()
	for _, r := range rows {
		if r.AutoResumeAtMs == 0 {
			continue
		}
		if r.AutoResumeAtMs <= nowMs {
			if err := p.store.PauseClearIfExpired(ctx, r.Scope, r.ScopeValue, nowMs); err != nil {
				return fmt.Errorf("PauseClearIfExpired(%q,%q): %w", r.Scope, r.ScopeValue, err)
			}
		}
	}
	return nil
}

func (p *Pauser) StartScheduler(ctx context.Context, cadence time.Duration) error {
	if cadence <= 0 {
		return errors.New("StartScheduler: cadence must be > 0")
	}
	t := time.NewTicker(cadence)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			_ = p.RunSchedulerOnce(ctx)
		}
	}
}
