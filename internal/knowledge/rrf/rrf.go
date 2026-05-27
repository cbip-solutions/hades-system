// SPDX-License-Identifier: MIT
// Package rrf implements Reciprocal Rank Fusion (RRF) per Cormack et al. 2009.
//
// Extracted from internal/knowledge/aggregator/rrf.go (release D, commit
// 8d8f8b03 originally) to a CGO-free sub-package so consumers that cannot
// link the aggregator's CGO-bound sqlite3 driver (compliance tests using
// ncruces/go-sqlite3 + internal/augment's compliance fixtures) can reuse
// the canonical Fuse implementation without inlining a copy.
//
// Pre-fix state: release C-5 (aggregator_consumer.go) inlined 135
// LOC of RRF to avoid sqlite-driver "Register called twice" panic when
// the augment package was imported alongside the compliance test driver.
// This package replaces the inline copy with a shared, CGO-free
// implementation that the aggregator package re-exports for backward
// compatibility.
//
// # Boundary
//
// - No CGO imports (compile-checked via the no-cgo test build tag).
// - No imports of internal/store (inv-hades-031).
// - No web calls (inv-hades-129).
//
// Algorithm (per Cormack et al. 2009 "Reciprocal Rank Fusion outperforms
// Condorcet and individual Rank Learning Methods"):
//
// score(d) = Σ_s ( boost_s × 1/(k + rank_s(d)) )
//
// where rank_s(d) is the 1-based rank of document d in source s's result
// list, boost_s is 1.5 (PinBoost) when source == "pin" else 1.0, and k
// defaults to 60 (DefaultK) when k ≤ 0.
//
// Type contract:
//
// - QueryResult mirrors aggregator.QueryResult (and augment.QueryResult)
// field-for-field; consumers convert at the boundary if their domain
// type carries extra fields.
// - TopK is the per-source ranked input window.
// - Fuse returns []QueryResult sorted descending by Score, capped to limit.
package rrf

import "sort"

const (
	DefaultK = 60

	DefaultLimit = 20

	PinBoost = 1.5

	MaxPriority = 99
)

var sourcePriority = map[string]int{
	"fts":   1,
	"vec":   2,
	"graph": 3,
	"pin":   4,
}

func priorityRank(source string) int {
	if p, ok := sourcePriority[source]; ok {
		return p
	}
	return MaxPriority
}

type QueryResult struct {
	NoteID           string
	Score            float64
	Title            string
	Snippet          string
	ProjectID        string
	AuditChainAnchor string
	Source           string
}

type TopK struct {
	Source  string
	Results []QueryResult
}

type fusedEntry struct {
	score      float64
	bestSource string

	title            string
	snippet          string
	projectID        string
	auditChainAnchor string
}

func Fuse(perSourceTopKs []TopK, k int, limit int) []QueryResult {
	if len(perSourceTopKs) == 0 {
		return nil
	}

	if k <= 0 {
		k = DefaultK
	}

	if limit <= 0 {
		limit = DefaultLimit
	}

	hasAny := false
	for i := range perSourceTopKs {
		if len(perSourceTopKs[i].Results) > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}

	fused := make(map[string]*fusedEntry)

	noteOrder := make([]string, 0)

	for _, topK := range perSourceTopKs {
		src := topK.Source

		boost := 1.0
		if src == "pin" {
			boost = PinBoost
		}

		for rank, result := range topK.Results {

			contribution := boost / float64(k+rank+1)

			entry, exists := fused[result.NoteID]
			if !exists {
				entry = &fusedEntry{
					score:            contribution,
					bestSource:       src,
					title:            result.Title,
					snippet:          result.Snippet,
					projectID:        result.ProjectID,
					auditChainAnchor: result.AuditChainAnchor,
				}
				fused[result.NoteID] = entry
				noteOrder = append(noteOrder, result.NoteID)
			} else {
				entry.score += contribution

				if priorityRank(src) < priorityRank(entry.bestSource) {
					entry.bestSource = src
					entry.title = result.Title
					entry.snippet = result.Snippet
					entry.projectID = result.ProjectID
					entry.auditChainAnchor = result.AuditChainAnchor
				}
			}
		}
	}

	out := make([]QueryResult, 0, len(fused))
	for _, noteID := range noteOrder {
		entry := fused[noteID]
		out = append(out, QueryResult{
			NoteID:           noteID,
			Score:            entry.score,
			Title:            entry.title,
			Snippet:          entry.snippet,
			ProjectID:        entry.projectID,
			AuditChainAnchor: entry.auditChainAnchor,
			Source:           entry.bestSource,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		pi := priorityRank(out[i].Source)
		pj := priorityRank(out[j].Source)
		if pi != pj {
			return pi < pj
		}
		return out[i].NoteID < out[j].NoteID
	})

	if len(out) > limit {
		out = out[:limit]
	}

	return out
}
