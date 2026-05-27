// SPDX-License-Identifier: MIT
// Package budget provides the daemon-side budget engine.
//
// rows. release layers four engines on top:
//
// - axes: multi-axis attribution (project x doctrine x stage x task + operation + worker_id) via cost_axis_tags
// - enforce: hierarchical hard-pause cap check across 4 scopes (project / doctrine / stage / worker_id), most-restrictive wins
// - anomaly: z-score sliding window per scope (Welford epsilon-stable)
// - pause: 4-scope state machine + auto-resume scheduler
//
// The package never imports internal/store directly. All
// storage access goes through the BudgetStore interface declared here;
// internal/daemon/dispatcheradapter/budget_hooks.go satisfies it.
//
// Compile-time anchors for the four invariants:
//
// invariant: preCallEnforcedBeforeUpstream (enforce.go)
// invariant: axisTagCompleteness (axes.go)
// invariant: anomalyDeterministic (anomaly.go)
// invariant: hierarchicalPrecedence (enforce.go)
package budget

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

var ErrAxisIncomplete = errors.New("budget: axis tags incomplete (one or more of project/doctrine/stage/task missing)")

var ErrAxisInsertFailed = errors.New("budget: axis tag insert failed")

var requiredAxes = []string{"project", "doctrine", "stage", "task"}

var optionalAxes = []string{"operation", "worker_id", "augmentation"}

const AugmentationAxisName = "augmentation"

func RequiredAxes() []string {
	out := make([]string, len(requiredAxes))
	copy(out, requiredAxes)
	return out
}

type BudgetStore interface {
	InsertCostAxisTag(ctx context.Context, costID int64, name, value string) error
	EmitAxisTagLoss(ctx context.Context, costID int64, missingAxis string) error
	QueryAxisTags(ctx context.Context, costID int64) (map[string]string, error)
	QueryCostIDsByAxis(ctx context.Context, name, value string) ([]int64, error)
	QueryAxisTagLosses(ctx context.Context, costID int64) ([]string, error)

	PauseGet(ctx context.Context, scope, scopeValue string) (active bool, autoResumeAt int64, err error)

	PauseSet(ctx context.Context, scope, scopeValue, reason string, startedAtMs, autoResumeAt int64) error
	PauseClear(ctx context.Context, scope, scopeValue string) error

	PauseClearIfExpired(ctx context.Context, scope, scopeValue string, beforeMs int64) error
	PauseListActive(ctx context.Context) ([]PauseRow, error)

	AnomalyAppend(ctx context.Context, row AnomalyRow) error
	AnomalyWindow(ctx context.Context, scope, scopeValue string, limit int) ([]float64, error)

	RolledUSDByAxis(ctx context.Context, axisName, axisValue string, sinceMs int64) (float64, error)
}

type AxisTagger struct {
	store BudgetStore
}

func NewAxisTagger(store BudgetStore) *AxisTagger {
	if store == nil {
		panic("NewAxisTagger: store is nil — inv-zen-077 requires a real BudgetStore")
	}
	return &AxisTagger{store: store}
}

func (t *AxisTagger) Tag(ctx context.Context, costID int64, axisTags map[string]string) error {
	if costID <= 0 {
		return fmt.Errorf("Tag: cost_id must be > 0 (got %d)", costID)
	}
	if axisTags == nil {
		axisTags = map[string]string{}
	}

	allAxes := append([]string{}, requiredAxes...)
	allAxes = append(allAxes, optionalAxes...)
	for _, axis := range allAxes {
		v, present := axisTags[axis]
		if !present || v == "" {
			continue
		}
		if err := t.store.InsertCostAxisTag(ctx, costID, axis, v); err != nil {

			return fmt.Errorf("%w: axis=%q: %w", ErrAxisInsertFailed, axis, err)
		}
	}

	var missing []string
	for _, axis := range requiredAxes {
		if v, present := axisTags[axis]; !present || v == "" {
			missing = append(missing, axis)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	for _, axis := range missing {
		if err := t.store.EmitAxisTagLoss(ctx, costID, axis); err != nil {

			return fmt.Errorf("%w: cost_id=%d missing=%v: emit failed: %w",
				ErrAxisIncomplete, costID, missing, err)
		}
	}
	return fmt.Errorf("%w: cost_id=%d missing=%v", ErrAxisIncomplete, costID, missing)
}

func axisTagCompleteness() error { return ErrAxisIncomplete }

var _ = axisTagCompleteness
