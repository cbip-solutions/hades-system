// SPDX-License-Identifier: MIT
// Package quotaadapter bridges *store.Store to internal/quota's
// OverrideStore interface (Layer 3 operator override spec
// §1 Q10).
//
// invariant boundary: internal/quota MUST NOT import internal/store.
// This adapter is the only package permitted to translate between
// quota.Override (value type owned by the quota package) and
// store.PriorityOverrideRow (SQLite-backed row owned by store). The
// import list of this file is the single legitimate co-location of
// internal/quota and internal/store anywhere in the codebase, enforced
// by the invariant compliance test in
// tests/compliance/inv_hades_122_inv_hades_031_plan7_packages_test.go.
//
// invariant audit hook: every Set / Reset emits an event row in the
// shared events table inside the SAME transaction as the
// priority_overrides change. Atomicity is load-bearing — release hash-
// chain integrity depends on event-row presence whenever the
// priority_overrides row mutates. The transaction commits only after
// BOTH the row write AND the audit event row write succeed; a failure
// rolls both back via the deferred Rollback closure pattern.
//
// Type translation strategy: each adapter method does a field-by-field
// copy between the quota-side type (quota.Override) and the store-side
// type (store.PriorityOverrideRow). Two type sets are intentional,
// mirroring the bypassadapter / projectctxadapter pattern:
// 1. The quota package never gains a transitive SQLite dependency so
// unit tests stay fast and run cross-platform.
// 2. Future schema changes (e.g., adding a column) absorb here without
// rippling into internal/quota.
//
// Time precision: the priority_overrides schema (migration 060) stores
// expires_at + created_at as TIMESTAMP (RFC3339 TEXT under the
// ncruces/go-sqlite3 driver). The adapter normalises both to UTC before
// persisting; reads return UTC. Tests assert time.Equal (zone-agnostic
// comparison) plus zone-explicit equality.
package quotaadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/quota"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	s *store.Store
}

func New(s *store.Store) *Adapter {
	if s == nil {
		panic("quotaadapter.New: store is nil")
	}
	return &Adapter{s: s}
}

// NewOverrideStore returns the adapter typed as quota.OverrideStore.
// Daemon bootstrap consumes via this constructor; the wider
// *Adapter exposes other helpers daemon bootstrap doesn't need.
//
// The *store.Store argument MUST be non-nil; a nil store causes the
// underlying New to panic per the defensive contract.
func NewOverrideStore(s *store.Store) quota.OverrideStore {
	return New(s)
}

var _ quota.OverrideStore = (*Adapter)(nil)

func (a *Adapter) Get(ctx context.Context, alias string) (*quota.Override, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("quotaadapter.Get: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		return nil, fmt.Errorf("%w: alias is empty", quota.ErrInvalidOverride)
	}
	row, err := a.s.GetPriorityOverride(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("quotaadapter.Get(%q): %w", alias, err)
	}
	if row == nil {
		return nil, nil
	}
	ov := translateRowToOverride(*row)
	return &ov, nil
}

func (a *Adapter) Set(ctx context.Context, alias string, multiplier float64, expiresAt time.Time, reason string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("quotaadapter.Set: %w", err)
	}
	now := time.Now()
	if err := validateAdapterArgs(alias, multiplier, expiresAt, reason, now); err != nil {
		return err
	}
	row := store.PriorityOverrideRow{
		ProjectAlias: alias,
		Multiplier:   multiplier,
		ExpiresAt:    expiresAt.UTC(),
		Reason:       reason,
		CreatedAt:    now.UTC(),
	}
	tx, err := a.s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("quotaadapter.Set(%q): begin tx: %w", alias, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	replaced, err := a.s.UpsertPriorityOverrideTx(ctx, tx, row)
	if err != nil {
		return fmt.Errorf("quotaadapter.Set(%q): %w", alias, err)
	}
	if replaced {
		payload := buildPayload(alias, multiplier, expiresAt.UTC(), reason, "replaced")
		if err := a.s.InsertEventTx(ctx, tx, "quota.priority_boost.replaced", payload); err != nil {
			return fmt.Errorf("quotaadapter.Set(%q): emit replaced event: %w", alias, err)
		}
	}
	payload := buildPayload(alias, multiplier, expiresAt.UTC(), reason, "set")
	if err := a.s.InsertEventTx(ctx, tx, "quota.priority_boost.set", payload); err != nil {
		return fmt.Errorf("quotaadapter.Set(%q): emit set event: %w", alias, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("quotaadapter.Set(%q): commit: %w", alias, err)
	}
	committed = true
	return nil
}

func (a *Adapter) Reset(ctx context.Context, alias string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("quotaadapter.Reset: %w", err)
	}
	if strings.TrimSpace(alias) == "" {
		return fmt.Errorf("%w: alias is empty", quota.ErrInvalidOverride)
	}
	tx, err := a.s.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("quotaadapter.Reset(%q): begin tx: %w", alias, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := a.s.DeletePriorityOverrideTx(ctx, tx, alias); err != nil {
		return fmt.Errorf("quotaadapter.Reset(%q): %w", alias, err)
	}
	payload := buildPayload(alias, 0, time.Time{}, "", "reset")
	if err := a.s.InsertEventTx(ctx, tx, "quota.priority_boost.reset", payload); err != nil {
		return fmt.Errorf("quotaadapter.Reset(%q): emit reset event: %w", alias, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("quotaadapter.Reset(%q): commit: %w", alias, err)
	}
	committed = true
	return nil
}

func (a *Adapter) List(ctx context.Context) ([]quota.Override, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("quotaadapter.List: %w", err)
	}
	rows, err := a.s.ListPriorityOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("quotaadapter.List: %w", err)
	}
	out := make([]quota.Override, 0, len(rows))
	for _, r := range rows {
		out = append(out, translateRowToOverride(r))
	}
	return out, nil
}

func translateRowToOverride(r store.PriorityOverrideRow) quota.Override {
	return quota.Override{
		Alias:      r.ProjectAlias,
		Multiplier: r.Multiplier,
		ExpiresAt:  r.ExpiresAt.UTC(),
		Reason:     r.Reason,
		CreatedAt:  r.CreatedAt.UTC(),
	}
}

// validateAdapterArgs duplicates internal/quota.validateOverrideArgs
// because the latter is unexported. Keeping a local copy avoids
// exporting a purely-validation helper from the quota package while
// preserving defence in depth — validation happens BOTH at adapter Set
// AND inside the WFQ hot-path's ApplyOverride.
//
// The rule set MUST track quota/override.go in lockstep: alias non-
// empty (after trim), multiplier strictly > 0 and ≤ 100, expires_at
// strictly after now, reason non-empty (after trim).
//
// All errors wrap quota.ErrInvalidOverride so callers can
// errors.Is(err, quota.ErrInvalidOverride) regardless of which layer
// rejected.
func validateAdapterArgs(alias string, mult float64, expiresAt time.Time, reason string, now time.Time) error {
	if strings.TrimSpace(alias) == "" {
		return fmt.Errorf("%w: alias is empty", quota.ErrInvalidOverride)
	}
	if mult <= 0 {
		return fmt.Errorf("%w: multiplier must be > 0 (got %v)", quota.ErrInvalidOverride, mult)
	}
	if mult > 100 {
		return fmt.Errorf("%w: multiplier %v exceeds sanity ceiling 100 (likely operator typo)",
			quota.ErrInvalidOverride, mult)
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("%w: expiresAt %v is not strictly after now %v",
			quota.ErrInvalidOverride, expiresAt, now)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: reason is empty (audit trail demands operator intent)",
			quota.ErrInvalidOverride)
	}
	return nil
}

type auditPayload struct {
	Alias      string    `json:"alias"`
	Multiplier float64   `json:"multiplier"`
	ExpiresAt  time.Time `json:"expires_at"`
	Reason     string    `json:"reason"`
	Action     string    `json:"action"`
}

func buildPayload(alias string, mult float64, expiresAt time.Time, reason, action string) string {
	p := auditPayload{
		Alias:      alias,
		Multiplier: mult,
		ExpiresAt:  expiresAt,
		Reason:     reason,
		Action:     action,
	}
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf(`{"alias":%q,"action":%q,"err":%q}`, alias, action, err.Error())
	}
	return string(b)
}
