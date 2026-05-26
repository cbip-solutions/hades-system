// SPDX-License-Identifier: MIT
// Package inbox — prober.go
//
// Phase J Task J-5 adapter: exposes a slim Prober implementation that
// the cli/doctor_inbox.go layer consumes (cli.InboxProber). The split
// keeps inv-zen-031 clean (internal/cli imports internal/inbox; this
// package does NOT import internal/store).
//
// The Prober is read-only: it queries the daemon-level
// inbox_aggregator_cache table directly via *sql.DB plus a closure
// that opens per-project state.db handles for the cache-consistency
// reconciliation. Per spec §3.3 the aggregator cache is denormalized
// from per-project authoritative `inbox` tables — the consistency
// probe sums per-project COUNT(*) and compares to cache COUNT(*) per
// project_alias.
//
// inv-zen-113 anchor: drift > tolerance signals write-fanout failure
// (outbox replay missed; per spec §3.3 the aggregator is rebuildable
// from per-project sources via Aggregator.Rebuild).
//
// inv-zen-124 anchor: severity column is enforced at SQL CHECK level on
// both per-project inbox + daemon-level cache; the prober reads the
// distribution but does not validate the enum (the schema is the
// authoritative validator).
package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// PerProjectDBOpenerFn returns a borrowed read-only *sql.DB handle for
// the per-project state.db file at projects/<sha256>/state.db. The
// daemon owns the handle and any caching/lifecycle; the Prober keeps
// only the borrowed reference for the duration of a single probe call.
//
// MUST be safe for concurrent use.
type PerProjectDBOpenerFn func(ctx context.Context, alias string) (*sql.DB, error)

// OutboxPendingFn returns the current outbox queue depth — number of
// CacheWrite events enqueued but not yet drained. Wired by the daemon
// over Phase E-8's *Outbox.Pending() accessor.
//
// MUST be safe for concurrent use.
type OutboxPendingFn func(ctx context.Context) (int, error)

var ErrProberNilArg = errors.New("inbox.NewProber: nil argument")

type Prober struct {
	daemonDB *sql.DB

	perProjectDBOpener PerProjectDBOpenerFn

	outboxPending OutboxPendingFn
}

// NewProber wires a Prober. Caller MUST NOT call Close on the Prober.
//
// Panics on any nil arg: programmer error at boot. The daemon main
// loop is the canonical caller; partial-bootstrap state is a
// loud-failure condition.
func NewProber(daemonDB *sql.DB, perProjectOpener PerProjectDBOpenerFn, outboxPending OutboxPendingFn) *Prober {
	if daemonDB == nil {
		panic(fmt.Errorf("%w: daemonDB", ErrProberNilArg))
	}
	if perProjectOpener == nil {
		panic(fmt.Errorf("%w: perProjectOpener", ErrProberNilArg))
	}
	if outboxPending == nil {
		panic(fmt.Errorf("%w: outboxPending", ErrProberNilArg))
	}
	return &Prober{
		daemonDB:           daemonDB,
		perProjectDBOpener: perProjectOpener,
		outboxPending:      outboxPending,
	}
}

func (p *Prober) AggregatorCacheConsistent(ctx context.Context) (bool, int, string, error) {

	cacheRows, err := p.daemonDB.QueryContext(ctx,
		`SELECT project_alias, COUNT(*) FROM inbox_aggregator_cache GROUP BY project_alias`)
	if err != nil {
		return false, 0, "", fmt.Errorf("inbox.Prober.AggregatorCacheConsistent cache query: %w", err)
	}
	cacheCounts := map[string]int{}
	for cacheRows.Next() {
		var alias string
		var count int
		if err := cacheRows.Scan(&alias, &count); err != nil {
			cacheRows.Close()
			return false, 0, "", fmt.Errorf("inbox.Prober.AggregatorCacheConsistent cache scan: %w", err)
		}
		cacheCounts[alias] = count
	}
	cacheRows.Close()
	if err := cacheRows.Err(); err != nil {
		return false, 0, "", fmt.Errorf("inbox.Prober.AggregatorCacheConsistent cache rows: %w", err)
	}

	type result struct {
		alias        string
		perCount     int
		cacheCount   int
		unreachable  bool
		unreachReson string
	}
	results := make([]result, 0, len(cacheCounts))
	for alias, cacheCount := range cacheCounts {
		count, err := p.perProjectInboxCount(ctx, alias)
		r := result{alias: alias, cacheCount: cacheCount, perCount: count}
		if err != nil {
			r.unreachable = true
			r.unreachReson = err.Error()
		}
		results = append(results, r)
	}

	totalDrift := 0
	type mismatch struct {
		alias string
		drift int
		line  string
	}
	mismatches := make([]mismatch, 0, len(results))
	for _, r := range results {
		if r.unreachable {
			totalDrift += r.cacheCount
			mismatches = append(mismatches, mismatch{
				alias: r.alias,
				drift: r.cacheCount,
				line:  fmt.Sprintf("%s: per-project=unreachable cache=%d", r.alias, r.cacheCount),
			})
			continue
		}
		diff := abs(r.perCount - r.cacheCount)
		totalDrift += diff
		if diff != 0 {
			mismatches = append(mismatches, mismatch{
				alias: r.alias,
				drift: diff,
				line:  fmt.Sprintf("%s: per-project=%d cache=%d", r.alias, r.perCount, r.cacheCount),
			})
		}
	}
	sort.Slice(mismatches, func(i, j int) bool {
		if mismatches[i].drift != mismatches[j].drift {
			return mismatches[i].drift > mismatches[j].drift
		}
		return mismatches[i].alias < mismatches[j].alias
	})
	consistent := totalDrift == 0
	detail := ""
	if !consistent {
		lines := make([]string, 0, len(mismatches))
		for _, m := range mismatches {
			lines = append(lines, m.line)
		}
		detail = joinLines(lines)
	}
	return consistent, totalDrift, detail, nil
}

func (p *Prober) perProjectInboxCount(ctx context.Context, alias string) (int, error) {
	pdb, err := p.perProjectDBOpener(ctx, alias)
	if err != nil {
		return 0, err
	}
	if pdb == nil {
		return 0, errors.New("perProjectDBOpener returned nil DB")
	}
	var n int
	err = pdb.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (p *Prober) OutboxQueueDepth(ctx context.Context) (int, error) {
	n, err := p.outboxPending(ctx)
	if err != nil {
		return 0, fmt.Errorf("inbox.Prober.OutboxQueueDepth: %w", err)
	}
	return n, nil
}

func (p *Prober) DedupConstraintViolations(ctx context.Context) (int, error) {
	var n int
	err := p.daemonDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM (
			SELECT event_type, content_hash,
				   (created_at / 300) AS bucket
			FROM inbox_aggregator_cache
			GROUP BY event_type, content_hash, bucket
			HAVING COUNT(*) > 1
		)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("inbox.Prober.DedupConstraintViolations: %w", err)
	}
	return n, nil
}

func (p *Prober) SeverityDistribution24h(ctx context.Context) (map[string]int, int, error) {
	since := time.Now().Add(-24 * time.Hour).Unix()
	rows, err := p.daemonDB.QueryContext(ctx,
		`SELECT severity, COUNT(*) FROM inbox_aggregator_cache
		 WHERE created_at >= ? GROUP BY severity`, since)
	if err != nil {
		return nil, 0, fmt.Errorf("inbox.Prober.SeverityDistribution24h: %w", err)
	}
	defer rows.Close()
	dist := map[string]int{}
	urgent := 0
	for rows.Next() {
		var tier string
		var count int
		if err := rows.Scan(&tier, &count); err != nil {
			return nil, 0, fmt.Errorf("inbox.Prober.SeverityDistribution24h scan: %w", err)
		}
		dist[tier] = count
		if tier == "urgent" {
			urgent = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("inbox.Prober.SeverityDistribution24h rows: %w", err)
	}
	return dist, urgent, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func joinLines(s []string) string {
	out := ""
	for i, line := range s {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
