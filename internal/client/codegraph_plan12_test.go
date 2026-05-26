package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodegraphFileSuccess(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{
			Hits: []CodegraphHit{
				{Symbol: "Dispatch", File: "x.go", Line: 42, Kind: "func"},
				{Symbol: "Helper", File: "x.go", Line: 88, Kind: "func"},
			},
		})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Context360Response{
			Symbol:    "Dispatch",
			Callers:   []string{"a.go", "b.go"},
			Community: "C-014",
		})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ImpactResponse{
			Symbol:        "Dispatch",
			BlastRadius:   "high",
			Score:         87,
			AffectedFiles: []string{"a.go", "b.go", "c.go"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphFile(context.Background(), "x.go")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if len(resp.Symbols) != 2 {
		t.Errorf("Symbols = %d, want 2", len(resp.Symbols))
	}
	if resp.CommunityID != "C-014" {
		t.Errorf("CommunityID = %q, want C-014", resp.CommunityID)
	}
	if resp.BlastRadiusScore < 0.7 {
		t.Errorf("BlastRadiusScore = %v, want ≥0.7 for 'high'", resp.BlastRadiusScore)
	}
}

func TestCodegraphFileEmptyHits(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Context360Response{})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ImpactResponse{Score: 10, BlastRadius: "low"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphFile(context.Background(), "empty.go")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(resp.Symbols) != 0 {
		t.Errorf("expected zero symbols, got %d", len(resp.Symbols))
	}
}

func TestCodegraphFile503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphFile(context.Background(), "x.go")
	if err == nil {
		t.Fatal("expected 503 error")
	}
}

func TestCodegraphFileContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.CodegraphFile(ctx, "x.go")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestAuditEventByIDSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/audit/event/") {
			t.Errorf("path prefix wrong: %q", r.URL.Path)
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/audit/event/")

		_ = json.NewEncoder(w).Encode(map[string]any{
			"envelope": map[string]any{"id": id},
			"row": AuditEvent{
				ID:         id,
				ProjectID:  "proj-x",
				Type:       "task.complete",
				Doctrine:   "default",
				PayloadRaw: `{"ok":true}`,
				EmittedAt:  1715290000,
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	ev, err := c.AuditEventByID(context.Background(), "evt-abc-123")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ev.ID != "evt-abc-123" {
		t.Errorf("id = %q", ev.ID)
	}
	if ev.Type != "task.complete" {
		t.Errorf("type = %q", ev.Type)
	}
}

func TestAuditEventByID404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.AuditEventByID(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected 404 error")
	}
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Errorf("expected 404, got: %v", err)
	}
}

func TestAuditEventByIDInvalidID(t *testing.T) {
	c := NewWithBaseURL("http://invalid")

	_, err := c.AuditEventByID(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}

	_, err = c.AuditEventByID(context.Background(), "../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path-escape id")
	}
	_, err = c.AuditEventByID(context.Background(), "id?query=evil")
	if err == nil {
		t.Fatal("expected error for query-escape id")
	}
}

func TestAuditEventByID403Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.AuditEventByID(context.Background(), "valid-id")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsHTTPStatus(err, http.StatusForbidden) {
		t.Errorf("expected 403, got: %v", err)
	}
}

func TestAugmentCacheSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/augment/summary" {
			t.Errorf("path = %q (expected /v1/augment/summary)", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(AugmentSummaryResponse{
			Date:               "2026-05-12",
			TotalCost:          0.42,
			TokensConsumed:     12345,
			TokensCeiling:      30000,
			KGQueriesFired:     78,
			CacheHitRate:       0.74,
			LastIndexedRFC3339: "2026-05-12T18:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.AugmentCache(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.HitRate != 0.74 {
		t.Errorf("HitRate = %v", resp.HitRate)
	}
	if resp.TotalQueries != 78 {
		t.Errorf("TotalQueries = %v", resp.TotalQueries)
	}
}

func TestAugmentCache503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.AugmentCache(context.Background())
	if err == nil {
		t.Fatal("expected 503")
	}
}

func TestBlastRadiusScoreFromEnum(t *testing.T) {
	cases := []struct {
		enum  string
		score int
		want  float64
	}{
		{"low", 0, 0.25},
		{"medium", 0, 0.55},
		{"high", 0, 0.85},
		{"LOW", 0, 0.25},
		{"HIGH", 0, 0.85},
		{"", 50, 0.5},
		{"unknown", 25, 0.25},
		{"x", -10, 0},
		{"x", 200, 1},
		{"x", 100, 1},
	}
	for _, tc := range cases {
		got := blastRadiusScoreFromEnum(tc.enum, tc.score)
		if got != tc.want {
			t.Errorf("blastRadiusScoreFromEnum(%q, %d) = %v, want %v",
				tc.enum, tc.score, got, tc.want)
		}
	}
}

func TestCodegraphFileFiltersUnrelatedHits(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{
			Hits: []CodegraphHit{
				{Symbol: "Dispatch", File: "x.go", Line: 42, Kind: "func"},
				{Symbol: "Other", File: "y.go", Line: 1, Kind: "func"},
				{Symbol: "Helper", File: "x.go", Line: 99, Kind: "func"},
			},
		})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Context360Response{Callers: []string{"caller.go"}})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(ImpactResponse{BlastRadius: "low"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphFile(context.Background(), "x.go")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(resp.Symbols) != 2 {
		t.Errorf("expected 2 symbols (filtered), got %d", len(resp.Symbols))
	}
	for _, s := range resp.Symbols {
		if s.Name == "Other" {
			t.Errorf("Other should have been filtered out")
		}
	}
}

func TestCodegraphFileImpactErrorPropagates(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Context360Response{})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphFile(context.Background(), "x.go")
	if err == nil {
		t.Fatal("expected error from impact 503")
	}
}

func TestCodegraphFileContextErrorPropagates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphFile(context.Background(), "x.go")
	if err == nil {
		t.Fatal("expected error from context 500")
	}
}

func TestAugmentCacheContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.AugmentCache(ctx)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestCodegraphFileCarriesRealStructuralData(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/mcpgateway/codegraph":
			_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{Hits: []CodegraphHit{
				{Symbol: "Widget", File: "internal/x/a.go", Line: 10, Kind: "type"},
			}})
		case "/v1/mcpgateway/context":
			_ = json.NewEncoder(w).Encode(Context360Response{
				Symbol: "Widget", Callers: []string{"internal/y/c.go"}, Community: "internal/x",
				Coreness: 5, SCCID: 3, Cyclic: true,
			})
		case "/v1/mcpgateway/impact":
			_ = json.NewEncoder(w).Encode(ImpactResponse{
				Symbol: "Widget", BlastRadius: "high", Score: 72,
			})
		case "/v1/mcpgateway/cochange":
			_ = json.NewEncoder(w).Encode(CoChangeResponse{
				File: "internal/x/a.go",
				Peers: []CoChangePeerDTO{
					{Path: "internal/x/b.go", CouplingPercent: 60, SharedRevs: 6, WindowDays: 90},
				},
			})
		case "/v1/mcpgateway/health":
			_ = json.NewEncoder(w).Encode(CaronteHealthResponse{
				ProjectID: "p", LastIndexed: 1700000000, NodeCount: 100,
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphFile(context.Background(), "internal/x/a.go")
	if err != nil {
		t.Fatalf("CodegraphFile: %v", err)
	}
	if len(resp.Symbols) != 1 || resp.Symbols[0].Name != "Widget" {
		t.Errorf("Symbols = %+v", resp.Symbols)
	}
	if resp.CommunityID != "internal/x" {
		t.Errorf("CommunityID = %q; want internal/x (real context op)", resp.CommunityID)
	}
	if resp.BlastRadiusScore != 0.85 {
		t.Errorf("BlastRadiusScore = %v; want 0.85 (real impact level)", resp.BlastRadiusScore)
	}
	if resp.Coreness != 5 {
		t.Errorf("Coreness = %d; want 5 (from context op)", resp.Coreness)
	}
	if resp.SCCID != 3 {
		t.Errorf("SCCID = %d; want 3 (from context op)", resp.SCCID)
	}
	if !resp.Cyclic {
		t.Error("Cyclic = false; want true (from context op)")
	}
	if len(resp.CoChangePeers) != 1 || resp.CoChangePeers[0].Path != "internal/x/b.go" {
		t.Errorf("CoChangePeers = %+v; want 1 peer internal/x/b.go", resp.CoChangePeers)
	}
	if resp.CoChangePeers[0].CouplingPercent != 60 {
		t.Errorf("CouplingPercent = %v; want 60", resp.CoChangePeers[0].CouplingPercent)
	}
	if resp.LastIndexedRFC3339 == "" {
		t.Error("LastIndexedRFC3339 empty; want a formatted timestamp from caronte health")
	}
}

func TestCodegraphFileEnrichmentFailSoft(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/mcpgateway/codegraph":
			_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{Hits: []CodegraphHit{
				{Symbol: "W", File: "a.go", Line: 1, Kind: "type"},
			}})
		case "/v1/mcpgateway/context":
			_ = json.NewEncoder(w).Encode(Context360Response{Symbol: "W", Community: "x"})
		case "/v1/mcpgateway/impact":
			_ = json.NewEncoder(w).Encode(ImpactResponse{Symbol: "W", BlastRadius: "low", Score: 10})
		default:
			http.Error(w, "engine churn unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphFile(context.Background(), "a.go")
	if err != nil {
		t.Fatalf("CodegraphFile must not error on enrichment miss: %v", err)
	}
	if resp.CommunityID != "x" {
		t.Errorf("CommunityID = %q; core context must still populate", resp.CommunityID)
	}
	if resp.BlastRadiusScore != 0.25 {
		t.Errorf("BlastRadiusScore = %v; want 0.25 (low)", resp.BlastRadiusScore)
	}
	if len(resp.CoChangePeers) != 0 {
		t.Errorf("CoChangePeers = %+v; want empty on cochange failure", resp.CoChangePeers)
	}
	if resp.LastIndexedRFC3339 != "" {
		t.Errorf("LastIndexedRFC3339 = %q; want empty on health failure", resp.LastIndexedRFC3339)
	}
}
