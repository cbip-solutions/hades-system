// SPDX-License-Identifier: MIT
// internal/daemon/dispatcheradapter/dispatcheradapter.go
//
// Concrete adapter that bridges TWO independent boundaries between the
// orchestrator/dispatcher half of the daemon and the SQLite store:
//
// 1. dispatcher.CostSink path: receives a
// dispatcher.CostEvent (per-tier observability/audit, fired by the
// AsyncEmitter worker) and forwards it to an abstract `Store`
// interface. This path captures EVERY tier attempt — successes,
// failures, retries — so the drift audit can reconcile
// attribution. Field shape is dispatcher-flavoured (Status, LatencyMS,
// Err live here; cost-row idempotency lives in the OTHER path).
//
// 2. orchestrator.CostStore path: receives an
// orchestrator.CostLedgerRow (canonical cost ledger persistence with
// IdempotencyKey UNIQUE-enforced at the SQL layer for invariant no-
// double-charge) and forwards it to the concrete *store.Store via
// 1:1 field translation. Returning orchestrator.CostLedgerRow rather
// than store.CostLedgerRow is load-bearing: the orchestrator package
// never sees store types, preserving invariant.
//
// Both paths live on the same Adapter struct because the daemon
// constructs one adapter at boot and shares it across the dispatcher
// (CostSink wired through AsyncEmitter) and the CostCounters/F-7 startup
// hook (CostStore wired into NewCostCounters). Splitting them would
// require synchronised lifecycle of two adapter instances over the same
// underlying *store.Store — pointless duplication.
//
// Boundary: this file imports internal/store. That is the
// architectural intent: dispatcheradapter IS the boundary that absorbs
// the dependency. orchestrator / providers / dispatcher / bypass MUST
// NOT import internal/store; verified by static-invariant checks.
//
// Concurrency Adapter holds no mutable state of its own. Both paths
// forward directly to their backing Store (sink interface) or *store.Store
// (s field). Both backing stores are responsible for their own thread
// safety (the SQLite store serialises writers transparently via
// busy_timeout; in-memory test fakes use mutexes).

package dispatcheradapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type CostLedgerRow struct {
	Timestamp time.Time
	Project   string
	SessionID string
	Profile   string

	Provider     string
	Tier         providers.Tier
	Model        string
	InputTokens  int
	OutputTokens int
	Status       int
	LatencyMS    int64
	Err          string
}

// Store is the minimal surface the dispatcher.CostSink path requires.
// Tests use an in-memory implementation; daemon B-8 wiring passes a
// noop sink today (the concrete *store.Store is wired separately on
// the CostStore path; CostSink ↔ store integration lands when the
// dispatcher emitter is rewired to write to cost_ledger directly,
// which is out of F-6 scope and tracked under the drift audit).
//
// Concurrency implementations MUST tolerate concurrent calls from
// multiple goroutines. The Adapter forwards directly without locking
// (it has no mutable state of its own).
type Store interface {
	InsertCostLedger(ctx context.Context, row CostLedgerRow) error
}

type Adapter struct {
	sink Store
	s    *store.Store
}

// New returns an Adapter wrapping BOTH the dispatcher.CostSink path
// (sink — abstract Store interface for observability/audit) and the
// orchestrator.CostStore path (s — concrete *store.Store for canonical
// cost_ledger persistence).
//
// Both args are required. Panics on nil — same fail-fast posture as
// dispatcher.New, NewAsyncEmitter, NewCostCounters, and bypassadapter.New:
// a nil dependency at boot is a wiring bug that MUST surface before
// serving traffic, not a runtime failure mode.
func New(sink Store, s *store.Store) *Adapter {
	if sink == nil {
		panic("dispatcheradapter.New: sink is nil")
	}
	if s == nil {
		panic("dispatcheradapter.New: store is nil")
	}
	return &Adapter{sink: sink, s: s}
}

func (a *Adapter) Insert(ctx context.Context, evt dispatcher.CostEvent) error {
	return a.sink.InsertCostLedger(ctx, CostLedgerRow{
		Timestamp:    evt.Timestamp,
		Project:      evt.Project,
		SessionID:    evt.SessionID,
		Profile:      evt.Profile,
		Provider:     evt.Provider,
		Tier:         evt.Tier,
		Model:        evt.Model,
		InputTokens:  evt.InputTokens,
		OutputTokens: evt.OutputTokens,
		Status:       evt.Status,
		LatencyMS:    evt.LatencyMS,
		Err:          evt.Err,
	})
}

// InsertCostLedger (CostStore path) satisfies orchestrator.CostStore.
// Translates orchestrator.CostLedgerRow → store.CostLedgerRow (1:1 field
// copy; 15 fields, identical Go types per F-1 + F-5 mirror discipline)
// and forwards to the SQLite store via store.InsertCostLedger.
//
// Wraps store.ErrDuplicateIdempotency in orchestrator.ErrDuplicateIdempotency
// so callers checking via errors.Is(err, orchestrator.ErrDuplicateIdempotency)
// succeed. Note the orchestrator-level sentinel is the public contract;
// the store-level sentinel is intentionally NOT re-exposed (errors.Is
// against store.ErrDuplicateIdempotency returns false on the wrapped error)
// to keep boundary hygiene: orchestrator package callers MUST NOT acquire
// a transitive dependency on store sentinels.
//
// Concurrency SQLite serialises writers via busy_timeout; safe to call
// concurrently from multiple goroutines. The adapter holds no mutable
// state of its own.
func (a *Adapter) InsertCostLedger(row orchestrator.CostLedgerRow) (int64, error) {
	storeRow := orchestratorToStoreRow(row)
	id, err := store.InsertCostLedger(a.s.DB(), storeRow)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateIdempotency) {
			return 0, fmt.Errorf("%w: %v", orchestrator.ErrDuplicateIdempotency, err)
		}
		return 0, err
	}
	return id, nil
}

func (a *Adapter) QueryAllRecentCosts(since time.Time) ([]orchestrator.CostLedgerRow, error) {
	storeRows, err := store.QueryAllRecentCosts(a.s.DB(), since)
	if err != nil {
		return nil, err
	}
	out := make([]orchestrator.CostLedgerRow, len(storeRows))
	for i, r := range storeRows {
		out[i] = storeToOrchestratorRow(r)
	}
	return out, nil
}

func orchestratorToStoreRow(r orchestrator.CostLedgerRow) store.CostLedgerRow {
	return store.CostLedgerRow{
		ID:                  r.ID,
		IdempotencyKey:      r.IdempotencyKey,
		TS:                  r.TS,
		Project:             r.Project,
		Profile:             r.Profile,
		Provider:            r.Provider,
		Tier:                r.Tier,
		Model:               r.Model,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		CostUSD:             r.CostUSD,
		ConversationID:      r.ConversationID,
		SessionID:           r.SessionID,
		RequestHash:         r.RequestHash,
	}
}

func storeToOrchestratorRow(r store.CostLedgerRow) orchestrator.CostLedgerRow {
	return orchestrator.CostLedgerRow{
		ID:                  r.ID,
		IdempotencyKey:      r.IdempotencyKey,
		TS:                  r.TS,
		Project:             r.Project,
		Profile:             r.Profile,
		Provider:            r.Provider,
		Tier:                r.Tier,
		Model:               r.Model,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		CostUSD:             r.CostUSD,
		ConversationID:      r.ConversationID,
		SessionID:           r.SessionID,
		RequestHash:         r.RequestHash,
	}
}

type PinStoreAdapter struct {
	s *store.Store
}

// NewPinStoreAdapter constructs a PinStoreAdapter. Panics on nil store —
// same fail-fast posture as Adapter.New, NewCostCounters, NewPinOverrides,
// and dispatcher.New: a nil dependency at boot is a wiring bug that MUST
// surface before serving traffic.
func NewPinStoreAdapter(s *store.Store) *PinStoreAdapter {
	if s == nil {
		panic("dispatcheradapter.NewPinStoreAdapter: store is nil")
	}
	return &PinStoreAdapter{s: s}
}

func (p *PinStoreAdapter) Insert(row orchestrator.PinRow) error {
	return p.s.InsertPin(orchestratorToStorePinRow(row))
}

func (p *PinStoreAdapter) Delete(scope, scopeID string) error {
	return p.s.DeletePin(scope, scopeID)
}

func (p *PinStoreAdapter) Query(scope, scopeID string) (*orchestrator.PinRow, error) {
	storeRow, err := p.s.QueryPin(scope, scopeID)
	if err != nil || storeRow == nil {
		return nil, err
	}
	o := storeToOrchestratorPinRow(*storeRow)
	return &o, nil
}

func (p *PinStoreAdapter) ListAll() ([]orchestrator.PinRow, error) {
	storeRows, err := p.s.ListAllPins()
	if err != nil {
		return nil, err
	}
	if len(storeRows) == 0 {
		return nil, nil
	}
	out := make([]orchestrator.PinRow, len(storeRows))
	for i, r := range storeRows {
		out[i] = storeToOrchestratorPinRow(r)
	}
	return out, nil
}

func (p *PinStoreAdapter) PurgeExpired(now time.Time) (int, error) {
	return p.s.PurgeExpiredPins(now)
}

func orchestratorToStorePinRow(p orchestrator.PinRow) store.PinRow {
	return store.PinRow{
		ID:        p.ID,
		Scope:     p.Scope,
		ScopeID:   p.ScopeID,
		Tier:      p.Tier,
		Provider:  p.Provider,
		SetAt:     p.SetAt,
		ExpiresAt: p.ExpiresAt,
		Reason:    p.Reason,
	}
}

func storeToOrchestratorPinRow(p store.PinRow) orchestrator.PinRow {
	return orchestrator.PinRow{
		ID:        p.ID,
		Scope:     p.Scope,
		ScopeID:   p.ScopeID,
		Tier:      p.Tier,
		Provider:  p.Provider,
		SetAt:     p.SetAt,
		ExpiresAt: p.ExpiresAt,
		Reason:    p.Reason,
	}
}

var (
	_ dispatcher.CostSink    = (*Adapter)(nil)
	_ orchestrator.CostStore = (*Adapter)(nil)
	_ orchestrator.PinStore  = (*PinStoreAdapter)(nil)
)
