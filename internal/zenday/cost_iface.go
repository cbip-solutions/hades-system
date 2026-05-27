// SPDX-License-Identifier: MIT
// Package zenday — cost ledger contract.
//
// The local CostStore interface is the boundary between the zenday
// package and release's dispatcheradapter.CostStore. Production wiring
// adapts the dispatcher's concrete cost-ledger reader; tests substitute
// in-memory fakes (per invariant, zenday/ never imports
// internal/store).
package zenday

import (
	"context"
	"time"
)

// CostStatus is the snapshot renders as rank-4 cost-cap-warning
// items. Sourced's cost ledger via dispatcheradapter.CostStore;
// only projects with PercentUsed ≥ 80 are emitted as items per spec §1
// Q14 B (max 2 shown after sorting by % desc).
//
// PercentUsed semantics: 0..100 inclusive; values <80 are filtered out by
// collectCostLeg before cap is applied. Values >100 are clamped at the
// adapter layer (max-scope: cost-cap is a soft warning, not a hard gate).
type CostStatus struct {
	ProjectAlias string

	PercentUsed float64

	SpendUSD float64

	CapUSD float64
}

type CostStore interface {
	SpendByProject(ctx context.Context, from, to time.Time) ([]CostStatus, error)
}
