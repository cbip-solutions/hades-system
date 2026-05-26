// SPDX-License-Identifier: MIT
package scheduler

import (
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
)

// ComputeMissed quantifies how many ticks a Schedule missed between
// its LastRunAt and `now`, clamped to MissLookback.
//
// Algorithm (spec §1 Q9 C):
//
//  1. If LastRunAt.IsZero() → no prior fire; missed = 0. We do not
//     back-fill the period preceding INSERT — that would conflate
//     routine-creation with catch-up.
//  2. If TriggerType == TriggerHTTP → missed = 0. HTTP triggers are
//     push (POST /v1/schedules/{id}/fire), not pull, so by definition
//     they cannot accumulate "missed" fires regardless of gap.
//  3. If now <= LastRunAt → missed = 0. Defends against wall-clock
//     skew (NTP correction, container migration) — the next valid
//     tick is picked up on the next ComputeMissed call once the
//     clock advances past LastRunAt.
//  4. Clamp the gap [LastRunAt, now] to MissLookback. A zero
//     MissLookback means "unlimited" (no clamp); adapter paths that
//     omit the column inherit doctrine-default behaviour.
//  5. Walk the schedule from clamped(LastRunAt) to now and count the
//     ticks. Subtract 1 — the most recent tick equal to or before
//     now is the *upcoming fire* (the current tick), not a missed
//     one.
//
// The function is policy-agnostic: it returns the same MissedCount
// regardless of MissPolicy. The Skip / NotifyOnly behaviour is the
// dispatcher's job (consume the count, emit audit log, decide not
// to fire) — not ComputeMissed's. This keeps the audit trail honest:
// a Skip'd routine still records "missed N" in schedule_history.
//
// Lookback floor for git-poll (load-bearing): if a schedule somehow
// has PollIntervalSeconds < 60 (legacy row, manual edit, doctrine-
// floor parser-version skew), the interval is clamped to 60s before
// division. Without this an adapter that didn't enforce its own
// floor would multiply missed counts by (true / 60).
//
// Defence-in-depth on cron parse: a corrupt CronExpr in the row
// returns zero MissedFire — the caller surfaces the misconfiguration
// via Schedule.Status separately. We use doctrine.NameMaxScope as
// the doctrine for re-parse so the function never rejects a 30-59s
// cron that was accepted by the row's original parse (max-scope is
// the loosest of the three doctrines for granularity).
//
// Boundary (inv-zen-031): stdlib + internal/doctrine only. No
// internal/store, internal/providers, or private-tier1-module.
//
// Inv-zen-121 contract.
func ComputeMissed(s *Schedule, now time.Time) MissedFire {
	out := MissedFire{ScheduleID: s.ID}
	if s.LastRunAt.IsZero() {
		return out
	}
	if s.TriggerType == TriggerHTTP {
		return out
	}
	if !now.After(s.LastRunAt) {
		return out
	}

	clampedFrom := s.LastRunAt
	if s.MissLookback > 0 && now.Sub(s.LastRunAt) > s.MissLookback {
		clampedFrom = now.Add(-s.MissLookback)
		out.LookbackUsed = s.MissLookback
	} else {
		out.LookbackUsed = now.Sub(s.LastRunAt)
	}
	switch s.TriggerType {
	case TriggerCron:

		expr, err := ParseCron(s.TriggerConfig.CronExpr, doctrine.NameMaxScope)
		if err != nil {

			return out
		}

		var ticks []time.Time
		t := expr.Next(clampedFrom)
		for t.Before(now) || t.Equal(now) {
			ticks = append(ticks, t)
			t = expr.Next(t)
		}
		count := len(ticks) - 1
		if count < 0 {
			count = 0
		}
		out.MissedCount = count
		if len(ticks) > 0 {
			out.From = ticks[0]
		}
		out.To = now
	case TriggerGitPoll:

		interval := time.Duration(s.TriggerConfig.PollIntervalSeconds) * time.Second
		if interval < time.Minute {
			interval = time.Minute
		}
		gap := now.Sub(clampedFrom)
		count := int(gap/interval) - 1
		if count < 0 {
			count = 0
		}
		out.MissedCount = count
		out.From = clampedFrom.Add(interval)
		out.To = now
	}
	return out
}

// Coalesce returns a BackfillWindow when the Schedule's miss policy
// is MissPolicyCoalesce AND there are missed fires. The returned
// window has From = first missed tick wall clock, To = now (sourced
// from the input MissedFire).
//
// Returns (zero BackfillWindow, false) when:
//   - s == nil (defence-in-depth for adapter paths constructing a
//     Schedule from a partial daemon.db row).
//   - s.MissPolicy != MissPolicyCoalesce (Skip / CatchUpBounded /
//     NotifyOnly all do NOT consolidate; the dispatcher handles
//     them as N individual fires or audit-only).
//   - missed.MissedCount == 0 (no fires to coalesce; emitting an
//     empty window would surface as a no-op fire in the audit log).
//
// Why this is split from ComputeMissed: ComputeMissed is a pure
// measurement function (always honest about the count, regardless
// of policy). Coalesce is the policy decision: only one of the
// four MissPolicy values produces a BackfillWindow, and the
// dispatcher needs the (BackfillWindow, bool) shape to decide
// whether to emit a single Fire(BackfillWindow) call or a loop of
// Fire() calls clamped by the rate-limiter.
//
// Boundary (inv-zen-031): stdlib only at this site (Schedule and
// MissedFire are in-package types).
//
// Inv-zen-121 contract.
func Coalesce(s *Schedule, missed MissedFire) (BackfillWindow, bool) {
	if s == nil || s.MissPolicy != MissPolicyCoalesce {
		return BackfillWindow{}, false
	}
	if missed.MissedCount == 0 {
		return BackfillWindow{}, false
	}
	return BackfillWindow{From: missed.From, To: missed.To}, true
}
