// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/pin_overrides.go
//
// operator-facing pin surface (Set / Unset / Resolve / ListAll / UnpinAll)
// and owns the 5-min TTL sweep goroutine. Hierarchy resolution:
// session > project > global; first hit wins. Expired pins (ExpiresAt
// before/equal-to now) are filtered on read AND swept periodically — so
// the caller never honours a stale pin between sweep ticks.
//
// Boundary: this file imports stdlib only (context, errors,
// fmt, log/slog, sync, time). The orchestrator package MUST NOT import
// internal/store (master plan v2.0 §92 + system-design umbrella §879 +
// B-7 commit body). Pin row type is mirrored locally; the F-6-style
// dispatcheradapter (extended in I-4) performs 1:1 translation
// orchestrator.PinRow ↔ store.PinRow with a reflective parity test.
//
// Why mirror not import (F-5 / F-7 precedent):
// - F-5 pioneered the pattern with CostLedgerRow + ErrDuplicateIdempotency.
// - F-7 reused it for RebuildFromLedger.
// - I-2 continues the pattern for PinRow.
//
// Two type sets, intentionally identical in shape — keeps the boundary
// clean and preserves unit-test independence (orchestrator tests do not
// transitively pull SQLite). Future store-side schema changes are absorbed
// by the adapter, not rippled into the orchestrator.
//
// Concurrency PinOverrides itself is stateless after construction (the
// only field beyond `store` is `tickInterval`, set at construction or by
// the test fixture before StartTTLSweep is invoked). Set / Unset / Resolve
// / ListAll / UnpinAll are safe for concurrent invocation as long as the
// supplied PinStore is — the production *store.Store via dispatcheradapter
// is goroutine-safe. StartTTLSweep spawns one goroutine that exits when
// ctx is Done; the returned channel closes after exit so the daemon can
// wait on graceful shutdown (mirror F-7 StartHourlyMaintenance + D-3
// RecoveryScheduler.Run).

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// PinRow orchestrator-side pin row shape. Mirror of store.PinRow
// (intentionally identical) so dispatcheradapter performs 1:1 field-by-field
// forwarding. Keeping the type local maintains invariant boundary
// (orchestrator MUST NOT import internal/store; bridge via dispatcheradapter).
//
// Why mirror not import: master plan v2.0 §92 + system-design §879
// declare orchestrator/providers/dispatcher MUST NOT import
// internal/store. F-5 pioneered this pattern with CostLedgerRow; F-7
// built on it; I-2 continues.
//
// Field-for-field identical to store.PinRow (verified by reflective
// parity test in dispatcheradapter once I-4 lands). 8 fields total:
// ID, Scope, ScopeID, Tier, Provider, SetAt, ExpiresAt, Reason.
//
// Scope vocabulary: "session" | "project" | "global". ScopeID is "" for
// the global scope (matches store-side empty-string-not-NULL convention
// for the SQLite UNIQUE constraint). ExpiresAt is nil for permanent
// pins; non-nil for TTL pins.
type PinRow struct {
	ID        int64
	Scope     string
	ScopeID   string
	Tier      string
	Provider  string
	SetAt     time.Time
	ExpiresAt *time.Time
	Reason    string
}

type PinStore interface {
	Insert(p PinRow) error
	Delete(scope, scopeID string) error
	Query(scope, scopeID string) (*PinRow, error)
	ListAll() ([]PinRow, error)
	PurgeExpired(now time.Time) (int, error)
}

var defaultTTLSweepInterval = 5 * time.Minute

type PinOverrides struct {
	store PinStore

	tickInterval time.Duration
}

func NewPinOverrides(s PinStore) *PinOverrides {
	if s == nil {
		panic("NewPinOverrides: store is nil")
	}
	return &PinOverrides{store: s}
}

func (p *PinOverrides) Set(scope, scopeID, tier, provider string, ttl time.Duration, reason string) error {
	if scope == "" || tier == "" {
		return errors.New("Set: scope and tier are required")
	}
	if scope != "session" && scope != "project" && scope != "global" {
		return fmt.Errorf("Set: scope must be session|project|global, got %q", scope)
	}
	if scope == "global" && scopeID != "" {
		return errors.New("Set: scope=global requires empty scopeID")
	}
	if scope != "global" && scopeID == "" {
		return fmt.Errorf("Set: scope=%q requires non-empty scopeID", scope)
	}
	now := time.Now().UTC()
	row := PinRow{
		Scope:    scope,
		ScopeID:  scopeID,
		Tier:     tier,
		Provider: provider,
		SetAt:    now,
		Reason:   reason,
	}
	if ttl > 0 {
		exp := now.Add(ttl)
		row.ExpiresAt = &exp
	}
	if err := p.store.Insert(row); err != nil {
		return fmt.Errorf("Set: insert pin: %w", err)
	}
	return nil
}

func (p *PinOverrides) Unset(scope, scopeID string) error {
	if scope == "" {
		return errors.New("Unset: scope is required")
	}
	if err := p.store.Delete(scope, scopeID); err != nil {
		return fmt.Errorf("Unset: delete pin: %w", err)
	}
	return nil
}

// Resolve walks the hierarchy session > project > global, returning the
// first pin whose ExpiresAt is nil OR strictly After(now). session and
// project may be empty to signal "no scope known" — those scopes are
// skipped; global is always tried last.
//
// Returns (nil, nil) when no pin applies. SQL errors propagate as
// (nil, err) wrapped.
//
// Boundary semantic for ExpiresAt:
// - ExpiresAt nil → permanent pin → match.
// - ExpiresAt.After(now) → not yet expired → match.
// - !ExpiresAt.After(now) → at-or-past boundary → expired, skip.
//
// The "at boundary" treatment matches I-1's PurgeExpiredPins strict-`<`
// SQL boundary (a pin with ExpiresAt == now is filtered by Resolve but
// NOT yet purged by sweep). The asymmetry is intentional: read-time
// filtering errs toward "do not honour ambiguous pin"; sweep-time
// filtering errs toward "do not delete prematurely". The pin will be
// purged on the next sweep tick after time advances.
func (p *PinOverrides) Resolve(sessionID, projectID string) (*PinRow, error) {
	now := time.Now().UTC()
	candidates := make([][2]string, 0, 3)
	if sessionID != "" {
		candidates = append(candidates, [2]string{"session", sessionID})
	}
	if projectID != "" {
		candidates = append(candidates, [2]string{"project", projectID})
	}
	candidates = append(candidates, [2]string{"global", ""})

	for _, c := range candidates {
		row, err := p.store.Query(c[0], c[1])
		if err != nil {
			return nil, fmt.Errorf("Resolve: query %s/%s: %w", c[0], c[1], err)
		}
		if row == nil {
			continue
		}
		if row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
			// Stale pin between sweep ticks; skip (do NOT return). Sweep
			// will reap on next tick.
			continue
		}
		return row, nil
	}
	return nil, nil
}

// ListAll returns every non-expired pin. Order matches the underlying
// store's ListAll (newest-first in production via ORDER BY set_at DESC;
// fakePinStore returns map iteration order — tests do not assert order).
func (p *PinOverrides) ListAll() ([]PinRow, error) {
	rows, err := p.store.ListAll()
	if err != nil {
		return nil, fmt.Errorf("ListAll: %w", err)
	}
	now := time.Now().UTC()
	out := make([]PinRow, 0, len(rows))
	for i := range rows {
		r := rows[i]
		if r.ExpiresAt != nil && !r.ExpiresAt.After(now) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (p *PinOverrides) UnpinAll() (int, error) {
	rows, err := p.store.ListAll()
	if err != nil {
		return 0, fmt.Errorf("UnpinAll: list: %w", err)
	}
	deleted := 0
	for _, r := range rows {
		if err := p.store.Delete(r.Scope, r.ScopeID); err != nil {
			return deleted, fmt.Errorf("UnpinAll: delete %s/%s: %w", r.Scope, r.ScopeID, err)
		}
		deleted++
	}
	return deleted, nil
}

func (p *PinOverrides) StartTTLSweep(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	tick := p.tickInterval
	if tick <= 0 {
		tick = defaultTTLSweepInterval
	}
	go func() {
		defer close(done)
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.runSweep()
			}
		}
	}()
	return done
}

func (p *PinOverrides) runSweep() {
	purged, err := p.store.PurgeExpired(time.Now().UTC())
	if err != nil {
		slog.Warn("orchestrator: pin TTL sweep failed", "err", err)
		return
	}
	if purged > 0 {
		slog.Info("orchestrator: pin TTL sweep", "purged", purged)
	}
}
