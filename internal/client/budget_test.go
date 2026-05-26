package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestBudgetCapStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("axis") != "stage" || r.URL.Query().Get("value") != "design" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(client.BudgetCapStatus{RemainingUSD: 25.5, Blocked: false})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	cs, err := c.BudgetCapStatusCall(context.Background(), "stage", "design")
	if err != nil {
		t.Fatalf("BudgetCapStatusCall: %v", err)
	}
	if cs.RemainingUSD != 25.5 || cs.Blocked {
		t.Errorf("got %+v", cs)
	}
}

func TestBudgetCapStatus_RequiresArgs(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	if _, err := c.BudgetCapStatusCall(context.Background(), "", ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetRecord(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/record", func(w http.ResponseWriter, r *http.Request) {
		var req client.BudgetRecordReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.CostID != "c1" {
			t.Errorf("body: %+v", req)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]bool{"recorded": true})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if err := c.BudgetRecord(context.Background(), client.BudgetRecordReq{CostID: "c1", AmountUSD: 0.5}); err != nil {
		t.Fatalf("BudgetRecord: %v", err)
	}
}

func TestBudgetAxes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/axes", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]client.BudgetAxisTag{
			{AxisName: "project", AxisValue: "internal-platform-x"},
			{AxisName: "stage", AxisValue: "design"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	tags, err := c.BudgetAxes(context.Background(), "cost_42")
	if err != nil {
		t.Fatalf("BudgetAxes: %v", err)
	}
	if len(tags) != 2 || tags[0].AxisName != "project" {
		t.Errorf("got %+v", tags)
	}
}

func TestBudgetAxes_RequiresCostID(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	if _, err := c.BudgetAxes(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAnomaly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/anomaly", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("window") != "3600" {
			t.Errorf("window: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(client.BudgetAnomaly{ZScore: 5.7, Mean: 1.0, StdDev: 0.5, Samples: 100})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	a, err := c.BudgetAnomalyCall(context.Background(), "project", "internal-platform-x", 3600)
	if err != nil {
		t.Fatalf("BudgetAnomalyCall: %v", err)
	}
	if a.ZScore != 5.7 {
		t.Errorf("got %+v", a)
	}
}

func TestBudgetEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/events", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "since=1000") {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []client.BudgetEvent{
				{ID: "e1", Scope: "stage", Value: "design", EventType: "cap_set", AmountUSD: 5.0, OccurredAt: 1234},
			},
			"count": 1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	events, err := c.BudgetEvents(context.Background(), 1000, 50)
	if err != nil {
		t.Fatalf("BudgetEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "cap_set" {
		t.Errorf("got %+v", events)
	}
}

func TestBudgetPauseRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		var body client.BudgetPauseReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(client.BudgetPauseResp{State: "paused", Scope: body.Scope, Value: body.Value})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.BudgetPauseCall(context.Background(), client.BudgetPauseReq{Scope: "project", Value: "internal-platform-x", Reason: "x"})
	if err != nil {
		t.Fatalf("BudgetPauseCall: %v", err)
	}
	if resp.State != "paused" {
		t.Errorf("got %+v", resp)
	}
}

func TestBudgetResume(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.BudgetPauseResp{State: "running"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.BudgetResumeCall(context.Background(), client.BudgetResumeReq{Scope: "stage", Value: "design"})
	if err != nil {
		t.Fatalf("BudgetResumeCall: %v", err)
	}
	if resp.State != "running" {
		t.Errorf("got %+v", resp)
	}
}

func TestBudgetCapStatus_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/cap_status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.BudgetCapStatusCall(context.Background(), "stage", "design"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAxes_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/axes", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.BudgetAxes(context.Background(), "c1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAnomaly_RequiresArgs(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	if _, err := c.BudgetAnomalyCall(context.Background(), "", "", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetAnomaly_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/anomaly", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.BudgetAnomalyCall(context.Background(), "project", "x", 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetPause_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/pause", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.BudgetPauseCall(context.Background(), client.BudgetPauseReq{Scope: "x", Value: "y"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestBudgetResume_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/budget/resume", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.BudgetResumeCall(context.Background(), client.BudgetResumeReq{Scope: "x", Value: "y"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPauseModes_HasThreeCanonical(t *testing.T) {
	modes := client.PauseModes()
	if len(modes) != 3 {
		t.Fatalf("want 3 modes, got %d", len(modes))
	}
	names := map[string]bool{}
	defaults := 0
	var defaultName string
	for _, m := range modes {
		names[m.Name] = true
		if m.Default {
			defaults++
			defaultName = m.Name
		}
	}
	// Names MUST match the doctrine schema's accepted `pause_mode` values
	// (descriptive | quiet | fail_loud); operators copy these into
	// zenswarm.toml so a drift here makes the doctrine validator reject
	// downstream config (review F-2).
	for _, want := range []string{"descriptive", "quiet", "fail_loud"} {
		if !names[want] {
			t.Errorf("missing canonical mode %q", want)
		}
	}

	for _, bad := range []string{"paused_descriptive", "paused_quiet", "paused_after_apply"} {
		if names[bad] {
			t.Errorf("legacy/non-canonical mode %q must not appear in catalog", bad)
		}
	}
	// Exactly one default and it MUST be the max-scope builtin choice
	// (see internal/doctrine/builtin.go: MaxScopeBuiltin sets
	// Budget.PauseMode = "descriptive").
	if defaults != 1 {
		t.Errorf("want exactly one default mode, got %d", defaults)
	}
	if defaultName != "descriptive" {
		t.Errorf("default mode = %q, want %q (max-scope OOTB)", defaultName, "descriptive")
	}
}
