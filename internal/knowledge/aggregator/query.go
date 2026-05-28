//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package aggregator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

func (a *Aggregator) Query(ctx context.Context, req QueryRequest) ([]QueryResult, error) {

	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("aggregator: Query: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	queryEmbedding, err := a.embedder.Embed(ctx, req.Text)
	if err != nil {

		queryEmbedding = nil
	}

	var topKs []TopK
	switch req.Scope {
	case ScopePinnedOnly:
		tk, err := queryPinIndex(ctx, a, &req, queryEmbedding)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Query pinned-only: %w", err)
		}
		topKs = tk

	case ScopeProject:
		vault, err := a.store.OpenProjectVault(ctx, req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Query open vault %q: %w", req.ProjectID, err)
		}
		db, ok := vault.(*sql.DB)
		if !ok || db == nil {
			return nil, fmt.Errorf("aggregator: Query: vault for project %q is not a *sql.DB", req.ProjectID)
		}
		tk, err := queryDB(ctx, db, &req, queryEmbedding, req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Query project %q: %w", req.ProjectID, err)
		}
		topKs = tk

	case ScopeGlobal:
		tk, err := queryGlobal(ctx, a, &req, queryEmbedding)
		if err != nil {
			return nil, fmt.Errorf("aggregator: Query global: %w", err)
		}
		topKs = tk

	default:
		return nil, fmt.Errorf("aggregator: Query: unknown scope %q (invariant)", req.Scope)
	}

	fused := Fuse(topKs, rrfDefaultK, req.Limit)

	if req.AuditChainFilter {
		filtered := fused[:0]
		for _, r := range fused {
			if r.AuditChainAnchor != "" {
				filtered = append(filtered, r)
			}
		}
		fused = filtered
	}

	return fused, nil
}

func queryPinIndex(ctx context.Context, a *Aggregator, req *QueryRequest, queryEmbedding []float32) ([]TopK, error) {

	return queryDB(ctx, a.db, req, queryEmbedding, "")
}

func queryDB(ctx context.Context, db *sql.DB, req *QueryRequest, queryEmbedding []float32, projectIDLabel string) ([]TopK, error) {
	if db == nil {
		return nil, nil
	}

	var topKs []TopK

	ftsResults, err := queryFTS(ctx, db, req.Text, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("queryDB FTS: %w", err)
	}
	if len(ftsResults) > 0 {
		for i := range ftsResults {
			ftsResults[i].Source = pickSource(req.Scope, "fts")
		}
		topKs = append(topKs, TopK{Source: pickSource(req.Scope, "fts"), Results: ftsResults})
	}

	if queryEmbedding != nil {
		vecResults, err := queryVec(ctx, db, queryEmbedding, req.Limit, 0.92)
		if err != nil && !errors.Is(err, ErrVecUnavailable) {
			return nil, fmt.Errorf("queryDB vec: %w", err)
		}
		if len(vecResults) > 0 {
			for i := range vecResults {
				vecResults[i].Source = pickSource(req.Scope, "vec")
			}
			topKs = append(topKs, TopK{Source: pickSource(req.Scope, "vec"), Results: vecResults})
		}
	}

	seeds := make([]string, 0, 3)
	for i, r := range ftsResults {
		if i >= 3 {
			break
		}
		seeds = append(seeds, r.NoteID)
	}
	if len(seeds) > 0 {
		graphResults, err := queryGraph(ctx, db, seeds, req.WikilinkDepth, req.Limit)
		if err != nil {
			return nil, fmt.Errorf("queryDB graph: %w", err)
		}
		if len(graphResults) > 0 {
			for i := range graphResults {
				graphResults[i].Source = pickSource(req.Scope, "graph")
			}
			topKs = append(topKs, TopK{Source: pickSource(req.Scope, "graph"), Results: graphResults})
		}
	}

	_ = projectIDLabel
	return topKs, nil
}

func queryGlobal(ctx context.Context, a *Aggregator, req *QueryRequest, queryEmbedding []float32) ([]TopK, error) {
	var allTopKs []TopK
	var mu sync.Mutex

	pinReq := *req
	pinReq.Scope = ScopePinnedOnly
	pinTopKs, err := queryPinIndex(ctx, a, &pinReq, queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("queryGlobal pin index: %w", err)
	}
	allTopKs = append(allTopKs, pinTopKs...)

	projects, err := a.store.ListAuthorizedProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("queryGlobal ListAuthorizedProjects: %w", err)
	}

	var wg sync.WaitGroup
	for _, ph := range projects {
		ph := ph
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				return
			}
			vault, err := a.store.OpenProjectVault(ctx, ph.ProjectID)
			if err != nil || vault == nil {

				return
			}
			db, ok := vault.(*sql.DB)
			if !ok || db == nil {
				return
			}
			tk, err := queryDB(ctx, db, req, queryEmbedding, ph.ProjectID)
			if err != nil {

				return
			}
			mu.Lock()
			allTopKs = append(allTopKs, tk...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	return allTopKs, nil
}

// pickSource returns the canonical Source label for a query result.
//
// Contract
// - Scope==ScopePinnedOnly → always "pin" (triggers RRF pinBoost).
// - Any other scope → base (the sub-query's natural source: "fts", "vec", "graph").
//
// Rationale queryPinIndex operates on the aggregator's own promoted pins;
// operator-reviewed pins deserve the 1.5× boost in cross-scope comparisons.
// Per-project results from queryGlobal do NOT get the boost — they are raw
// retrieval matches, not operator-reviewed promotions.
func pickSource(scope Scope, base string) string {
	if scope == ScopePinnedOnly {
		return "pin"
	}
	return base
}
