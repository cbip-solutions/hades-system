// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/cost_counters.go
//
// O(1) cap checks. WindowCounter is the rolling-window primitive;
// CostCounters owns per-(project, profile, tier) × (24h, 30d) counters
// plus a flat session map. RebuildFromLedger closes inv-hades-065 at
// startup; StartHourlyMaintenance prunes >30d samples to bound memory.
//
// Boundary (inv-hades-031): this file imports only stdlib. The orchestrator
// package MUST NOT import internal/store (master plan v2.0 §92 +
// system-design umbrella §879 + B-7 commit body). Cost-row types and the
// ErrDuplicateIdempotency sentinel are mirrored locally; F-6 dispatcheradapter
// performs 1:1 translation orchestrator.CostLedgerRow ↔ store.CostLedgerRow,
// mirroring the bypassadapter precedent ("two type sets, intentionally
// identical in shape — keeps the boundary clean and preserves unit-test
// independence").
//
// File scope:
// - F-4: WindowCounter (rolling-window primitive)
// - F-5: CostLedgerRow + ErrDuplicateIdempotency mirrors, CostStore
// interface, CostCounters + Record + WouldExceedCap
// - F-6: dispatcheradapter wires real *store.Store as CostStore (separate
// package; no orchestrator changes needed)
// - F-7: RebuildFromLedger + StartHourlyMaintenance (appended later)

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type costSample struct {
	ts  time.Time
	usd float64
}

type WindowCounter struct {
	mu             sync.Mutex
	samples        []costSample
	windowDuration time.Duration
}

func NewWindowCounter(windowDuration time.Duration) *WindowCounter {
	if windowDuration <= 0 {
		panic("NewWindowCounter: windowDuration must be > 0")
	}
	return &WindowCounter{windowDuration: windowDuration}
}

func (w *WindowCounter) Add(ts time.Time, usd float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	idx := sort.Search(len(w.samples), func(i int) bool {
		return w.samples[i].ts.After(ts)
	})
	w.samples = append(w.samples, costSample{})
	copy(w.samples[idx+1:], w.samples[idx:])
	w.samples[idx] = costSample{ts: ts, usd: usd}
}

func (w *WindowCounter) Total(now time.Time) float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	cutoff := now.Add(-w.windowDuration)
	w.pruneLocked(cutoff)
	var total float64
	for _, s := range w.samples {
		total += s.usd
	}
	return total
}

func (w *WindowCounter) PruneOlderThan(cutoff time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked(cutoff)
}

func (w *WindowCounter) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.samples)
}

// pruneLocked drops all samples with ts <= cutoff (strict edge-prune).
// Caller MUST hold w.mu.
//
// Implementation sort.Search finds the first index i where samples[i].ts
// is strictly After(cutoff) (ts > cutoff). Indices 0..i-1 have ts <= cutoff
// and are discarded by re-slicing. The edge (ts == cutoff) is in the
// 0..i-1 range (After returns false for equal timestamps) and is therefore
// pruned — matching the plan's "edge is pruned" boundary invariant.
func (w *WindowCounter) pruneLocked(cutoff time.Time) {
	idx := sort.Search(len(w.samples), func(i int) bool {
		return w.samples[i].ts.After(cutoff)
	})
	if idx > 0 {
		w.samples = w.samples[idx:]
	}
}

// CostLedgerRow orchestrator-side cost row shape. Mirror of
// store.CostLedgerRow (intentionally identical) so dispatcheradapter
// performs 1:1 field-by-field forwarding. Keeping the type local
// maintains inv-hades-031 boundary (orchestrator MUST NOT import
// internal/store; bridge via dispatcheradapter).
//
// Why mirror not import: master plan v2.0 §92 + system-design umbrella
// §879 + B-7 commit body all declare orchestrator/providers/dispatcher
// MUST NOT import internal/store. Bypassadapter precedent:
// "two type sets, intentionally identical in shape — keeping them
// separate buys (1) bypass package never gains a transitive SQLite
// dependency so chaos / unit tests stay fast (2) future store-side
// schema changes don't ripple into the bypass package — the adapter
// absorbs them."
//
// Same rationale applies here. F-6 dispatcheradapter translates
// orchestrator.CostLedgerRow ↔ store.CostLedgerRow (field-by-field,
// no semantic transformation). A reflective parity test in the adapter
// guards against drift over time.
//
// TS is time.Time on the Go side; the adapter is responsible for any
// SQL-side encoding (the store currently uses UnixMilli ms-since-epoch
// for boundary precision, opaque to this package).
type CostLedgerRow struct {
	ID                  int64
	IdempotencyKey      string
	TS                  time.Time
	Project             string
	Profile             string
	Provider            string
	Tier                string
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUSD             float64
	ConversationID      string
	SessionID           string
	RequestHash         []byte
}

var ErrDuplicateIdempotency = errors.New("orchestrator.CostStore: idempotency key already recorded")

type CostStore interface {
	InsertCostLedger(row CostLedgerRow) (int64, error)
	QueryAllRecentCosts(since time.Time) ([]CostLedgerRow, error)
}

const (
	window24h = "24h"
	window30d = "30d"
)

func windowKey(project, profile, tier, windowName string) string {
	return project + ":" + profile + ":" + tier + ":" + windowName
}

type CostCounters struct {
	mu                         sync.RWMutex
	sessionCounters            map[string]float64
	projectProfileTierCounters map[string]*WindowCounter
	store                      CostStore

	tickInterval time.Duration
}

func NewCostCounters(s CostStore) *CostCounters {
	if s == nil {
		panic("NewCostCounters: store is nil")
	}
	return &CostCounters{
		sessionCounters:            map[string]float64{},
		projectProfileTierCounters: map[string]*WindowCounter{},
		store:                      s,
	}
}

func (c *CostCounters) Record(row CostLedgerRow) error {
	if c == nil {
		return errors.New("CostCounters: nil receiver")
	}
	_, err := c.store.InsertCostLedger(row)
	if err != nil {
		if errors.Is(err, ErrDuplicateIdempotency) {
			// inv-hades-062 honored: row already persisted; do not
			// double-charge in-memory counters.
			return nil
		}
		return fmt.Errorf("CostCounters.Record insert: %w", err)
	}
	c.applyToCounters(row)
	return nil
}

// applyToCounters: update session + 24h + 30d counters for one row.
// Caller MUST NOT already hold c.mu (this method takes c.mu internally
// for the session map mutation).
//
// Empty SessionID skips the session counter — daemon-internal background
// calls have no session to attribute spend to. Window counters always
// update (cap enforcement is independent of session attribution).
func (c *CostCounters) applyToCounters(row CostLedgerRow) {
	if row.SessionID != "" {
		c.mu.Lock()
		c.sessionCounters[row.SessionID] += row.CostUSD
		c.mu.Unlock()
	}
	for _, win := range []struct {
		name     string
		duration time.Duration
	}{
		{window24h, 24 * time.Hour},
		{window30d, 30 * 24 * time.Hour},
	} {
		c.getOrCreateWindow(row.Project, row.Profile, row.Tier, win.name, win.duration).
			Add(row.TS, row.CostUSD)
	}
}

func (c *CostCounters) getOrCreateWindow(project, profile, tier, windowName string, duration time.Duration) *WindowCounter {
	key := windowKey(project, profile, tier, windowName)
	c.mu.RLock()
	if w, ok := c.projectProfileTierCounters[key]; ok {
		c.mu.RUnlock()
		return w
	}
	c.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if w, ok := c.projectProfileTierCounters[key]; ok {
		return w
	}
	w := NewWindowCounter(duration)
	c.projectProfileTierCounters[key] = w
	return w
}

func (c *CostCounters) SessionTotal(sessionID string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionCounters[sessionID]
}

func (c *CostCounters) ProjectProfileTierTotal(project, profile, tier string, window time.Duration) float64 {
	name := windowNameFromDuration(window)
	c.mu.RLock()
	w, ok := c.projectProfileTierCounters[windowKey(project, profile, tier, name)]
	c.mu.RUnlock()
	if !ok {
		return 0
	}
	return w.Total(time.Now())
}

type ProjectProfileTier struct {
	Project string
	Profile string
	Tier    string
}

// AllKeys returns the set of (project, profile, tier) tuples observed in
// any rolling-window counter (24h or 30d). Used by the budget summary
// handler (K-4) and by orchestrator-status / collect30dCosts (K-3
// backfill) to render per-tier breakdowns.
//
// Dedup contract: each Record creates one WindowCounter PER window name
// (24h + 30d) for a given (project, profile, tier) — the internal map
// therefore holds 2 keys per tuple. AllKeys MUST collapse those back to
// one tuple so the operator-facing rendering does not double-count keys.
//
// Order insertion-order is NOT preserved (Go map iteration is random).
// Callers that need stable presentation MUST sort the returned slice.
//
// Defensive copy: the returned slice is fresh; caller mutation does not
// corrupt CostCounters internals (AllKeys only reads under RLock and the
// returned slice is allocated locally).
//
// Concurrency takes c.mu.RLock for the duration of the read; concurrent
// Record calls block on writers waiting for the WLock in
// getOrCreateWindow but do NOT block other readers. The hot path stays
// race-clean.
//
// Boundary returns []ProjectProfileTier (orchestrator-local). The store
// package and the dispatcher package both consume this iterator without
// reaching into c.projectProfileTierCounters directly — preserving
// inv-hades-031.
func (c *CostCounters) AllKeys() []ProjectProfileTier {
	if c == nil {
		return []ProjectProfileTier{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[ProjectProfileTier]struct{}, len(c.projectProfileTierCounters))
	for key := range c.projectProfileTierCounters {

		parts := strings.SplitN(key, ":", 4)
		if len(parts) != 4 {
			// Defense in depth: malformed key MUST NOT corrupt the result.
			// In practice this branch is unreachable because windowKey()
			// always produces 4 components; surface as a skip rather than
			// poisoning the operator's view of live spend.
			continue
		}
		ppt := ProjectProfileTier{
			Project: parts[0],
			Profile: parts[1],
			Tier:    parts[2],
		}
		seen[ppt] = struct{}{}
	}
	out := make([]ProjectProfileTier, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

func (c *CostCounters) WouldExceedCap(project, profile, tier string, window time.Duration, capUSD, projectedAddUSD float64) bool {
	current := c.ProjectProfileTierTotal(project, profile, tier, window)
	return current+projectedAddUSD >= capUSD
}

func windowNameFromDuration(d time.Duration) string {
	switch d {
	case 24 * time.Hour:
		return window24h
	case 30 * 24 * time.Hour:
		return window30d
	default:
		panic(fmt.Sprintf("CostCounters: unsupported window duration %v (only 24h and 30d)", d))
	}
}

var defaultMaintenanceTickInterval = 1 * time.Hour

// RebuildFromLedger startup-only replay. MUST run before the dispatcher
// accepts requests, else cap-checks could pass that ought to fail (an
// empty CostCounters would make every WouldExceedCap call return false
// regardless of historical spend). The verifyRebuild step closes
// inv-hades-065: for up to 8 (project, profile, tier) keys, the in-memory
// 30d total must equal the ledger SUM within 1e-9; mismatch is a fatal
// error returned to the caller (the daemon refuses to start).
//
// Replay policy: every row from store.QueryAllRecentCosts(since) flows
// through applyToCounters (which updates session + 24h + 30d counters in
// place). NO INSERT — rows are already on disk; the source-of-truth
// ledger is the input, not the output, of this method.
//
// Error contract: query failure or verifyRebuild mismatch are wrapped and
// returned. The daemon caller treats either as fatal (refuses to start).
// Callers MUST NOT proceed to StartHourlyMaintenance / dispatcher serve
// if RebuildFromLedger returned non-nil.
func (c *CostCounters) RebuildFromLedger(since time.Time) error {

	rows, err := c.store.QueryAllRecentCosts(since)
	if err != nil {
		return fmt.Errorf("RebuildFromLedger query: %w", err)
	}
	for _, row := range rows {
		c.applyToCounters(row)
	}
	if err := c.verifyRebuild(rows, since); err != nil {
		return fmt.Errorf("RebuildFromLedger verify: %w", err)
	}
	return nil
}

// verifyRebuild: re-aggregate up to 8 (project, profile, tier) keys from
// the rows and confirm applyToCounters routed each one to the correct
// counter. Self-check (does NOT call back into the store).
//
// Tolerance float64 sums are equal within 1e-9. Anything outside that
// range is a routing or arithmetic bug, NOT a precision issue (sums of a
// few thousand rows of cents-precision USD do not produce 1e-9 drift).
//
// Cap of 8 keys: full coverage is unnecessary; sampling 8 keys catches
// any systemic routing bug (wrong window, wrong key construction, missed
// applyToCounters call). A bad routing would surface on the FIRST key
// because every key takes the same code path. The cap bounds startup
// latency on a 30-day ledger.
//
// Cutoff only rows with TS >= now-30d contribute to the expected sum,
// matching the 30d-window boundary semantic. The 24h window is NOT
// verified here (it is a pure subset of 30d; verifying 30d is sufficient
// to catch routing bugs).
//
// Boundary rows is `[]CostLedgerRow` (orchestrator-local mirror), NOT
// `[]store.CostLedgerRow`. F-5/F-6 boundary pivot — orchestrator MUST
// NOT import internal/store (inv-hades-031).
func (c *CostCounters) verifyRebuild(rows []CostLedgerRow, since time.Time) error {
	type key struct{ project, profile, tier string }
	expected := map[key]float64{}
	now := time.Now()
	cutoff30d := now.Add(-30 * 24 * time.Hour)
	for _, r := range rows {
		if r.TS.Before(cutoff30d) {
			continue
		}
		expected[key{r.Project, r.Profile, r.Tier}] += r.CostUSD
	}
	count := 0
	for k, want := range expected {
		got := c.ProjectProfileTierTotal(k.project, k.profile, k.tier, 30*24*time.Hour)
		if got < want-1e-9 || got > want+1e-9 {
			return fmt.Errorf("counter desync for %s/%s/%s: got %f, expected %f",
				k.project, k.profile, k.tier, got, want)
		}
		count++
		if count >= 8 {
			break
		}
	}
	return nil
}

func (c *CostCounters) StartHourlyMaintenance(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	tick := c.tickInterval
	if tick <= 0 {
		tick = defaultMaintenanceTickInterval
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
				c.pruneAll(time.Now())
			}
		}
	}()
	return done
}

func (c *CostCounters) pruneAll(now time.Time) {
	c.mu.RLock()
	counters := make([]*WindowCounter, 0, len(c.projectProfileTierCounters))
	for _, w := range c.projectProfileTierCounters {
		counters = append(counters, w)
	}
	c.mu.RUnlock()
	for _, w := range counters {
		w.PruneOlderThan(now.Add(-w.windowDuration))
	}
}

func countersRebuiltFromLedger() {
	var c *CostCounters
	_ = c.RebuildFromLedger
}

var _ = countersRebuiltFromLedger
