// SPDX-License-Identifier: MIT
// Package aggregator — rrf.go
//
// type-conversion shim over the CGO-free internal/knowledge/rrf package.
// The canonical RRF implementation moved there so consumers that cannot
// link the aggregator's CGO-bound sqlite3 driver (compliance tests using
// ncruces/go-sqlite3 + internal/augment) can import the shared Fuse
// without inlining a copy.
//
// Backward compat: aggregator.Fuse keeps its signature and exported
// surface; existing call sites (D-5 query.go) continue to compile and
// behave identically. The wrapper translates aggregator.{QueryResult,
// TopK} ↔ rrf.{QueryResult, TopK} at the boundary; both types are
// field-for-field identical so the conversion is essentially a copy.
//
// inv-zen-031 boundary: rrf imports stdlib only; no internal/store dep.
//
// shim. FuseWeighted is the cross-source weighted RRF variant used by Plan
// 14's ecosystem dispatcher (spec §4.2 step 6) to merge per-ecosystem
// retrieval results weighted by router confidence. The implementation lives
// here (not in the rrf sub-package) because it consumes aggregator-package
// constants — defaultQueryLimit from types.go — and the symmetric pair with
// Fuse keeps callers' import paths simple. Fuse is unchanged.
package aggregator

import (
	"sort"

	"github.com/cbip-solutions/hades-system/internal/knowledge/rrf"
)

const (
	rrfDefaultK = rrf.DefaultK
	pinBoost    = rrf.PinBoost
	maxPriority = rrf.MaxPriority
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
	return maxPriority
}

func Fuse(perSourceTopKs []TopK, k int, limit int) []QueryResult {
	if len(perSourceTopKs) == 0 {
		return nil
	}

	rrfTopKs := make([]rrf.TopK, len(perSourceTopKs))
	for i, t := range perSourceTopKs {
		rrfTopKs[i] = rrf.TopK{
			Source:  t.Source,
			Results: toRRFResults(t.Results),
		}
	}

	rrfOut := rrf.Fuse(rrfTopKs, k, limit)

	return fromRRFResults(rrfOut)
}

func toRRFResults(in []QueryResult) []rrf.QueryResult {
	out := make([]rrf.QueryResult, len(in))
	for i, r := range in {
		out[i] = rrf.QueryResult{
			NoteID:           r.NoteID,
			Score:            r.Score,
			Title:            r.Title,
			Snippet:          r.Snippet,
			ProjectID:        r.ProjectID,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
		}
	}
	return out
}

func fromRRFResults(in []rrf.QueryResult) []QueryResult {
	out := make([]QueryResult, len(in))
	for i, r := range in {
		out[i] = QueryResult{
			NoteID:           r.NoteID,
			Score:            r.Score,
			Title:            r.Title,
			Snippet:          r.Snippet,
			ProjectID:        r.ProjectID,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
		}
	}
	return out
}

type weightedFusedEntry struct {
	score            float64
	bestSource       string
	title            string
	snippet          string
	projectID        string
	auditChainAnchor string
}

func FuseWeighted(perSourceTopKs []TopK, weights map[string]float64, k int, limit int) []QueryResult {
	if len(perSourceTopKs) == 0 {
		return nil
	}

	if k <= 0 {
		k = rrfDefaultK
	}
	if limit <= 0 {
		limit = defaultQueryLimit
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

	fused := make(map[string]*weightedFusedEntry)
	noteOrder := make([]string, 0)

	for _, topK := range perSourceTopKs {
		src := topK.Source
		w := weights[src]

		boost := 1.0
		if src == "pin" {
			boost = rrf.PinBoost
		}

		for rank, r := range topK.Results {

			contribution := w * boost / float64(k+rank+1)

			entry, exists := fused[r.NoteID]
			if !exists {
				fused[r.NoteID] = &weightedFusedEntry{
					score:            contribution,
					bestSource:       src,
					title:            r.Title,
					snippet:          r.Snippet,
					projectID:        r.ProjectID,
					auditChainAnchor: r.AuditChainAnchor,
				}
				noteOrder = append(noteOrder, r.NoteID)
				continue
			}
			entry.score += contribution

			if priorityRank(src) < priorityRank(entry.bestSource) {
				entry.bestSource = src
				entry.title = r.Title
				entry.snippet = r.Snippet
				entry.projectID = r.ProjectID
				entry.auditChainAnchor = r.AuditChainAnchor
			}
		}
	}

	out := make([]QueryResult, 0, len(fused))
	for _, noteID := range noteOrder {
		e := fused[noteID]
		out = append(out, QueryResult{
			NoteID:           noteID,
			Score:            e.score,
			Source:           e.bestSource,
			Title:            e.title,
			Snippet:          e.snippet,
			ProjectID:        e.projectID,
			AuditChainAnchor: e.auditChainAnchor,
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
