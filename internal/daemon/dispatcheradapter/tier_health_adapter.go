// SPDX-License-Identifier: MIT
// tier_health_adapter.go — boundary bridge: orchestrator.TierHealthSink ->
// store.tier_health_samples.
//
// TierHealthSampleAdapter satisfies orchestrator.TierHealthSink and
// forwards each sample to store.InsertTierHealthSample via 1:1 field
// translation. It is a SEPARATE type from *Adapter: Go forbids two methods
// of the same name with incompatible signatures on one struct, and
// *Adapter already carries Insert (CostSink) + InsertCostLedger
// (CostStore). Same rationale + precedent as PinStoreAdapter.
//
// Boundary (inv-hades-031): this file imports internal/store — that is the
// architectural intent; dispatcheradapter IS the boundary that absorbs the
// store dependency. The orchestrator package never sees store types.
//
// Concurrency holds no mutable state; *store.Store serialises writers via
// SQLite busy_timeout; safe for concurrent calls.
package dispatcheradapter

import (
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type TierHealthSampleAdapter struct {
	s *store.Store
}

// NewTierHealthSampleAdapter constructs a TierHealthSampleAdapter. Panics
// on a nil store — same fail-fast posture as Adapter.New /
// NewPinStoreAdapter a nil dependency at boot is a wiring bug that MUST
// surface before serving traffic.
func NewTierHealthSampleAdapter(s *store.Store) *TierHealthSampleAdapter {
	if s == nil {
		panic("dispatcheradapter.NewTierHealthSampleAdapter: store is nil")
	}
	return &TierHealthSampleAdapter{s: s}
}

func (a *TierHealthSampleAdapter) RecordHealthSample(row orchestrator.TierHealthSampleRow) error {
	return store.InsertTierHealthSample(a.s.DB(), store.TierHealthSampleRow{
		TS:           row.TS,
		Provider:     row.Provider,
		Tier:         row.Tier,
		Success:      row.Success,
		LatencyMS:    row.LatencyMS,
		ErrorPattern: row.ErrorPattern,
	})
}

var _ orchestrator.TierHealthSink = (*TierHealthSampleAdapter)(nil)
