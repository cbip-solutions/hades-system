// SPDX-License-Identifier: MIT
// Package handlers — knowledge_aggregator.go.
//
// Five HTTP handler functions for the release knowledge aggregator surface:
//
// POST /v1/knowledge/aggregator/query — FTS + structured filter via
// Aggregator.QueryFTS
// POST /v1/knowledge/aggregator/promote — promote note to pin-index
// POST /v1/knowledge/aggregator/unpromote — remove note from pin-index
// GET /v1/knowledge/aggregator/list — list pinned notes
// POST /v1/knowledge/aggregator/rebuild — enqueue async re-embed
//
// These are DISTINCT from the release knowledge handler factory functions
// (KnowledgeQueryHandler, KnowledgeReindexHandler, KnowledgeStatsHandler in
// knowledge.go) which back the legacy /v1/knowledge/{query,reindex,stats}
// routes. Both surfaces coexist; the release aggregator routes delegate to an
// AggregatorService interface (structural typing) to avoid importing
// internal/knowledge/aggregator directly in the handlers package.
//
// Driver-conflict isolation: internal/knowledge/aggregator's db.go imports
// mattn/go-sqlite3 (CGO). This handlers package imports internal/store which
// brings in ncruces/go-sqlite3. Both register the "sqlite3" SQL driver name
// and would panic on double-registration if both packages are imported in the
// same binary. By using structural typing (AggregatorService interface rather
// than a concrete *aggregator.Aggregator), the handlers package avoids
// importing aggregator directly — the aggregator import lives only in
// internal/daemon/knowledgeadapter (invariant bridge) and in
// cmd/hades-ctld (the binary, which tolerates the CGO dep).
//
// Route registration: RegisterKnowledgeAggregatorRoutes(mux, h) mounts all
// five routes. Called by srv.RegisterKnowledgeAggregator after main.go
// composes the full subsystem.
//
// Status-code mapping (mirrors inbox_p7 + hadesday patterns):
//
// 400 — invalid JSON body or missing required field
// 500 — opaque backend failure (sql I/O, embedder error, etc.)
// 200 — success
// 202 — rebuild enqueued (async; forward-compat seam D-12)
//
// invariant: this file does NOT import internal/store directly.
// invariant: empty reason in promote/unpromote → ErrPromoteReasonRequired
// → HTTP 400 (detected via error string; errors.Is not available without
// importing aggregator, which we intentionally avoid here).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const aggHandlerTimeout = 30 * time.Second

type AggQueryResult struct {
	NoteID           string  `json:"note_id"`
	Title            string  `json:"title"`
	ProjectID        string  `json:"project_id"`
	Score            float64 `json:"score"`
	Snippet          string  `json:"snippet,omitempty"`
	AuditChainAnchor string  `json:"audit_chain_anchor,omitempty"`
	Source           string  `json:"source"`
}

type AggPromoteResult struct {
	NoteID           string    `json:"note_id"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
	PromotedAt       time.Time `json:"promoted_at"`
	Idempotent       bool      `json:"idempotent,omitempty"`
}

type AggUnpromoteResult struct {
	NoteID       string    `json:"note_id"`
	UnpromotedAt time.Time `json:"unpromoted_at"`
	Idempotent   bool      `json:"idempotent,omitempty"`
}

type AggPinNote struct {
	NoteID           string    `json:"note_id"`
	ProjectID        string    `json:"project_id"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	FrontmatterJSON  string    `json:"frontmatter_json"`
	PromotedAt       time.Time `json:"promoted_at"`
	PromotedBy       string    `json:"promoted_by"`
	PromoteReason    string    `json:"promote_reason"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
}

type AggQueryRequest struct {
	Text      string `json:"text"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type AggQueryResponse struct {
	Results []AggQueryResult `json:"results"`
}

type AggPromoteRequest struct {
	NoteID     string `json:"note_id"`
	ProjectID  string `json:"project_id"`
	OperatorID string `json:"operator_id"`
	Reason     string `json:"reason"`
}

type AggPromoteResponse struct {
	NoteID           string    `json:"note_id"`
	AuditChainAnchor string    `json:"audit_chain_anchor"`
	PromotedAt       time.Time `json:"promoted_at"`
	Idempotent       bool      `json:"idempotent,omitempty"`
}

type AggUnpromoteRequest struct {
	NoteID     string `json:"note_id"`
	ProjectID  string `json:"project_id,omitempty"`
	OperatorID string `json:"operator_id"`
	Reason     string `json:"reason"`
}

type AggUnpromoteResponse struct {
	NoteID       string    `json:"note_id"`
	UnpromotedAt time.Time `json:"unpromoted_at"`
	Idempotent   bool      `json:"idempotent,omitempty"`
}

type AggListResponse struct {
	Notes []AggPinNote `json:"notes"`
}

type AggRebuildResponse struct {
	Status    string `json:"status"`
	ProjectID string `json:"project_id,omitempty"`
}

type AggregatorService interface {
	AggQueryFTS(ctx context.Context, queryText string, limit int) ([]AggQueryResult, error)

	AggPromote(ctx context.Context, noteID, projectID, operatorID, reason string) (*AggPromoteResult, error)

	AggUnpromote(ctx context.Context, noteID, operatorID, reason string) (*AggUnpromoteResult, error)

	AggListPins(ctx context.Context, projectID string) ([]AggPinNote, error)

	AggEnqueueRebuild(ctx context.Context, projectID string) error
}

var ErrAggPromoteReasonRequired = errors.New(
	"handler: promote/unpromote reason required (inv-hades-146)",
)

var ErrAggWorkerNotStarted = errors.New(
	"handler: embed_worker not started (Phase J forward-compat seam)",
)

func isPromoteReasonRequired(err error) bool {
	if errors.Is(err, ErrAggPromoteReasonRequired) {
		return true
	}

	return err != nil && strings.Contains(err.Error(), "inv-hades-146")
}

func isWorkerNotStarted(err error) bool {
	return errors.Is(err, ErrAggWorkerNotStarted)
}

type KnowledgeAggregatorHandlers struct {
	Agg AggregatorService
}

func RegisterKnowledgeAggregatorRoutes(mux *http.ServeMux, h *KnowledgeAggregatorHandlers) {
	mux.HandleFunc("POST /v1/knowledge/aggregator/query", h.handleAggQuery)
	mux.HandleFunc("POST /v1/knowledge/aggregator/promote", h.handleAggPromote)
	mux.HandleFunc("POST /v1/knowledge/aggregator/unpromote", h.handleAggUnpromote)
	mux.HandleFunc("GET /v1/knowledge/aggregator/list", h.handleAggList)
	mux.HandleFunc("POST /v1/knowledge/aggregator/rebuild", h.handleAggRebuild)
}

func (h *KnowledgeAggregatorHandlers) handleAggQuery(w http.ResponseWriter, r *http.Request) {
	var req AggQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	ctx, cancel := context.WithTimeout(r.Context(), aggHandlerTimeout)
	defer cancel()

	results, err := h.Agg.AggQueryFTS(ctx, req.Text, limit)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if results == nil {
		results = []AggQueryResult{}
	}
	writeJSON(w, http.StatusOK, AggQueryResponse{Results: results})
}

func (h *KnowledgeAggregatorHandlers) handleAggPromote(w http.ResponseWriter, r *http.Request) {
	var req AggPromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aggHandlerTimeout)
	defer cancel()

	res, err := h.Agg.AggPromote(ctx, req.NoteID, req.ProjectID, req.OperatorID, req.Reason)
	if err != nil {
		if isPromoteReasonRequired(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "promote failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, AggPromoteResponse{
		NoteID:           res.NoteID,
		AuditChainAnchor: res.AuditChainAnchor,
		PromotedAt:       res.PromotedAt,
		Idempotent:       res.Idempotent,
	})
}

func (h *KnowledgeAggregatorHandlers) handleAggUnpromote(w http.ResponseWriter, r *http.Request) {
	var req AggUnpromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aggHandlerTimeout)
	defer cancel()

	res, err := h.Agg.AggUnpromote(ctx, req.NoteID, req.OperatorID, req.Reason)
	if err != nil {
		if isPromoteReasonRequired(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "unpromote failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, AggUnpromoteResponse{
		NoteID:       res.NoteID,
		UnpromotedAt: res.UnpromotedAt,
		Idempotent:   res.Idempotent,
	})
}

func (h *KnowledgeAggregatorHandlers) handleAggList(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")

	ctx, cancel := context.WithTimeout(r.Context(), aggHandlerTimeout)
	defer cancel()

	notes, err := h.Agg.AggListPins(ctx, projectID)
	if err != nil {
		http.Error(w, "list failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if notes == nil {
		notes = []AggPinNote{}
	}
	writeJSON(w, http.StatusOK, AggListResponse{Notes: notes})
}

func (h *KnowledgeAggregatorHandlers) handleAggRebuild(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"project_id,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx, cancel := context.WithTimeout(r.Context(), aggHandlerTimeout)
	defer cancel()

	if err := h.Agg.AggEnqueueRebuild(ctx, req.ProjectID); err != nil {
		if !isWorkerNotStarted(err) {
			http.Error(w, "rebuild enqueue failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

	}

	writeJSON(w, http.StatusAccepted, AggRebuildResponse{
		Status:    "queued",
		ProjectID: req.ProjectID,
	})
}
