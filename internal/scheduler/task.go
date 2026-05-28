// SPDX-License-Identifier: MIT
package scheduler

import (
	"fmt"
	"time"
)

type TaskParams struct {
	ID string

	ProjectAlias string

	Action string
	// FireAt is the absolute wall-clock time the operator wants the
	// Task to fire; MUST be > the `now` argument passed to NewTask.
	FireAt time.Time

	MissPolicy MissPolicy

	MissLookback time.Duration
}

// NewTask constructs an ephemeral one-shot Schedule (Tier=TierTask).
//
// `now` is the construction-time clock; FireAt MUST be strictly after
// `now` (FireAt == now is rejected — fires-immediately is not the
// one-shot contract).
//
// NextRunAt = FireAt + ComputeJitter(ID, FireAt-now). The period
// passed to ComputeJitter is the gap-to-fire (FireAt-now), NOT a
// nominal "every N minutes" period — Tasks are one-shot and have no
// recurrence period. ComputeJitter's branching only depends on whether
// the period is >=1h (recurring 15min cap) vs <1h (one-shot 90s cap),
// so the gap-to-fire is the right input.
//
// TriggerType is set to TriggerCron with a sentinel CronExpr
// ("* * * * *") so that Schedule.Validate() — which requires CronExpr
// non-empty when TriggerType==TriggerCron — passes on the returned
// row. The dispatcher MUST NOT re-evaluate this cron for Tasks; it
// reads NextRunAt directly. (Tasks do not call ParseCron and thus
// bypass the doctrine granularity floor — by design, since the floor
// is about cron-driven recurrence and a Task's <1min gap is operator
// intent, not an inadvertent thrash.)
//
// Validation order (load-bearing — earliest gate names the most-
// specific error so adapter callers can route the failure):
//
// 1. empty ID/ProjectAlias/Action → ErrInvalidSchedule (defence in
// depth; field name surfaced in the error message).
// 2. FireAt <= now → ErrInvalidSchedule (FireAt must
// be in the future).
// 3. Schedule.Validate() → ErrInvalidSchedule (catches
// CreatedAt zero, MissPolicy unknown, etc. — should never trip
// since this constructor sets all required fields).
//
// Boundary (invariant): stdlib + the in-package ComputeJitter
// function only. No internal/store, internal/providers, or
// tier1-sidecar imports.
func NewTask(p TaskParams, now time.Time) (*Schedule, error) {
	if p.ID == "" {
		return nil, fmt.Errorf("%w: empty ID", ErrInvalidSchedule)
	}
	if p.ProjectAlias == "" {
		return nil, fmt.Errorf("%w: empty ProjectAlias", ErrInvalidSchedule)
	}
	if p.Action == "" {
		return nil, fmt.Errorf("%w: empty Action", ErrInvalidSchedule)
	}
	if !p.FireAt.After(now) {
		return nil, fmt.Errorf("%w: FireAt %v must be after now %v",
			ErrInvalidSchedule, p.FireAt, now)
	}
	period := p.FireAt.Sub(now)
	jitter := ComputeJitter(p.ID, period)
	s := &Schedule{
		ID:           p.ID,
		Tier:         TierTask,
		ProjectAlias: p.ProjectAlias,
		Action:       p.Action,
		TriggerType:  TriggerCron,
		TriggerConfig: TriggerConfig{

			CronExpr: "* * * * *",
		},
		MissPolicy:   p.MissPolicy,
		MissLookback: p.MissLookback,
		Status:       StatusEnabled,
		NextRunAt:    p.FireAt.Add(jitter),
		CreatedAt:    now,
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// MarkTaskFired transitions a TierTask Schedule to Status=Disabled
// after a successful (or final-failed) fire. Routines do NOT call this;
// routines auto-advance via Routine.Advance to the next tick. Loops do
// NOT call this; loops re-tick via Loop.Tick until the bound
// session is gone.
//
// Idempotent + nil-safe + tier-safe (defence in depth):
//
// - nil pointer → no-op (adapter read miss tolerated).
// - non-Tier-Task schedule → no-op (caller bug; do NOT auto-disable
// a Routine or Loop — that would silently break recurrence).
// - already StatusDisabled → still set to StatusDisabled (idempotent).
//
// The function does NOT also clear NextRunAt: history readers want to
// know "the task fired at this scheduled time", which NextRunAt
// records. The Status=Disabled gate alone is what the dispatcher tick
// checks (via Schedule.DueAt).
func MarkTaskFired(s *Schedule) {
	if s == nil || s.Tier != TierTask {
		return
	}
	s.Status = StatusDisabled
}
