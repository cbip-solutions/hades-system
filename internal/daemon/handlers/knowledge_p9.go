// SPDX-License-Identifier: MIT
// Package handlers — knowledge_p9.go (Plan 9 Phase H Task H-2).
//
// 5 NEW operator-facing knowledge aggregator endpoints surfacing Phase D
// substrate (federated query + opt-in promote per Q6 C) over
// /v1/knowledge/*. inv-zen-150 + inv-zen-031: handlers consume the
// KnowledgeAdapterP9 interface and never import internal/knowledge/*
// directly. inv-zen-146: promote/unpromote require non-empty, non-whitespace
// --reason (auto-promote bypass structurally impossible from this surface).
//
//	GET  /v1/knowledge/query     — federated/pinned/chain-verified search
//	POST /v1/knowledge/promote   — operator-gated promote (--reason required)
//	POST /v1/knowledge/unpromote — operator-gated reverse (--reason required)
//	GET  /v1/knowledge/list      — list notes (pinned filter optional)
//	POST /v1/knowledge/rebuild   — full re-embed + re-index, returns 202+job_id
//
// Graceful degradation (Plan 2 pattern): any nil KnowledgeAdapterP9 passed
// to a constructor returns an http.HandlerFunc that immediately responds
// 503 {"error":"feature not configured","code":"plan9_knowledge_unavailable"}.
// Phase H-10 wires *daemon.Server to satisfy KnowledgeAdapterP9 once the
// Phase D adapter is available; during development the 503 makes intent
// explicit.
//
// Boundary invariants:
//
//	inv-zen-031: handler never imports internal/store directly.
//	inv-zen-150: handler never imports internal/knowledge/{aggregator,embed}
//	             directly; all calls go via KnowledgeAdapterP9.
//
// Wire KnowledgeQueryReqP9, KnowledgeResultP9, KnowledgeNoteP9,
// KnowledgeRebuildRespP9 declared inline.
//
// operatorFromContext is a provisional stub returning "" until Phase H-6
// lands the auth package extension. The adapter records "anonymous" in
// audit events when operatorID is "". Phase K sentinel test confirms.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

type KnowledgeQueryReqP9 struct {
	Query      string `json:"q"`
	Scope      string `json:"scope"`
	ProjectID  string `json:"project_id,omitempty"`
	AuditChain bool   `json:"audit_chain,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type KnowledgeResultP9 struct {
	NoteID     string  `json:"note_id"`
	ProjectID  string  `json:"project_id,omitempty"`
	Path       string  `json:"path,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Score      float64 `json:"score"`
	ChainProof string  `json:"audit_chain_proof,omitempty"`
}

type KnowledgeNoteP9 struct {
	NoteID    string `json:"note_id"`
	ProjectID string `json:"project_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Pinned    bool   `json:"pinned"`
	UpdatedAt int64  `json:"updated_at_unix,omitempty"`
}

type KnowledgeRebuildRespP9 struct {
	JobID     string `json:"job_id"`
	StartedAt int64  `json:"started_at_unix,omitempty"`
}

type KnowledgeAdapterP9 interface {
	Query(ctx context.Context, req KnowledgeQueryReqP9) ([]KnowledgeResultP9, error)
	Promote(ctx context.Context, noteID, reason, operatorID string) error
	Unpromote(ctx context.Context, noteID, reason, operatorID string) error
	List(ctx context.Context, projectID string, pinnedOnly bool) ([]KnowledgeNoteP9, error)
	Rebuild(ctx context.Context, projectID string) (KnowledgeRebuildRespP9, error)
}

func knowledgeP9Unavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "feature not configured",
		"code":  "plan9_knowledge_unavailable",
	})
}

func KnowledgeP9Query(s KnowledgeAdapterP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			knowledgeP9Unavailable(w)
			return
		}
		q := r.URL.Query()
		req := KnowledgeQueryReqP9{
			Query:     q.Get("q"),
			Scope:     q.Get("scope"),
			ProjectID: q.Get("project_id"),
			Limit:     50,
		}
		if req.Query == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q required"})
			return
		}
		if q.Get("audit_chain") == "true" {
			req.AuditChain = true
		}
		if v := q.Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				req.Limit = n
			}
		}
		rows, err := s.Query(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []KnowledgeResultP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func KnowledgeP9Promote(s KnowledgeAdapterP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			knowledgeP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			NoteID     string `json:"note_id"`
			Reason     string `json:"reason"`
			OperatorID string `json:"operator_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.NoteID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "note_id required"})
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "reason required (inv-zen-146; auto-promote forbidden)",
			})
			return
		}

		operatorID := knowledgeOperatorFromContext(r.Context())
		if operatorID == "" {
			operatorID = req.OperatorID
		}
		if err := s.Promote(r.Context(), req.NoteID, req.Reason, operatorID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func KnowledgeP9Unpromote(s KnowledgeAdapterP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			knowledgeP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			NoteID     string `json:"note_id"`
			Reason     string `json:"reason"`
			OperatorID string `json:"operator_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.NoteID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "note_id required"})
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "reason required (inv-zen-146; auto-unpromote forbidden)",
			})
			return
		}
		operatorID := knowledgeOperatorFromContext(r.Context())
		if operatorID == "" {
			operatorID = req.OperatorID
		}
		if err := s.Unpromote(r.Context(), req.NoteID, req.Reason, operatorID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func KnowledgeP9List(s KnowledgeAdapterP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			knowledgeP9Unavailable(w)
			return
		}
		projectID := r.URL.Query().Get("project_id")
		pinnedOnly := r.URL.Query().Get("pinned_only") == "true"
		rows, err := s.List(r.Context(), projectID, pinnedOnly)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []KnowledgeNoteP9{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func KnowledgeP9Rebuild(s KnowledgeAdapterP9) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			knowledgeP9Unavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			ProjectID string `json:"project_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}
		resp, err := s.Rebuild(r.Context(), req.ProjectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, resp)
	}
}

func knowledgeOperatorFromContext(ctx context.Context) string {
	return auth.OperatorIDFromContext(ctx)
}
