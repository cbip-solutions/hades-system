// SPDX-License-Identifier: MIT
// Package hadesday — audit_section.go
//
// Extends release hadesday with a NEW collector that walks per-project release
// substrate health (chain integrity + backup status + ADR transitions today
// + research cache hit rate + state freshness) and produces BriefItem per
// project. LeverageRank assigned per worst-substrate-status:
//
// - FAIL → RankCritical (1): any FAIL probe → operator must investigate immediately
// - WARN → RankAlertNeeded (3): any WARN probe → operator awareness needed today
// - OK → RankInfoSummary (7): all OK → daily reassurance summary
//
// reuse of hadesday.BriefItem and hadesday.SortByLeverage unchanged.
//
// Thresholds are derived from AuditProjectStatus.DoctrineName so this package
// does NOT import internal/doctrine — preserving the invariant boundary
// (hadesday accepts only interface parameters; no concrete store or doctrine
// types cross the package boundary).
package hadesday

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	RankCritical LeverageRank = RankOperatorGate

	RankAlertNeeded LeverageRank = RankUrgentEvent

	RankInfoSummary LeverageRank = RankInfoImmediate
)

type AuditProjectStatus struct {
	Alias string

	DoctrineName string

	ChainLastVerifyAge time.Duration

	TamperEventsLast7d int

	LitestreamLag time.Duration

	ColdArchiveAge time.Duration

	S3Reachable bool

	AdrTransitionsToday int

	AdrTransitionDescriptions []string

	ResearchCacheHitRate float64

	ResearchCacheHitsToday int

	ResearchCacheMissesToday int

	StateLastRegenerateAge time.Duration
}

type AuditSectionDeps struct {
	Projects []AuditProjectStatus

	Now time.Time
}

func CollectAuditSection(ctx context.Context, deps AuditSectionDeps) ([]BriefItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("CollectAuditSection: %w", err)
	}
	if deps.Now.IsZero() {
		return nil, fmt.Errorf("CollectAuditSection: deps.Now must be non-zero")
	}

	items := make([]BriefItem, 0, len(deps.Projects))
	for _, p := range deps.Projects {
		items = append(items, buildAuditProjectItem(p, deps.Now))
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Rank != items[j].Rank {
			return items[i].Rank < items[j].Rank
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return items, nil
}

func buildAuditProjectItem(p AuditProjectStatus, now time.Time) BriefItem {
	thr := auditDoctrineThresholds(p.DoctrineName)

	chainStatus := evalAuditChainStatus(p, thr)
	backupStatus := evalAuditBackupStatus(p, thr)
	stateStatus := evalAuditStateStatus(p, thr)

	worst := worstAuditStatus(chainStatus, backupStatus, stateStatus)
	rank := auditRankFromStatus(worst)

	body := renderAuditProjectBodyLines(p, chainStatus, backupStatus, stateStatus)

	return BriefItem{
		Rank:      rank,
		Project:   p.Alias,
		EventType: "plan9.audit",
		Message:   body,
		Action:    auditProjectAction(worst, p.Alias),
		Source:    "plan-9-audit",
		CreatedAt: now,
	}
}

type auditSubstrateStatus int

const (
	auditStatusOK auditSubstrateStatus = iota
	auditStatusWarn
	auditStatusFail
)

type auditThresholds struct {
	chainVerifyCadence time.Duration

	litestreamLagThreshold time.Duration

	coldArchiveThreshold time.Duration

	stateFreshnessThreshold time.Duration
}

func auditDoctrineThresholds(name string) auditThresholds {
	switch name {
	case "max-scope":

		return auditThresholds{
			chainVerifyCadence:      24 * time.Hour,
			litestreamLagThreshold:  60 * time.Second,
			coldArchiveThreshold:    9 * 24 * time.Hour,
			stateFreshnessThreshold: 7 * 24 * time.Hour,
		}
	case "capa-firewall":

		return auditThresholds{
			chainVerifyCadence:      24 * time.Hour,
			litestreamLagThreshold:  60 * time.Second,
			coldArchiveThreshold:    9 * 24 * time.Hour,
			stateFreshnessThreshold: 24 * time.Hour,
		}
	default:

		return auditThresholds{
			chainVerifyCadence:      24 * time.Hour,
			litestreamLagThreshold:  60 * time.Second,
			coldArchiveThreshold:    9 * 24 * time.Hour,
			stateFreshnessThreshold: 7 * 24 * time.Hour,
		}
	}
}

func evalAuditChainStatus(p AuditProjectStatus, thr auditThresholds) auditSubstrateStatus {
	switch {
	case p.TamperEventsLast7d > 3:
		return auditStatusFail
	case p.ChainLastVerifyAge > 2*thr.chainVerifyCadence:
		return auditStatusFail
	case p.TamperEventsLast7d > 0:
		return auditStatusWarn
	case p.ChainLastVerifyAge > thr.chainVerifyCadence:
		return auditStatusWarn
	default:
		return auditStatusOK
	}
}

func evalAuditBackupStatus(p AuditProjectStatus, thr auditThresholds) auditSubstrateStatus {
	switch {
	case !p.S3Reachable:
		return auditStatusFail
	case p.LitestreamLag > 2*thr.litestreamLagThreshold:
		return auditStatusFail
	case p.ColdArchiveAge > 2*thr.coldArchiveThreshold:
		return auditStatusFail
	case p.LitestreamLag > thr.litestreamLagThreshold:
		return auditStatusWarn
	case p.ColdArchiveAge > thr.coldArchiveThreshold:
		return auditStatusWarn
	default:
		return auditStatusOK
	}
}

func evalAuditStateStatus(p AuditProjectStatus, thr auditThresholds) auditSubstrateStatus {
	warnAt := time.Duration(float64(thr.stateFreshnessThreshold) * 0.8)
	switch {
	case p.StateLastRegenerateAge > thr.stateFreshnessThreshold:
		return auditStatusFail
	case p.StateLastRegenerateAge > warnAt:
		return auditStatusWarn
	default:
		return auditStatusOK
	}
}

func worstAuditStatus(statuses ...auditSubstrateStatus) auditSubstrateStatus {
	worst := auditStatusOK
	for _, s := range statuses {
		if s > worst {
			worst = s
		}
	}
	return worst
}

func auditRankFromStatus(s auditSubstrateStatus) LeverageRank {
	switch s {
	case auditStatusFail:
		return RankCritical
	case auditStatusWarn:
		return RankAlertNeeded
	default:
		return RankInfoSummary
	}
}

func auditGlyph(s auditSubstrateStatus) string {
	switch s {
	case auditStatusFail:
		return "✗"
	case auditStatusWarn:
		return "⚠"
	default:
		return "✓"
	}
}

func auditProjectAction(worst auditSubstrateStatus, alias string) string {
	switch worst {
	case auditStatusFail:
		return fmt.Sprintf("hades audit status --project %s", alias)
	case auditStatusWarn:
		return fmt.Sprintf("hades audit history --project %s --since 7d", alias)
	default:
		return ""
	}
}

func renderAuditProjectBodyLines(
	p AuditProjectStatus,
	chain, backup, state auditSubstrateStatus,
) string {
	var b strings.Builder

	b.WriteString("  • Chain integrity: last verify ")
	b.WriteString(auditHumanDuration(p.ChainLastVerifyAge))
	b.WriteString(" ago ")
	b.WriteString(auditGlyph(chain))
	if p.TamperEventsLast7d > 0 {
		b.WriteString(fmt.Sprintf(" (%d tamper events last 7d)", p.TamperEventsLast7d))
	}
	b.WriteString("\n")

	b.WriteString("  • Backup: ")
	if !p.S3Reachable {
		b.WriteString("S3 unreachable ✗")
	} else {
		litestreamGlyph := auditGlyph(auditLitestreamSubstatus(p.LitestreamLag))
		coldArchiveGlyph := auditGlyph(auditColdArchiveSubstatus(p.ColdArchiveAge))
		b.WriteString(fmt.Sprintf("Litestream lag %s %s; cold archive %s ago %s",
			auditHumanDuration(p.LitestreamLag), litestreamGlyph,
			auditHumanDuration(p.ColdArchiveAge), coldArchiveGlyph,
		))
	}
	b.WriteString("\n")

	if len(p.AdrTransitionDescriptions) > 0 {
		b.WriteString(fmt.Sprintf("  • ADRs: %d proposed (%s)",
			p.AdrTransitionsToday,
			strings.Join(p.AdrTransitionDescriptions, ", "),
		))
	} else {
		b.WriteString(fmt.Sprintf("  • ADRs: %d transitions today", p.AdrTransitionsToday))
	}
	b.WriteString("\n")

	totalRequests := p.ResearchCacheHitsToday + p.ResearchCacheMissesToday
	if totalRequests > 0 {
		b.WriteString(fmt.Sprintf("  • Research cache: %.0f%% hit rate today (%d hits, %d misses)\n",
			p.ResearchCacheHitRate*100,
			p.ResearchCacheHitsToday,
			p.ResearchCacheMissesToday,
		))
	} else {
		b.WriteString(fmt.Sprintf("  • Research cache: %.0f%% hit rate today\n",
			p.ResearchCacheHitRate*100))
	}

	b.WriteString("  • State: last regenerate ")
	b.WriteString(auditHumanDuration(p.StateLastRegenerateAge))
	b.WriteString(" ago ")
	b.WriteString(auditGlyph(state))
	b.WriteString("\n")

	return b.String()
}

func auditLitestreamSubstatus(lag time.Duration) auditSubstrateStatus {
	const threshold = 60 * time.Second
	switch {
	case lag > 2*threshold:
		return auditStatusFail
	case lag > threshold:
		return auditStatusWarn
	default:
		return auditStatusOK
	}
}

func auditColdArchiveSubstatus(age time.Duration) auditSubstrateStatus {
	const threshold = 9 * 24 * time.Hour
	switch {
	case age > 2*threshold:
		return auditStatusFail
	case age > threshold:
		return auditStatusWarn
	default:
		return auditStatusOK
	}
}

func auditHumanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func RenderAuditSection(items []BriefItem, now time.Time) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## [release audit + persistence] — %s\n\n",
		now.Format("2006-01-02")))
	for _, it := range items {
		glyph := auditRankGlyph(it.Rank)
		b.WriteString(fmt.Sprintf("%s Project %s:\n", glyph, it.Project))

		b.WriteString(it.Message)
		b.WriteString("\n")
	}
	return b.String()
}

func auditRankGlyph(rank LeverageRank) string {
	switch rank {
	case RankCritical:
		return "✗"
	case RankAlertNeeded:
		return "⚠"
	default:
		return "✓"
	}
}

func init() {

	if RankCritical != RankOperatorGate {
		panic("hadesday audit_section: RankCritical alias drift from RankOperatorGate")
	}
	if RankAlertNeeded != RankUrgentEvent {
		panic("hadesday audit_section: RankAlertNeeded alias drift from RankUrgentEvent")
	}
	if RankInfoSummary != RankInfoImmediate {
		panic("hadesday audit_section: RankInfoSummary alias drift from RankInfoImmediate")
	}
}
