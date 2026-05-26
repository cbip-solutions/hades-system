package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestResearchCacheGet_Hit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/get", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hash") != "abc" {
			t.Errorf("hash: %q", r.URL.Query().Get("hash"))
		}
		_ = json.NewEncoder(w).Encode(client.ResearchCacheGetResp{
			Hit: true, ResponseJSON: `{"x":1}`, TTLUnix: 9999999999,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.ResearchCacheGet(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ResearchCacheGet: %v", err)
	}
	if !resp.Hit || resp.ResponseJSON != `{"x":1}` {
		t.Errorf("got %+v", resp)
	}
}

func TestResearchCacheGet_Miss(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/get", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"hit":false}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.ResearchCacheGet(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ResearchCacheGet: %v", err)
	}
	if resp.Hit {
		t.Errorf("expected miss, got %+v", resp)
	}
}

func TestResearchCacheGet_RequiresHash(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	_, err := c.ResearchCacheGet(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestResearchCacheSet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/set", func(w http.ResponseWriter, r *http.Request) {
		var req client.ResearchCacheSetReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Hash != "h1" {
			t.Errorf("hash: %q", req.Hash)
		}
		_ = json.NewEncoder(w).Encode(client.ResearchCacheSetResp{Stored: true, TTLUnix: 1234})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	resp, err := c.ResearchCacheSet(context.Background(), client.ResearchCacheSetReq{
		Hash: "h1", ResponseJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("ResearchCacheSet: %v", err)
	}
	if !resp.Stored {
		t.Errorf("not stored")
	}
}

func TestResearchCacheList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ResearchCacheEntry{
				{Hash: "abc", BytesSize: 100, CreatedAt: 1000, TTLUnix: 2000},
			},
			"count": 1,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	items, err := c.ResearchCacheList(context.Background(), 50, 0)
	if err != nil {
		t.Fatalf("ResearchCacheList: %v", err)
	}
	if len(items) != 1 || items[0].Hash != "abc" {
		t.Errorf("got %+v", items)
	}
}

func TestResearchCacheClear(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/clear", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]int64
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["older_than_seconds"] != 86400 {
			t.Errorf("body: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"deleted": 12})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	n, err := c.ResearchCacheClear(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("ResearchCacheClear: %v", err)
	}
	if n != 12 {
		t.Errorf("deleted: %d", n)
	}
}

func TestResearchCacheClear_RequiresPositive(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	if _, err := c.ResearchCacheClear(context.Background(), 0); err == nil {
		t.Fatal("expected error for zero")
	}
}

func TestResearchCacheStats(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheStats{
			TotalEntries: 50, TotalBytes: 100000, ExpiredCount: 3,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	stats, err := c.ResearchCacheStatsCall(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalEntries != 50 || stats.ExpiredCount != 3 {
		t.Errorf("got %+v", stats)
	}
}

func TestResearchCacheShow_Hit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/show", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ResearchCacheShow{
			Hash: "abc", ResponseJSON: `{"y":1}`, BytesSize: 7, TTLUnix: 9999999999,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	show, hit, err := c.ResearchCacheShow(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !hit || show.Hash != "abc" {
		t.Errorf("got %+v", show)
	}
}

func TestResearchCacheShow_Miss(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/show", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"not found"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	_, hit, err := c.ResearchCacheShow(context.Background(), "abc")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if hit {
		t.Errorf("expected miss")
	}
}

func TestResearchCacheShow_RequiresHash(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	if _, _, err := c.ResearchCacheShow(context.Background(), ""); err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheGet_OtherError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/get", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheGet(context.Background(), "abc"); err == nil {
		t.Fatal("expected non-404 error")
	}
}

func TestResearchCacheSet_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/set", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheSet(context.Background(), client.ResearchCacheSetReq{Hash: "h"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheList_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheList(context.Background(), 0, 0); err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheList_Pagination(t *testing.T) {
	var captured string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/list", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []client.ResearchCacheEntry{},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheList(context.Background(), 25, 50); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(captured, "limit=25") || !strings.Contains(captured, "offset=50") {
		t.Errorf("query: %s", captured)
	}
}

func TestResearchCacheClear_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/clear", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheClear(context.Background(), time.Hour); err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheStats_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, err := c.ResearchCacheStatsCall(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestResearchCacheShow_OtherError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/research/cache/show", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	if _, _, err := c.ResearchCacheShow(context.Background(), "abc"); err == nil {
		t.Fatal("expected non-404 error")
	}
}

func TestResearchSourcesResolve_DaemonReturnsList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "max-scope",
			"research": map[string]any{
				"sources": []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	got, err := c.ResearchSourcesResolve(context.Background())
	if err != nil {
		t.Fatalf("ResearchSourcesResolve: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("expected 5 sources, got %d", len(got))
	}
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	for _, want := range []string{"web_search", "arxiv", "github_search", "code_graph", "ecosystem_docs"} {
		if !names[want] {
			t.Errorf("missing %q", want)
		}
	}
}

func TestResearchSourcesResolve_DaemonStubReturnsFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/doctrine/state", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "max-scope"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := client.NewWithBaseURL(srv.URL)
	got, err := c.ResearchSourcesResolve(context.Background())
	if err != nil {
		t.Fatalf("ResearchSourcesResolve: %v", err)
	}
	if len(got) == 0 {
		t.Error("stub state should fall back to default catalog, got empty")
	}
}

func TestResearchSourcesResolve_DaemonDown(t *testing.T) {
	c := client.NewWithBaseURL("http://127.0.0.1:1")
	got, err := c.ResearchSourcesResolve(context.Background())
	if err != nil {
		t.Fatalf("daemon-down should fall back without error: %v", err)
	}
	if len(got) == 0 {
		t.Error("expected fallback sources")
	}
}

func TestResearchSourcesFromList_DescriptionEnrichment(t *testing.T) {
	got := client.ResearchSourcesFromList([]string{"web_search", "future-source"})
	if len(got) != 2 {
		t.Fatalf("got %d entries", len(got))
	}
	for _, s := range got {
		if s.Description == "" {
			t.Errorf("missing description for %q", s.Name)
		}
		if s.Source != "doctrine" {
			t.Errorf("Source field should be 'doctrine': %+v", s)
		}
	}
}

func TestResearchSourcesResolveFromState_GoFieldShape(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{
		"Research": map[string]any{
			"Sources": []any{"web_search", "arxiv"},
		},
	}
	got, ok := c.ResearchSourcesResolveFromState(state)
	if !ok {
		t.Fatal("Go-field-shape state should resolve")
	}
	if len(got) != 2 {
		t.Errorf("got %d sources", len(got))
	}
}

func TestResearchSourcesResolveFromState_StubReturnsEmpty(t *testing.T) {
	c := client.NewWithBaseURL("http://x")
	state := client.DoctrineState{"name": "max-scope"}
	got, ok := c.ResearchSourcesResolveFromState(state)
	if ok {
		t.Error("stub state should return ok=false")
	}
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestResearchSourcesDefault_ReturnsThree(t *testing.T) {
	got := client.ResearchSourcesDefault()
	if len(got) != 3 {
		t.Errorf("default should have 3 entries, got %d", len(got))
	}
}
