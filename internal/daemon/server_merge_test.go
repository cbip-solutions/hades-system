package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

func TestServer_MergeRoutesReturnServiceUnavailableWhenUnwired(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	type call struct {
		method string
		path   string
		body   string
	}
	calls := []call{
		{http.MethodGet, "/v1/merge/inspect?id=abc", ""},
		{http.MethodPost, "/v1/merge/replay", `{"session_id":"s-1"}`},
		{http.MethodGet, "/v1/merge/score-explain?outcome_id=o-1", ""},
		{http.MethodGet, "/v1/merge/baseline?session_id=s-1", ""},
		{http.MethodGet, "/v1/merge/cache/status", ""},
		{http.MethodPost, "/v1/merge/cache/clear", ""},
		{http.MethodGet, "/v1/merge/config", ""},
		{http.MethodGet, "/v1/merge/anomaly?since=24h", ""},
	}

	for _, c := range calls {
		req, err := http.NewRequestWithContext(context.Background(), c.method, ts.URL+c.path, strings.NewReader(c.body))
		if err != nil {
			t.Fatalf("%s %s: build req: %v", c.method, c.path, err)
		}
		if c.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: do: %v", c.method, c.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status=%d body=%s, want 503 (no merge handler injected)",
				c.method, c.path, resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}
}

func TestServer_MergeRoutesReachInjectedHandler(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	cache := merge.NewCache()
	scoring := merge.ScoringConfig{
		AlphaReviewerWeight:  1.0,
		BetaPatchSizePenalty: 0.5,
		GammaFlakePenalty:    0.25,
	}
	h := handlers.NewMergeHandler(nil, cache, nil, "max-scope", scoring)
	srv.SetMergeHandler(h)

	if got := srv.MergeHandler(); got != h {
		t.Errorf("MergeHandler() accessor mismatch: got %p, want %p", got, h)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	t.Run("cache_status", func(t *testing.T) {
		resp, err := ts.Client().Get(ts.URL + "/v1/merge/cache/status")
		if err != nil {
			t.Fatalf("GET /v1/merge/cache/status: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, body)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := got["size"]; !ok {
			t.Errorf("response missing 'size' field: %v", got)
		}
		if _, ok := got["hit_rate_pct"]; !ok {
			t.Errorf("response missing 'hit_rate_pct' field: %v", got)
		}
	})

	t.Run("config", func(t *testing.T) {
		resp, err := ts.Client().Get(ts.URL + "/v1/merge/config")
		if err != nil {
			t.Fatalf("GET /v1/merge/config: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, body)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if d, _ := got["doctrine"].(string); d != "max-scope" {
			t.Errorf("doctrine = %q, want %q", d, "max-scope")
		}
		scoringMap, ok := got["scoring"].(map[string]any)
		if !ok {
			t.Fatalf("scoring field missing or wrong shape: %v", got)
		}
		if a, _ := scoringMap["alpha"].(float64); a != 1.0 {
			t.Errorf("scoring.alpha = %v, want 1.0", scoringMap["alpha"])
		}
		if b, _ := scoringMap["beta"].(float64); b != 0.5 {
			t.Errorf("scoring.beta = %v, want 0.5", scoringMap["beta"])
		}
		if g, _ := scoringMap["gamma"].(float64); g != 0.25 {
			t.Errorf("scoring.gamma = %v, want 0.25", scoringMap["gamma"])
		}
	})

	echoCalls := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"inspect", http.MethodGet, "/v1/merge/inspect?id=req-hash-1", ""},
		{"replay", http.MethodPost, "/v1/merge/replay", `{"session_id":"s-1"}`},
		{"score-explain", http.MethodGet, "/v1/merge/score-explain?outcome_id=o-1", ""},
		{"baseline", http.MethodGet, "/v1/merge/baseline?session_id=s-1", ""},
		{"anomaly", http.MethodGet, "/v1/merge/anomaly?since=24h", ""},
	}
	for _, c := range echoCalls {
		c := c
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), c.method, ts.URL+c.path, strings.NewReader(c.body))
			if err != nil {
				t.Fatalf("build req: %v", err)
			}
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s %s: status=%d body=%s, want 200", c.method, c.path, resp.StatusCode, strings.TrimSpace(string(body)))
			}
		})
	}

	t.Run("cache_clear", func(t *testing.T) {
		resp, err := ts.Client().Post(ts.URL+"/v1/merge/cache/clear", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /v1/merge/cache/clear: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("status=%d body=%s, want 200", resp.StatusCode, body)
		}
		var got map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if cleared, _ := got["cleared"].(bool); !cleared {
			t.Errorf("cleared = %v, want true: %v", got["cleared"], got)
		}
		if intact, _ := got["eventlog_intact"].(bool); !intact {
			t.Errorf("eventlog_intact = %v, want true: %v", got["eventlog_intact"], got)
		}
	})
}

func TestServer_MergeHandlerSetterRoundTrip(t *testing.T) {
	st := newTestStore(t)
	srv := New(st, Config{})

	if got := srv.MergeHandler(); got != nil {
		t.Errorf("pre-set MergeHandler() = %p, want nil", got)
	}

	cache := merge.NewCache()
	h := handlers.NewMergeHandler(nil, cache, nil, "max-scope", merge.ScoringConfig{})
	srv.SetMergeHandler(h)
	if got := srv.MergeHandler(); got != h {
		t.Errorf("post-set MergeHandler() = %p, want %p", got, h)
	}

	srv.SetMergeHandler(nil)
	if got := srv.MergeHandler(); got != nil {
		t.Errorf("post-nil-reset MergeHandler() = %p, want nil", got)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	resp, err := ts.Client().Get(ts.URL + "/v1/merge/cache/status")
	if err != nil {
		t.Fatalf("GET /v1/merge/cache/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("post-nil-reset status=%d, want 503", resp.StatusCode)
	}
}
