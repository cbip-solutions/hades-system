package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBudgetAxes_NilTags(t *testing.T) {

	srv := newBudgetServer()

	h := BudgetAxes(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/axes?cost_id=cost-unknown", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBudgetEvents_WithParams(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetEvents(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/events?since=1000&limit=10", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestBudgetEvents_NilEvents(t *testing.T) {

	srv := newBudgetServer()
	h := BudgetEvents(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWorkforceWorkers_NilRows(t *testing.T) {
	srv := &workforceServer{workers: nil}
	h := WorkforceWorkers(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/workers", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWorkforceCheckpoints_NilRows(t *testing.T) {
	srv := &workforceServer{checkpoints: nil}
	h := WorkforceCheckpoints(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/checkpoints", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWorkforceAggregations_NilRows(t *testing.T) {
	srv := &workforceServer{aggregations: nil}
	h := WorkforceAggregations(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/aggregations", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestWorkforceAggregations_WithWindow(t *testing.T) {
	srv := &workforceServer{
		aggregations: []AggregationRow{{ID: "agg-w", Layer: "l2-to-l3", WindowStart: 0, WindowEnd: 600}},
	}
	h := WorkforceAggregations(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/aggregations?window=600", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestNewTokenBucket_ZeroRate(t *testing.T) {
	b := newTokenBucket(0)
	if b.capacity != 1 {
		t.Errorf("capacity: want 1, got %f", b.capacity)
	}

	ok, _ := b.tryConsume()
	if !ok {
		t.Error("zero-rate bucket should allow first request")
	}
}

func TestWorkforceAggregations_InvalidWindow(t *testing.T) {
	srv := &workforceServer{}
	h := WorkforceAggregations(srv)

	req := httptest.NewRequest(http.MethodGet, "/v1/workforce/aggregations?window=0", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}
