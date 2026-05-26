package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsReindex_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/ecosystem/reindex" {
			t.Errorf("path = %s, want /v1/knowledge/ecosystem/reindex", r.URL.Path)
		}
		var req DocsReindexRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.Ecosystem != "go" {
			t.Errorf("Ecosystem = %q, want go", req.Ecosystem)
		}
		if !req.DeltaOnly {
			t.Errorf("DeltaOnly = false, want true")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DocsReindexResponse{
			PackagesIngested:   42,
			ChunksIngested:     1234,
			SymbolsRegistered:  567,
			ChangeNodesCreated: 12,
			ElapsedMs:          15000,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.DocsReindex(context.Background(), DocsReindexRequest{
		Ecosystem: "go", DeltaOnly: true,
	})
	if err != nil {
		t.Fatalf("DocsReindex: %v", err)
	}
	if resp.PackagesIngested != 42 {
		t.Errorf("PackagesIngested = %d, want 42", resp.PackagesIngested)
	}
	if resp.ChunksIngested != 1234 {
		t.Errorf("ChunksIngested = %d, want 1234", resp.ChunksIngested)
	}
	if resp.ElapsedMs != 15000 {
		t.Errorf("ElapsedMs = %d, want 15000", resp.ElapsedMs)
	}
}

func TestDocsReindex_EmptyRequestBody(t *testing.T) {
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		rawBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if _, err := c.DocsReindex(context.Background(), DocsReindexRequest{}); err != nil {
		t.Fatalf("DocsReindex: %v", err)
	}
	if strings.Contains(rawBody, `"ecosystem"`) {
		t.Errorf("empty Ecosystem should be omitted, got: %s", rawBody)
	}
	if strings.Contains(rawBody, `"version"`) {
		t.Errorf("empty Version should be omitted, got: %s", rawBody)
	}

	if !strings.Contains(rawBody, `"delta_only"`) {
		t.Errorf("delta_only must be present (no omitempty), got: %s", rawBody)
	}
}

func TestDocsReindex_503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ingester offline", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.DocsReindex(context.Background(), DocsReindexRequest{})
	if err == nil {
		t.Fatal("expected 503 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestEcosystemPin_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/ecosystem/pin" {
			t.Errorf("path = %s, want /v1/ecosystem/pin", r.URL.Path)
		}
		var req EcosystemPinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Ecosystem != "go" {
			t.Errorf("Ecosystem = %q, want go", req.Ecosystem)
		}
		if req.Version != "1.22.0" {
			t.Errorf("Version = %q, want 1.22.0", req.Version)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.EcosystemPin(context.Background(), "go", "1.22.0"); err != nil {
		t.Fatalf("EcosystemPin: %v", err)
	}
}

func TestEcosystemPin_404Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "(ecosystem, version) not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.EcosystemPin(context.Background(), "go", "9.9.9")
	if err == nil {
		t.Fatal("expected 404 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err = %v, want HTTPError 404", err)
	}
}

func TestEcosystemPin_409AlreadyPinned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "already pinned", http.StatusConflict)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.EcosystemPin(context.Background(), "go", "1.22.0")
	if err == nil {
		t.Fatal("expected 409 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusConflict) {
		t.Errorf("err = %v, want HTTPError 409", err)
	}
}

func TestEcosystemPin_RejectsEmptyArgs(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("EcosystemPin should not dial the daemon when args are empty")
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	cases := []struct {
		eco, ver string
	}{
		{"", "1.22.0"},
		{"go", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if err := c.EcosystemPin(context.Background(), tc.eco, tc.ver); err == nil {
			t.Errorf("EcosystemPin(%q, %q) = nil; want non-empty validation error", tc.eco, tc.ver)
		}
	}
}

func TestEcosystemPrunePreview_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/ecosystem/prune-preview") {
			t.Errorf("path = %s, want /v1/ecosystem/prune-preview", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("ecosystem") != "python" {
			t.Errorf("ecosystem query = %q, want python", q.Get("ecosystem"))
		}
		if q.Get("version") != "3.11.9" {
			t.Errorf("version query = %q, want 3.11.9", q.Get("version"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EcosystemPrunePreview{
			Ecosystem:      "python",
			Version:        "3.11.9",
			ChunkCount:     42,
			ChunkFP32Count: 42,
			SymbolCount:    17,
			ChangeCount:    3,
			FTS5Count:      42,
			Pinned:         false,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.EcosystemPrunePreview(context.Background(), "python", "3.11.9")
	if err != nil {
		t.Fatalf("EcosystemPrunePreview: %v", err)
	}
	if resp.ChunkCount != 42 {
		t.Errorf("ChunkCount = %d, want 42", resp.ChunkCount)
	}
	if resp.Pinned {
		t.Error("Pinned = true; want false on a fresh preview")
	}
}

func TestEcosystemPrunePreview_PinnedFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EcosystemPrunePreview{
			Ecosystem:  "go",
			Version:    "1.22.0",
			ChunkCount: 99,
			Pinned:     true,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.EcosystemPrunePreview(context.Background(), "go", "1.22.0")
	if err != nil {
		t.Fatalf("EcosystemPrunePreview: %v", err)
	}
	if !resp.Pinned {
		t.Error("Pinned = false; want true")
	}
}

func TestEcosystemPrunePreview_404Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.EcosystemPrunePreview(context.Background(), "go", "0.0.0")
	if err == nil {
		t.Fatal("expected 404 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err = %v, want HTTPError 404", err)
	}
}

func TestEcosystemPrunePreview_RejectsEmptyArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("EcosystemPrunePreview should not dial when args are empty")
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if _, err := c.EcosystemPrunePreview(context.Background(), "", "1.0.0"); err == nil {
		t.Error("EcosystemPrunePreview(empty eco) = nil; want validation error")
	}
	if _, err := c.EcosystemPrunePreview(context.Background(), "go", ""); err == nil {
		t.Error("EcosystemPrunePreview(empty ver) = nil; want validation error")
	}
}

func TestEcosystemPrune_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/v1/ecosystem/version" {
			t.Errorf("path = %s, want /v1/ecosystem/version", r.URL.Path)
		}
		var req EcosystemPruneRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Ecosystem != "rust" {
			t.Errorf("Ecosystem = %q, want rust", req.Ecosystem)
		}
		if req.Version != "1.70.0" {
			t.Errorf("Version = %q, want 1.70.0", req.Version)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.EcosystemPrune(context.Background(), "rust", "1.70.0"); err != nil {
		t.Fatalf("EcosystemPrune: %v", err)
	}
}

func TestEcosystemPrune_409Pinned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "version is pinned (indefinite_retain=true)", http.StatusConflict)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.EcosystemPrune(context.Background(), "go", "1.22.0")
	if err == nil {
		t.Fatal("expected 409 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusConflict) {
		t.Errorf("err = %v, want HTTPError 409", err)
	}
}

func TestEcosystemPrune_404Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.EcosystemPrune(context.Background(), "go", "0.0.0")
	if err == nil {
		t.Fatal("expected 404 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("err = %v, want HTTPError 404", err)
	}
}

func TestEcosystemPrune_RejectsEmptyArgs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("EcosystemPrune should not dial when args are empty")
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	if err := c.EcosystemPrune(context.Background(), "", "1.0.0"); err == nil {
		t.Error("EcosystemPrune(empty eco) = nil; want validation error")
	}
	if err := c.EcosystemPrune(context.Background(), "go", ""); err == nil {
		t.Error("EcosystemPrune(empty ver) = nil; want validation error")
	}
}

func TestDocsStatus_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/ecosystem/status" {
			t.Errorf("path = %s, want /v1/knowledge/ecosystem/status", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DocsStatusResponse{
			Ecosystems: []EcosystemStatus{
				{
					Ecosystem:     "go",
					ChunkCount:    1234,
					SymbolCount:   567,
					StorageBytes:  10485760,
					LastPolled:    1736424000,
					LastIndexed:   1736424000,
					RetentionDays: 90,
				},
				{
					Ecosystem:     "python",
					ChunkCount:    890,
					SymbolCount:   234,
					StorageBytes:  5242880,
					LastPolled:    1736500000,
					LastIndexed:   1736500000,
					RetentionDays: 90,
				},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.DocsStatus(context.Background())
	if err != nil {
		t.Fatalf("DocsStatus: %v", err)
	}
	if len(resp.Ecosystems) != 2 {
		t.Fatalf("Ecosystems = %d, want 2", len(resp.Ecosystems))
	}
	if resp.Ecosystems[0].Ecosystem != "go" {
		t.Errorf("[0].Ecosystem = %q", resp.Ecosystems[0].Ecosystem)
	}
	if resp.Ecosystems[1].ChunkCount != 890 {
		t.Errorf("[1].ChunkCount = %d, want 890", resp.Ecosystems[1].ChunkCount)
	}
}

func TestDocsStatus_EmptyEcosystems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ecosystems":[]}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.DocsStatus(context.Background())
	if err != nil {
		t.Fatalf("DocsStatus: %v", err)
	}
	if len(resp.Ecosystems) != 0 {
		t.Errorf("Ecosystems = %d, want 0", len(resp.Ecosystems))
	}
}

func TestDocsSources_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/ecosystem/sources" {
			t.Errorf("path = %s, want /v1/knowledge/ecosystem/sources", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DocsSourcesResponse{
			Sources: []SourceStatus{
				{
					Name:        "pkg.go.dev",
					Ecosystem:   "go",
					SourceType:  "registry",
					URL:         "https://pkg.go.dev/",
					TTLHours:    24,
					LastIndexed: 1736424000,
					Status:      "ok",
				},
				{
					Name:        "pypi",
					Ecosystem:   "python",
					SourceType:  "registry",
					URL:         "https://pypi.org/",
					TTLHours:    24,
					LastIndexed: 1736300000,
					Status:      "stale",
				},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.DocsSources(context.Background())
	if err != nil {
		t.Fatalf("DocsSources: %v", err)
	}
	if len(resp.Sources) != 2 {
		t.Fatalf("Sources = %d, want 2", len(resp.Sources))
	}
	if resp.Sources[0].Name != "pkg.go.dev" {
		t.Errorf("[0].Name = %q", resp.Sources[0].Name)
	}
	if resp.Sources[1].Status != "stale" {
		t.Errorf("[1].Status = %q, want stale", resp.Sources[1].Status)
	}
	if resp.Sources[0].TTLHours != 24 {
		t.Errorf("[0].TTLHours = %d, want 24", resp.Sources[0].TTLHours)
	}
}

func TestDocsRouterRetrain_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/ecosystem/router/retrain" {
			t.Errorf("path = %s, want /v1/knowledge/ecosystem/router/retrain", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RouterRetrainResponse{
			CheckpointPath: "/home/user/.local/share/zen-swarm/router/classifier.bin",
			Accuracy:       0.987,
			ElapsedMs:      4500,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.DocsRouterRetrain(context.Background())
	if err != nil {
		t.Fatalf("DocsRouterRetrain: %v", err)
	}
	if resp.Accuracy != 0.987 {
		t.Errorf("Accuracy = %v, want 0.987", resp.Accuracy)
	}
	if !strings.Contains(resp.CheckpointPath, "classifier.bin") {
		t.Errorf("CheckpointPath = %q, want suffix classifier.bin", resp.CheckpointPath)
	}
	if resp.ElapsedMs != 4500 {
		t.Errorf("ElapsedMs = %d, want 4500", resp.ElapsedMs)
	}
}

func TestDocsRouterRetrain_500Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "corpus generator panicked", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.DocsRouterRetrain(context.Background())
	if err == nil {
		t.Fatal("expected 500 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("err = %v, want HTTPError 500", err)
	}
}

func TestEcosystemPrune_503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "pruner offline", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	err := c.EcosystemPrune(context.Background(), "go", "1.0.0")
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestEcosystemPrunePreview_503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "preview offline", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.EcosystemPrunePreview(context.Background(), "go", "1.0.0")
	if err == nil {
		t.Fatal("expected 503 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestDocsStatus_500Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "store unavailable", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.DocsStatus(context.Background())
	if err == nil {
		t.Fatal("expected 500 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("err = %v, want HTTPError 500", err)
	}
}

func TestDocsSources_500Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "source registry corrupt", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.DocsSources(context.Background())
	if err == nil {
		t.Fatal("expected 500 to propagate")
	}
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Errorf("err = %v, want HTTPError 500", err)
	}
}
