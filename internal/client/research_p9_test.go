package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func startResearchP9TestServer(t *testing.T, route string, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(route, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewWithBaseURL(srv.URL), srv
}

func TestResearchHistory_OK(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/history",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("filter") != "cache_hit" {
				t.Errorf("filter: %q", q.Get("filter"))
			}
			if q.Get("project_id") != "p" {
				t.Errorf("project_id: %q", q.Get("project_id"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ResearchHistoryEntry{
					{Query: "max-scope", FindingsCount: 5, Source: "cache_hit_exact"},
				},
				"count": 1,
			})
		})
	rows, err := c.ResearchHistory(context.Background(), ResearchHistoryFilter{
		Filter:    "cache_hit",
		ProjectID: "p",
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
	if rows[0].Source != "cache_hit_exact" {
		t.Errorf("source: %q", rows[0].Source)
	}
}

func TestResearchHistory_SinceParam(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/history",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("since") != "1000" {
				t.Errorf("since: %q", r.URL.Query().Get("since"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ResearchHistoryEntry{},
				"count": 0,
			})
		})
	rows, err := c.ResearchHistory(context.Background(), ResearchHistoryFilter{Since: 1000})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil slice")
	}
}

func TestResearchHistory_NilItemsNormalized(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.ResearchHistory(context.Background(), ResearchHistoryFilter{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestResearchHistory_HTTPError(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/history",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"feature not configured","code":"plan9_research_unavailable"}`))
		})
	_, err := c.ResearchHistory(context.Background(), ResearchHistoryFilter{})
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 503 {
		t.Errorf("err shape: %v", err)
	}
}

func TestResearchCacheStatsP9_OK(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/stats",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(ResearchCacheStatsP9{
				TotalEntries:           10,
				TotalBytes:             1024,
				FreshnessLagSeconds:    30,
				RevalidationQueueDepth: 2,
				StuckQueriesCount:      0,
			})
		})
	stats, err := c.ResearchCacheStatsP9(context.Background(), "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stats.TotalEntries != 10 {
		t.Errorf("total_entries: %d", stats.TotalEntries)
	}
	if stats.FreshnessLagSeconds != 30 {
		t.Errorf("freshness_lag_seconds: %d", stats.FreshnessLagSeconds)
	}
}

func TestResearchCacheStatsP9_ProjectScoped(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/stats",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("project_id") != "internal-platform-x" {
				t.Errorf("project_id: %q", r.URL.Query().Get("project_id"))
			}
			json.NewEncoder(w).Encode(ResearchCacheStatsP9{TotalEntries: 3})
		})
	stats, err := c.ResearchCacheStatsP9(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if stats.TotalEntries != 3 {
		t.Errorf("total_entries: %d", stats.TotalEntries)
	}
}

func TestResearchCacheStatsP9_HTTPError(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/stats",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"db error"}`))
		})
	_, err := c.ResearchCacheStatsP9(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}

func TestResearchCacheInvalidate_OK(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "POST /v1/research/cache/invalidate",
		func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "poisoned query") {
				t.Errorf("body: %s", string(body))
			}
			json.NewEncoder(w).Encode(map[string]any{"invalidated": 7})
		})
	n, err := c.ResearchCacheInvalidate(context.Background(), "poisoned query")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 7 {
		t.Errorf("invalidated: %d", n)
	}
}

func TestResearchCacheInvalidate_ZeroCount(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "POST /v1/research/cache/invalidate",
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"invalidated": 0})
		})
	n, err := c.ResearchCacheInvalidate(context.Background(), "no-match-query")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestResearchCacheInvalidate_HTTPError(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "POST /v1/research/cache/invalidate",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"query required"}`))
		})
	_, err := c.ResearchCacheInvalidate(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 400 {
		t.Errorf("err shape: %v", err)
	}
}

func TestResearchCacheListP9_OK(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("project_id") != "internal-platform-x" {
				t.Errorf("project_id: %q", q.Get("project_id"))
			}
			if q.Get("source") != "https://arxiv.org" {
				t.Errorf("source: %q", q.Get("source"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ResearchCacheEntryP9{
					{Hash: "abc123", BytesSize: 2048, SourceURL: "https://arxiv.org/abs/1"},
				},
				"count": 1,
			})
		})
	rows, err := c.ResearchCacheListP9(context.Background(), "internal-platform-x", "https://arxiv.org")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("rows: %d", len(rows))
	}
	if rows[0].SourceURL != "https://arxiv.org/abs/1" {
		t.Errorf("source_url: %q", rows[0].SourceURL)
	}
}

func TestResearchCacheListP9_NoFilter(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/list",
		func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("project_id") != "" {
				t.Errorf("unexpected project_id: %q", q.Get("project_id"))
			}
			if q.Get("source") != "" {
				t.Errorf("unexpected source: %q", q.Get("source"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []ResearchCacheEntryP9{},
				"count": 0,
			})
		})
	rows, err := c.ResearchCacheListP9(context.Background(), "", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("expected non-nil slice")
	}
}

func TestResearchCacheListP9_NilItemsNormalized(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"items":null,"count":0}`))
		})
	rows, err := c.ResearchCacheListP9(context.Background(), "", "")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rows == nil {
		t.Error("nil items must be normalized to empty slice")
	}
}

func TestResearchCacheListP9_HTTPError(t *testing.T) {
	c, _ := startResearchP9TestServer(t, "GET /v1/research/cache/list",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"db error"}`))
		})
	_, err := c.ResearchCacheListP9(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("err shape: %v", err)
	}
}
