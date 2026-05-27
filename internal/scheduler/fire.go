// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

type FireDeps struct {
	Now func() time.Time

	Doctrine doctrine.Name

	Quota QuotaPreFlightChecker

	Dispatcher Dispatcher

	Eventlog EventEmitter

	RateLimit RateLimiter

	Store Store
}

// Fire orchestrates one fire attempt for s. The orchestration pipeline
// (spec §3.2 / Task D-12):
//
// 1. Resolve EffectiveMissPolicy(s, doctrine) — invariant.
// 2. Compute MissedFire via ComputeMissed(s, now).
// 3. Apply miss policy for any historical gap:
// - MissPolicySkip → emit one EventRoutineSkipped per
// missed; the current tick still fires.
// - MissPolicyNotifyOnly → emit one EventMissedFire with
// action-needed reason; the current tick still fires.
// - MissPolicyCoalesce → fire ONCE (the current tick) with
// a non-nil BackfillWindow describing the gap; do NOT emit per-
// missed events (the single fire IS the audit record).
// - MissPolicyCatchUpBounded → fire N catch-up dispatches gated
// by deps.RateLimit (1/30s/project); on rate-limit denial emit
// EventRateLimited and proceed to the current tick.
// 4. Fire the current (now-due) tick:
// - Pre-flight Quota.PreFlight (returns ErrQuotaCap on deny;
// emits EventQuotaCapReached).
// - Pre-flight RateLimit.Allow (returns ErrRateLimited on deny;
// emits EventRateLimited; persists OutcomeRateLimited history).
// - Dispatch via deps.Dispatcher.Dispatch (single-egress).
// - On success: emit EventRoutineFired, persist OutcomeSuccess
// history, advance s.LastRunAt, advance store.UpdateNextRun.
// - On failure: emit EventRoutineFailed, persist OutcomeFailed
// history, return wrapped dispatcher error; do NOT advance.
//
// Boundary (invariant / invariant): dispatch happens ONLY through
// deps.Dispatcher. This file imports stdlib + internal/doctrine only;
// it MUST NOT import internal/providers or private-tier1-module.
// Compile-checked by D-14 + boundary tests.
//
// # Returns
//
// - nil on success or a pure-skip case (skip is not an error).
// - errors.Is(ErrInvalidSchedule) when s is nil or fails Validate.
// - errors.Is(ErrQuotaCap) when quota.PreFlight returned !Allowed.
// - errors.Is(ErrRateLimited) when rate-limit denied the current tick.
// - the underlying dispatcher / quota infrastructural error wrapped
// with %w when the failure is below the deny boundary (e.g. SQLite
// locked, network error from quota adapter). Callers MUST use
// errors.Is on the sentinels.
//
// Inv-hades-080 / invariant / invariant contract.
func Fire(ctx context.Context, s *Schedule, deps FireDeps) error {

	_ = jitterDeterministicSentinel
	_ = missPolicyDoctrineSentinel
	_ = dispatcherSingleEgressSentinel

	if s == nil {
		return fmt.Errorf("%w: nil Schedule", ErrInvalidSchedule)
	}
	if err := s.Validate(); err != nil {
		return err
	}
	now := deps.Now().UTC()

	missed := ComputeMissed(s, now)
	policy := EffectiveMissPolicy(s, deps.Doctrine)

	var backfill *BackfillWindow
	if missed.MissedCount > 0 {
		switch policy {
		case MissPolicySkip:

			for i := 0; i < missed.MissedCount; i++ {
				_ = deps.Eventlog.Emit(ctx, Event{
					Kind:         EventRoutineSkipped,
					ScheduleID:   s.ID,
					ProjectAlias: s.ProjectAlias,
					At:           now,
					Reason:       "miss-policy=skip",
				})
			}
		case MissPolicyNotifyOnly:
			_ = deps.Eventlog.Emit(ctx, Event{
				Kind:         EventMissedFire,
				ScheduleID:   s.ID,
				ProjectAlias: s.ProjectAlias,
				At:           now,
				Reason:       fmt.Sprintf("missed=%d (capa-firewall: action needed)", missed.MissedCount),
			})
		case MissPolicyCoalesce:

			if window, ok := Coalesce(s, missed); ok {
				backfill = &window
			}
		case MissPolicyCatchUpBounded:

			for i := 0; i < missed.MissedCount; i++ {
				if !deps.RateLimit.Allow(ctx, s.ProjectAlias, now) {
					_ = deps.Eventlog.Emit(ctx, Event{
						Kind:         EventRateLimited,
						ScheduleID:   s.ID,
						ProjectAlias: s.ProjectAlias,
						At:           now,
						Reason:       "1/30s/project rate limit (catch-up)",
					})
					break
				}
				if err := dispatchOne(ctx, s, deps, now, nil); err != nil {
					return err
				}
			}
		}
	}

	return fireCurrentTick(ctx, s, deps, now, backfill)
}

func fireCurrentTick(ctx context.Context, s *Schedule, deps FireDeps, now time.Time, backfill *BackfillWindow) error {

	dec, err := deps.Quota.PreFlight(ctx, s.ProjectAlias, deps.Doctrine)
	if err != nil {
		return fmt.Errorf("scheduler.Fire: quota.PreFlight: %w", err)
	}
	if !dec.Allowed {
		_ = deps.Eventlog.Emit(ctx, Event{
			Kind:         EventQuotaCapReached,
			ScheduleID:   s.ID,
			ProjectAlias: s.ProjectAlias,
			At:           now,
			Reason:       dec.Reason,
		})
		return fmt.Errorf("%w: %s", ErrQuotaCap, dec.Reason)
	}

	if !deps.RateLimit.Allow(ctx, s.ProjectAlias, now) {
		_ = deps.Eventlog.Emit(ctx, Event{
			Kind:         EventRateLimited,
			ScheduleID:   s.ID,
			ProjectAlias: s.ProjectAlias,
			At:           now,
			Reason:       "1/30s/project rate limit",
		})
		_ = deps.Store.AppendHistory(ctx, HistoryEntry{
			ScheduleID: s.ID,
			FiredAt:    now,
			Outcome:    OutcomeRateLimited,
			Reason:     "rate-limited",
		})
		return ErrRateLimited
	}
	return dispatchOne(ctx, s, deps, now, backfill)
}

func dispatchOne(ctx context.Context, s *Schedule, deps FireDeps, now time.Time, backfill *BackfillWindow) error {
	in := DispatchInput{
		ProjectAlias: s.ProjectAlias,
		Action:       s.Action,
		Metadata: map[string]string{
			"schedule_id":  s.ID,
			"tier":         s.Tier.String(),
			"trigger_type": s.TriggerType.String(),
		},
	}
	if backfill != nil {
		in.BackfillWindow = backfill
	}
	start := time.Now()
	res, dispErr := deps.Dispatcher.Dispatch(ctx, in)
	durationMs := time.Since(start).Milliseconds()
	if dispErr != nil {
		_ = deps.Eventlog.Emit(ctx, Event{
			Kind:         EventRoutineFailed,
			ScheduleID:   s.ID,
			ProjectAlias: s.ProjectAlias,
			At:           now,
			Reason:       dispErr.Error(),
		})
		_ = deps.Store.AppendHistory(ctx, HistoryEntry{
			ScheduleID: s.ID,
			FiredAt:    now,
			Outcome:    OutcomeFailed,
			Reason:     dispErr.Error(),
			DurationMs: durationMs,
		})

		return fmt.Errorf("scheduler.Fire: dispatch: %w", dispErr)
	}

	_ = deps.Eventlog.Emit(ctx, Event{
		Kind:         EventRoutineFired,
		ScheduleID:   s.ID,
		ProjectAlias: s.ProjectAlias,
		At:           now,
		CostUSD:      res.CostUSD,
	})

	histDuration := res.DurationMs
	if histDuration == 0 {
		histDuration = durationMs
	}
	_ = deps.Store.AppendHistory(ctx, HistoryEntry{
		ScheduleID: s.ID,
		FiredAt:    now,
		Outcome:    OutcomeSuccess,
		CostUSD:    res.CostUSD,
		DurationMs: histDuration,
	})
	s.LastRunAt = now

	if next := nextRunAfter(s, deps.Doctrine, now); !next.IsZero() {
		_ = deps.Store.UpdateNextRun(ctx, s.ID, now, next)
		s.NextRunAt = next
	}
	return nil
}

// nextRunAfter returns the Schedule's next planned fire time. For
// TierRoutine + TriggerCron this is `cron.Next(now) + ComputeJitter`,
// matching Routine.Plan exactly. For TierTask
// (one-shot) we return zero — the task is auto-disabled by the tick
// driver after first fire. For TriggerHTTP / TriggerGitPoll we return
// zero — these tiers do not own a scheduler-driven next-run cursor.
//
// On parse error (legacy row, post-edit corruption) returns zero;
// the caller's UpdateStatus path is responsible for surfacing the
// failure to the operator separately.
func nextRunAfter(s *Schedule, d doctrine.Name, now time.Time) time.Time {
	if s.Tier != TierRoutine || s.TriggerType != TriggerCron {
		return time.Time{}
	}
	expr, err := ParseCron(s.TriggerConfig.CronExpr, d)
	if err != nil {
		return time.Time{}
	}
	base := expr.Next(now)
	period := base.Sub(now)
	jitter := ComputeJitter(s.ID, period)
	return base.Add(jitter)
}

var _ = errors.New
