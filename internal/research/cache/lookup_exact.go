//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrCacheMiss = errors.New("research_cache: cache miss")

var ErrProjectIDRequired = errors.New("research_cache: project_id required (inv-zen-148)")

func LookupExact(ctx context.Context, db *DB, query, projectID, sessionID string) (*LookupResult, error) {
	if projectID == "" {
		return nil, ErrProjectIDRequired
	}
	queryHash := ComputeQueryHash(query)

	dispatch, err := selectMostRecentDispatchByHash(ctx, db.SQL, queryHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("research_cache: select dispatch by hash: %w", err)
	}

	findings, err := selectFindingsByDispatchID(ctx, db.SQL, dispatch.ID)
	if err != nil {
		return nil, fmt.Errorf("research_cache: select findings for dispatch %q: %w", dispatch.ID, err)
	}

	return &LookupResult{
		Hit:             true,
		HitReason:       CacheHitExact,
		FreshnessStatus: FreshnessUnknown,
		Dispatch:        &dispatch,
		Findings:        findings,
	}, nil
}

func selectMostRecentDispatchByHash(ctx context.Context, raw *sql.DB, hash string) (Dispatch, error) {
	row := raw.QueryRowContext(ctx,
		`SELECT id, query, status, created_at, updated_at
		   FROM research_dispatches
		  WHERE query_text_hash = ? AND status = ?
		    AND invalidated_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT 1`,
		hash, string(DispatchStatusDone),
	)

	var d Dispatch
	var status string
	if err := row.Scan(
		&d.ID, &d.Query, &status, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {

		return Dispatch{}, err
	}
	d.Status = DispatchStatus(status)
	return d, nil
}

func selectFindingsByDispatchID(ctx context.Context, raw *sql.DB, dispatchID string) ([]Finding, error) {
	rows, err := raw.QueryContext(ctx,
		`SELECT id, dispatch_id, url, title, snippet,
		        freshness_status, retrieved_at,
		        content_hash, body_inline_blob, body_path
		   FROM research_findings
		  WHERE dispatch_id = ?
		  ORDER BY id ASC`,
		dispatchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Finding
	for rows.Next() {
		var f Finding
		var freshness string
		var contentHash sql.NullString
		var bodyPath sql.NullString
		if err := rows.Scan(
			&f.ID, &f.DispatchID, &f.URL, &f.Title, &f.Snippet,
			&freshness, &f.RetrievedAt,
			&contentHash, &f.BodyInlineBlob, &bodyPath,
		); err != nil {
			return nil, err
		}
		f.Freshness = FreshnessStatus(freshness)
		if contentHash.Valid {
			f.ContentHash = contentHash.String
		}
		if bodyPath.Valid {
			f.BodyPath = bodyPath.String
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
