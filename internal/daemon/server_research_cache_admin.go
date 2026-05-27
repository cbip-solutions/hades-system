// SPDX-License-Identifier: MIT
// Package daemon — server_research_cache_admin.go.
//
// Operator-admin methods on *Server backing handlers.ResearchCacheAdminCtx.
// These read/aggregate/delete on the research_cache table directly via
// the embedded *store.Store; the research MCP path
// (ResearchCacheGet/Set in server_phase_g_defaults.go) is unchanged.
package daemon

import (
	"errors"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

func (s *Server) ResearchCacheList(limit, offset int) ([]handlers.ResearchCacheEntry, error) {
	if s.store == nil {
		return []handlers.ResearchCacheEntry{}, nil
	}
	rows, err := s.store.DB().Query(
		`SELECT hash, length(response_json), created_at, ttl_unix
		   FROM research_cache
		  ORDER BY created_at DESC
		  LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]handlers.ResearchCacheEntry, 0, limit)
	for rows.Next() {
		var e handlers.ResearchCacheEntry
		if err := rows.Scan(&e.Hash, &e.BytesSize, &e.CreatedAt, &e.TTLUnix); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) ResearchCacheClear(cutoffUnix int64) (int64, error) {
	if s.store == nil {
		return 0, nil
	}
	res, err := s.store.DB().Exec(
		`DELETE FROM research_cache WHERE created_at < ?`,
		cutoffUnix,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Server) ResearchCacheStats() (handlers.ResearchCacheStats, error) {
	if s.store == nil {
		return handlers.ResearchCacheStats{}, nil
	}
	var stats handlers.ResearchCacheStats
	row := s.store.DB().QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(length(response_json)), 0),
		        COALESCE(MIN(created_at), 0),
		        COALESCE(MAX(created_at), 0)
		   FROM research_cache`,
	)
	if err := row.Scan(&stats.TotalEntries, &stats.TotalBytes, &stats.OldestUnix, &stats.NewestUnix); err != nil {
		return stats, err
	}
	now := time.Now().Unix()
	exp := s.store.DB().QueryRow(
		`SELECT COUNT(*) FROM research_cache WHERE ttl_unix < ?`, now,
	)
	if err := exp.Scan(&stats.ExpiredCount); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *Server) ResearchCacheShow(hash string) (handlers.ResearchCacheShow, bool, error) {
	if s.store == nil {
		return handlers.ResearchCacheShow{}, false, nil
	}
	var show handlers.ResearchCacheShow
	row := s.store.DB().QueryRow(
		`SELECT hash, response_json, length(response_json), created_at, ttl_unix
		   FROM research_cache WHERE hash = ?`,
		hash,
	)
	err := row.Scan(&show.Hash, &show.ResponseJSON, &show.BytesSize, &show.CreatedAt, &show.TTLUnix)
	if errors.Is(err, errSQLNoRows) || isNoRowsErr(err) {
		return handlers.ResearchCacheShow{}, false, nil
	}
	if err != nil {
		return handlers.ResearchCacheShow{}, false, err
	}
	if show.TTLUnix < time.Now().Unix() {
		show.Expired = true
	}
	return show, true, nil
}

var errSQLNoRows = errors.New("sql: no rows in result set")

func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "sql: no rows in result set"
}
