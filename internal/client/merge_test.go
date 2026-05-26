package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestMergeClient_Inspect_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/merge/inspect" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "abc" {
			t.Errorf("id = %s", r.URL.Query().Get("id"))
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(client.MergeInspectResult{
			RequestHash:    "abc",
			GenerationID:   42,
			Mode:           "Normal",
			WinnerID:       "h1",
			IntegrationSHA: "ints",
			TestsPassed:    true,
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.Inspect(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if res.WinnerID != "h1" {
		t.Errorf("WinnerID = %s want h1", res.WinnerID)
	}
	if res.GenerationID != 42 {
		t.Errorf("GenerationID = %d want 42", res.GenerationID)
	}
	if !res.TestsPassed {
		t.Errorf("TestsPassed = false want true")
	}
}

func TestMergeClient_Inspect_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	if _, err := c.Inspect(context.Background(), "abc"); err == nil {
		t.Fatal("expected error on 500")
	} else if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v want 500 substring", err)
	}
}

func TestMergeClient_Replay_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s want POST", r.Method)
		}
		if r.URL.Path != "/v1/merge/replay" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %s", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["session_id"] != "sess-1" {
			t.Errorf("session_id = %s want sess-1", body["session_id"])
		}
		_ = json.NewEncoder(w).Encode(client.MergeReplayResult{
			SessionID:      "sess-1",
			EventsReplayed: 17,
			OutcomeMatch:   true,
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.Replay(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.EventsReplayed != 17 || !res.OutcomeMatch {
		t.Errorf("got %+v", res)
	}
}

func TestMergeClient_Replay_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	if _, err := c.Replay(context.Background(), "sess-1"); err == nil {
		t.Fatal("expected error on 502")
	}
}

func TestMergeClient_ScoreExplain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("outcome_id") != "out-7" {
			t.Errorf("outcome_id = %s", r.URL.Query().Get("outcome_id"))
		}
		_ = json.NewEncoder(w).Encode(client.MergeScoreExplainResult{
			WinnerID:        "h1",
			TiebreakApplied: true,
			AllScores:       map[string]float64{"h1": 0.92, "h2": 0.91},
			Formula:         "argmax(test_pass) → tiebreak(α·reviewer − β·patch_size − γ·flake)",
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.ScoreExplain(context.Background(), "out-7")
	if err != nil {
		t.Fatalf("ScoreExplain: %v", err)
	}
	if !res.TiebreakApplied {
		t.Errorf("TiebreakApplied = false want true")
	}
	if len(res.AllScores) != 2 {
		t.Errorf("AllScores len = %d want 2", len(res.AllScores))
	}
}

func TestMergeClient_BaselineShow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("session_id") != "sess-9" {
			t.Errorf("session_id = %s", r.URL.Query().Get("session_id"))
		}
		_ = json.NewEncoder(w).Encode(client.MergeBaselineShowResult{
			SessionID:  "sess-9",
			BaseSHA:    "deadbeef",
			PassingSet: []string{"t1", "t2", "t3"},
			DurationMs: 4242,
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.BaselineShow(context.Background(), "sess-9")
	if err != nil {
		t.Fatalf("BaselineShow: %v", err)
	}
	if res.BaseSHA != "deadbeef" {
		t.Errorf("BaseSHA = %s want deadbeef", res.BaseSHA)
	}
	if len(res.PassingSet) != 3 {
		t.Errorf("PassingSet len = %d want 3", len(res.PassingSet))
	}
	if res.DurationMs != 4242 {
		t.Errorf("DurationMs = %d want 4242", res.DurationMs)
	}
}

func TestMergeClient_CacheStatus_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/merge/cache/status" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.MergeCacheStatusResult{
			Size:        47,
			HitRatePct:  23.5,
			LastRebuilt: "2026-05-05T12:00:00Z",
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.CacheStatus(context.Background())
	if err != nil {
		t.Fatalf("CacheStatus: %v", err)
	}
	if res.Size != 47 {
		t.Errorf("Size = %d want 47", res.Size)
	}
	if res.HitRatePct != 23.5 {
		t.Errorf("HitRatePct = %v want 23.5", res.HitRatePct)
	}
}

func TestMergeClient_CacheClear(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s want POST", r.Method)
		}
		if r.URL.Path != "/v1/merge/cache/clear" {
			t.Errorf("path = %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("body should be empty, got %q", string(body))
		}
		// Daemon currently returns a small status object; client
		// MUST tolerate a missing body too (out=nil branch). Echo
		// the production shape.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"cleared":         true,
			"eventlog_intact": true,
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	if err := c.CacheClear(context.Background()); err != nil {
		t.Fatalf("CacheClear: %v", err)
	}
}

func TestMergeClient_CacheClear_NoBodyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)

	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	if err := c.CacheClear(context.Background()); err != nil {
		t.Fatalf("CacheClear with empty body: %v", err)
	}
}

func TestMergeClient_ConfigShow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.MergeConfigShowResult{
			Doctrine: "max-scope",
			Scoring: client.MergeScoringConfig{
				Alpha: 1.0, Beta: 0.5, Gamma: 0.25,
			},
			Timeouts: client.MergeTimeoutsConfig{
				BaselineSec: 300, CandidateSec: 600, FlakeRerunSec: 300,
			},
			ModeMapping: map[string]string{
				"60": "Degraded60", "80": "Degraded80", "90": "ctx_cancel",
			},
			AnomalyThresholds: map[string]any{
				"flake_rate_threshold_pct": 5.0,
			},
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.ConfigShow(context.Background())
	if err != nil {
		t.Fatalf("ConfigShow: %v", err)
	}
	if res.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %s want max-scope", res.Doctrine)
	}
	if res.Scoring.Alpha != 1.0 {
		t.Errorf("Alpha = %v want 1.0", res.Scoring.Alpha)
	}
	if res.Timeouts.BaselineSec != 300 {
		t.Errorf("BaselineSec = %d want 300", res.Timeouts.BaselineSec)
	}
	if res.ModeMapping["80"] != "Degraded80" {
		t.Errorf("ModeMapping[80] = %s want Degraded80", res.ModeMapping["80"])
	}
}

func TestMergeClient_AnomalyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("since") != "24h" {
			t.Errorf("since = %s want 24h", r.URL.Query().Get("since"))
		}
		_ = json.NewEncoder(w).Encode(client.MergeAnomalyListResult{
			Anomalies: []client.MergeAnomalyEntry{
				{
					Type:            "FlakeRateAboveThreshold",
					Severity:        "High",
					ThresholdBreach: "rate 12.00% > 5.00%",
					Detail:          "γ scoring penalty consistently activated",
					Timestamp:       "2026-05-05T10:00:00Z",
				},
			},
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.AnomalyList(context.Background(), "24h")
	if err != nil {
		t.Fatalf("AnomalyList: %v", err)
	}
	if len(res.Anomalies) != 1 {
		t.Fatalf("Anomalies len = %d want 1", len(res.Anomalies))
	}
	a := res.Anomalies[0]
	if a.Type != "FlakeRateAboveThreshold" || a.Severity != "High" {
		t.Errorf("anomaly = %+v", a)
	}
}

func TestMergeClient_AnomalyList_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.MergeAnomalyListResult{
			Anomalies: []client.MergeAnomalyEntry{},
		})
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	res, err := c.AnomalyList(context.Background(), "7d")
	if err != nil {
		t.Fatalf("AnomalyList: %v", err)
	}
	if len(res.Anomalies) != 0 {
		t.Errorf("expected empty Anomalies, got %d", len(res.Anomalies))
	}
}

func TestMergeClient_SatisfiesInterface(t *testing.T) {
	var _ client.MergeClient = (*client.MergeHTTPClient)(nil)

	c := client.NewMergeClient(http.DefaultClient, "http://example.invalid")
	if c == nil {
		t.Fatal("NewMergeClient returned nil")
	}
}

func TestMergeClient_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := client.NewMergeClient(srv.Client(), srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Inspect(ctx, "abc"); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
