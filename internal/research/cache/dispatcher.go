//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

// Package cache — dispatcher.go
//
// Dispatcher is the cache-aware research MCP integration layer (release
// ). It composes the exact lookup (F-5), semantic lookup (F-6),
// HEAD revalidation (F-7), and fresh MCP dispatch into the canonical
// LookupOrDispatch flow defined in spec §3.5.
//
// # Flow
//
// 1. Validate projectID (inv-hades-148): empty → ErrProjectIDRequired.
// 2. Emit EventResearchDispatchInitiated.
// 3. If !SkipCache:
// a. LookupExact → on hit, revalidate (unless SkipRevalidate), emit
// cache_hit_exact + findings_returned, return.
// b. Embedder.Embed → LookupSemantic → on hit, revalidate, emit
// cache_hit_semantic + findings_returned, return.
// 4. MCP.Dispatch (fresh research).
// 5. Writeback: INSERT research_dispatches (DONE) + INSERT research_findings
// (StoreBody per finding) + INSERT research_query_vec (if embedding != nil).
// 6. Emit findings_returned, return LookupResult{HitReason: CacheHitFresh}.
//
// # MCPClient boundary (inv-hades-088 single-egress)
//
// All MCP calls go through the MCPClient interface; in production this is
// wired to internal/daemon/dispatcheradapter. The cache package
// NEVER makes direct HTTP or provider calls — those live in the provider tier.
//
// # Embedder boundary
//
// The Embedder interface produces 384-float32 embeddings. In production this
// is wired to the model shared at daemon startup; in tests an inline
// mock is used (see dispatcher_test.go). A nil Embedder causes semantic lookup
// to be skipped gracefully (embedding not available — fall straight to MCP).
//
// # Revalidation
//
// Cache hits are revalidated via Revalidator.Validate before being returned to
// the caller, unless SkipRevalidate=true (used in tests for determinism). When
// revalidation returns FreshnessExpired the finding is not updated here — gc
// handles removal; the dispatcher returns the stale result with the
// freshness status as-is so the caller sees the full picture.
//
// # inv-hades-031
//
// This package MUST NOT import internal/store. The MCPClient + Embedder
// interfaces and the Sink interface decouple the cache package from the daemon
// layer.
package cache

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const CacheHitFresh CacheHitReason = "fresh"

type MCPClient interface {
	Dispatch(ctx context.Context, query string) (FreshFindings, error)
}

type Embedder interface {
	Embed(ctx context.Context, query string) ([]float32, error)
}

type FreshFinding struct {
	SourceURL string

	SourceURLCanonical string

	Ext string

	Body []byte

	RetrievedAt time.Time
}

type FreshFindings struct {
	Query string

	Findings []FreshFinding
}

type Dispatcher struct {
	DB          *DB
	CAS         *CAS
	Revalidator *Revalidator
	Sink        Sink
	MCP         MCPClient
	Embedder    Embedder
}

type DispatchRequest struct {
	Query string

	ProjectID string

	SessionID string

	SkipCache bool

	// SkipRevalidate disables Revalidator.Validate on cache hits. Used in
	// tests for determinism (tests do not want HTTP revalidation) and when
	// the caller already knows the findings are fresh (e.g., just cached).
	//
	// In production, leave false: revalidation prevents serving stale content
	// that has changed at the source without a new dispatch.
	SkipRevalidate bool
}

func (d *Dispatcher) LookupOrDispatch(ctx context.Context, req DispatchRequest) (*LookupResult, error) {

	if req.ProjectID == "" {
		return nil, ErrProjectIDRequired
	}

	queryHash := ComputeQueryHash(req.Query)
	now := time.Now().UTC()

	_ = EmitDispatchInitiated(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, now)

	if !req.SkipCache {

		exactRes, err := LookupExact(ctx, d.DB, req.Query, req.ProjectID, req.SessionID)
		if err == nil && exactRes.Hit {

			freshness := FreshnessFresh
			if !req.SkipRevalidate {
				freshness = d.revalidateFindings(ctx, exactRes.Findings)
			}
			_ = EmitCacheHitExact(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, freshness, time.Now().UTC())
			_ = EmitFindingsReturned(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, len(exactRes.Findings), CacheHitExact, freshness, time.Now().UTC())
			exactRes.FreshnessStatus = freshness
			return exactRes, nil
		}

		if err != nil && err != ErrCacheMiss {
			return nil, fmt.Errorf("research_cache: dispatcher exact lookup: %w", err)
		}

		if d.Embedder != nil {
			embedding, embedErr := d.Embedder.Embed(ctx, req.Query)
			if embedErr != nil {

				log.Printf("research_cache: dispatcher embed query %q: %v (skipping semantic)", req.Query, embedErr)
			} else {
				semRes, semErr := LookupSemantic(ctx, d.DB, embedding, req.ProjectID, req.SessionID)
				if semErr == nil && semRes.Hit {
					freshness := FreshnessFresh
					if !req.SkipRevalidate {
						freshness = d.revalidateFindings(ctx, semRes.Findings)
					}
					_ = EmitCacheHitSemantic(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, freshness, time.Now().UTC())
					_ = EmitFindingsReturned(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, len(semRes.Findings), CacheHitSemantic, freshness, time.Now().UTC())
					semRes.FreshnessStatus = freshness
					return semRes, nil
				}
				if semErr != nil && semErr != ErrCacheMiss {
					return nil, fmt.Errorf("research_cache: dispatcher semantic lookup: %w", semErr)
				}
			}
		}
	}

	fresh, mcpErr := d.MCP.Dispatch(ctx, req.Query)
	if mcpErr != nil {
		return nil, fmt.Errorf("research_cache: dispatcher MCP.Dispatch: %w", mcpErr)
	}

	result, wbErr := d.writeBack(ctx, req, queryHash, fresh)
	if wbErr != nil {
		return nil, fmt.Errorf("research_cache: dispatcher writeback: %w", wbErr)
	}

	_ = EmitFindingsReturned(ctx, d.Sink, 0, req.ProjectID, req.SessionID, queryHash, len(result.Findings), CacheHitFresh, FreshnessFresh, time.Now().UTC())

	return result, nil
}

func (d *Dispatcher) writeBack(ctx context.Context, req DispatchRequest, queryHash string, fresh FreshFindings) (*LookupResult, error) {
	now := time.Now().UTC()
	nowUnix := now.Unix()

	dispatchTextID := "fresh-" + queryHash[:16] + "-" + itoa(int(nowUnix))

	res, err := d.DB.SQL.ExecContext(ctx,
		`INSERT INTO research_dispatches
		 (id, query, query_text_hash, status,
		  project_id, session_id, cache_hit_reason,
		  created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dispatchTextID,
		CanonicalizeQuery(req.Query),
		queryHash,
		string(DispatchStatusDone),
		req.ProjectID,
		req.SessionID,
		string(CacheHitMiss),
		nowUnix,
		nowUnix,
	)
	if err != nil {
		return nil, fmt.Errorf("insert research_dispatches: %w", err)
	}

	rowid, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("LastInsertId research_dispatches: %w", err)
	}

	findings := make([]Finding, 0, len(fresh.Findings))
	for i, ff := range fresh.Findings {
		retrievedAt := ff.RetrievedAt
		if retrievedAt.IsZero() {
			retrievedAt = now
		}
		f := Finding{
			ID:                 dispatchTextID + "-f-" + itoa(i),
			DispatchID:         dispatchTextID,
			URL:                ff.SourceURL,
			Title:              "", // MCP findings do not carry a separate title in F-10
			Snippet:            "",
			Freshness:          FreshnessFresh,
			RetrievedAt:        retrievedAt.Unix(),
			RetrievalTimestamp: retrievedAt,
			SourceURLCanonical: ff.SourceURLCanonical,
		}

		ext := ff.Ext
		if ext == "" {
			ext = "bin"
		}
		if storeErr := StoreBody(&f, ff.Body, d.CAS, ext); storeErr != nil {
			return nil, fmt.Errorf("StoreBody finding %d: %w", i, storeErr)
		}

		_, err := d.DB.SQL.ExecContext(ctx,
			`INSERT INTO research_findings
			 (id, dispatch_id, url, title, snippet, freshness_status, retrieved_at,
			  content_hash, body_inline_blob, body_path)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ID, f.DispatchID, f.URL, f.Title, f.Snippet,
			string(f.Freshness), f.RetrievedAt,
			nullableString(f.ContentHash),
			nullableBlob(f.BodyInlineBlob),
			nullableString(f.BodyPath),
		)
		if err != nil {
			return nil, fmt.Errorf("insert research_findings[%d]: %w", i, err)
		}

		findings = append(findings, f)
	}

	if d.Embedder != nil {
		embedding, embedErr := d.Embedder.Embed(ctx, req.Query)
		if embedErr != nil {

			log.Printf("research_cache: writeBack embed query %q: %v (vec entry skipped)", req.Query, embedErr)
		} else if len(embedding) == EmbeddingDim {
			embBytes := embeddingToBytes(embedding)
			_, vecErr := d.DB.SQL.ExecContext(ctx,
				`INSERT INTO research_query_vec(rowid, embedding) VALUES (?, ?)`,
				rowid, embBytes,
			)
			if vecErr != nil {

				log.Printf("research_cache: writeBack insert research_query_vec rowid=%d: %v (semantic lookup degraded)", rowid, vecErr)
			}
		}
	}

	dispatch := &Dispatch{
		ID:        dispatchTextID,
		Query:     CanonicalizeQuery(req.Query),
		Status:    DispatchStatusDone,
		CreatedAt: nowUnix,
		UpdatedAt: nowUnix,
	}

	return &LookupResult{
		Hit:             true,
		HitReason:       CacheHitFresh,
		FreshnessStatus: FreshnessFresh,
		Dispatch:        dispatch,
		Findings:        findings,
	}, nil
}

func (d *Dispatcher) revalidateFindings(ctx context.Context, findings []Finding) FreshnessStatus {
	if len(findings) == 0 {
		return FreshnessFresh
	}

	aggregate := FreshnessFresh
	for _, f := range findings {
		if f.URL == "" {

			aggregate = worseFreshness(aggregate, FreshnessStale)
			continue
		}
		result, err := d.Revalidator.Validate(ctx, f)
		if err != nil {
			log.Printf("research_cache: revalidate finding %q URL %q: %v (treating as stale)", f.ID, f.URL, err)
			aggregate = worseFreshness(aggregate, FreshnessStale)
			continue
		}
		aggregate = worseFreshness(aggregate, result.Status)
	}
	return aggregate
}

func worseFreshness(a, b FreshnessStatus) FreshnessStatus {
	rank := func(s FreshnessStatus) int {
		switch s {
		case FreshnessExpired:
			return 3
		case FreshnessStale:
			return 2
		default:
			return 1
		}
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return sql.NullString{String: s, Valid: true}
}

func nullableBlob(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
