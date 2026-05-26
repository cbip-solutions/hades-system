// SPDX-License-Identifier: MIT
// Package handlers — knowledge.go (Plan 7 Phase G Task G-11).
//
// Three routes for the Plan 7 knowledge aggregator operator surface:
//
//	POST /v1/knowledge/query    — hybrid FTS5 + structured filter
//	POST /v1/knowledge/reindex  — cold rebuild dispatch
//	GET  /v1/knowledge/stats    — index statistics (count + by-type +
//	                              last-indexed timestamp)
//
// These dispatch to a KnowledgeIndex façade wired at daemon boot (Phase
// I composes the *internal/knowledge index DB + scanner + parser into a
// KnowledgeIndex satisfying interface and hands it to SetKnowledgeIndex).
// Until Phase I lands the routes return 503 — the operator surface is
// final-shape day 1 (`zen knowledge query` reaches a real route) but the
// substrate ships in Phase I bootstrap (mirrors the Plan 7 D-13 schedule
// + E-12 inbox + F-10 zen-day patterns).
//
// Status-code mapping (mirrors the inbox_p7 + zenday patterns):
//
//	503  — KnowledgeIndex() not yet wired (Phase I bootstrap will
//	       register the façade at boot; tests inject fakes via
//	       SetKnowledgeIndex).
//	400  — invalid JSON body.
//	422  — validation rejected the input (unknown FileType in filter).
//	500  — opaque query / reindex / stats failure (sql I/O, scanner
//	       error count > threshold, etc).
//	200  — success; bodies documented per route below.
//
// inv-zen-031 boundary: this handler imports internal/knowledge value
// types only (Query / Result / Doc / FileType / sentinel errors). No
// internal/store imports — the KnowledgeIndex interface is structural
// and the daemon-side accessor returns it as the same interface, keeping
// the boundary at the interface layer (mirrors handlers.InboxStore +
// handlers.ScheduleStore + handlers.DayGenerator gate patterns). The
// knowledge package owns its own DB at
// `~/.cache/zen-swarm/knowledge-index/index.db` (per the inv-zen-031
// documented exception in docs/operations/knowledge-aggregator-boundary.md).
//
// inv-zen-129 boundary: the handler does NOT process q.Remote=true /
// q.AuditChain=true even though the underlying knowledge.Execute returns
// distinct sentinel errors for both. The CLI layer intercepts both flags
// BEFORE the round-trip per spec §1 Q17 + the G-12/G-13 sentinel-anchor
// contract; if a misbehaving caller sends Remote/AuditChain over the
// wire the handler reports 422 (validation rejected) — defense in depth
// on the boundary.
//
// CLI surface (handled in internal/cli/knowledge.go):
//
//	zen knowledge query <text> [--type X] [--project Y] [--since 7d] [--limit N] [--format text|json|md] [--code-symbol foo]
//	zen knowledge reindex [--full] [--project alias]
//	zen knowledge stats
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/knowledge"
)

type ReindexRequest struct {
	Full         bool
	ProjectAlias string
}

type ReindexResult struct {
	Indexed int
	Errors  int
}

type KnowledgeStats struct {
	TotalDocs       int
	ByType          map[string]int
	LastIndexedUnix int64
}

type KnowledgeIndex interface {
	Query(ctx context.Context, q knowledge.Query) ([]knowledge.Result, error)
	Reindex(ctx context.Context, req ReindexRequest) (ReindexResult, error)
	Stats(ctx context.Context) (KnowledgeStats, error)
}

type knowledgeIndexAccessor interface {
	KnowledgeIndex() KnowledgeIndex
}

func resolveKnowledgeIndex(s any) KnowledgeIndex {
	acc, ok := s.(knowledgeIndexAccessor)
	if !ok {
		return nil
	}
	return acc.KnowledgeIndex()
}

func knowledgeUnavailable(w http.ResponseWriter) {
	http.Error(w, "knowledge index not configured", http.StatusServiceUnavailable)
}

const knowledgeHandlerTimeout = 30 * time.Second

const reindexHandlerTimeout = 5 * time.Minute

type KnowledgeQueryRequest struct {
	FreeText     string   `json:"free_text,omitempty"`
	ProjectAlias []string `json:"project_alias,omitempty"`
	Type         []string `json:"type,omitempty"`
	SinceSeconds int64    `json:"since_seconds,omitempty"`
	Limit        int      `json:"limit,omitempty"`
	CodeSymbol   string   `json:"code_symbol,omitempty"`
}

type KnowledgeQueryResponse struct {
	Rows []KnowledgeResultRow `json:"rows"`
}

type KnowledgeResultRow struct {
	FilePath     string    `json:"file_path"`
	ProjectID    string    `json:"project_id"`
	ProjectAlias string    `json:"project_alias"`
	FileType     string    `json:"file_type"`
	Title        string    `json:"title"`
	LastModified time.Time `json:"last_modified"`
	Score        float64   `json:"score"`
	Snippet      string    `json:"snippet"`
}

type KnowledgeReindexRequest struct {
	Full         bool   `json:"full,omitempty"`
	ProjectAlias string `json:"project_alias,omitempty"`
}

type KnowledgeReindexResponse struct {
	OK      bool `json:"ok"`
	Indexed int  `json:"indexed"`
	Errors  int  `json:"errors,omitempty"`
}

type KnowledgeStatsResponse struct {
	TotalDocs       int            `json:"total_docs"`
	ByType          map[string]int `json:"by_type"`
	LastIndexedUnix int64          `json:"last_indexed_unix"`
}

func KnowledgeQueryHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idx := resolveKnowledgeIndex(s)
		if idx == nil {
			knowledgeUnavailable(w)
			return
		}
		var req KnowledgeQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}

		q := knowledge.Query{
			FreeText:      req.FreeText,
			ProjectFilter: append([]string(nil), req.ProjectAlias...),
			Limit:         req.Limit,
			CodeSymbol:    req.CodeSymbol,
		}
		for _, t := range req.Type {
			ft := knowledge.FileType(t)

			switch ft {
			case knowledge.FileTypeMemory, knowledge.FileTypeResearch,
				knowledge.FileTypeADR, knowledge.FileTypeSpec,
				knowledge.FileTypePlan, knowledge.FileTypeHandoff:
				q.TypeFilter = append(q.TypeFilter, ft)
			default:
				http.Error(w, fmt.Sprintf("unknown file_type: %q", t), http.StatusUnprocessableEntity)
				return
			}
		}
		if req.SinceSeconds > 0 {
			d := time.Duration(req.SinceSeconds) * time.Second
			q.SinceFilter = &d
		}
		ctx, cancel := context.WithTimeout(r.Context(), knowledgeHandlerTimeout)
		defer cancel()
		results, err := idx.Query(ctx, q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := KnowledgeQueryResponse{Rows: make([]KnowledgeResultRow, 0, len(results))}
		for _, res := range results {
			out.Rows = append(out.Rows, KnowledgeResultRow{
				FilePath:     res.Doc.FilePath,
				ProjectID:    res.Doc.ProjectID,
				ProjectAlias: res.Doc.ProjectAlias,
				FileType:     string(res.Doc.FileType),
				Title:        res.Doc.Title,
				LastModified: res.Doc.LastModified,
				Score:        res.Score,
				Snippet:      res.Snippet,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func KnowledgeReindexHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idx := resolveKnowledgeIndex(s)
		if idx == nil {
			knowledgeUnavailable(w)
			return
		}
		var req KnowledgeReindexRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), reindexHandlerTimeout)
		defer cancel()
		res, err := idx.Reindex(ctx, ReindexRequest{
			Full:         req.Full,
			ProjectAlias: req.ProjectAlias,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, KnowledgeReindexResponse{
			OK:      true,
			Indexed: res.Indexed,
			Errors:  res.Errors,
		})
	}
}

func KnowledgeStatsHandler(s any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idx := resolveKnowledgeIndex(s)
		if idx == nil {
			knowledgeUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), knowledgeHandlerTimeout)
		defer cancel()
		stats, err := idx.Stats(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := KnowledgeStatsResponse{
			TotalDocs:       stats.TotalDocs,
			ByType:          stats.ByType,
			LastIndexedUnix: stats.LastIndexedUnix,
		}
		if out.ByType == nil {
			out.ByType = map[string]int{}
		}
		writeJSON(w, http.StatusOK, out)
	}
}
