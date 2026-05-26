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
	"time"
)

func TestCodegraphQueryHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/mcpgateway/codegraph" {
			t.Errorf("path = %s, want /v1/mcpgateway/codegraph", r.URL.Path)
		}
		var req CodegraphQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		if req.Query != "MergeEngine" {
			t.Errorf("Query = %q, want MergeEngine", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{
			Hits: []CodegraphHit{
				{Symbol: "MergeEngine.Run", File: "internal/merge/engine.go", Line: 42, Kind: "func"},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CodegraphQuery(context.Background(), CodegraphQueryRequest{Query: "MergeEngine", Limit: 5})
	if err != nil {
		t.Fatalf("CodegraphQuery: %v", err)
	}
	if len(resp.Hits) != 1 {
		t.Fatalf("Hits = %d, want 1", len(resp.Hits))
	}
	if resp.Hits[0].Symbol != "MergeEngine.Run" {
		t.Fatalf("Symbol = %q, want MergeEngine.Run", resp.Hits[0].Symbol)
	}
	if resp.Hits[0].Line != 42 {
		t.Fatalf("Line = %d, want 42", resp.Hits[0].Line)
	}
}

func TestImpactHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/impact" {
			t.Errorf("path = %s, want /v1/mcpgateway/impact", r.URL.Path)
		}
		var req ImpactRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ImpactResponse{
			Symbol:        req.Symbol,
			BlastRadius:   "high",
			Score:         77,
			AffectedFiles: []string{"internal/merge/engine.go"},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Impact(context.Background(), ImpactRequest{Symbol: "MergeEngine.Run"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if resp.BlastRadius != "high" {
		t.Fatalf("BlastRadius = %q, want high", resp.BlastRadius)
	}
	if resp.Score != 77 {
		t.Fatalf("Score = %d, want 77", resp.Score)
	}
	if len(resp.AffectedFiles) != 1 {
		t.Fatalf("AffectedFiles = %v", resp.AffectedFiles)
	}
}

func TestContext360HappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/context" {
			t.Errorf("path = %s, want /v1/mcpgateway/context", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Context360Response{
			Symbol:    "Dispatcher",
			Callers:   []string{"orchestrator.Run"},
			Callees:   []string{"provider.Call"},
			Neighbors: []string{"Router"},
			Community: "dispatch-subsystem",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Context360(context.Background(), Context360Request{Symbol: "Dispatcher"})
	if err != nil {
		t.Fatalf("Context360: %v", err)
	}
	if resp.Community != "dispatch-subsystem" {
		t.Fatalf("Community = %q, want dispatch-subsystem", resp.Community)
	}
	if len(resp.Callers) != 1 {
		t.Fatalf("Callers = %v", resp.Callers)
	}
}

func TestWikiHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway/wiki" {
			t.Errorf("path = %s, want /v1/mcpgateway/wiki", r.URL.Path)
		}
		var req WikiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WikiResponse{
			Module:   req.Module,
			Markdown: "# dispatch-subsystem wiki\n",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.Wiki(context.Background(), WikiRequest{Module: "internal/daemon/dispatcher"})
	if err != nil {
		t.Fatalf("Wiki: %v", err)
	}
	if resp.Module != "internal/daemon/dispatcher" {
		t.Fatalf("Module = %q, want internal/daemon/dispatcher", resp.Module)
	}
	if resp.Markdown != "# dispatch-subsystem wiki\n" {
		t.Fatalf("Markdown = %q", resp.Markdown)
	}
}

func TestMCPRestartHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/daemon/restart-mcp" {
			t.Errorf("path = %s, want /v1/daemon/restart-mcp", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(MCPRestartResponse{
			Name:       body.Name,
			Status:     "restarted",
			DurationMs: 87,
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.MCPRestart(context.Background(), "caronte")
	if err != nil {
		t.Fatalf("MCPRestart: %v", err)
	}
	if resp.Name != "caronte" {
		t.Fatalf("Name = %q, want caronte", resp.Name)
	}
	if resp.Status != "restarted" {
		t.Fatalf("Status = %q, want restarted", resp.Status)
	}
}

func TestCaronteProbeViaToolsCall(t *testing.T) {
	t.Parallel()
	var (
		gotPath   string
		gotMethod string
		gotHeader string
		gotBody   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"ProjectID": "zen-swarm-aa11",
				"NodeCount": 1234,
				"EdgeCount": 5678,
				"PackageCount": 42,
				"CyclicSCCs": 0,
				"Languages": ["go", "ts"],
				"Degraded": false,
				"ResolveMode": "vta",
				"LastIndexed": 1700000000
			}
		}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	resp, err := c.CaronteProbe(context.Background(), "engine.healthy", "zen-swarm-aa11")
	if err != nil {
		t.Fatalf("CaronteProbe: %v", err)
	}
	if gotPath != "/v1/mcpgateway" {
		t.Errorf("path = %q; want /v1/mcpgateway", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q; want POST", gotMethod)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
	if !strings.Contains(gotBody, `"method":"tools/call"`) {
		t.Errorf("body missing tools/call method invocation: %s", gotBody)
	}
	if !strings.Contains(gotBody, "mcp_zen-swarm_caronte_get_health") {
		t.Errorf("body missing tool name `mcp_zen-swarm_caronte_get_health`: %s", gotBody)
	}
	if resp == nil {
		t.Fatal("resp is nil")
	}
	if resp.Status != "ok" {
		t.Errorf("synthesized status = %q; want ok (Degraded=false)", resp.Status)
	}
}

func TestCaronteProbeReportsDegradedAsFail(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"result": {
				"ProjectID": "zen-swarm-aa11",
				"NodeCount": 0,
				"EdgeCount": 0,
				"PackageCount": 0,
				"CyclicSCCs": 0,
				"Languages": [],
				"Degraded": true,
				"ResolveMode": "stale_snapshot",
				"LastIndexed": 0
			}
		}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	resp, err := c.CaronteProbe(context.Background(), "engine.healthy", "zen-swarm-aa11")
	if err != nil {
		t.Fatalf("CaronteProbe: %v", err)
	}
	if resp.Status != "fail" {
		t.Errorf("status = %q; want fail (Degraded=true)", resp.Status)
	}
}

func TestCaronteProbeJSONRPCError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jsonrpc": "2.0",
			"id": 1,
			"error": {"code": -32000, "message": "engine not constructed"}
		}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	_, err := c.CaronteProbe(context.Background(), "engine.healthy", "zen-swarm-aa11")
	if err == nil {
		t.Fatal("expected error from JSON-RPC error envelope; got nil")
	}
	if !strings.Contains(err.Error(), "engine not constructed") {
		t.Errorf("error message missing JSON-RPC message: %v", err)
	}
}

func TestCodegraphQuerySendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphQuery(context.Background(), CodegraphQueryRequest{
		Query: "x", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("CodegraphQuery: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestCodegraphQueryNoHeaderWhenEmptyAlias(t *testing.T) {
	t.Parallel()
	var headerSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, headerSeen = r.Header["X-Zen-Project-Id"]
		if h := r.Header.Get("X-Zen-Project-ID"); h != "" {
			headerSeen = true
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CodegraphQueryResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphQuery(context.Background(), CodegraphQueryRequest{Query: "x"})
	if err != nil {
		t.Fatalf("CodegraphQuery: %v", err)
	}
	if headerSeen {
		t.Error("X-Zen-Project-ID set with empty ProjectAlias; daemon-default-project path should leave the header unset")
	}
}

func TestImpactSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ImpactResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Impact(context.Background(), ImpactRequest{
		Symbol: "Foo.Bar", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestContext360SendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Context360Response{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Context360(context.Background(), Context360Request{
		Symbol: "Foo.Bar", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Context360: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestWikiSendsProjectIDHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Zen-Project-ID")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WikiResponse{})
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.Wiki(context.Background(), WikiRequest{
		Module: "internal/x", ProjectAlias: "zen-swarm-aa11",
	})
	if err != nil {
		t.Fatalf("Wiki: %v", err)
	}
	if gotHeader != "zen-swarm-aa11" {
		t.Errorf("X-Zen-Project-ID = %q; want zen-swarm-aa11", gotHeader)
	}
}

func TestCodegraphQuery404SurfacesAsHTTPError404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := NewWithBaseURL(srv.URL)
	_, err := c.CodegraphQuery(context.Background(), CodegraphQueryRequest{Query: "x"})
	if err == nil {
		t.Fatal("expected 404 error; got nil")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusNotFound {
		t.Errorf("expected *HTTPError with Status=404; got %v", err)
	}
}

func TestSynthesizeCaronteProbeRow_IndexFreshness(t *testing.T) {
	t.Parallel()
	t.Run("never_indexed", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("index.freshness", &caronteHealthReportShape{LastIndexed: 0})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn", row.Status)
		}
		if !strings.Contains(row.Detail, "never indexed") {
			t.Errorf("detail = %q; want never-indexed", row.Detail)
		}
	})
	t.Run("recent", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("index.freshness", &caronteHealthReportShape{
			LastIndexed: time.Now().Add(-1 * time.Hour).Unix(),
		})
		if row.Status != "ok" {
			t.Errorf("status = %q; want ok", row.Status)
		}
	})
	t.Run("stale", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("index.freshness", &caronteHealthReportShape{
			LastIndexed: time.Now().Add(-10 * 24 * time.Hour).Unix(),
		})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn", row.Status)
		}
		if !strings.Contains(row.Detail, "threshold") {
			t.Errorf("detail = %q; want threshold mention", row.Detail)
		}
	})
}

func TestSynthesizeCaronteProbeRow_LanguageCoverage(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("language.coverage", &caronteHealthReportShape{Languages: nil})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn", row.Status)
		}
	})
	t.Run("populated", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("language.coverage", &caronteHealthReportShape{
			Languages: []string{"go", "ts"},
		})
		if row.Status != "ok" {
			t.Errorf("status = %q; want ok", row.Status)
		}
		if !strings.Contains(row.Detail, "go") || !strings.Contains(row.Detail, "ts") {
			t.Errorf("detail = %q; expected language list", row.Detail)
		}
	})
}

func TestSynthesizeCaronteProbeRow_ProjectDBStatus(t *testing.T) {
	t.Parallel()
	t.Run("no_project_id", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("project-db.status", &caronteHealthReportShape{ProjectID: ""})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn (no project id)", row.Status)
		}
	})
	t.Run("zero_nodes", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("project-db.status", &caronteHealthReportShape{
			ProjectID: "zen-test", NodeCount: 0,
		})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn (empty db)", row.Status)
		}
	})
	t.Run("populated", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("project-db.status", &caronteHealthReportShape{
			ProjectID: "zen-test", NodeCount: 100, EdgeCount: 200, PackageCount: 5,
		})
		if row.Status != "ok" {
			t.Errorf("status = %q; want ok", row.Status)
		}
		if !strings.Contains(row.Detail, "zen-test") {
			t.Errorf("detail = %q; want project id mention", row.Detail)
		}
	})
}

func TestSynthesizeCaronteProbeRow_RerankAvailable(t *testing.T) {
	t.Parallel()
	t.Run("degraded", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("rerank.available", &caronteHealthReportShape{Degraded: true})
		if row.Status != "warn" {
			t.Errorf("status = %q; want warn", row.Status)
		}
		if !strings.Contains(row.Detail, "scripts/download-bge-model.sh") {
			t.Errorf("detail = %q; expected install-script reference", row.Detail)
		}
	})
	t.Run("healthy", func(t *testing.T) {
		row := synthesizeCaronteProbeRow("rerank.available", &caronteHealthReportShape{Degraded: false})
		if row.Status != "ok" {
			t.Errorf("status = %q; want ok", row.Status)
		}
	})
}

func TestSynthesizeCaronteProbeRow_UnknownCheck(t *testing.T) {
	t.Parallel()
	row := synthesizeCaronteProbeRow("bogus.check", &caronteHealthReportShape{})
	if row.Status != "warn" {
		t.Errorf("status = %q; want warn", row.Status)
	}
	if !strings.Contains(row.Detail, "engine.healthy") || !strings.Contains(row.Detail, "rerank.available") {
		t.Errorf("detail = %q; expected valid-check list", row.Detail)
	}
}

func TestSynthesizeCaronteProbeRow_EngineHealthyDegradedDetailIncludesResolveMode(t *testing.T) {
	t.Parallel()
	row := synthesizeCaronteProbeRow("engine.healthy", &caronteHealthReportShape{
		Degraded: true, ResolveMode: "stale_snapshot",
	})
	if row.Status != "fail" {
		t.Errorf("status = %q; want fail", row.Status)
	}
	if !strings.Contains(row.Detail, "stale_snapshot") {
		t.Errorf("detail = %q; want resolve mode mention", row.Detail)
	}
}

func TestCaronteProbeEmptyResultEnvelope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1}`))
	}))
	t.Cleanup(srv.Close)
	c := NewWithBaseURL(srv.URL)
	_, err := c.CaronteProbe(context.Background(), "engine.healthy", "")
	if err == nil {
		t.Fatal("expected error on empty result envelope; got nil")
	}
	if !strings.Contains(err.Error(), "empty result") {
		t.Errorf("err = %v; expected 'empty result' message", err)
	}
}

func TestCoordinationProbeHappyPath(t *testing.T) {
	t.Parallel()
	var gotCheck string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/coordination/probe" {
			t.Errorf("path = %s, want /v1/coordination/probe", r.URL.Path)
		}
		gotCheck = r.URL.Query().Get("check")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CoordinationProbeResp{Status: "ok", Detail: "lock acquired"})
	}))
	defer srv.Close()

	c := NewWithBaseURL(srv.URL)
	resp, err := c.CoordinationProbe(context.Background(), "lock-healthy")
	if err != nil {
		t.Fatalf("CoordinationProbe: %v", err)
	}
	if gotCheck != "lock-healthy" {
		t.Fatalf("check = %q, want lock-healthy", gotCheck)
	}
	if resp.Status != "ok" {
		t.Fatalf("Status = %q, want ok", resp.Status)
	}
}
