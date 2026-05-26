package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/cli"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func newTestHandler(t *testing.T) (*handlers.MergeHandler, *httptest.Server) {
	t.Helper()
	cache := merge.NewCache()
	h := &handlers.MergeHandler{
		Cache:    cache,
		Doctrine: "max-scope",
		Scoring: merge.ScoringConfig{
			AlphaReviewerWeight:  1.0,
			BetaPatchSizePenalty: 0.5,
			GammaFlakePenalty:    0.25,
		},
	}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return h, srv
}

func TestMergeHandler_RegistersAllRoutes(t *testing.T) {
	_, srv := newTestHandler(t)

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/merge/inspect?id=abc"},
		{http.MethodPost, "/v1/merge/replay"},
		{http.MethodGet, "/v1/merge/score-explain?outcome_id=oid"},
		{http.MethodGet, "/v1/merge/baseline?session_id=sess"},
		{http.MethodGet, "/v1/merge/cache/status"},
		{http.MethodPost, "/v1/merge/cache/clear"},
		{http.MethodGet, "/v1/merge/config"},
		{http.MethodGet, "/v1/merge/anomaly?since=24h"},
	}
	for _, tc := range cases {
		req, err := http.NewRequestWithContext(context.Background(), tc.method, srv.URL+tc.path, nil)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s -> %d want 200", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestMergeHandler_Inspect_EchoesID(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/inspect?id=abc123")
	if err != nil {
		t.Fatalf("GET inspect: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeInspectResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RequestHash != "abc123" {
		t.Errorf("RequestHash = %s want abc123", got.RequestHash)
	}
}

func TestMergeHandler_Replay_DecodesSessionID(t *testing.T) {
	_, srv := newTestHandler(t)
	body := strings.NewReader(`{"session_id": "sess-99"}`)
	resp, err := http.Post(srv.URL+"/v1/merge/replay", "application/json", body)
	if err != nil {
		t.Fatalf("POST replay: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got cli.MergeReplayResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != "sess-99" {
		t.Errorf("SessionID = %s want sess-99", got.SessionID)
	}
}

func TestMergeHandler_ScoreExplain_EchoesOutcomeID(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/score-explain?outcome_id=out-7")
	if err != nil {
		t.Fatalf("GET score-explain: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeScoreExplainResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.WinnerID != "out-7" {
		t.Errorf("WinnerID = %s want out-7", got.WinnerID)
	}
	if !strings.Contains(got.Formula, "argmax") {
		t.Errorf("Formula = %s want substring 'argmax'", got.Formula)
	}
}

func TestMergeHandler_Baseline_EchoesSessionID(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/baseline?session_id=sess-baseline")
	if err != nil {
		t.Fatalf("GET baseline: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeBaselineShowResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionID != "sess-baseline" {
		t.Errorf("SessionID = %s want sess-baseline", got.SessionID)
	}
	if got.PassingSet == nil {
		t.Error("PassingSet should be non-nil empty slice (JSON-stable)")
	}
}

func TestMergeHandler_CacheStatus_FreshCacheZeroSize(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Size != 0 {
		t.Errorf("Size = %d want 0 (fresh cache)", got.Size)
	}
}

func TestMergeHandler_CacheStatus_EchoesCacheSize(t *testing.T) {
	h, srv := newTestHandler(t)

	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "base",
		EngineVersion: "v1",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat/x", HeadSHA: "h1"},
		},
	}
	h.Cache.Store(req, merge.MergeOutcome{IntegrationSHA: "h1", TestsPassed: true})

	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Size != 1 {
		t.Errorf("Size = %d want 1", got.Size)
	}
}

func TestMergeHandler_CacheStatus_NilCacheReturnsZero(t *testing.T) {
	h := &handlers.MergeHandler{Doctrine: "max-scope"}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Size != 0 {
		t.Errorf("Size = %d want 0 (nil cache)", got.Size)
	}
}

func TestMergeHandler_CacheStatus_MarkRebuiltSurfacesTimestamp(t *testing.T) {
	h, srv := newTestHandler(t)
	when := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	h.MarkRebuilt(when, "")

	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := when.Format(time.RFC3339)
	if got.LastRebuilt != want {
		t.Errorf("LastRebuilt = %s want %s", got.LastRebuilt, want)
	}
	if got.RebuildError != "" {
		t.Errorf("RebuildError = %q want empty", got.RebuildError)
	}
}

func TestMergeHandler_CacheStatus_RebuildErrorSurfaces(t *testing.T) {
	h, srv := newTestHandler(t)
	when := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	h.MarkRebuilt(when, "decode failure at gen 42")

	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.RebuildError != "decode failure at gen 42" {
		t.Errorf("RebuildError = %q want 'decode failure at gen 42'", got.RebuildError)
	}
}

func TestMergeHandler_CacheClear_EmptiesTheCache(t *testing.T) {
	h, srv := newTestHandler(t)

	req := merge.MergeRequest{
		TargetBranch:  "main",
		BaseSHA:       "base",
		EngineVersion: "v1",
		Candidates: []merge.MergeCandidate{
			{Branch: "feat/x", HeadSHA: "h1"},
		},
	}
	h.Cache.Store(req, merge.MergeOutcome{IntegrationSHA: "h1", TestsPassed: true})
	if h.Cache.Size() != 1 {
		t.Fatalf("setup failed: Size = %d want 1", h.Cache.Size())
	}

	resp, err := http.Post(srv.URL+"/v1/merge/cache/clear", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cache/clear: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if h.Cache.Size() != 0 {
		t.Errorf("Size after clear = %d want 0", h.Cache.Size())
	}

	var body map[string]any
	resp2, err := http.Post(srv.URL+"/v1/merge/cache/clear", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cache/clear (2): %v", err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cleared, _ := body["cleared"].(bool); !cleared {
		t.Errorf("cleared field = %v want true", body["cleared"])
	}
}

func TestMergeHandler_CacheClear_NilCacheReturns200(t *testing.T) {
	h := &handlers.MergeHandler{Doctrine: "max-scope"}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/v1/merge/cache/clear", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cache/clear: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d want 200 (nil cache)", resp.StatusCode)
	}
}

func TestMergeHandler_Config_SurfacesScoringFields(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/config")
	if err != nil {
		t.Fatalf("GET config: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeConfigShowResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Doctrine != "max-scope" {
		t.Errorf("Doctrine = %s want max-scope", got.Doctrine)
	}
	if got.Scoring.Alpha != 1.0 {
		t.Errorf("Alpha = %v want 1.0", got.Scoring.Alpha)
	}
	if got.Scoring.Beta != 0.5 {
		t.Errorf("Beta = %v want 0.5", got.Scoring.Beta)
	}
	if got.Scoring.Gamma != 0.25 {
		t.Errorf("Gamma = %v want 0.25", got.Scoring.Gamma)
	}
	if got.Timeouts.BaselineSec != 300 {
		t.Errorf("BaselineSec = %d want 300", got.Timeouts.BaselineSec)
	}
	if got.ModeMapping["80"] != "Degraded80" {
		t.Errorf("ModeMapping[80] = %s want Degraded80", got.ModeMapping["80"])
	}
	if got.AnomalyThresholds == nil {
		t.Error("AnomalyThresholds should be non-nil")
	}

	if rate, ok := got.AnomalyThresholds["flake_rate_threshold_pct"].(float64); !ok || rate != 5.0 {
		t.Errorf("flake_rate_threshold_pct = %v want 5.0", got.AnomalyThresholds["flake_rate_threshold_pct"])
	}
}

func TestMergeHandler_Anomaly_EmptyListByDefault(t *testing.T) {
	_, srv := newTestHandler(t)
	resp, err := http.Get(srv.URL + "/v1/merge/anomaly?since=24h")
	if err != nil {
		t.Fatalf("GET anomaly: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeAnomalyListResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Anomalies == nil {
		t.Error("Anomalies should be non-nil empty slice (JSON-stable)")
	}
	if len(got.Anomalies) != 0 {
		t.Errorf("Anomalies len = %d want 0 (F.4 thin shape)", len(got.Anomalies))
	}
}

func TestMergeHandler_MethodEnforcement_GETReturns405OnPOST(t *testing.T) {
	_, srv := newTestHandler(t)
	cases := []string{
		"/v1/merge/inspect?id=abc",
		"/v1/merge/score-explain?outcome_id=oid",
		"/v1/merge/baseline?session_id=sess",
		"/v1/merge/cache/status",
		"/v1/merge/config",
		"/v1/merge/anomaly?since=24h",
	}
	for _, p := range cases {
		resp, err := http.Post(srv.URL+p, "application/json", nil)
		if err != nil {
			t.Errorf("POST %s: %v", p, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("POST %s -> %d want 405", p, resp.StatusCode)
		}
	}
}

func TestMergeHandler_MethodEnforcement_POSTReturns405OnGET(t *testing.T) {
	_, srv := newTestHandler(t)
	cases := []string{
		"/v1/merge/replay",
		"/v1/merge/cache/clear",
	}
	for _, p := range cases {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Errorf("GET %s: %v", p, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET %s -> %d want 405", p, resp.StatusCode)
		}
	}
}

func TestNewMergeHandler_PopulatesStartedAt(t *testing.T) {
	cache := merge.NewCache()
	scoring := merge.ScoringConfig{AlphaReviewerWeight: 1.0}
	h := handlers.NewMergeHandler(nil, cache, nil, "default", scoring)
	if h == nil {
		t.Fatal("NewMergeHandler returned nil")
	}
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET cache/status: %v", err)
	}
	defer resp.Body.Close()
	var got cli.MergeCacheStatusResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.LastRebuilt == "" {
		t.Error("LastRebuilt should be non-empty (startedAt fallback)")
	}
	if _, err := time.Parse(time.RFC3339, got.LastRebuilt); err != nil {
		t.Errorf("LastRebuilt = %q is not RFC3339: %v", got.LastRebuilt, err)
	}
}
