// SPDX-License-Identifier: MIT
// Package handlers — budget_plan4.go.
//
// 7 routes proxying to internal/budget/ engine:
//
// GET /v1/budget/cap_status — pre-call cap check
// POST /v1/budget/record — post-call axis tagging
// GET /v1/budget/axes — read axis tags for a cost_id
// GET /v1/budget/anomaly — z-score inspection
// GET /v1/budget/events — event log query
// POST /v1/budget/pause — operator manual pause
// POST /v1/budget/resume — operator manual resume
//
// Note existing HADES design routes /v1/budget, /v1/budget/{project}, /v1/budget/{project}/raise
// remain in budget.go unchanged. HADES design routes use sub-path keys (cap_status, record, etc.)
// to avoid collision.
//
// invariant: never imports internal/budget directly; all access via BudgetCtx interface.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type BudgetCapStatusResult struct {
	RemainingUSD float64 `json:"remaining_usd"`
	Blocked      bool    `json:"blocked"`
	BlockedScope string  `json:"blocked_scope,omitempty"`
}

type BudgetRecordReq struct {
	CostID      string            `json:"cost_id"`
	AmountUSD   float64           `json:"amount_usd"`
	AxisTags    map[string]string `json:"axis_tags"`
	OperationID string            `json:"operation_id,omitempty"`
	WorkerID    string            `json:"worker_id,omitempty"`
}

type BudgetAxisTag struct {
	AxisName  string `json:"axis_name"`
	AxisValue string `json:"axis_value"`
}

type BudgetAnomalyResult struct {
	ZScore  float64 `json:"z_score"`
	Mean    float64 `json:"mean"`
	StdDev  float64 `json:"std_dev"`
	Samples int64   `json:"samples"`
}

type BudgetEventRow struct {
	ID         string  `json:"id"`
	Scope      string  `json:"scope"`
	Value      string  `json:"value"`
	EventType  string  `json:"event_type"`
	AmountUSD  float64 `json:"amount_usd,omitempty"`
	OccurredAt int64   `json:"occurred_at"`
}

type BudgetCtx interface {
	BudgetCapStatus(axis, value string) (BudgetCapStatusResult, error)
	BudgetRecord(req BudgetRecordReq) error
	BudgetAxes(costID string) ([]BudgetAxisTag, error)
	BudgetAnomalyCheck(scope, value string, windowSec int64) (BudgetAnomalyResult, error)
	BudgetEvents(sinceUnix int64, limitN int) ([]BudgetEventRow, error)
	BudgetPause(scope, value, reason string) (string, error)
	BudgetResume(scope, value string) (string, error)
}

func BudgetCapStatus(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		axis := r.URL.Query().Get("axis")
		value := r.URL.Query().Get("value")
		if axis == "" || value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "axis and value query params required",
			})
			return
		}
		result, err := s.BudgetCapStatus(axis, value)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func BudgetRecord(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req BudgetRecordReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("invalid JSON: %s", err),
			})
			return
		}
		if req.CostID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cost_id required"})
			return
		}
		if err := s.BudgetRecord(req); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]bool{"recorded": true})
	}
}

func BudgetAxes(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		costID := r.URL.Query().Get("cost_id")
		if costID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cost_id required"})
			return
		}
		tags, err := s.BudgetAxes(costID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tags == nil {
			tags = []BudgetAxisTag{}
		}
		writeJSON(w, http.StatusOK, tags)
	}
}

func BudgetAnomaly(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		value := r.URL.Query().Get("value")
		if scope == "" || value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope and value required"})
			return
		}
		window := int64(3600)
		if v := r.URL.Query().Get("window"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				window = n
			}
		}
		result, err := s.BudgetAnomalyCheck(scope, value, window)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func BudgetEvents(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var since int64
		if v := r.URL.Query().Get("since"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("invalid since param %q: must be unix timestamp (int64)", v),
				})
				return
			}
			since = n
		}
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		events, err := s.BudgetEvents(since, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if events == nil {
			events = []BudgetEventRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events)})
	}
}

func BudgetPause(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			Scope  string `json:"scope"`
			Value  string `json:"value"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Scope == "" || body.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope and value required"})
			return
		}
		state, err := s.BudgetPause(body.Scope, body.Value, body.Reason)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"state": state, "scope": body.Scope, "value": body.Value})
	}
}

func BudgetResume(s BudgetCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			Scope string `json:"scope"`
			Value string `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Scope == "" || body.Value == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope and value required"})
			return
		}
		state, err := s.BudgetResume(body.Scope, body.Value)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"state": state, "scope": body.Scope, "value": body.Value})
	}
}
