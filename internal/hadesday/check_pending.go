// SPDX-License-Identifier: MIT
// Package hadesday — `hades day --check-pending` introspection composer.
//
// CheckPending is the operator-pull preview surface that answers two
// questions without touching the morning/EOD archive files:
//
// 1. When does the next scheduled morning brief fire?
// 2. How many action-needed / urgent items have arrived since the
// last successful brief?
//
// Per spec §6.1 mid-day operator pull example: the CLI invokes
// `hades day --check-pending`; the daemon HTTP handler invokes the same
// composer via the (*Generator).CheckPending façade. Output is
// ephemeral — there is no archive write; callers Render the doc
// directly to stdout.
//
// Failure-tolerance ladder (mirrors morning brief's "preview is
// informational" stance):
//
// HARD (return error):
// - inbox.Query fails (zero counts that masquerade as "no
// pending" would deceive the operator into a false sense of
// quiet — better to surface the outage than mask it).
//
// SOFT (continue + render):
// - scheduler.QueryHistory fails → cutoff falls back to now-24h.
// - schedulerNextFire fails → NextScheduledAt = zero time
// (renders as "0001-01-01 00:00:00", operator reads as
// "scheduler down").
package hadesday

import (
	"context"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type SchedulerNextFireReader interface {
	NextFire(ctx context.Context, scheduleID string) (time.Time, error)
}

type CheckPendingDeps struct {
	Inbox InboxQuerier

	Scheduler SchedulerHistorian

	NextFire SchedulerNextFireReader

	Clock Clock

	MorningBriefID string
}

const historyLookbackWindow = 7 * 24 * time.Hour

const noHistoryFallbackWindow = 24 * time.Hour

func CheckPending(ctx context.Context, deps CheckPendingDeps) (BriefDoc, error) {
	now := deps.Clock.Now().UTC()
	lastBriefAt := time.Time{}

	if hist, err := deps.Scheduler.QueryHistory(ctx, deps.MorningBriefID,
		now.Add(-historyLookbackWindow), now); err == nil {
		for _, h := range hist {
			if h.Outcome == "success" && h.FiredAt.After(lastBriefAt) {
				lastBriefAt = h.FiredAt
			}
		}
	}

	next, _ := deps.NextFire.NextFire(ctx, deps.MorningBriefID)

	since := lastBriefAt
	if since.IsZero() {
		since = now.Add(-noHistoryFallbackWindow)
	}

	rows, err := deps.Inbox.Query(ctx, InboxListFilter{Since: &since, IncludeAcked: false})
	if err != nil {
		return BriefDoc{}, fmt.Errorf("check-pending inbox: %w", err)
	}

	var actionNeeded, urgent int
	for _, r := range rows {
		switch r.Severity {
		case inbox.SeverityActionNeeded:
			actionNeeded++
		case inbox.SeverityUrgent:
			urgent++
		}
	}

	return BriefDoc{
		Type:                BriefTypeCheckPending,
		NextScheduledAt:     next,
		PendingActionNeeded: actionNeeded,
		PendingUrgent:       urgent,
	}, nil
}
