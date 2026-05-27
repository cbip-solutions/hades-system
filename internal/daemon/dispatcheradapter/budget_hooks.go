// SPDX-License-Identifier: MIT
// Package dispatcheradapter is the bridge from the release dispatcher to
// the release budget engine. It is the SINGLE import-edge between
// internal/daemon/dispatcher/ and internal/budget/, satisfying invariant:
// internal/budget/ never imports internal/store/ directly; this package
// imports both.
//
// # release surface
//
// BudgetAdapter implements budget.BudgetStore against
// *store.Store and exposes:
//
// - PreCall(scopes, caps, estimated) — hierarchical cap check
// - PostCall(costID, axisTags) — write axis tags only
// - PostCallWithCost(costID, axisTags, costUSD) — full chain: tags +
// anomaly window append + detector update + auto-pause-on-trigger
// - RunBudgetSchedulers(ctx) — pause auto-resume goroutine
//
// # Option A coordination (METHODOLOGY.md §4.7.5)
//
// branch but NOT yet merged to main; cost_ledger does not exist on this
// branch. The plan-spec called for PostCall to read cost_usd via a JOIN
// against cost_ledger. To allow to ship standalone:
//
// - PostCall(ctx, costID, axisTags) writes axis tags only (Tagger-only).
// - PostCallWithCost(ctx, costID, axisTags, costUSD) is the variant the
// dispatcher will call, passing the upstream response's cost_usd
// directly (already known at the call site without needing to
// re-query cost_ledger). This is structurally cleaner than the
// plan's read-after-write JOIN approach.
// - RolledUSDByAxis returns (0, nil). After release F-1 merges, a
// follow-up task will replace the body with the JOIN against
// cost_ledger; tests assert the (0, nil) contract today.
//
// # Boundary discipline
//
// internal/budget/ has zero imports of internal/store/. The BudgetStore
// interface declared in internal/budget/axes.go is the only surface
// the engine sees; this file is the only concrete implementation. The
// dispatcher (internal/daemon/dispatcher/) will call only the exported
// PreCall/PostCall(*) methods; it never reaches into internal/budget/
// directly (the AST grep test in tests/compliance/inv_zen_076_test.go
// enforces this once wires the import).
package dispatcheradapter

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/budget"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type BudgetAdapter struct {
	s        *store.Store
	gate     *budget.Gate
	tagger   *budget.AxisTagger
	detector *budget.AnomalyDetector
	pauser   *budget.Pauser
}

func NewBudgetAdapter(s *store.Store) *BudgetAdapter {
	if s == nil {
		panic("NewBudgetAdapter: store is nil")
	}
	a := &BudgetAdapter{s: s}
	a.gate = budget.NewGate(a)
	a.tagger = budget.NewAxisTagger(a)
	a.detector = budget.NewAnomalyDetector(a, 4.0, 60)
	a.pauser = budget.NewPauser(a)
	return a
}

func (a *BudgetAdapter) Pauser() *budget.Pauser { return a.pauser }

func (a *BudgetAdapter) Detector() *budget.AnomalyDetector { return a.detector }

func (a *BudgetAdapter) Tagger() *budget.AxisTagger { return a.tagger }

func (a *BudgetAdapter) Gate() *budget.Gate { return a.gate }

func (a *BudgetAdapter) SetAnomalyConfig(threshold float64, window int) {
	a.detector = budget.NewAnomalyDetector(a, threshold, window)
}

// PreCall is the invariant entry point. Dispatcher MUST call before
// every backend.Forward(...).
func (a *BudgetAdapter) PreCall(ctx context.Context, scopes budget.Scopes, caps budget.Caps, estimated float64) (budget.Decision, error) {
	return a.gate.Check(ctx, scopes, caps, estimated)
}

func (a *BudgetAdapter) PostCall(ctx context.Context, costID int64, axisTags map[string]string) error {
	return a.tagger.Tag(ctx, costID, axisTags)
}

func (a *BudgetAdapter) PostCallWithCost(ctx context.Context, costID int64, axisTags map[string]string, costUSD float64) error {
	if err := a.tagger.Tag(ctx, costID, axisTags); err != nil {
		return err
	}
	nowMs := time.Now().UnixMilli()
	for scope, value := range axisTagsRelevantToAnomaly(axisTags) {
		if err := a.AppendAnomalySampleByCostID(ctx, scope, value, costID, costUSD, nowMs); err != nil {
			return fmt.Errorf("AppendAnomalySampleByCostID(%q,%q,%d): %w", scope, value, costID, err)
		}
		res, err := a.detector.Update(ctx, scope, value, costUSD)
		if err != nil {
			return fmt.Errorf("anomaly Update(%q,%q): %w", scope, value, err)
		}
		if res.Triggered {
			if err := a.pauser.Trigger(ctx, scope, value,
				fmt.Sprintf("z_score=%.2f mean=%.4f std=%.4f", res.ZScore, res.Mean, res.Std),
				time.Hour); err != nil {
				return fmt.Errorf("anomaly-pause Trigger(%q,%q): %w", scope, value, err)
			}
		}
	}
	return nil
}

func (a *BudgetAdapter) AppendAnomalySample(ctx context.Context, scope, scopeValue string, sampleUSD float64, sampledAtMs int64) error {
	return appendAnomalySampleFn(a.s, scope, scopeValue, sampleUSD, sampledAtMs)
}

// appendAnomalySampleFn is a test seam: production calls
// store.AppendAnomalySample; tests may override to inject errors.
//
// NOTE(release): tests using this seam MUST NOT call t.Parallel() — the seam is
// process-global and concurrent overrides race. Same applies to
// appendAnomalySampleByCostIDFn below.
var appendAnomalySampleFn = func(s *store.Store, scope, scopeValue string, sampleUSD float64, sampledAtMs int64) error {
	return store.AppendAnomalySample(s.DB(), scope, scopeValue, sampleUSD, sampledAtMs)
}

func (a *BudgetAdapter) AppendAnomalySampleByCostID(ctx context.Context, scope, scopeValue string, costID int64, sampleUSD float64, sampledAtMs int64) error {
	return appendAnomalySampleByCostIDFn(a.s, scope, scopeValue, costID, sampleUSD, sampledAtMs)
}

// appendAnomalySampleByCostIDFn is a test seam: production calls
// store.AppendAnomalySampleByCostID; tests may override to inject errors.
//
// NOTE(release): tests using this seam MUST NOT call t.Parallel() — the seam is
// process-global and concurrent overrides race.
var appendAnomalySampleByCostIDFn = func(s *store.Store, scope, scopeValue string, costID int64, sampleUSD float64, sampledAtMs int64) error {
	return store.AppendAnomalySampleByCostID(s.DB(), scope, scopeValue, costID, sampleUSD, sampledAtMs)
}

func axisTagsRelevantToAnomaly(axisTags map[string]string) map[string]string {
	out := map[string]string{}
	for _, scope := range budget.ValidPauseScopes() {
		if v, ok := axisTags[scope]; ok && v != "" {
			out[scope] = v
		}
	}
	return out
}

func (a *BudgetAdapter) RunBudgetSchedulers(ctx context.Context) error {
	return a.pauser.StartScheduler(ctx, time.Minute)
}

func (a *BudgetAdapter) InsertCostAxisTag(ctx context.Context, costID int64, name, value string) error {
	return store.InsertCostAxisTag(a.s.DB(), costID, name, value)
}

func (a *BudgetAdapter) EmitAxisTagLoss(ctx context.Context, costID int64, missingAxis string) error {
	return store.EmitAxisTagLoss(a.s.DB(), costID, missingAxis)
}

func (a *BudgetAdapter) QueryAxisTags(ctx context.Context, costID int64) (map[string]string, error) {
	tags, err := store.QueryCostAxisTags(a.s.DB(), costID)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, t := range tags {
		out[t.AxisName] = t.AxisValue
	}
	return out, nil
}

func (a *BudgetAdapter) QueryCostIDsByAxis(ctx context.Context, name, value string) ([]int64, error) {
	return store.QueryCostIDsByAxis(a.s.DB(), name, value)
}

func (a *BudgetAdapter) QueryAxisTagLosses(ctx context.Context, costID int64) ([]string, error) {
	rows, err := store.QueryAxisTagLosses(a.s.DB(), costID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.MissingAxis)
	}
	return out, nil
}

func (a *BudgetAdapter) PauseGet(ctx context.Context, scope, scopeValue string) (bool, int64, error) {
	return store.GetBudgetPause(a.s.DB(), scope, scopeValue)
}

func (a *BudgetAdapter) PauseSet(ctx context.Context, scope, scopeValue, reason string, startedAtMs, autoResumeAtMs int64) error {
	if startedAtMs == 0 {
		startedAtMs = time.Now().UnixMilli()
	}
	return upsertBudgetPauseFn(a.s, scope, scopeValue, reason, startedAtMs, autoResumeAtMs)
}

var upsertBudgetPauseFn = func(s *store.Store, scope, scopeValue, reason string, startedAtMs, autoResumeAtMs int64) error {
	return store.UpsertBudgetPause(s.DB(), scope, scopeValue, reason, startedAtMs, autoResumeAtMs)
}

func (a *BudgetAdapter) PauseClear(ctx context.Context, scope, scopeValue string) error {
	return store.DeleteBudgetPause(a.s.DB(), scope, scopeValue)
}

func (a *BudgetAdapter) PauseClearIfExpired(ctx context.Context, scope, scopeValue string, beforeMs int64) error {
	return deleteBudgetPauseIfExpiredFn(a.s, scope, scopeValue, beforeMs)
}

var deleteBudgetPauseIfExpiredFn = func(s *store.Store, scope, scopeValue string, beforeMs int64) error {
	return store.DeleteBudgetPauseIfExpired(s.DB(), scope, scopeValue, beforeMs)
}

func (a *BudgetAdapter) PauseListActive(ctx context.Context) ([]budget.PauseRow, error) {
	rows, err := store.ListActiveBudgetPauses(a.s.DB())
	if err != nil {
		return nil, err
	}
	out := make([]budget.PauseRow, 0, len(rows))
	for _, r := range rows {
		var autoMs int64
		if !r.AutoResumeAt.IsZero() {
			autoMs = r.AutoResumeAt.UnixMilli()
		}
		out = append(out, budget.PauseRow{
			Scope:          r.Scope,
			ScopeValue:     r.ScopeValue,
			Reason:         r.Reason,
			StartedAtMs:    r.StartedAt.UnixMilli(),
			AutoResumeAtMs: autoMs,
		})
	}
	return out, nil
}

func (a *BudgetAdapter) AnomalyAppend(ctx context.Context, row budget.AnomalyRow) error {
	return insertBudgetAnomalyFn(a.s, row.Scope, row.ScopeValue, row.ZScore, row.Mean, row.Std, row.WindowSize, row.DetectedAt.UnixMilli())
}

var insertBudgetAnomalyFn = func(s *store.Store, scope, scopeValue string, zScore, mean, std float64, windowSize int, detectedAtMs int64) error {
	return store.InsertBudgetAnomaly(s.DB(), scope, scopeValue, zScore, mean, std, windowSize, detectedAtMs)
}

func (a *BudgetAdapter) AnomalyWindow(ctx context.Context, scope, scopeValue string, limit int) ([]float64, error) {
	return queryAnomalyWindowFn(a.s, scope, scopeValue, limit)
}

var queryAnomalyWindowFn = func(s *store.Store, scope, scopeValue string, limit int) ([]float64, error) {
	return store.QueryAnomalyWindow(s.DB(), scope, scopeValue, limit)
}

func (a *BudgetAdapter) RolledUSDByAxis(ctx context.Context, axisName, axisValue string, sinceMs int64) (float64, error) {

	return 0, nil
}
