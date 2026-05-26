package views

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/client"
)

func TestCodegraphDispatchQuerySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/codegraph" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.CodegraphQueryResponse{
			Hits: []client.CodegraphHit{
				{Symbol: "Dispatch", File: "x.go", Line: 42, Kind: "func"},
			},
		})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	cmd := v.dispatchQuery("MATCH (n) RETURN n")
	if cmd == nil {
		t.Fatal("expected non-nil Cmd")
	}
	msg := cmd()
	m, ok := msg.(codegraphSubPanelMsg)
	if !ok {
		t.Fatalf("expected codegraphSubPanelMsg, got %T", msg)
	}
	if m.err != nil {
		t.Errorf("unexpected err: %v", m.err)
	}
	if !strings.Contains(m.body, "Dispatch") {
		t.Errorf("expected Dispatch in body, got: %q", m.body)
	}
}

func TestCodegraphDispatchQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	cmd := v.dispatchQuery("x")
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err == nil {
		t.Fatal("expected error from 503")
	}
}

func TestCodegraphDispatchImpactSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/impact" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.ImpactResponse{
			Symbol:        "Dispatch",
			BlastRadius:   "high",
			Score:         87,
			AffectedFiles: []string{"a.go", "b.go"},
		})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.SetCurrentFile("dispatch.go")
	cmd := v.dispatchImpact()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err != nil {
		t.Errorf("err = %v", m.err)
	}
	if !strings.Contains(m.body, "high") {
		t.Errorf("expected 'high' in body, got: %q", m.body)
	}
}

func TestCodegraphDispatchImpactError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.SetCurrentFile("foo.go")
	cmd := v.dispatchImpact()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err == nil {
		t.Fatal("expected error")
	}
}

func TestCodegraphDispatchWikiSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/wiki" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.WikiResponse{
			Module:   "C-014",
			Markdown: "# Community C-014\n\nSubsystem details.",
		})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.community = communityInfo{ID: "C-014"}
	cmd := v.dispatchWiki()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err != nil {
		t.Errorf("err = %v", m.err)
	}
	if !strings.Contains(m.body, "Community C-014") {
		t.Errorf("expected wiki body, got: %q", m.body)
	}
}

func TestCodegraphDispatchWikiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.community = communityInfo{ID: "X"}
	cmd := v.dispatchWiki()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err == nil {
		t.Fatal("expected error")
	}
}

func TestCodegraphDispatchCrossProjectSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/knowledge/query" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(client.KnowledgeQueryResponse{
			Rows: []client.KnowledgeResultRow{
				{ProjectAlias: "internal-platform-x", FilePath: "x.go", Score: 0.95},
			},
		})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.symbols = []symbolEntry{{Name: "Dispatch", Kind: "func"}}
	cmd := v.dispatchCrossProject()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err != nil {
		t.Errorf("err = %v", m.err)
	}
	if !strings.Contains(m.body, "internal-platform-x") {
		t.Errorf("expected internal-platform-x in body, got: %q", m.body)
	}
}

func TestCodegraphDispatchCrossProjectNoMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.KnowledgeQueryResponse{Rows: nil})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.symbols = []symbolEntry{{Name: "Sym", Kind: "func"}}
	cmd := v.dispatchCrossProject()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if !strings.Contains(m.body, "no cross-project hits") {
		t.Errorf("expected (no hits) body, got: %q", m.body)
	}
}

func TestCodegraphDispatchCrossProjectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.symbols = []symbolEntry{{Name: "X", Kind: "func"}}
	cmd := v.dispatchCrossProject()
	msg := cmd()
	m := msg.(codegraphSubPanelMsg)
	if m.err == nil {
		t.Fatal("expected error")
	}
}

func TestCodegraphRefetchNilClient(t *testing.T) {
	if NewCodegraphView(nil).Refetch() != nil {
		t.Error("expected nil Refetch with nil client")
	}
}

func TestCodegraphRefetchNoCurrentFile(t *testing.T) {
	if NewCodegraphView(client.NewWithBaseURL("http://x")).Refetch() != nil {
		t.Error("expected nil Refetch with no current file")
	}
}

func TestCodegraphRefetchSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.CodegraphQueryResponse{
			Hits: []client.CodegraphHit{
				{Symbol: "Dispatch", File: "dispatch.go", Line: 1, Kind: "func"},
			},
		})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Context360Response{
			Symbol: "Dispatch", Callers: []string{"a.go"}, Community: "C-001",
		})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ImpactResponse{Symbol: "X", BlastRadius: "medium"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	v := NewCodegraphView(client.NewWithBaseURL(srv.URL))
	v.SetCurrentFile("dispatch.go")
	cmd := v.Refetch()
	msg := cmd()
	m := msg.(codegraphDataMsg)
	if m.err != nil {
		t.Fatalf("err = %v", m.err)
	}
	if len(m.symbols) != 1 || m.symbols[0].Name != "Dispatch" {
		t.Errorf("symbols = %+v", m.symbols)
	}
	if m.community.ID != "C-001" {
		t.Errorf("community.ID = %q", m.community.ID)
	}
}

func TestCodegraphRefetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()
	v := NewCodegraphView(client.NewWithBaseURL(srv.URL))
	v.SetCurrentFile("x.go")
	msg := v.Refetch()()
	if (msg.(codegraphDataMsg)).err == nil {
		t.Fatal("expected error")
	}
}

func TestCodegraphRefetchMapsCorenessAndCoChange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/mcpgateway/codegraph", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.CodegraphQueryResponse{
			Hits: []client.CodegraphHit{
				{Symbol: "Dispatch", File: "dispatch.go", Line: 1, Kind: "func"},
			},
		})
	})
	mux.HandleFunc("/v1/mcpgateway/context", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Context360Response{
			Symbol:    "Dispatch",
			Callers:   []string{"a.go"},
			Community: "C-001",
			Coreness:  4,
			SCCID:     9,
			Cyclic:    true,
		})
	})
	mux.HandleFunc("/v1/mcpgateway/impact", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.ImpactResponse{Symbol: "Dispatch", BlastRadius: "high", Score: 87})
	})
	mux.HandleFunc("/v1/mcpgateway/cochange", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.CoChangeResponse{
			Peers: []client.CoChangePeerDTO{
				{Path: "internal/y/b.go", CouplingPercent: 70, SharedRevs: 12},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	v := NewCodegraphView(client.NewWithBaseURL(srv.URL))
	v.SetCurrentFile("dispatch.go")
	cmd := v.Refetch()
	msg := cmd()
	m, ok := msg.(codegraphDataMsg)
	if !ok {
		t.Fatalf("expected codegraphDataMsg, got %T", msg)
	}
	if m.err != nil {
		t.Fatalf("unexpected err: %v", m.err)
	}
	if m.coreness != 4 {
		t.Errorf("coreness = %d, want 4", m.coreness)
	}
	if m.sccID != 9 {
		t.Errorf("sccID = %d, want 9", m.sccID)
	}
	if !m.cyclic {
		t.Errorf("cyclic = false, want true")
	}
	if len(m.coChangePeers) != 1 || m.coChangePeers[0].Path != "internal/y/b.go" {
		t.Errorf("coChangePeers = %+v; want 1 peer internal/y/b.go", m.coChangePeers)
	}
	if m.coChangePeers[0].CouplingPercent != 70 {
		t.Errorf("CouplingPercent = %.0f, want 70", m.coChangePeers[0].CouplingPercent)
	}
}

func TestCodegraphDispatchCrossProjectFallbackToFile(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.KnowledgeQueryRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.CodeSymbol != "dispatch.go" {
			t.Errorf("expected CodeSymbol=dispatch.go (fallback), got: %q", req.CodeSymbol)
		}
		_ = json.NewEncoder(w).Encode(client.KnowledgeQueryResponse{})
	}))
	defer srv.Close()

	c := client.NewWithBaseURL(srv.URL)
	v := NewCodegraphView(c)
	v.SetCurrentFile("dispatch.go")
	cmd := v.dispatchCrossProject()
	cmd()
}
