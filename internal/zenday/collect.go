// SPDX-License-Identifier: MIT
// Package zenday — parallel source fan-out collector.
//
// Collect is the canonical entry point that fans out across N source
// legs (inbox cache, scheduler history, gh CLI poll, autonomous state,
// cost ledger, eventlog HandoffPosted) in parallel, classifies and
// ranks each result, applies per-rank caps from spec §1 Q14 B, and
// returns the merged [BriefItem] slice.
//
// Partial-tolerance (spec §6.9 friction primitive): if at least one leg
// succeeds, Collect returns the partial item set + nil error. Only when
// every leg fails does Collect return [ErrSourceCollectFailed] wrapping
// the aggregate. Per-leg failures are surfaced via the legErrors slice
// so the caller (eod.go / morning.go) can fold them into the brief
// footer or telemetry without aborting brief generation.
//
// Boundary discipline (inv-zen-031, Phase F slice): every dependency is
// an interface accepted via [CollectDeps]; the package never imports
// internal/store. Production wiring lives in Phase I daemon Start();
// tests substitute fakes for each leg.
package zenday

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type CollectDeps struct {
	Inbox InboxQuerier

	Scheduler SchedulerHistorian

	Git GitCli

	Autonomy AutonomyStateReader

	Cost CostStore

	Eventlog EventReader

	AuditProjects AuditProjectsProvider
}

type AuditProjectsProvider interface {
	GetAuditProjects(ctx context.Context) ([]AuditProjectStatus, error)
}

type legResult struct {
	leg   string
	items []BriefItem
	err   error
}

func Collect(ctx context.Context, deps CollectDeps, since time.Time, eod bool) ([]BriefItem, []error, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrCollectCancelled, err)
	}
	now := time.Now().UTC()

	const legCount = 7
	results := make(chan legResult, legCount)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectInboxLeg(ctx, deps.Inbox, since)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectSchedulerLeg(ctx, deps.Scheduler, since, now)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectGitLeg(ctx, deps.Git, since)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectAutonomyLeg(ctx, deps.Autonomy)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectCostLeg(ctx, deps.Cost, since, now)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectEventlogLeg(ctx, deps.Eventlog, eod, since, now)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- collectPlan9AuditLeg(ctx, deps.AuditProjects, now)
	}()

	wg.Wait()
	close(results)

	var (
		merged    []BriefItem
		legErrors []error
		successes int
	)
	for r := range results {
		if r.err != nil {
			legErrors = append(legErrors, fmt.Errorf("leg %s: %w", r.leg, r.err))
			continue
		}
		successes++
		merged = append(merged, r.items...)
	}

	if successes == 0 {

		return nil, legErrors, fmt.Errorf("%w: %d leg failures", ErrSourceCollectFailed, len(legErrors))
	}

	// Apply per-rank caps from spec §1 Q14 B at this layer (NOT at
	// Render): cost-cap-warning ≤ 2, autonomous-milestone ≤ 1,
	// external-activity ≤ 1.
	merged = applyPerRankCaps(merged)

	return merged, legErrors, nil
}

func applyPerRankCaps(items []BriefItem) []BriefItem {
	if len(items) == 0 {
		return items
	}

	buckets := make(map[LeverageRank][]BriefItem)
	rankOrder := make([]LeverageRank, 0)
	for _, it := range items {
		if _, seen := buckets[it.Rank]; !seen {
			rankOrder = append(rankOrder, it.Rank)
		}
		buckets[it.Rank] = append(buckets[it.Rank], it)
	}

	caps := map[LeverageRank]int{
		RankCostCapWarning:      2,
		RankAutonomousMilestone: 1,
		RankExternalActivity:    1,
	}

	out := make([]BriefItem, 0, len(items))
	for _, rank := range rankOrder {
		bucket := buckets[rank]
		if c, hasCap := caps[rank]; hasCap && len(bucket) > c {
			if rank == RankCostCapWarning {

				sort.SliceStable(bucket, func(i, j int) bool {
					return parsePercent(string(bucket[i].Severity)) > parsePercent(string(bucket[j].Severity))
				})
			} else {

				sort.SliceStable(bucket, func(i, j int) bool {
					return bucket[i].CreatedAt.After(bucket[j].CreatedAt)
				})
			}
			bucket = bucket[:c]
		}
		out = append(out, bucket...)
	}
	return out
}

func parsePercent(s string) float64 {
	var pct float64
	if _, err := fmt.Sscanf(s, "%f%%", &pct); err != nil {
		return 0
	}
	return pct
}

func classifySeverityToRank(severity inbox.Severity, eventType string) LeverageRank {
	switch severity {
	case inbox.SeverityUrgent:
		return RankUrgentEvent
	case inbox.SeverityActionNeeded:
		if hasPrefix(eventType, "scheduler.") {
			return RankFailedScheduledJob
		}
		return RankInfoImmediate
	case inbox.SeverityInfoImmediate:
		return RankInfoImmediate
	case inbox.SeverityInfoDigest:
		if hasPrefix(eventType, "autonomy.") {
			return RankAutonomousMilestone
		}
		return RankInfoImmediate
	default:
		return RankInfoImmediate
	}
}

func hasPrefix(s, p string) bool {
	if len(p) > len(s) {
		return false
	}
	return s[:len(p)] == p
}

func collectInboxLeg(ctx context.Context, store InboxQuerier, since time.Time) legResult {
	if store == nil {
		return legResult{leg: "inbox", err: errors.New("nil InboxQuerier")}
	}
	rows, err := store.Query(ctx, InboxListFilter{Since: &since, Limit: 50, IncludeAcked: false})
	if err != nil {
		return legResult{leg: "inbox", err: err}
	}
	out := make([]BriefItem, 0, len(rows))
	for _, r := range rows {
		bi := BriefItem{
			Rank:      classifySeverityToRank(r.Severity, r.EventType),
			Severity:  r.Severity,
			Project:   r.ProjectAlias,
			EventType: r.EventType,

			Message:   r.EventType,
			Source:    fmt.Sprintf("inbox:%d", r.NotificationID),
			CreatedAt: r.CreatedAt,
		}
		out = append(out, bi)
	}
	return legResult{leg: "inbox", items: out}
}

// collectSchedulerLeg fans out the failed-jobs leg. Filters by Outcome
// == "failed" so scheduled jobs that succeeded or rate-limited do not
// pollute the brief.
func collectSchedulerLeg(ctx context.Context, store SchedulerHistorian, since, now time.Time) legResult {
	if store == nil {
		return legResult{leg: "scheduler", err: errors.New("nil SchedulerHistorian")}
	}
	hist, err := store.QueryHistory(ctx, "", since, now)
	if err != nil {
		return legResult{leg: "scheduler", err: err}
	}
	out := make([]BriefItem, 0)
	for _, h := range hist {
		if h.Outcome != "failed" {
			continue
		}
		out = append(out, BriefItem{
			Rank:      RankFailedScheduledJob,
			Severity:  inbox.SeverityActionNeeded,
			Project:   h.ProjectAlias,
			EventType: "scheduler.routine_failed",
			Message:   fmt.Sprintf("scheduled job %q failed (%s)", h.Action, h.Reason),
			Action:    fmt.Sprintf("zen schedule run --now %s", h.ScheduleID),
			Source:    fmt.Sprintf("scheduled-job:%s", h.ScheduleID),
			CreatedAt: h.FiredAt,
		})
	}
	return legResult{leg: "scheduler", items: out}
}

func collectGitLeg(ctx context.Context, cli GitCli, since time.Time) legResult {
	if cli == nil {
		return legResult{leg: "git", err: errors.New("nil GitCli")}
	}
	acts, err := cli.RecentActivity(ctx, since)
	if err != nil {
		return legResult{leg: "git", err: err}
	}
	out := make([]BriefItem, 0, len(acts))
	for _, a := range acts {
		bi := BriefItem{
			Rank:      RankExternalActivity,
			Severity:  inbox.SeverityInfoImmediate,
			Project:   a.ProjectAlias,
			EventType: "git." + a.Kind,
			Message:   a.Description,
			Source:    "external:gh:" + a.ProjectAlias,
			CreatedAt: a.CreatedAt,
		}
		if a.URL != "" {
			bi.Action = a.URL
		}
		out = append(out, bi)
	}
	return legResult{leg: "git", items: out}
}

func collectAutonomyLeg(ctx context.Context, reader AutonomyStateReader) legResult {
	if reader == nil {
		return legResult{leg: "autonomy", err: errors.New("nil AutonomyStateReader")}
	}
	snaps, err := reader.Snapshot(ctx)
	if err != nil {
		return legResult{leg: "autonomy", err: err}
	}
	out := make([]BriefItem, 0, len(snaps))
	for _, s := range snaps {
		switch s.State {
		case "paused":
			out = append(out, BriefItem{
				Rank:      RankOperatorGate,
				Severity:  inbox.SeverityActionNeeded,
				Project:   s.ProjectAlias,
				EventType: "autonomy.paused",
				Message:   fmt.Sprintf("autonomous-mode paused: %s", s.PauseReason),
				Action:    fmt.Sprintf("zen autonomy ack %s", s.ProjectAlias),
				Source:    fmt.Sprintf("operator-gate:%s.autonomous-paused", s.ProjectAlias),
				CreatedAt: s.LastMilestoneAt,
			})
		case "active":
			if s.LastMilestoneAt.IsZero() {
				continue
			}
			out = append(out, BriefItem{
				Rank:      RankAutonomousMilestone,
				Severity:  inbox.SeverityInfoDigest,
				Project:   s.ProjectAlias,
				EventType: "autonomy.milestone",
				Message:   s.LastMilestone,
				Source:    fmt.Sprintf("autonomy:%s.milestone", s.ProjectAlias),
				CreatedAt: s.LastMilestoneAt,
			})
		}
	}
	return legResult{leg: "autonomy", items: out}
}

// collectCostLeg fans out the cost-ledger leg. Emits rank-4 cap-warning
// items for projects with PercentUsed >= 80 (per-rank cap of 2 applied
// downstream by applyPerRankCaps).
//
// The Severity field is repurposed to carry the canonical "%.1f%%"
// percent encoding so applyPerRankCaps can sort by descending percent
// without re-fetching the CostStatus.
func collectCostLeg(ctx context.Context, store CostStore, from, to time.Time) legResult {
	if store == nil {
		return legResult{leg: "cost", err: errors.New("nil CostStore")}
	}
	statuses, err := store.SpendByProject(ctx, from, to)
	if err != nil {
		return legResult{leg: "cost", err: err}
	}
	out := make([]BriefItem, 0)
	for _, s := range statuses {
		if s.PercentUsed < 80.0 {
			continue
		}
		out = append(out, BriefItem{
			Rank:      RankCostCapWarning,
			Severity:  inbox.Severity(fmt.Sprintf("%.1f%%", s.PercentUsed)),
			Project:   s.ProjectAlias,
			EventType: "cost.cap_warning",
			Message:   fmt.Sprintf("at %.1f%% daily cap — approaches threshold", s.PercentUsed),
			Source:    fmt.Sprintf("cost-cap:%s", s.ProjectAlias),
			CreatedAt: to,
		})
	}
	return legResult{leg: "cost", items: out}
}

// collectEventlogLeg fans out the eventlog HandoffPosted leg. Only
// runs when eod=true (morning briefs do not consume HandoffPosted).
//
// The leg returns no items — the eod composer in eod.go re-queries
// directly to decode HandoffPostedEvent payloads into
// ProjectStatusSection. This leg is a connectivity-probe so an
// eventlog outage during EOD generation surfaces in legErrors before
// eod.go's typed pass.
func collectEventlogLeg(ctx context.Context, reader EventReader, eod bool, from, to time.Time) legResult {
	if !eod {
		// Morning briefs do not consume HandoffPosted leg.
		return legResult{leg: "eventlog"}
	}
	if reader == nil {
		return legResult{leg: "eventlog", err: errors.New("nil EventReader")}
	}
	if _, err := reader.QueryByType(ctx, "HandoffPosted", from, to); err != nil {
		return legResult{leg: "eventlog", err: err}
	}
	return legResult{leg: "eventlog"}
}

func collectPlan9AuditLeg(ctx context.Context, provider AuditProjectsProvider, now time.Time) legResult {
	if provider == nil {

		return legResult{leg: "plan-9-audit"}
	}
	projects, err := provider.GetAuditProjects(ctx)
	if err != nil {
		return legResult{leg: "plan-9-audit", err: fmt.Errorf("GetAuditProjects: %w", err)}
	}
	items, err := CollectAuditSection(ctx, AuditSectionDeps{
		Projects: projects,
		Now:      now,
	})
	if err != nil {
		return legResult{leg: "plan-9-audit", err: err}
	}
	return legResult{leg: "plan-9-audit", items: items}
}
