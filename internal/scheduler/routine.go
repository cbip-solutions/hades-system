// SPDX-License-Identifier: MIT
package scheduler

import (
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type Routine struct {
	schedule *Schedule
	cron     *CronExpr
}

func NewRoutine(s *Schedule, d doctrine.Name) (*Routine, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: nil Schedule", ErrInvalidSchedule)
	}
	if s.Tier != TierRoutine {
		return nil, fmt.Errorf("%w: tier %v != TierRoutine", ErrInvalidSchedule, s.Tier)
	}
	if s.TriggerType != TriggerCron {
		return nil, fmt.Errorf("%w: routine requires TriggerCron, got %v", ErrInvalidSchedule, s.TriggerType)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	expr, err := ParseCron(s.TriggerConfig.CronExpr, d)
	if err != nil {
		return nil, err
	}
	return &Routine{schedule: s, cron: expr}, nil
}

func (r *Routine) Schedule() *Schedule { return r.schedule }

func (r *Routine) Plan(now time.Time) time.Time {
	base := r.cron.Next(now)
	period := base.Sub(now)
	jitter := ComputeJitter(r.schedule.ID, period)
	return base.Add(jitter)
}

// Advance updates s.NextRunAt to the next planned fire after `after`.
//
// Two canonical call sites:
//
//  1. After a successful fire, the dispatcher calls Advance(LastRunAt)
//     to schedule the next tick — passing LastRunAt (not now) means
//     the next tick is anchored to the actual fire wall clock, not
//     to whenever Advance happens to run (which may be later if the
//     fire took non-trivial time).
//  2. After daemon recovery, the dispatcher calls Advance(now) to
//     re-anchor the schedule past the gap. The miss policy
//     (D-5/D-6/D-12 path) decides whether to backfill the gap; this
//     function only updates the next-fire pointer.
//
// Mutates the underlying Schedule in place. Callers MUST persist the
// mutated row via the scheduleradapter before releasing the
// per-Schedule lock — otherwise a daemon crash before persistence
// would re-fire the same tick on restart.
func (r *Routine) Advance(after time.Time) {
	r.schedule.NextRunAt = r.Plan(after)
}
