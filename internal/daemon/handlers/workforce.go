// SPDX-License-Identifier: MIT
// Package handlers — workforce.go.
//
// Read-only inspection endpoints for workforce state:
//
// GET /v1/workforce/specs — list WorkerSpec rows
// GET /v1/workforce/workers — list active Worker rows
// GET /v1/workforce/checkpoints — list CheckpointQueue entries
// GET /v1/workforce/fix_prompts — list FixPromptQueue entries
// GET /v1/workforce/aggregations — list AggregationStream windows
//
// All endpoints support limit/offset pagination and type-specific filter params.
// These endpoints serve operator inspection (hades workforce status) and
//
// inv-hades-031: never imports internal/workforce directly; WorkforceCtx is the bridge.
package handlers

import (
	"net/http"
	"strconv"
)

type WorkerSpecRow struct {
	ID           string   `json:"id"`
	Variant      string   `json:"variant"`
	TaskTier     string   `json:"task_tier"`
	ModelClass   string   `json:"model_class"`
	DoctrineName string   `json:"doctrine_name"`
	ProjectID    string   `json:"project_id"`
	Tools        []string `json:"tools"`
	CreatedAt    int64    `json:"created_at"`
}

type WorkerRow struct {
	ID        string `json:"id"`
	SpecID    string `json:"spec_id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id"`
	ThreadID  string `json:"thread_id"`
	StartedAt int64  `json:"started_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type CheckpointRow struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	ThreadID  string `json:"thread_id"`
	StateJSON string `json:"state_json"`
	CreatedAt int64  `json:"created_at"`
}

type FixPromptRow struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	FromLayer string `json:"from_layer"`
	Prompt    string `json:"prompt"`
	Consumed  bool   `json:"consumed"`
	CreatedAt int64  `json:"created_at"`
}

type AggregationRow struct {
	ID          string `json:"id"`
	Layer       string `json:"layer"`
	WindowStart int64  `json:"window_start"`
	WindowEnd   int64  `json:"window_end"`
	SummaryJSON string `json:"summary_json"`
}

type WorkforceCtx interface {
	WorkforceSpecs(limit, offset int, filter string) ([]WorkerSpecRow, error)
	WorkforceWorkers(limit, offset int, status string) ([]WorkerRow, error)
	WorkforceCheckpoints(taskID string, limit, offset int) ([]CheckpointRow, error)
	WorkforceFixPrompts(taskID string, limit, offset int) ([]FixPromptRow, error)
	WorkforceAggregations(layer string, windowSec int64, limit int) ([]AggregationRow, error)
}

func parsePagination(r *http.Request) (limit, offset int) {
	limit = 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

func WorkforceSpecs(s WorkforceCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := parsePagination(r)
		filter := r.URL.Query().Get("variant")
		rows, err := s.WorkforceSpecs(limit, offset, filter)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []WorkerSpecRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func WorkforceWorkers(s WorkforceCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := parsePagination(r)
		status := r.URL.Query().Get("status")
		rows, err := s.WorkforceWorkers(limit, offset, status)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []WorkerRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func WorkforceCheckpoints(s WorkforceCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := parsePagination(r)
		taskID := r.URL.Query().Get("task_id")
		rows, err := s.WorkforceCheckpoints(taskID, limit, offset)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []CheckpointRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func WorkforceFixPrompts(s WorkforceCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, offset := parsePagination(r)
		taskID := r.URL.Query().Get("task_id")
		rows, err := s.WorkforceFixPrompts(taskID, limit, offset)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []FixPromptRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}

func WorkforceAggregations(s WorkforceCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		layer := r.URL.Query().Get("layer")
		window := int64(300)
		if v := r.URL.Query().Get("window"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
				window = n
			}
		}
		limit, _ := parsePagination(r)
		rows, err := s.WorkforceAggregations(layer, window, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows == nil {
			rows = []AggregationRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows, "count": len(rows)})
	}
}
