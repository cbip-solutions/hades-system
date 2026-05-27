//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package aggregator

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

var edgeTypeWeight = map[string]float64{
	"wikilink": 1.0,
	"backlink": 0.8,
	"relates":  0.5,
}

func hopWeight(hop int) float64 {
	return 1.0 / float64(hop)
}

type frontierEdge struct {
	sourceSeed string
	target     string
	linkType   string
}

type visitEntry struct {
	hop      int
	edgeType string

	fromSeeds map[string]struct{}
}

func (a *Aggregator) QueryGraph(ctx context.Context, seedNoteIDs []string, depth, limit int) ([]QueryResult, error) {
	if len(seedNoteIDs) == 0 {
		return nil, nil
	}
	if depth <= 0 {
		depth = defaultWikilinkDepth
	}
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	return queryGraph(ctx, a.db, seedNoteIDs, depth, limit)
}

func queryGraph(
	ctx context.Context,
	db *sql.DB,
	seedNoteIDs []string,
	depth, limit int,
) ([]QueryResult, error) {

	seedSet := make(map[string]struct{}, len(seedNoteIDs))
	for _, id := range seedNoteIDs {
		seedSet[id] = struct{}{}
	}

	maxNodes := limit * 5

	visited := make(map[string]*visitEntry, len(seedNoteIDs)+16)
	for _, id := range seedNoteIDs {

		visited[id] = &visitEntry{
			hop:       0,
			edgeType:  "",
			fromSeeds: map[string]struct{}{id: {}},
		}
	}

	type pendingExpand struct {
		nodeID    string
		fromSeeds map[string]struct{}
	}

	frontier := make([]pendingExpand, 0, len(seedNoteIDs))
	for _, id := range seedNoteIDs {
		frontier = append(frontier, pendingExpand{
			nodeID:    id,
			fromSeeds: visited[id].fromSeeds,
		})
	}

	for hop := 1; hop <= depth && len(frontier) > 0; hop++ {

		frontierEdgeSources := make([]frontierEdge, 0, len(frontier))
		for _, f := range frontier {

			frontierEdgeSources = append(frontierEdgeSources, frontierEdge{sourceSeed: f.nodeID})
		}

		nodeFromSeeds := make(map[string]map[string]struct{}, len(frontier))
		for _, f := range frontier {
			nodeFromSeeds[f.nodeID] = f.fromSeeds
		}

		sourceNodes := make([]string, len(frontier))
		for i, f := range frontier {
			sourceNodes[i] = f.nodeID
		}
		_ = frontierEdgeSources

		edges, err := expandFrontier(ctx, db, sourceNodes)
		if err != nil {
			return nil, fmt.Errorf("aggregator: queryGraph hop=%d expandFrontier: %w", hop, err)
		}

		nextFrontier := make([]pendingExpand, 0, len(edges))

		for _, edge := range edges {
			if len(visited) >= maxNodes {
				break
			}

			parentFromSeeds := nodeFromSeeds[edge.sourceSeed]

			if existing, seen := visited[edge.target]; seen {
				// Already visited: merge the parent's fromSeeds into the existing
				// entry to boost in-degree counting. Do NOT re-enqueue.
				for seedID := range parentFromSeeds {
					existing.fromSeeds[seedID] = struct{}{}
				}
				continue
			}

			childFromSeeds := make(map[string]struct{}, len(parentFromSeeds))
			for seedID := range parentFromSeeds {
				childFromSeeds[seedID] = struct{}{}
			}

			visited[edge.target] = &visitEntry{
				hop:       hop,
				edgeType:  edge.linkType,
				fromSeeds: childFromSeeds,
			}
			nextFrontier = append(nextFrontier, pendingExpand{
				nodeID:    edge.target,
				fromSeeds: childFromSeeds,
			})
		}

		frontier = nextFrontier
	}

	candidateIDs := make([]string, 0, len(visited))
	for id := range visited {
		if _, isSeed := seedSet[id]; isSeed {
			continue
		}
		candidateIDs = append(candidateIDs, id)
	}

	if len(candidateIDs) == 0 {
		return nil, nil
	}

	meta, err := fetchPinMetadata(ctx, db, candidateIDs)
	if err != nil {
		return nil, fmt.Errorf("aggregator: queryGraph fetchPinMetadata: %w", err)
	}

	results := make([]QueryResult, 0, len(meta))
	for id, qr := range meta {
		entry := visited[id]
		if entry == nil {

			continue
		}

		ew, ok := edgeTypeWeight[entry.edgeType]
		if !ok {
			ew = 0.3
		}

		hw := hopWeight(entry.hop)
		inDegree := len(entry.fromSeeds)
		logFactor := math.Log1p(float64(inDegree))

		score := hw * ew * logFactor

		if score == 0 {
			score = hw * ew * 1e-3

			if score == 0 {
				score = hw * 0.3 * 1e-3
			}
		}

		qr.Score = score
		qr.Source = "graph"
		results = append(results, qr)
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func expandFrontier(ctx context.Context, db *sql.DB, sourceNodes []string) ([]frontierEdge, error) {
	if len(sourceNodes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(sourceNodes))
	args := make([]interface{}, len(sourceNodes))
	for i, id := range sourceNodes {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT source_note_id, target_note_id, link_type
		 FROM knowledge_pin_wikilinks
		 WHERE source_note_id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("expandFrontier: %w", err)
	}
	defer rows.Close()

	var edges []frontierEdge
	for rows.Next() {
		var e frontierEdge
		if err := rows.Scan(&e.sourceSeed, &e.target, &e.linkType); err != nil {
			return nil, fmt.Errorf("expandFrontier scan: %w", err)
		}
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expandFrontier rows: %w", err)
	}
	return edges, nil
}

func fetchPinMetadata(ctx context.Context, db *sql.DB, noteIDs []string) (map[string]QueryResult, error) {
	if len(noteIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(noteIDs))
	args := make([]interface{}, len(noteIDs))
	for i, id := range noteIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT note_id, title, substr(content, 1, 200), project_id, audit_chain_anchor
		 FROM knowledge_pin_index
		 WHERE note_id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetchPinMetadata: %w", err)
	}
	defer rows.Close()

	result := make(map[string]QueryResult, len(noteIDs))
	for rows.Next() {
		var qr QueryResult
		if err := rows.Scan(
			&qr.NoteID,
			&qr.Title,
			&qr.Snippet,
			&qr.ProjectID,
			&qr.AuditChainAnchor,
		); err != nil {
			return nil, fmt.Errorf("fetchPinMetadata scan: %w", err)
		}
		result[qr.NoteID] = qr
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetchPinMetadata rows: %w", err)
	}
	return result, nil
}
