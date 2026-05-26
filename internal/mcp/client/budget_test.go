package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/mcp/client"
)

func fakeBudgetServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		axis := r.URL.Query().Get("axis")
		value := r.URL.Query().Get("value")
		resp := client.CapStatusResponse{
			Axis:         axis,
			Value:        value,
			RemainingUSD: 4.20,
			Allowed:      true,
			BlockedScope: "",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req client.RecordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.CostID == "" {
			http.Error(w, "missing cost_id", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("/v1/budget/axes", func(w http.ResponseWriter, r *http.Request) {
		costID := r.URL.Query().Get("cost_id")
		tags := []client.AxisTag{
			{CostID: costID, Axis: "project", Value: "internal-platform-x"},
			{CostID: costID, Axis: "stage", Value: "design"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tags)
	})

	mux.HandleFunc("/v1/budget/anomaly", func(w http.ResponseWriter, r *http.Request) {
		scope := r.URL.Query().Get("scope")
		resp := client.AnomalyResponse{
			Scope:   scope,
			ZScore:  1.2,
			Mean:    0.50,
			Std:     0.10,
			Samples: 100,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		events := []client.BudgetEvent{
			{
				ID:        "evt-001",
				Type:      "cap_hit",
				Scope:     "project",
				CostUSD:   5.00,
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(events)
	})

	mux.HandleFunc("/v1/budget/rollup", func(w http.ResponseWriter, r *http.Request) {
		resp := client.RollupResponse{
			TotalUSD:  4.20,
			Breakdown: map[string]float64{"design": 1.10, "build": 3.10},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := client.PauseStateResponse{
			Scope:     "stage",
			Active:    true,
			PauseMode: "descriptive",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := client.PauseStateResponse{
			Scope:     "stage",
			Active:    false,
			PauseMode: "descriptive",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	return httptest.NewServer(mux)
}

func newBudgetClient(t *testing.T, srv *httptest.Server) *client.BudgetClient {
	t.Helper()
	c := newTestClient(t, srv)
	return client.NewBudgetClient(c)
}

func TestBudgetCapStatus_AllowedResponse(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	resp, err := bc.CapStatus(context.Background(), "stage", "design")
	if err != nil {
		t.Fatalf("CapStatus: %v", err)
	}
	if !resp.Allowed {
		t.Error("expected Allowed=true")
	}
	if resp.RemainingUSD <= 0 {
		t.Errorf("RemainingUSD = %f, want > 0", resp.RemainingUSD)
	}
	if resp.Axis != "stage" {
		t.Errorf("Axis = %q, want 'stage'", resp.Axis)
	}
	if resp.Value != "design" {
		t.Errorf("Value = %q, want 'design'", resp.Value)
	}
}

func TestBudgetRecord_Success(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	req := client.RecordRequest{
		CostID:    "cost-abc-123",
		AmountUSD: 0.042,
		AxisTags: []client.AxisTag{
			{Axis: "project", Value: "internal-platform-x"},
			{Axis: "stage", Value: "build"},
			{Axis: "task_id", Value: "task-007"},
		},
	}
	if err := bc.Record(context.Background(), req); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestBudgetRecord_MissingCostIDRejected(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)

	req := client.RecordRequest{
		CostID:    "",
		AmountUSD: 0.01,
	}
	if err := bc.Record(context.Background(), req); err == nil {
		t.Fatal("expected error for missing cost_id, got nil")
	}
}

func TestBudgetAxes_ReturnsTags(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	tags, err := bc.Axes(context.Background(), "cost-xyz")
	if err != nil {
		t.Fatalf("Axes: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("len(tags) = %d, want 2", len(tags))
	}
	for _, tag := range tags {
		if tag.CostID != "cost-xyz" {
			t.Errorf("tag.CostID = %q, want 'cost-xyz'", tag.CostID)
		}
	}
}

func TestBudgetAnomalyCheck_ReturnsZScore(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	resp, err := bc.AnomalyCheck(context.Background(), "project", "1h")
	if err != nil {
		t.Fatalf("AnomalyCheck: %v", err)
	}
	if resp.ZScore <= 0 {
		t.Errorf("ZScore = %f, want > 0", resp.ZScore)
	}
	if resp.Samples <= 0 {
		t.Errorf("Samples = %d, want > 0", resp.Samples)
	}
}

func TestBudgetEvents_ReturnsList(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	events, err := bc.Events(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one event, got none")
	}
	if events[0].ID == "" {
		t.Error("event ID is empty")
	}
}

func TestBudgetRollup_ReturnsBreakdown(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	resp, err := bc.Rollup(context.Background(), "stage", "design", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("Rollup: %v", err)
	}
	if resp.TotalUSD <= 0 {
		t.Errorf("TotalUSD = %f, want > 0", resp.TotalUSD)
	}
	if len(resp.Breakdown) == 0 {
		t.Error("Breakdown is empty, want non-empty map")
	}
}

func TestBudgetRollup_ZeroSince(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)

	resp, err := bc.Rollup(context.Background(), "project", "internal-platform-x", time.Time{})
	if err != nil {
		t.Fatalf("Rollup zero since: %v", err)
	}
	if resp == nil {
		t.Fatal("Rollup returned nil response")
	}
}

func TestBudgetPause_ActivatesPause(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	resp, err := bc.Pause(context.Background(), "stage", "manual operator halt")
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if !resp.Active {
		t.Error("Pause: expected active=true")
	}
	if resp.Scope != "stage" {
		t.Errorf("Pause: Scope = %q, want 'stage'", resp.Scope)
	}
}

func TestBudgetResume_DeactivatesPause(t *testing.T) {
	srv := fakeBudgetServer(t)
	defer srv.Close()

	bc := newBudgetClient(t, srv)
	resp, err := bc.Resume(context.Background(), "stage")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resp.Active {
		t.Error("Resume: expected active=false")
	}
}
