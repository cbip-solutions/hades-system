// SPDX-License-Identifier: MIT
// Package evolution is Caronte's L4 layer: the git-history-derived signals
// the structural layers (L1 parse, L2 resolve, L3 k-core/SCC) cannot see.
// It produces two products into the per-project caronte.db (via the
// store API): the CO-CHANGE matrix (which files change together, code-maat
// coupling_degree) and per-file CHURN (touch-count + author-count over a
// window). Both are computed by parsing `git log` (os/exec) and filtered to
// reject mega-commits + gated by a cold-start threshold so the engine never
// fabricates a coupling score from too little history.
//
// Boundary: this package and all of
// internal/caronte NEVER import internal/store. It writes through the
// injected *store.Store handle; it does not open a DB.
//
// Purity this package is git-log parse + arithmetic — no CGO, no SQLite
// here. Only the store METHODS it calls touch SQLite, and the *store.Store
// is injected by the engine. GOOS=linux CGO_ENABLED=0 builds it cleanly.
package evolution

import (
	"errors"
	"fmt"
)

var ErrParamsBelowFloor = errors.New("evolution: co-change params below doctrine floor")

const (
	WindowDefaultDays  = 90
	WindowExtendedDays = 180
	WindowFull         = 0
)

type Params struct {
	MinRevisions int

	MinSharedRevisions int

	MinCouplingPercent float64

	MaxChangesetSize int

	MinTotalCommits int

	WindowDays int

	FollowRenames bool
}

func DefaultParams() Params {
	return Params{
		MinRevisions:       5,
		MinSharedRevisions: 3,
		MinCouplingPercent: 30,
		MaxChangesetSize:   50,
		MinTotalCommits:    50,
		WindowDays:         WindowDefaultDays,
		FollowRenames:      true,
	}
}

// Validate enforces the spec §8 doctrine floors (and the max_changeset
// ceiling). Returns ErrParamsBelowFloor (wrapped with the offending field)
// when a parameter is weaker than its evidence-backed minimum. The daemon's
// ParamsAccessor MUST call Validate (or rely on DefaultParams) so a hand-
// edited doctrine cannot lower a threshold into fabrication territory.
//
// Floors MinRevisions ≥ 5, MinSharedRevisions ≥ 3, MinCouplingPercent ≥ 30,
// MinTotalCommits ≥ 50, WindowDays > 0 (a positive window is required; use
// a separate code path for full-history scans). Ceiling:
// MaxChangesetSize ≤ 50 (a higher value re-admits mega-commits) AND ≥ 1
// (a non-positive ceiling would skip every commit).
func (p Params) Validate() error {
	if p.MinRevisions < 5 {
		return fmt.Errorf("%w: MinRevisions=%d < 5", ErrParamsBelowFloor, p.MinRevisions)
	}
	if p.MinSharedRevisions < 3 {
		return fmt.Errorf("%w: MinSharedRevisions=%d < 3", ErrParamsBelowFloor, p.MinSharedRevisions)
	}
	if p.MinCouplingPercent < 30 {
		return fmt.Errorf("%w: MinCouplingPercent=%v < 30", ErrParamsBelowFloor, p.MinCouplingPercent)
	}
	if p.MaxChangesetSize < 1 || p.MaxChangesetSize > 50 {
		return fmt.Errorf("%w: MaxChangesetSize=%d outside [1,50]", ErrParamsBelowFloor, p.MaxChangesetSize)
	}
	if p.MinTotalCommits < 50 {
		return fmt.Errorf("%w: MinTotalCommits=%d < 50", ErrParamsBelowFloor, p.MinTotalCommits)
	}
	if p.WindowDays <= 0 {
		return fmt.Errorf("%w: WindowDays=%d must be positive (use WindowFull=0 via a non-Validate path for full-history scans)", ErrParamsBelowFloor, p.WindowDays)
	}
	return nil
}

type ParamsAccessor interface {
	// CoChangeParams returns the effective Params for projectID. The impl
	// MUST return a Validate()-passing Params (or DefaultParams); the
	// Builder defensively re-validates and falls back on failure.
	CoChangeParams(projectID string) Params
}
