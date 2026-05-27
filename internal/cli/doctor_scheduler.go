// SPDX-License-Identifier: MIT
// Package cli — doctor_scheduler.go
//
// Task J-4: scheduler subsystem doctor probe. Four aspects per
// spec §6.7 (queue.depth, missed_fires.recent, wfq.saturation,
// dispatcher.bound). RunSchedulerProbe is delegate-only; impl in
// internal/scheduler/prober.go.
//
// invariant + invariant anchor: the dispatcher.bound aspect probes
// that the scheduler can reach the release dispatcher at runtime — a
// non-nil "scheduler.fire dispatched directly" path would surface here.
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// SchedulerProber is the contract RunSchedulerProbe consumes.
//
// All methods MUST be safe for concurrent use, MUST honour ctx, and
// SHOULD return within 1 second.
type SchedulerProber interface {
	QueueDepth(ctx context.Context) (total int, byProject map[string]int, err error)

	MissedFires24h(ctx context.Context) (total int, byProject map[string]int, err error)

	WfqSaturation(ctx context.Context) (maxPct int, maxAlias string, err error)

	DispatcherBound(ctx context.Context) error
}

const (
	schedulerQueueWarnAt  = 5
	schedulerQueueFailAt  = 10
	schedulerMissedWarnAt = 1
	schedulerMissedFailAt = 6
	schedulerWfqWarnPct   = 80
	schedulerWfqFailPct   = 95
)

func RunSchedulerProbe(ctx context.Context, p SchedulerProber) ([]ProbeResult, error) {
	out := make([]ProbeResult, 0, 4)
	out = append(out, runSchedulerQueueDepth(ctx, p))
	out = append(out, runSchedulerMissedFires(ctx, p))
	out = append(out, runSchedulerWfqSaturation(ctx, p))
	out = append(out, runSchedulerDispatcherBound(ctx, p))
	return out, nil
}

func runSchedulerQueueDepth(ctx context.Context, p SchedulerProber) ProbeResult {
	r := ProbeResult{Name: "scheduler.queue.depth"}
	total, byProject, err := p.QueueDepth(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "queue.depth query failed"
		r.Detail = err.Error()
		return r
	}
	r.Message = fmt.Sprintf("%d pending across daemon", total)
	if len(byProject) > 0 {
		r.Detail = formatProjectMap(byProject)
	}
	switch {
	case total >= schedulerQueueFailAt:
		r.Status = ProbeFail
		r.Hint = "WFQ saturated; some project may be starved. Inspect: zen schedule queue. Mitigate via per-project priority boost: zen project priority --boost <alias> --duration 1h"
	case total >= schedulerQueueWarnAt:
		r.Status = ProbeWarn
		r.Hint = "approaching WFQ saturation threshold (10); inspect: zen schedule queue"
	default:
		r.Status = ProbeOK
	}
	return r
}

func runSchedulerMissedFires(ctx context.Context, p SchedulerProber) ProbeResult {
	r := ProbeResult{Name: "scheduler.missed_fires.recent"}
	total, byProject, err := p.MissedFires24h(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "missed_fires query failed"
		r.Detail = err.Error()
		return r
	}
	r.Message = fmt.Sprintf("%d MissedFire events in last 24h", total)
	if len(byProject) > 0 {
		r.Detail = formatProjectMap(byProject)
	}
	switch {
	case total >= schedulerMissedFailAt:
		r.Status = ProbeFail
		r.Hint = "high miss rate suggests cron daemon down or system suspended; inspect: zen schedule history --since 24h"
	case total >= schedulerMissedWarnAt:
		r.Status = ProbeWarn
		r.Hint = "occasional misses are normal under doctrine 'catch-up-bounded'; inspect: zen schedule history --since 24h"
	default:
		r.Status = ProbeOK
	}
	return r
}

func runSchedulerWfqSaturation(ctx context.Context, p SchedulerProber) ProbeResult {
	r := ProbeResult{Name: "scheduler.wfq.saturation"}
	maxPct, maxAlias, err := p.WfqSaturation(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "wfq.saturation query failed"
		r.Detail = err.Error()
		return r
	}
	if maxAlias == "" {
		r.Status = ProbeOK
		r.Message = "no active WFQ queues"
		return r
	}
	r.Message = fmt.Sprintf("max %d%% (project %s)", maxPct, maxAlias)
	switch {
	case maxPct >= schedulerWfqFailPct:
		r.Status = ProbeFail
		r.Hint = fmt.Sprintf("project %s saturating WFQ tokens; consider operator override: zen project priority --boost %s --duration 30m", maxAlias, maxAlias)
	case maxPct >= schedulerWfqWarnPct:
		r.Status = ProbeWarn
	default:
		r.Status = ProbeOK
	}
	return r
}

func runSchedulerDispatcherBound(ctx context.Context, p SchedulerProber) ProbeResult {
	r := ProbeResult{Name: "scheduler.dispatcher.bound"}
	if err := p.DispatcherBound(ctx); err != nil {
		r.Status = ProbeFail
		r.Message = "dispatcher unreachable"
		r.Detail = err.Error()
		r.Hint = "inv-zen-080 + inv-zen-123: scheduler.fire MUST dispatch via Plan 3 dispatcher. Check: zen orchestrator status; restart daemon if dispatcher process died"
		return r
	}
	r.Status = ProbeOK
	r.Message = "Plan 3 dispatcher reachable"
	return r
}

func formatProjectMap(m map[string]int) string {
	type pair struct {
		alias string
		count int
	}
	pairs := make([]pair, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, pair{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].alias < pairs[j].alias
	})
	var sb strings.Builder
	for i, p := range pairs {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%s: %d", p.alias, p.count))
	}
	return sb.String()
}
