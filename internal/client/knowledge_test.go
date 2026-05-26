package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKnowledgeQueryHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/query" {
			t.Errorf("path = %s, want /v1/knowledge/query", r.URL.Path)
		}
		var req KnowledgeQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.FreeText != "hello" {
			t.Errorf("FreeText = %q, want hello", req.FreeText)
		}
		if req.Limit != 5 {
			t.Errorf("Limit = %d, want 5", req.Limit)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeQueryResponse{
			Rows: []KnowledgeResultRow{
				{
					FilePath:     "/tmp/x.md",
					ProjectAlias: "internal-platform-x",
					FileType:     "memory",
					Title:        "X",
					LastModified: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
					Score:        0.42,
					Snippet:      "[hello] world",
				},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.KnowledgeQuery(context.Background(), KnowledgeQueryRequest{FreeText: "hello", Limit: 5})
	if err != nil {
		t.Fatalf("KnowledgeQuery: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Title != "X" {
		t.Errorf("Title = %q, want X", rows[0].Title)
	}
	if rows[0].Score != 0.42 {
		t.Errorf("Score = %v, want 0.42", rows[0].Score)
	}
	if !strings.Contains(rows[0].Snippet, "hello") {
		t.Errorf("Snippet missing 'hello': %q", rows[0].Snippet)
	}
}

func TestKnowledgeQueryEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeQueryResponse{Rows: []KnowledgeResultRow{}})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.KnowledgeQuery(context.Background(), KnowledgeQueryRequest{})
	if err != nil {
		t.Fatalf("KnowledgeQuery: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil, want non-nil empty slice")
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want 0", len(rows))
	}
}

func TestKnowledgeQueryNullRowsTreatedAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":null}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	rows, err := c.KnowledgeQuery(context.Background(), KnowledgeQueryRequest{})
	if err != nil {
		t.Fatalf("KnowledgeQuery: %v", err)
	}
	if rows == nil {
		t.Error("rows is nil; client should return non-nil empty slice")
	}
}

func TestKnowledgeQuery503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "knowledge index not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeQuery(context.Background(), KnowledgeQueryRequest{})
	if err == nil {
		t.Fatal("expected 503 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestKnowledgeQueryFiltersWireField(t *testing.T) {
	got := KnowledgeQueryRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rows":[]}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeQuery(context.Background(), KnowledgeQueryRequest{
		FreeText:     "abc",
		ProjectAlias: []string{"internal-platform-x", "zen-swarm"},
		Type:         []string{"memory", "adr"},
		SinceSeconds: 86400,
		Limit:        7,
		CodeSymbol:   "fooFn",
	})
	if err != nil {
		t.Fatalf("KnowledgeQuery: %v", err)
	}
	if got.FreeText != "abc" {
		t.Errorf("FreeText = %q", got.FreeText)
	}
	if len(got.ProjectAlias) != 2 || got.ProjectAlias[0] != "internal-platform-x" {
		t.Errorf("ProjectAlias = %v", got.ProjectAlias)
	}
	if len(got.Type) != 2 || got.Type[0] != "memory" {
		t.Errorf("Type = %v", got.Type)
	}
	if got.SinceSeconds != 86400 {
		t.Errorf("SinceSeconds = %d", got.SinceSeconds)
	}
	if got.Limit != 7 {
		t.Errorf("Limit = %d", got.Limit)
	}
	if got.CodeSymbol != "fooFn" {
		t.Errorf("CodeSymbol = %q", got.CodeSymbol)
	}
}

func TestKnowledgeReindexHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/reindex" {
			t.Errorf("path = %s, want /v1/knowledge/reindex", r.URL.Path)
		}
		var req KnowledgeReindexRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if !req.Full {
			t.Errorf("Full = false, want true")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeReindexResponse{
			OK:      true,
			Indexed: 42,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.KnowledgeReindex(context.Background(), KnowledgeReindexRequest{Full: true})
	if err != nil {
		t.Fatalf("KnowledgeReindex: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK = false, want true")
	}
	if resp.Indexed != 42 {
		t.Errorf("Indexed = %d, want 42", resp.Indexed)
	}
}

func TestKnowledgeReindex503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "knowledge index not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeReindex(context.Background(), KnowledgeReindexRequest{})
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want 503", err)
	}
}

func TestKnowledgeStatsHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/stats" {
			t.Errorf("path = %s, want /v1/knowledge/stats", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(KnowledgeStatsResponse{
			TotalDocs: 123,
			ByType: map[string]int{
				"memory":   80,
				"research": 30,
				"adr":      13,
			},
			LastIndexedUnix: 1715000000,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	stats, err := c.KnowledgeStats(context.Background())
	if err != nil {
		t.Fatalf("KnowledgeStats: %v", err)
	}
	if stats.TotalDocs != 123 {
		t.Errorf("TotalDocs = %d, want 123", stats.TotalDocs)
	}
	if stats.ByType["memory"] != 80 {
		t.Errorf("ByType[memory] = %d, want 80", stats.ByType["memory"])
	}
	if stats.LastIndexedUnix != 1715000000 {
		t.Errorf("LastIndexedUnix = %d, want 1715000000", stats.LastIndexedUnix)
	}
}

func TestKnowledgeStatsNullByTypeTreatedAsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_docs":0,"by_type":null,"last_indexed_unix":0}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	stats, err := c.KnowledgeStats(context.Background())
	if err != nil {
		t.Fatalf("KnowledgeStats: %v", err)
	}
	if stats.ByType == nil {
		t.Error("ByType is nil; client should normalise to empty map")
	}
}

func TestKnowledgeStats503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "knowledge index not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.KnowledgeStats(context.Background())
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want 503", err)
	}
}
