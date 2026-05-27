// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/research/cache"
)

type ResearchAdapterDeps struct {
	DB  *cache.DB
	Now func() int64
}

type ResearchAdapter struct {
	db  *cache.DB
	now func() int64
}

var _ handlers.ResearchStoreP9 = (*ResearchAdapter)(nil)

func NewResearchAdapter(deps ResearchAdapterDeps) (*ResearchAdapter, error) {
	if deps.DB == nil {
		return nil, errors.New("plan9adapter: research DB is required")
	}
	now := deps.Now
	if now == nil {
		now = func() int64 { return time.Now().UTC().Unix() }
	}
	return &ResearchAdapter{db: deps.DB, now: now}, nil
}

func (a *ResearchAdapter) History(ctx context.Context, filter handlers.ResearchHistoryFilterP9) ([]handlers.ResearchHistoryEntryP9, error) {
	limit := normalizeResearchLimit(filter.Limit)
	where := []string{"1=1"}
	args := []any{}
	if filter.ProjectID != "" {
		where = append(where, "d.project_id = ?")
		args = append(args, filter.ProjectID)
	}
	if filter.Since > 0 {
		where = append(where, "COALESCE(d.dispatched_at, d.created_at) >= ?")
		args = append(args, filter.Since)
	}
	switch filter.Filter {
	case "", "all":
	case "cache_hit":
		where = append(where, "d.cache_hit_reason IN (?, ?)")
		args = append(args, string(cache.CacheHitExact), string(cache.CacheHitSemantic))
	case "cache_miss":
		where = append(where, "(d.cache_hit_reason IS NULL OR d.cache_hit_reason IN (?, ?))")
		args = append(args, string(cache.CacheHitMiss), string(cache.CacheHitExpired))
	default:
		return nil, fmt.Errorf("plan9adapter: unsupported research history filter %q", filter.Filter)
	}
	args = append(args, limit)
	rows, err := a.db.SQL.QueryContext(ctx,
		`SELECT d.query,
		        COALESCE(d.dispatched_at, d.created_at),
		        COUNT(f.id),
		        COALESCE(d.cache_hit_reason, '')
		   FROM research_dispatches d
		   LEFT JOIN research_findings f ON f.dispatch_id = d.id
		  WHERE `+strings.Join(where, " AND ")+`
		  GROUP BY d.id
		  ORDER BY COALESCE(d.dispatched_at, d.created_at) DESC
		  LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]handlers.ResearchHistoryEntryP9, 0, limit)
	for rows.Next() {
		var row handlers.ResearchHistoryEntryP9
		var reason string
		if err := rows.Scan(&row.Query, &row.DispatchedAt, &row.FindingsCount, &reason); err != nil {
			return nil, err
		}
		row.Source = researchHistorySource(reason)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (a *ResearchAdapter) CacheStats(ctx context.Context, projectID string) (handlers.ResearchCacheStatsP9, error) {
	where := []string{"d.invalidated_at IS NULL"}
	args := []any{}
	if projectID != "" {
		where = append(where, "d.project_id = ?")
		args = append(args, projectID)
	}
	rows, err := a.db.SQL.QueryContext(ctx,
		`SELECT f.body_inline_blob, f.body_path, f.retrieved_at,
		        f.freshness_status, f.last_validated_at
		   FROM research_findings f
		   JOIN research_dispatches d ON d.id = f.dispatch_id
		  WHERE `+strings.Join(where, " AND "),
		args...,
	)
	if err != nil {
		return handlers.ResearchCacheStatsP9{}, err
	}
	defer rows.Close()

	stats := handlers.ResearchCacheStatsP9{}
	now := a.now()
	var oldest, newest int64
	for rows.Next() {
		var inline []byte
		var bodyPath sql.NullString
		var retrieved int64
		var freshness string
		var lastValidated sql.NullInt64
		if err := rows.Scan(&inline, &bodyPath, &retrieved, &freshness, &lastValidated); err != nil {
			return stats, err
		}
		stats.TotalEntries++
		stats.TotalBytes += int64(len(inline))
		if bodyPath.Valid && bodyPath.String != "" {
			if info, err := os.Stat(bodyPath.String); err == nil {
				stats.TotalBytes += info.Size()
			}
		}
		if oldest == 0 || retrieved < oldest {
			oldest = retrieved
		}
		if retrieved > newest {
			newest = retrieved
		}
		if freshness == string(cache.FreshnessExpired) {
			stats.ExpiredCount++
		}
		if freshness == string(cache.FreshnessExpired) || freshness == string(cache.FreshnessStale) {
			stats.RevalidationQueueDepth++
		}
		anchor := retrieved
		if lastValidated.Valid {
			anchor = lastValidated.Int64
		}
		if lag := int(now - anchor); lag > stats.FreshnessLagSeconds {
			stats.FreshnessLagSeconds = lag
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}
	stats.OldestUnix = oldest
	stats.NewestUnix = newest

	stuck, err := a.stuckQueries(ctx, projectID, now-3600)
	if err != nil {
		return stats, err
	}
	stats.StuckQueriesCount = stuck
	return stats, nil
}

func (a *ResearchAdapter) CacheInvalidate(ctx context.Context, query string) (int, error) {
	return cache.InvalidateByQuery(ctx, a.db, query, "operator-requested-cache-invalidate", a.now())
}

func (a *ResearchAdapter) CacheList(ctx context.Context, projectID, sourcePrefix string) ([]handlers.ResearchCacheEntryP9, error) {
	where := []string{"d.invalidated_at IS NULL"}
	args := []any{}
	if projectID != "" {
		where = append(where, "d.project_id = ?")
		args = append(args, projectID)
	}
	if sourcePrefix != "" {
		where = append(where, "COALESCE(f.source_url_canonical, f.url) LIKE ?")
		args = append(args, sourcePrefix+"%")
	}
	rows, err := a.db.SQL.QueryContext(ctx,
		`SELECT f.id,
		        f.body_inline_blob,
		        f.body_path,
		        f.retrieved_at,
		        COALESCE(f.source_url_canonical, f.url),
		        COALESCE(f.content_hash, '')
		   FROM research_findings f
		   JOIN research_dispatches d ON d.id = f.dispatch_id
		  WHERE `+strings.Join(where, " AND ")+`
		  ORDER BY f.retrieved_at DESC
		  LIMIT 500`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]handlers.ResearchCacheEntryP9, 0)
	for rows.Next() {
		var e handlers.ResearchCacheEntryP9
		var inline []byte
		var bodyPath sql.NullString
		if err := rows.Scan(&e.Hash, &inline, &bodyPath, &e.CreatedAt, &e.SourceURL, &e.ContentHash); err != nil {
			return nil, err
		}
		e.BytesSize = int64(len(inline))
		if bodyPath.Valid && bodyPath.String != "" {
			if info, err := os.Stat(bodyPath.String); err == nil {
				e.BytesSize += info.Size()
			}
		}
		e.TTLUnix = ttlUnix(e.SourceURL, e.CreatedAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (a *ResearchAdapter) stuckQueries(ctx context.Context, projectID string, cutoff int64) (int, error) {
	where := []string{"status IN (?, ?)", "updated_at < ?"}
	args := []any{string(cache.DispatchStatusPending), string(cache.DispatchStatusRunning), cutoff}
	if projectID != "" {
		where = append(where, "project_id = ?")
		args = append(args, projectID)
	}
	row := a.db.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM research_dispatches WHERE `+strings.Join(where, " AND "),
		args...,
	)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func normalizeResearchLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func researchHistorySource(reason string) string {
	switch cache.CacheHitReason(reason) {
	case cache.CacheHitExact:
		return "cache_hit_exact"
	case cache.CacheHitSemantic:
		return "cache_hit_semantic"
	default:
		return "fresh_dispatch"
	}
}

func ttlUnix(source string, createdAt int64) int64 {
	ttl := cache.LookupTTL(source)
	if ttl == cache.TTLPermanent {
		return 0
	}
	return createdAt + int64(ttl.Seconds())
}
