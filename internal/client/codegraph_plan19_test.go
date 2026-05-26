package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWhyHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/mcpgateway/why" {
			t.Errorf("path = %s, want /v1/mcpgateway/why", r.URL.Path)
		}
		var req WhyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Symbol != "internal/orchestrator/merge.Engine" {
			t.Errorf("Symbol = %q", req.Symbol)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WhyResponse{
			Subject: req.Symbol,
			LinkedADRs: []WhyLinkedADR{
				{ADRID: "ADR-0081", ADRTitle: "Augment lanes", LinkKind: "explicit_ref", Confidence: 0.9, Stale: false},
			},
			SemanticPassages: []WhySemanticPassage{
				{SourceID: "ADR-0081", SourceKind: "adr", Text: "lanes 1/3/5", Score: 0.8},
			},
			LoreTrailers: []WhyLoreEntry{
				{CommitSHA: "abc123", TrailerKind: "constraint", Body: "no subprocess", AuthoredAt: 1700000000},
			},
			Degraded: false,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Why(context.Background(), WhyRequest{Symbol: "internal/orchestrator/merge.Engine"})
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if resp.Subject != "internal/orchestrator/merge.Engine" {
		t.Errorf("Subject = %q", resp.Subject)
	}
	if len(resp.LinkedADRs) != 1 || resp.LinkedADRs[0].ADRID != "ADR-0081" {
		t.Errorf("LinkedADRs = %+v", resp.LinkedADRs)
	}
	if len(resp.LoreTrailers) != 1 || resp.LoreTrailers[0].TrailerKind != "constraint" {
		t.Errorf("LoreTrailers = %+v", resp.LoreTrailers)
	}
}

func TestWhyErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.Why(context.Background(), WhyRequest{Symbol: "pkg.Sym"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestRiskHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/risk" {
			t.Errorf("path = %s, want /v1/mcpgateway/risk", r.URL.Path)
		}
		var req RiskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(req.ChangedSymbols) != 1 || req.ChangedSymbols[0] != "pkg.Sym" {
			t.Errorf("ChangedSymbols = %v", req.ChangedSymbols)
		}
		if len(req.ChangedFiles) != 1 || req.ChangedFiles[0] != "a/b.go" {
			t.Errorf("ChangedFiles = %v", req.ChangedFiles)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RiskResponse{
			Level: "high", Score: 0.72,
			Cone: 0.5, Coreness: 0.6, Churn: 0.3, Coupling: 0.2,
			TopAffected: []string{"pkg.A", "pkg.B"},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Risk(context.Background(), RiskRequest{
		ChangedSymbols: []string{"pkg.Sym"}, ChangedFiles: []string{"a/b.go"},
	})
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	if resp.Level != "high" || resp.Score != 0.72 {
		t.Errorf("Level/Score = %q/%v", resp.Level, resp.Score)
	}
	if len(resp.TopAffected) != 2 {
		t.Errorf("TopAffected = %v", resp.TopAffected)
	}
}

func TestRiskErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.Risk(context.Background(), RiskRequest{ChangedSymbols: []string{"pkg.Sym"}})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestCoChangeHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/cochange" {
			t.Errorf("path = %s, want /v1/mcpgateway/cochange", r.URL.Path)
		}
		var req CoChangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.File != "internal/x/a.go" {
			t.Errorf("File = %q", req.File)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CoChangeResponse{
			File: req.File,
			Peers: []CoChangePeerDTO{
				{Path: "internal/x/b.go", CouplingPercent: 60, SharedRevs: 6, WindowDays: 90},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CoChange(context.Background(), CoChangeRequest{File: "internal/x/a.go"})
	if err != nil {
		t.Fatalf("CoChange: %v", err)
	}
	if len(resp.Peers) != 1 || resp.Peers[0].Path != "internal/x/b.go" {
		t.Errorf("Peers = %+v", resp.Peers)
	}
	if resp.Peers[0].CouplingPercent != 60 {
		t.Errorf("CouplingPercent = %v", resp.Peers[0].CouplingPercent)
	}
}

func TestCoChangeErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.CoChange(context.Background(), CoChangeRequest{File: "internal/x/a.go"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestImplHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/impl" {
			t.Errorf("path = %s, want /v1/mcpgateway/impl", r.URL.Path)
		}
		var req ImplRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Interface != "io.Writer" {
			t.Errorf("Interface = %q", req.Interface)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ImplResponse{
			Interface: req.Interface,
			Implementations: []ImplDTO{
				{InterfaceID: "io.Writer", ImplID: "bytes.Buffer", Confidence: "exact_vta", Reachable: true},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Impl(context.Background(), ImplRequest{Interface: "io.Writer"})
	if err != nil {
		t.Fatalf("Impl: %v", err)
	}
	if len(resp.Implementations) != 1 || resp.Implementations[0].ImplID != "bytes.Buffer" {
		t.Errorf("Implementations = %+v", resp.Implementations)
	}
	if resp.Implementations[0].Confidence != "exact_vta" {
		t.Errorf("Confidence = %q", resp.Implementations[0].Confidence)
	}
}

func TestImplErrorPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	_, err := c.Impl(context.Background(), ImplRequest{Interface: "io.Writer"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestWhySendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WhyResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Why(context.Background(), WhyRequest{
		Symbol: "pkg.Sym", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Why: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestRiskSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RiskResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Risk(context.Background(), RiskRequest{
		ChangedSymbols: []string{"pkg.Sym"},
		ProjectAlias:   "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestCoChangeSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CoChangeResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.CoChange(context.Background(), CoChangeRequest{
		File: "internal/x/a.go", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("CoChange: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestImplSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ImplResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Impl(context.Background(), ImplRequest{
		Interface: "io.Writer", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Impl: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestCaronteHealthSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CaronteHealthResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.CaronteHealth(context.Background(), CaronteHealthRequest{
		ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("CaronteHealth: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}
