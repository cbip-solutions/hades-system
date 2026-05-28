// SPDX-License-Identifier: MIT
// internal/orchestrator/depth.go
//
// D-4 wholesale-replaces the D-2 DecideWidth scaffolding with the spec
// §5.3.2 min-over-5-factors formula. Three factors are wired in this stage:
//
// 1. doctrine.MaxWidth — hard ceiling from the canonical §1 design choice C matrix.
// 2. spec.ParallelizableUpperBound — spec-side parallelism cap.
// 3. machine_capacity — worktree pool capacity (o.cfg.PoolCapacity in RunStage4).
//
// Factors 4 (cost-budget ceiling) and 5 (reviewer-cap) land in Phases G/H
// by mutating DoctrineBounds.MaxWidth at runtime before calling DecideWidth.
// The structural formula — width = min(factor1, factor2, factor3) — is
// frozen here and never changes.
//
// D-5 wholesale-replaces DecideDepth with the canonical spec §5.3.2 formula:
// depth = ceil(log_W(N)), capped to doctrine MaxDepth. Precision-safe via
// integer ceiling-division (avoids float log(W^k)/log(W) = k-ε drift).
package orchestrator

import (
	"context"
	"errors"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

var ErrZeroWidth = errors.New("orchestrator: decided width is zero (no parallelism feasible)")

var ErrZeroDepth = errors.New("orchestrator: zero tasks; depth undefined")

type DoctrineBounds struct {
	Floor    int
	MaxWidth int
	MaxDepth int
}

var doctrineBoundsTable = map[string]DoctrineBounds{

	"max-scope": {Floor: 8, MaxWidth: 32, MaxDepth: 5},

	"default": {Floor: 3, MaxWidth: 12, MaxDepth: 3},

	"capa-firewall": {Floor: 5, MaxWidth: 15, MaxDepth: 4},
}

func DoctrineBoundsFor(name string) DoctrineBounds {
	if b, ok := doctrineBoundsTable[name]; ok {
		return b
	}

	return doctrineBoundsTable["default"]
}

func doctrineBoundsFor(name string) DoctrineBounds { return DoctrineBoundsFor(name) }

func DecideWidth(req BuildRequest, machineCapacity int, bounds DoctrineBounds) (int, error) {
	if req.Spec == nil {
		return 0, fmt.Errorf("%w: spec required", ErrInvalidBuildRequest)
	}
	parallelizable := req.Spec.ParallelizableUpperBound()
	if parallelizable <= 0 || machineCapacity <= 0 || bounds.MaxWidth <= 0 {
		return 0, fmt.Errorf("%w: parallelizable=%d capacity=%d max_width=%d",
			ErrZeroWidth, parallelizable, machineCapacity, bounds.MaxWidth)
	}
	w := bounds.MaxWidth
	if parallelizable < w {
		w = parallelizable
	}
	if machineCapacity < w {
		w = machineCapacity
	}
	return w, nil
}

func DecideDepth(n, w, maxDepth int) (int, error) {
	if maxDepth < 1 {
		return 0, fmt.Errorf("%w: max_depth=%d must be ≥ 1", ErrInvalidConfig, maxDepth)
	}
	if w < 1 {
		return 0, fmt.Errorf("%w: width=%d must be ≥ 1", ErrInvalidBuildRequest, w)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: task_count=%d", ErrZeroDepth, n)
	}
	if n == 1 {
		return 1, nil
	}
	if w == 1 {

		if n > maxDepth {
			return maxDepth, nil
		}
		return n, nil
	}

	depth := 0
	for n > 1 {
		n = (n + w - 1) / w
		depth++
		if depth == maxDepth {

			break
		}
	}
	return depth, nil
}

type eventlogQuerier interface {
	Query(ctx context.Context, sessionID string, since int64) ([]eventlog.Record, error)
}

type researchGate struct {
	log eventlogQuerier
}

func NewResearchGate(log eventlogQuerier) (ResearchGate, error) {
	if log == nil {
		return nil, fmt.Errorf("%w: eventlog querier is nil", ErrInvalidConfig)
	}
	return &researchGate{log: log}, nil
}

func (g *researchGate) Check(ctx context.Context, sessionID string) error {
	records, err := g.log.Query(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("research gate query: %w", err)
	}
	for _, rec := range records {
		if rec.EventType == eventlog.EvtResearchCompleted {
			return nil
		}
	}
	return fmt.Errorf("%w: session=%s", ErrResearchGateNotPassed, sessionID)
}
