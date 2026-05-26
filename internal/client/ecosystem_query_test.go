package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEcosystemQueryHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/knowledge/ecosystem/query" {
			t.Errorf("path = %s, want /v1/knowledge/ecosystem/query", r.URL.Path)
		}
		var req EcosystemQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.Query != "context.Context" {
			t.Errorf("Query = %q, want context.Context", req.Query)
		}
		if req.Ecosystem != "go" {
			t.Errorf("Ecosystem = %q, want go", req.Ecosystem)
		}
		if req.MaxResults != 5 {
			t.Errorf("MaxResults = %d, want 5", req.MaxResults)
		}
		if req.Doctrine != "max-scope" {
			t.Errorf("Doctrine = %q, want max-scope", req.Doctrine)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EcosystemQueryResponse{
			Chunks: []EcosystemChunk{
				{
					PackageName:     "context",
					SymbolPath:      "context.Context",
					Kind:            "interface",
					Version:         "1.22.0",
					ContentText:     "Context carries a deadline...",
					SourceURL:       "https://pkg.go.dev/context#Context",
					SimilarityScore: 0.82,
					RerankerScore:   0.95,
					CitationID:      "doc_1",
				},
			},
			Citations: []EcosystemCitation{
				{ID: "doc_1", SymbolPath: "context.Context", SourceURL: "https://pkg.go.dev/context#Context"},
			},
			Provenance: EcosystemProvenance{
				DetectedVersion:   "1.22.0",
				DetectionLayer:    1,
				RoutingMethod:     "single",
				DoctrineApplied:   "max-scope",
				RoutingEcosystems: []string{"go"},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{
		Query: "context.Context", Ecosystem: "go", MaxResults: 5, Doctrine: "max-scope",
	})
	if err != nil {
		t.Fatalf("EcosystemQuery: %v", err)
	}
	if len(resp.Chunks) != 1 {
		t.Fatalf("Chunks = %d, want 1", len(resp.Chunks))
	}
	if resp.Chunks[0].SymbolPath != "context.Context" {
		t.Errorf("SymbolPath = %q", resp.Chunks[0].SymbolPath)
	}
	if resp.Chunks[0].RerankerScore != 0.95 {
		t.Errorf("RerankerScore = %v, want 0.95", resp.Chunks[0].RerankerScore)
	}
	if resp.Provenance.DetectedVersion != "1.22.0" {
		t.Errorf("DetectedVersion = %q", resp.Provenance.DetectedVersion)
	}
	if resp.Provenance.DetectionLayer != 1 {
		t.Errorf("DetectionLayer = %d", resp.Provenance.DetectionLayer)
	}
	if resp.Abstained {
		t.Error("Abstained = true, want false")
	}
}

func TestEcosystemQueryEmptyChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chunks":[],"abstained":false,"provenance":{"routing_method":"single","doctrine_applied":"default","detection_layer":0}}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{Query: "nothing"})
	if err != nil {
		t.Fatalf("EcosystemQuery: %v", err)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
	if len(resp.Chunks) != 0 {
		t.Errorf("Chunks = %d, want 0", len(resp.Chunks))
	}
}

func TestEcosystemQueryAbstained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(EcosystemQueryResponse{
			Abstained:     true,
			AbstainReason: "low confidence across all ecosystems",
			Provenance:    EcosystemProvenance{RoutingMethod: "broadcast", DoctrineApplied: "capa-firewall"},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{Query: "ambiguous"})
	if err != nil {
		t.Fatalf("EcosystemQuery: %v", err)
	}
	if !resp.Abstained {
		t.Error("Abstained = false, want true")
	}
	if !strings.Contains(resp.AbstainReason, "low confidence") {
		t.Errorf("AbstainReason = %q", resp.AbstainReason)
	}
	if resp.Provenance.DoctrineApplied != "capa-firewall" {
		t.Errorf("DoctrineApplied = %q", resp.Provenance.DoctrineApplied)
	}
}

func TestEcosystemQuery503Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ecosystem dispatcher not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{Query: "q"})
	if err == nil {
		t.Fatal("expected 503 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Errorf("err = %v, want HTTPError 503", err)
	}
}

func TestEcosystemQuery422Propagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ecosystem must be go|python|typescript|rust", http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{Query: "q", Ecosystem: "kotlin"})
	if err == nil {
		t.Fatal("expected 422 to propagate as error")
	}
	if !IsHTTPStatus(err, http.StatusUnprocessableEntity) {
		t.Errorf("err = %v, want HTTPError 422", err)
	}
}

// TestEcosystemQueryWireFieldOmitEmpty — empty optional fields do not
// appear on the wire (json:omitempty). Defense-in-depth against future
// drift where a developer drops the omitempty tag.
func TestEcosystemQueryWireFieldOmitEmpty(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		raw = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chunks":[],"abstained":false,"provenance":{}}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.EcosystemQuery(context.Background(), EcosystemQueryRequest{Query: "q"})
	if err != nil {
		t.Fatalf("EcosystemQuery: %v", err)
	}
	if strings.Contains(raw, `"ecosystem"`) {
		t.Errorf("empty Ecosystem should be omitted, got: %s", raw)
	}
	if strings.Contains(raw, `"version"`) {
		t.Errorf("empty Version should be omitted, got: %s", raw)
	}
	if strings.Contains(raw, `"max_results"`) {
		t.Errorf("zero MaxResults should be omitted, got: %s", raw)
	}
}
