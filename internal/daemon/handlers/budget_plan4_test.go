package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type budgetServer struct {
	capRemaining  float64
	capBlocked    bool
	capBlockScope string
	recorded      []BudgetRecordReq
	axesData      map[string][]BudgetAxisTag
	anomalyZ      float64
	events        []BudgetEventRow
	pauseState    map[string]string
}

func newBudgetServer() *budgetServer {
	return &budgetServer{
		capRemaining: 42.00,
		axesData:     make(map[string][]BudgetAxisTag),
		pauseState:   make(map[string]string),
	}
}

func (b *budgetServer) BudgetCapStatus(axis, value string) (BudgetCapStatusResult, error) {
	return BudgetCapStatusResult{
		RemainingUSD: b.capRemaining,
		Blocked:      b.capBlocked,
		BlockedScope: b.capBlockScope,
	}, nil
}

func (b *budgetServer) BudgetRecord(req BudgetRecordReq) error {
	b.recorded = append(b.recorded, req)
	return nil
}

func (b *budgetServer) BudgetAxes(costID string) ([]BudgetAxisTag, error) {
	return b.axesData[costID], nil
}

func (b *budgetServer) BudgetAnomalyCheck(scope, value string, windowSec int64) (BudgetAnomalyResult, error) {
	return BudgetAnomalyResult{ZScore: b.anomalyZ, Mean: 1.0, StdDev: 0.5, Samples: 10}, nil
}

func (b *budgetServer) BudgetEvents(sinceUnix int64, limitN int) ([]BudgetEventRow, error) {
	return b.events, nil
}

func (b *budgetServer) BudgetPause(scope, value, reason string) (string, error) {
	b.pauseState[scope+":"+value] = "paused"
	return "paused", nil
}

func (b *budgetServer) BudgetResume(scope, value string) (string, error) {
	b.pauseState[scope+":"+value] = "running"
	return "running", nil
}

func TestBudgetCapStatus_OK(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetCapStatus(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/cap_status?axis=stage&value=design", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp BudgetCapStatusResult
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.RemainingUSD != 42.00 {
		t.Errorf("remaining_usd: got %f", resp.RemainingUSD)
	}
}

func TestBudgetCapStatus_MissingAxis(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetCapStatus(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/cap_status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetRecord_OK(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetRecord(srv)
	body := BudgetRecordReq{
		CostID:      "cost-001",
		AmountUSD:   0.05,
		AxisTags:    map[string]string{"project": "internal-platform-x", "stage": "design"},
		OperationID: "op-abc",
		WorkerID:    "w-1",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/record", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(srv.recorded) != 1 {
		t.Fatal("expected 1 recorded event")
	}
}

func TestBudgetRecord_MissingCostID(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetRecord(srv)
	body := BudgetRecordReq{AmountUSD: 0.01}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/record", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBudgetAxes_OK(t *testing.T) {
	srv := newBudgetServer()
	srv.axesData["cost-99"] = []BudgetAxisTag{
		{AxisName: "project", AxisValue: "zen"},
		{AxisName: "stage", AxisValue: "build"},
	}
	h := BudgetAxes(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/axes?cost_id=cost-99", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp []BudgetAxisTag
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) != 2 {
		t.Errorf("want 2 axes, got %d", len(resp))
	}
}

func TestBudgetAnomaly_OK(t *testing.T) {
	srv := newBudgetServer()
	srv.anomalyZ = 3.8
	h := BudgetAnomaly(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/anomaly?scope=project&value=internal-platform-x&window=3600", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp BudgetAnomalyResult
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ZScore != 3.8 {
		t.Errorf("z_score: got %f", resp.ZScore)
	}
}

func TestBudgetEvents_Empty(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetEvents(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/events", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestBudgetPause_OK(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetPause(srv)
	body := map[string]string{"scope": "project", "value": "internal-platform-x", "reason": "manual operator pause"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/pause", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.pauseState["project:internal-platform-x"] != "paused" {
		t.Error("scope not paused")
	}
}

func TestBudgetEvents_BadSinceParam(t *testing.T) {
	srv := newBudgetServer()
	h := BudgetEvents(srv)
	req := httptest.NewRequest(http.MethodGet, "/v1/budget/events?since=yesterday", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("BadSinceParam: got status %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "since") {
		t.Errorf("BadSinceParam: body %q does not mention 'since'", w.Body.String())
	}
}

func TestBudgetResume_OK(t *testing.T) {
	srv := newBudgetServer()
	srv.pauseState["stage:design"] = "paused"
	h := BudgetResume(srv)
	body := map[string]string{"scope": "stage", "value": "design"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/budget/resume", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.pauseState["stage:design"] != "running" {
		t.Error("scope not resumed")
	}
}
