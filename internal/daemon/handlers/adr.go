// SPDX-License-Identifier: MIT
// Package handlers — adr.go (Plan 9 Phase H Task H-3).
//
// 9 NEW operator-facing ADR endpoints surfacing Phase E substrate
// (Structured MADR machine-readable index per Q7 A) over /v1/adr/*.
// inv-zen-146: write endpoints (accept/reject/supersede) require non-empty
// reason — 400 on violation. inv-zen-031: handler never imports
// internal/store. Wire types (ADRDoc, ADRListFilter, ADRGraph, ADRTransition,
// ADRManifest) are declared locally to keep the handler boundary decoupled
// from internal/adr — Phase H-10 wires *daemon.Server to satisfy ADRIndex
// via the production adr.Index implementation.
//
//	POST /v1/adr/propose    — draft + auto-assigned ID (non-interactive; CLI phase I prompts $EDITOR)
//	GET  /v1/adr/show       — render frontmatter + body
//	GET  /v1/adr/list       — filter by status/plan/risk_level
//	GET  /v1/adr/graph      — supersede chain DAG (nodes + edges)
//	GET  /v1/adr/history    — transition log for one ADR
//	POST /v1/adr/accept     — emit adr.accepted event (reason mandatory; inv-zen-146)
//	POST /v1/adr/reject     — emit adr.rejected event (reason mandatory)
//	POST /v1/adr/supersede  — link old→new chain + emit adr.superseded (reason mandatory)
//	POST /v1/adr/index      — regenerate dual manifest (check=true for CI gate dry-run)
//
// Graceful degradation (Plan 2 pattern): any nil ADRIndex passed to a
// constructor returns an http.HandlerFunc that immediately responds with
// HTTP 503 {"error":"feature not configured","code":"plan9_adr_unavailable"}.
// Phase H-10 wires *daemon.Server once the Phase E adr.Index adapter is
// available; during development the 503 makes intent explicit.
//
// Boundary invariants:
//
//	inv-zen-031: handler never imports internal/store directly.
//	inv-zen-146: accept/reject/supersede require non-empty reason; 400 on violation.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

type ADRDoc struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Topic       string            `json:"topic"`
	Plan        string            `json:"plan"`
	RiskLevel   string            `json:"risk_level,omitempty"`
	Frontmatter map[string]string `json:"frontmatter"`
	Body        string            `json:"body,omitempty"`
	CreatedAt   int64             `json:"created_at_unix"`
	UpdatedAt   int64             `json:"updated_at_unix"`
}

type ADRListFilter struct {
	Status    string `json:"status"`
	Plan      string `json:"plan"`
	RiskLevel string `json:"risk_level"`
	Limit     int    `json:"limit"`
}

type ADRGraphNode struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type ADRGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type ADRGraph struct {
	Nodes []ADRGraphNode `json:"nodes"`
	Edges []ADRGraphEdge `json:"edges"`
}

type ADRTransition struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	At     int64  `json:"at_unix"`
	Reason string `json:"reason,omitempty"`
}

type ADRManifest struct {
	GeneratedAt int64  `json:"generated_at_unix"`
	ADRCount    int    `json:"adr_count"`
	Manifest    string `json:"manifest_json"`
	Graph       string `json:"graph_json"`
}

type ADRCtx interface {
	Propose(ctx context.Context, topic string) (ADRDoc, error)

	Show(ctx context.Context, id string) (ADRDoc, error)

	List(ctx context.Context, filter ADRListFilter) ([]ADRDoc, error)

	Graph(ctx context.Context, fromID string, depth int) (ADRGraph, error)

	History(ctx context.Context, id string) ([]ADRTransition, error)

	Accept(ctx context.Context, id, reason string) error

	Reject(ctx context.Context, id, reason string) error

	Supersede(ctx context.Context, oldID, newID, reason string) error

	RegenerateIndex(ctx context.Context, dryRun bool) (ADRManifest, error)
}

func adrUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "feature not configured",
		"code":  "plan9_adr_unavailable",
	})
}

func ADRPropose(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Topic     string `json:"topic"`
			PlanRange string `json:"plan_range"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.Topic == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "topic required"})
			return
		}
		doc, err := s.Propose(r.Context(), req.Topic)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, doc)
	}
}

func ADRShow(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		doc, err := s.Show(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if doc.ID == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, doc)
	}
}

func ADRList(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		filter := ADRListFilter{
			Status:    r.URL.Query().Get("status"),
			Plan:      r.URL.Query().Get("plan"),
			RiskLevel: r.URL.Query().Get("risk_level"),
			Limit:     200,
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				filter.Limit = n
			}
		}
		rows, err := s.List(r.Context(), filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ADRDoc{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func ADRGraphHandler(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		from := r.URL.Query().Get("from")
		if from == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from required"})
			return
		}
		depth := 1
		if v := r.URL.Query().Get("depth"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
				depth = n
			}
		}
		g, err := s.Graph(r.Context(), from, depth)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, g)
	}
}

func ADRHistoryHandler(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
			return
		}
		rows, err := s.History(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []ADRTransition{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func ADRAccept(s ADRCtx) http.HandlerFunc {
	if s == nil {
		return func(w http.ResponseWriter, r *http.Request) { adrUnavailable(w) }
	}
	return adrTransition(s.Accept)
}

func ADRReject(s ADRCtx) http.HandlerFunc {
	if s == nil {
		return func(w http.ResponseWriter, r *http.Request) { adrUnavailable(w) }
	}
	return adrTransition(s.Reject)
}

func adrTransition(fn func(context.Context, string, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.ID == "" || req.Reason == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "id and reason required (inv-zen-146)",
			})
			return
		}
		if err := fn(r.Context(), req.ID, req.Reason); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ADRSupersede(s ADRCtx) http.HandlerFunc {
	if s == nil {
		return func(w http.ResponseWriter, r *http.Request) { adrUnavailable(w) }
	}
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			OldID  string `json:"old_id"`
			NewID  string `json:"new_id"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if req.OldID == "" || req.NewID == "" || req.Reason == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "old_id, new_id and reason required (inv-zen-146)",
			})
			return
		}
		if err := s.Supersede(r.Context(), req.OldID, req.NewID, req.Reason); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func ADRIndex(s ADRCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s == nil {
			adrUnavailable(w)
			return
		}
		defer r.Body.Close()
		var req struct {
			Check bool `json:"check"`
		}

		_ = json.NewDecoder(r.Body).Decode(&req)
		m, err := s.RegenerateIndex(r.Context(), req.Check)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, m)
	}
}
