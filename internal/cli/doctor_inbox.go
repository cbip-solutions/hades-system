// SPDX-License-Identifier: MIT
// Package cli — doctor_inbox.go
//
// task: inbox subsystem doctor probe. Four aspects per
// spec §6.7 (aggregator.cache.consistent, outbox.queue.depth,
// dedup.window.health, severity.distribution).
//
// invariant anchor: aggregator.cache.consistent reconciles per-project
// authoritative inbox row counts vs the daemon-level
// inbox_aggregator_cache. Drift > tolerance signals write-fanout failure
// or outbox replay missed.
//
// invariant anchor: severity.distribution exposes the 4-tier enum
// (urgent / action-needed / info-immediate / info-digest). Detail
// rendering uses the canonical order so operator output is stable.
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type InboxProber interface {
	AggregatorCacheConsistent(ctx context.Context) (consistent bool, driftRows int, detail string, err error)

	OutboxQueueDepth(ctx context.Context) (int, error)

	DedupConstraintViolations(ctx context.Context) (int, error)

	SeverityDistribution24h(ctx context.Context) (dist map[string]int, urgentCount int, err error)
}

const (
	inboxCacheDriftWarnRows = 1
	inboxCacheDriftFailRows = 3
	inboxOutboxWarnDepth    = 50
	inboxOutboxFailDepth    = 200
	inboxUrgentWarnPerDay   = 5
)

var inboxSeverityTiersOrder = []string{"urgent", "action-needed", "info-immediate", "info-digest"}

var inboxKnownSeverityTiers = map[string]bool{
	"urgent":         true,
	"action-needed":  true,
	"info-immediate": true,
	"info-digest":    true,
}

func RunInboxProbe(ctx context.Context, p InboxProber) ([]ProbeResult, error) {
	out := make([]ProbeResult, 0, 4)
	out = append(out, runInboxAggregator(ctx, p))
	out = append(out, runInboxOutbox(ctx, p))
	out = append(out, runInboxDedup(ctx, p))
	out = append(out, runInboxSeverity(ctx, p))
	return out, nil
}

func runInboxAggregator(ctx context.Context, p InboxProber) ProbeResult {
	r := ProbeResult{Name: "inbox.aggregator.cache.consistent"}
	consistent, drift, detail, err := p.AggregatorCacheConsistent(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "aggregator consistency query failed"
		r.Detail = err.Error()
		return r
	}
	if consistent {
		r.Status = ProbeOK
		r.Message = "per-project counts match aggregator cache"
		return r
	}
	r.Message = fmt.Sprintf("%d row drift between per-project and cache", drift)
	r.Detail = detail
	switch {
	case drift >= inboxCacheDriftFailRows:
		r.Status = ProbeFail
		r.Hint = "rebuild aggregator cache: hades inbox rebuild-cache (HADES design spec §3.3 — cache is rebuildable from per-project authoritative)"
	case drift >= inboxCacheDriftWarnRows:
		r.Status = ProbeWarn
		r.Hint = "minor drift may be in-flight write; if persistent: hades inbox rebuild-cache"
	default:

		r.Status = ProbeOK
	}
	return r
}

func runInboxOutbox(ctx context.Context, p InboxProber) ProbeResult {
	r := ProbeResult{Name: "inbox.outbox.queue.depth"}
	depth, err := p.OutboxQueueDepth(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "outbox query failed"
		r.Detail = err.Error()
		return r
	}
	r.Message = fmt.Sprintf("%d pending notifications", depth)
	switch {
	case depth >= inboxOutboxFailDepth:
		r.Status = ProbeFail
		r.Hint = "delivery saturated; check HADES design channel adapters or osascript availability; flush via: hades inbox flush"
	case depth >= inboxOutboxWarnDepth:
		r.Status = ProbeWarn
		r.Hint = "delivery is backing up; spec §6.5 expects steady-state ≤50"
	default:
		r.Status = ProbeOK
	}
	return r
}

func runInboxDedup(ctx context.Context, p InboxProber) ProbeResult {
	r := ProbeResult{Name: "inbox.dedup.window.health"}
	v, err := p.DedupConstraintViolations(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "dedup constraint query failed"
		r.Detail = err.Error()
		return r
	}
	if v == 0 {
		r.Status = ProbeOK
		r.Message = "(event_type,content_hash,bucket) UNIQUE constraint clean"
		return r
	}
	r.Status = ProbeFail
	r.Message = fmt.Sprintf("%d rows violate dedup UNIQUE constraint", v)
	r.Hint = "UNIQUE constraint violated; schema drift detected — verify migration 058 applied: hades daemon migrations status; if needed, restore from backup"
	return r
}

func runInboxSeverity(ctx context.Context, p InboxProber) ProbeResult {
	r := ProbeResult{Name: "inbox.severity.distribution"}
	dist, urgent, err := p.SeverityDistribution24h(ctx)
	if err != nil {
		r.Status = ProbeFail
		r.Message = "severity query failed"
		r.Detail = err.Error()
		return r
	}
	r.Detail = formatSeverityDist(dist)
	switch {
	case urgent >= inboxUrgentWarnPerDay:
		r.Status = ProbeWarn
		r.Message = fmt.Sprintf("%d urgent notifications in last 24h", urgent)
		r.Hint = "review severity classifier tuning OR investigate the urgent events: hades inbox --severity urgent --since 24h"
	default:
		r.Status = ProbeOK
		r.Message = fmt.Sprintf("%d urgent / 24h (within budget)", urgent)
	}
	return r
}

func formatSeverityDist(dist map[string]int) string {
	parts := []string{}
	for _, t := range inboxSeverityTiersOrder {
		if count, ok := dist[t]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", t, count))
		}
	}
	extras := []string{}
	for k, v := range dist {
		if !inboxKnownSeverityTiers[k] {
			extras = append(extras, fmt.Sprintf("%s=%d", k, v))
		}
	}
	sort.Strings(extras)
	parts = append(parts, extras...)
	return strings.Join(parts, ", ")
}
