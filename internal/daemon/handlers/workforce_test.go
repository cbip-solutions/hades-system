package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type workforceServer struct {
	specs        []WorkerSpecRow
	workers      []WorkerRow
	checkpoints  []CheckpointRow
	fixPrompts   []FixPromptRow
	aggregations []AggregationRow
}

func (s *workforceServer) WorkforceSpecs(limit, offset int, filter string) ([]WorkerSpecRow, error) {
	return s.specs, nil
}
func (s *workforceServer) WorkforceWorkers(limit, offset int, status string) ([]WorkerRow, error) {
	return s.workers, nil
}
func (s *workforceServer) WorkforceCheckpoints(taskID string, limit, offset int) ([]CheckpointRow, error) {
	return s.checkpoints, nil
}
func (s *workforceServer) WorkforceFixPrompts(taskID string, limit, offset int) ([]FixPromptRow, error) {
	return s.fixPrompts, nil
}
func (s *workforceServer) WorkforceAggregations(layer string, windowSec int64, limit int) ([]AggregationRow, error) {
	return s.aggregations, nil
}

func TestWorkforceSpecs_Empty(t *testing.T) {
	srv := &workforceServer{}
	h := WorkforceSpecs(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/specs", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	items, _ := resp["items"].([]any)
	if len(items) != 0 {
		t.Errorf("want 0 items, got %d", len(items))
	}
}

func TestWorkforceSpecs_WithData(t *testing.T) {
	srv := &workforceServer{
		specs: []WorkerSpecRow{
			{ID: "spec-1", Variant: "worker", TaskTier: "medium", DoctrineName: "max-scope"},
			{ID: "spec-2", Variant: "reviewer-l2", TaskTier: "simple", DoctrineName: "default"},
		},
	}
	h := WorkforceSpecs(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/specs?limit=10&offset=0", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	items, _ := resp["items"].([]any)
	if len(items) != 2 {
		t.Errorf("want 2 items, got %d", len(items))
	}
}

func TestWorkforceWorkers_StatusFilter(t *testing.T) {
	srv := &workforceServer{
		workers: []WorkerRow{{ID: "w-1", SpecID: "spec-1", Status: "in_progress"}},
	}
	h := WorkforceWorkers(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/workers?status=in_progress", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWorkforceCheckpoints_ByTask(t *testing.T) {
	srv := &workforceServer{
		checkpoints: []CheckpointRow{{ID: "ckpt-1", TaskID: "task-42", ThreadID: "thread-abc"}},
	}
	h := WorkforceCheckpoints(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/checkpoints?task_id=task-42", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("want 1 checkpoint, got %d", len(items))
	}
}

func TestWorkforceFixPrompts_Empty(t *testing.T) {
	srv := &workforceServer{}
	h := WorkforceFixPrompts(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/fix_prompts", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWorkforceAggregations_Layer(t *testing.T) {
	srv := &workforceServer{
		aggregations: []AggregationRow{
			{ID: "agg-1", Layer: "l2-to-l3", WindowStart: 1000, WindowEnd: 1030},
		},
	}
	h := WorkforceAggregations(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/aggregations?layer=l2-to-l3", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	items, _ := resp["items"].([]any)
	if len(items) != 1 {
		t.Errorf("want 1 aggregation, got %d", len(items))
	}
}
