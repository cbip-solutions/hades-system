package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type fakeGwCtx struct {
	h http.Handler
}

func (f *fakeGwCtx) MCPGateway() http.Handler { return f.h }

func fakeGw(t *testing.T, assertion func(toolName string, args map[string]any, headers http.Header), result func(toolName string) (any, *jsonrpcErrorPayload)) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mcpgateway" {
			t.Errorf("inner path = %q, want /v1/mcpgateway", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var in struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      int            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			t.Fatalf("decode inner request: %v", err)
		}
		if in.JSONRPC != "2.0" {
			t.Errorf("jsonrpc = %q, want 2.0", in.JSONRPC)
		}
		if in.Method != "tools/call" {
			t.Errorf("method = %q, want tools/call", in.Method)
		}
		toolName, _ := in.Params["name"].(string)
		argsAny, _ := in.Params["arguments"]
		args, _ := argsAny.(map[string]any)
		assertion(toolName, args, r.Header)

		payload, errPayload := result(toolName)
		w.Header().Set("Content-Type", "application/json")
		if errPayload != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"error": errPayload,
			})
			return
		}

		text, _ := json.Marshal(payload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": string(text)}},
				"isError": false,
			},
		})
	})
}

type jsonrpcErrorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func caronteHits(hits ...caronteHit) map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"node":  h.node,
			"score": h.score,
			"url":   h.url,
		})
	}
	return map[string]any{"hits": out, "project_id": "test-proj"}
}

type caronteHit struct {
	node  string
	score float64
	url   string
}

func TestCodegraphQueryRESTHappyPath(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_query" {
				t.Errorf("toolName = %q, want caronte.query", toolName)
			}
			if args["query"] != "MergeEngine" {
				t.Errorf("args[query] = %v, want MergeEngine", args["query"])
			}
			if args["project_id"] != "internal-platform-x" {
				t.Errorf("args[project_id] = %v, want internal-platform-x", args["project_id"])
			}
		},
		func(string) (any, *jsonrpcErrorPayload) {
			return caronteHits(caronteHit{
				node: "MergeEngine.Run", score: 0.92,
				url: "caronte://internal-platform-x/internal/merge/engine.go:42?kind=func",
			}), nil
		},
	)
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: gw})

	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "MergeEngine", ProjectAlias: "internal-platform-x", Limit: 5})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp handlers.CodegraphRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(resp.Hits))
	}
	got := resp.Hits[0]
	if got.Symbol != "MergeEngine.Run" {
		t.Errorf("Symbol = %q", got.Symbol)
	}
	if got.File != "internal/merge/engine.go" {
		t.Errorf("File = %q", got.File)
	}
	if got.Line != 42 {
		t.Errorf("Line = %d", got.Line)
	}
	if got.Kind != "func" {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Confidence != 92 {
		t.Errorf("Confidence = %d", got.Confidence)
	}
}

func TestCodegraphQueryRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/codegraph", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestCodegraphQueryRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: nil})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "mcpgateway not configured") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestCodegraphQueryRESTEmptyQuery(t *testing.T) {
	t.Parallel()
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: " "})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCodegraphQueryRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCodegraphQueryRESTLimitTruncates(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(string, map[string]any, http.Header) {},
		func(string) (any, *jsonrpcErrorPayload) {
			return caronteHits(
				caronteHit{node: "A", score: 0.9, url: "caronte://p/a.go:1"},
				caronteHit{node: "B", score: 0.8, url: "caronte://p/b.go:2"},
				caronteHit{node: "C", score: 0.7, url: "caronte://p/c.go:3"},
			), nil
		},
	)
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X", Limit: 2})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp handlers.CodegraphRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Hits) != 2 {
		t.Errorf("hits = %d, want 2 (limit truncates)", len(resp.Hits))
	}
}

func TestCodegraphQueryRESTHeaderForwarding(t *testing.T) {
	t.Parallel()
	var gotDoctrine, gotMode, gotSession, gotProject string
	gw := fakeGw(t,
		func(_ string, _ map[string]any, h http.Header) {
			gotDoctrine = h.Get("X-Zen-Doctrine")
			gotMode = h.Get("X-Zen-Mode")
			gotSession = h.Get("X-Zen-Session-ID")
			gotProject = h.Get("X-Zen-Project-ID")
		},
		func(string) (any, *jsonrpcErrorPayload) { return caronteHits(), nil },
	)
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	r.Header.Set("X-Zen-Doctrine", "max-scope")
	r.Header.Set("X-Zen-Mode", "interactive")
	r.Header.Set("X-Zen-Session-ID", "sess-1")
	r.Header.Set("X-Zen-Project-ID", "proj-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if gotDoctrine != "max-scope" {
		t.Errorf("doctrine = %q", gotDoctrine)
	}
	if gotMode != "interactive" {
		t.Errorf("mode = %q", gotMode)
	}
	if gotSession != "sess-1" {
		t.Errorf("session = %q", gotSession)
	}
	if gotProject != "proj-1" {
		t.Errorf("project = %q", gotProject)
	}
}

func TestCodegraphQueryRESTJSONRPCMethodNotFound(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(string, map[string]any, http.Header) {},
		func(string) (any, *jsonrpcErrorPayload) {
			return nil, &jsonrpcErrorPayload{Code: -32601, Message: "method not found"}
		},
	)
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
}

func TestCodegraphQueryRESTJSONRPCServerError(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(string, map[string]any, http.Header) {},
		func(string) (any, *jsonrpcErrorPayload) {
			return nil, &jsonrpcErrorPayload{Code: -32000, Message: "caronte unreachable"}
		},
	)
	h := handlers.CodegraphQueryREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
	if !strings.Contains(w.Body.String(), "caronte unreachable") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestImpactRESTHappyPath(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_impact" {
				t.Errorf("toolName = %q, want caronte.impact", toolName)
			}
			cs, _ := args["changed_symbols"].([]any)
			if len(cs) != 1 || cs[0] != "MergeEngine.Run" {
				t.Errorf("args[changed_symbols] = %v, want [MergeEngine.Run]", args["changed_symbols"])
			}
		},
		func(string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"Score":       0.85,
				"Level":       "high",
				"TopAffected": []string{"internal/merge/engine.go", "internal/orchestrator.go"},
			}, nil
		},
	)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.ImpactRESTRequest{Symbol: "MergeEngine.Run"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp handlers.ImpactRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Symbol != "MergeEngine.Run" {
		t.Errorf("Symbol = %q", resp.Symbol)
	}
	if resp.BlastRadius != "high" {
		t.Errorf("BlastRadius = %q, want high (from RiskScore.Level)", resp.BlastRadius)
	}
	if resp.Score != 85 {
		t.Errorf("Score = %d, want 85 (0.85*100)", resp.Score)
	}
	if len(resp.AffectedFiles) != 2 {
		t.Errorf("AffectedFiles = %v, want 2 (from RiskScore.TopAffected)", resp.AffectedFiles)
	}
}

func TestImpactRESTBlastRadiusMedium(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(string, map[string]any, http.Header) {},
		func(string) (any, *jsonrpcErrorPayload) {
			return map[string]any{"Score": 0.55, "Level": "medium", "TopAffected": []string{}}, nil
		},
	)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.ImpactRESTRequest{Symbol: "S"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.ImpactRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.BlastRadius != "medium" {
		t.Errorf("BlastRadius = %q, want medium", resp.BlastRadius)
	}
}

func TestImpactRESTBlastRadiusLow(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(string, map[string]any, http.Header) {},
		func(string) (any, *jsonrpcErrorPayload) {
			return map[string]any{"Score": 0.20, "Level": "low", "TopAffected": []string{}}, nil
		},
	)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.ImpactRESTRequest{Symbol: "S"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var resp handlers.ImpactRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.BlastRadius != "low" {
		t.Errorf("BlastRadius = %q, want low", resp.BlastRadius)
	}
}

func TestImpactRESTMissingSymbol(t *testing.T) {
	t.Parallel()
	h := handlers.ImpactREST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.ImpactRESTRequest{Symbol: ""})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestImpactRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.ImpactREST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/impact", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestImpactRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.ImpactREST(&fakeGwCtx{h: nil})
	body, _ := json.Marshal(handlers.ImpactRESTRequest{Symbol: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestContext360RESTHappyPath(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_context" {
				t.Errorf("toolName = %q, want caronte.context", toolName)
			}
			if args["symbol"] != "Dispatcher" {
				t.Errorf("args[symbol] = %v, want Dispatcher", args["symbol"])
			}
		},
		func(string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"Symbol":    "Dispatcher",
				"Callers":   []string{"orchestrator.Run"},
				"Callees":   []string{"provider.Call"},
				"Neighbors": []string{"Router"},
				"Community": "dispatch-subsystem",
				"Coreness":  3,
				"SCCID":     0,
				"Cyclic":    false,
			}, nil
		},
	)
	h := handlers.Context360REST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.Context360RESTRequest{Symbol: "Dispatcher"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp handlers.Context360RESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Symbol != "Dispatcher" {
		t.Errorf("Symbol = %q", resp.Symbol)
	}
	if len(resp.Callers) != 1 || resp.Callers[0] != "orchestrator.Run" {
		t.Errorf("Callers = %v", resp.Callers)
	}
	if len(resp.Callees) != 1 || resp.Callees[0] != "provider.Call" {
		t.Errorf("Callees = %v", resp.Callees)
	}
	if len(resp.Neighbors) != 1 || resp.Neighbors[0] != "Router" {
		t.Errorf("Neighbors = %v", resp.Neighbors)
	}
	if resp.Community != "dispatch-subsystem" {
		t.Errorf("Community = %q", resp.Community)
	}
}

func TestContext360RESTMissingSymbol(t *testing.T) {
	t.Parallel()
	h := handlers.Context360REST(&fakeGwCtx{h: http.NotFoundHandler()})
	body, _ := json.Marshal(handlers.Context360RESTRequest{})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestContext360RESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.Context360REST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/context", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestContext360RESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.Context360REST(&fakeGwCtx{h: nil})
	body, _ := json.Marshal(handlers.Context360RESTRequest{Symbol: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestWikiRESTNotRegisteredYet(t *testing.T) {
	t.Parallel()

	gw := fakeGw(t,
		func(toolName string, _ map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_wiki" {
				t.Errorf("toolName = %q, want caronte.wiki", toolName)
			}
		},
		func(string) (any, *jsonrpcErrorPayload) {
			return nil, &jsonrpcErrorPayload{Code: -32601, Message: "method not found"}
		},
	)
	h := handlers.WikiREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.WikiRESTRequest{Module: "internal/daemon"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/wiki", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "wiki tool not registered") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestWikiRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.WikiREST(&fakeGwCtx{h: http.NotFoundHandler()})
	r := httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/wiki", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestWikiRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.WikiREST(&fakeGwCtx{h: nil})
	body, _ := json.Marshal(handlers.WikiRESTRequest{Module: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/wiki", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestWikiRESTHappyPath(t *testing.T) {
	t.Parallel()
	gw := fakeGw(t,
		func(toolName string, args map[string]any, _ http.Header) {
			if toolName != "mcp_zen-swarm_caronte_wiki" {
				t.Errorf("toolName = %q, want caronte.wiki", toolName)
			}
			if args["module"] != "internal/daemon" {
				t.Errorf("args[module] = %v, want internal/daemon", args["module"])
			}
		},
		func(string) (any, *jsonrpcErrorPayload) {

			return map[string]any{
				"module":   "internal/daemon",
				"markdown": "# internal/daemon\n- Dispatcher\n- Orchestrator\n",
			}, nil
		},
	)
	h := handlers.WikiREST(&fakeGwCtx{h: gw})
	body, _ := json.Marshal(handlers.WikiRESTRequest{Module: "internal/daemon"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/wiki", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp handlers.WikiRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Module != "internal/daemon" {
		t.Errorf("Module = %q", resp.Module)
	}
	if !strings.HasPrefix(resp.Markdown, "# internal/daemon") {
		t.Errorf("Markdown = %q", resp.Markdown)
	}
}

func fakeCaronteGateway(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var text string
		switch {
		case strings.HasSuffix(req.Params.Name, "_get_why"):
			text = `{"Subject":"pkg.M","LinkedADRs":[{"ADRID":"ADR-0081","ADRTitle":"lanes","LinkKind":"explicit_ref","Confidence":0.9,"Stale":false}],"SemanticPassages":[{"SourceID":"ADR-0081","SourceKind":"adr","Text":"x","Score":0.8}],"LoreTrailers":[{"CommitSHA":"abc","TrailerKind":"constraint","Body":"no subprocess","AuthoredAt":1700000000}],"Degraded":false}`
		case strings.HasSuffix(req.Params.Name, "_get_risk"):
			text = `{"Score":0.72,"Level":"high","Cone":0.5,"Coreness":0.6,"Churn":0.3,"Coupling":0.2,"TopAffected":["pkg.A","pkg.B"]}`
		case strings.HasSuffix(req.Params.Name, "_get_cochange"):
			text = `{"peers":[{"Path":"b.go","CouplingPercent":60,"SharedRevs":6,"WindowDays":90}]}`
		case strings.HasSuffix(req.Params.Name, "_get_implementations"):
			text = `{"implementations":[{"InterfaceID":"io.Writer","ImplID":"bytes.Buffer","Confidence":"exact_vta","Reachable":true}]}`
		case strings.HasSuffix(req.Params.Name, "_get_health"):
			text = `{"ProjectID":"p","NodeCount":100,"EdgeCount":250,"PackageCount":12,"CyclicSCCs":1,"Languages":["go"],"Degraded":false,"ResolveMode":"vta","LastIndexed":1700000000}`
		case strings.HasSuffix(req.Params.Name, "_context"):
			text = `{"Symbol":"pkg/x.B","Callers":["pkg/x.A"],"Callees":["pkg/y.C"],"Neighbors":[],"Community":"pkg/x","Coreness":2,"SCCID":1,"Cyclic":false}`
		case strings.HasSuffix(req.Params.Name, "_impact"):
			text = `{"Score":0.72,"Level":"high","Cone":0.5,"Coreness":0.6,"Churn":0.3,"Coupling":0.2,"TopAffected":["pkg/x.A","pkg/y.C"]}`
		case strings.HasSuffix(req.Params.Name, "_wiki"):
			text = `{"module":"pkg/x","markdown":"# pkg/x\n- A"}`
		default:
			text = `{"hits":[{"node":"pkg/x.Widget","score":0.9,"url":"caronte://p/pkg/x.Widget"}],"project_id":"p"}`
		}
		resp := map[string]any{
			"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{"content": []map[string]any{{"type": "text", "text": text}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func TestContext360RESTPartitionsRealNeighbourhood(t *testing.T) {
	t.Parallel()
	gw := fakeCaronteGateway(t)
	h := handlers.Context360REST(&fakeGwCtx{h: gw})
	rec := httptest.NewRecorder()
	body := `{"symbol":"pkg/x.B","project_alias":"proj-1"}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.Context360RESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Callers) != 1 || out.Callers[0] != "pkg/x.A" {
		t.Errorf("Callers = %v; want [pkg/x.A] (real context, not alias)", out.Callers)
	}
	if out.Community != "pkg/x" {
		t.Errorf("Community = %q; want pkg/x", out.Community)
	}
}

func TestImpactRESTUsesRealRiskScore(t *testing.T) {
	t.Parallel()
	gw := fakeCaronteGateway(t)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	rec := httptest.NewRecorder()
	body := `{"symbol":"pkg/x.A","project_alias":"proj-1"}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.ImpactRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BlastRadius != "high" {
		t.Errorf("BlastRadius = %q; want high (from RiskScore.Level)", out.BlastRadius)
	}
	if len(out.AffectedFiles) != 2 {
		t.Errorf("AffectedFiles = %v; want 2 (from RiskScore.TopAffected)", out.AffectedFiles)
	}
}

func TestWikiRESTReturnsRealMarkdown(t *testing.T) {
	t.Parallel()
	gw := fakeCaronteGateway(t)
	h := handlers.WikiREST(&fakeGwCtx{h: gw})
	rec := httptest.NewRecorder()
	body := `{"module":"pkg/x","project_alias":"proj-1"}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/wiki", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s (wiki should be real now, not 503)", rec.Code, rec.Body.String())
	}
	var out handlers.WikiRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Markdown == "" || !strings.Contains(out.Markdown, "pkg/x") {
		t.Errorf("Wiki Markdown = %q; want non-empty per-module markdown", out.Markdown)
	}
}

func TestImpactRESTScoreFieldMapping(t *testing.T) {
	t.Parallel()
	gw := fakeCaronteGateway(t)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	rec := httptest.NewRecorder()
	body := `{"symbol":"pkg/x.A","project_alias":"proj-1"}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.ImpactRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if out.Score != 72 {
		t.Errorf("Score = %d; want 72 (float 0.72 → int [0,100])", out.Score)
	}
}

func TestContext360RESTErrorPath(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.Context360REST(&fakeGwCtx{h: errGw})
	body := `{"symbol":"pkg/x.B","project_alias":"proj-1"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", strings.NewReader(body)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}

func TestImpactRESTArgsChangedSymbols(t *testing.T) {
	t.Parallel()
	var gotArgs map[string]any
	gw := fakeGw(t,
		func(_ string, args map[string]any, _ http.Header) {
			gotArgs = args
		},
		func(string) (any, *jsonrpcErrorPayload) {
			return map[string]any{
				"Score": 0.5, "Level": "medium", "TopAffected": []string{"pkg/a.go"},
			}, nil
		},
	)
	h := handlers.ImpactREST(&fakeGwCtx{h: gw})
	body := `{"symbol":"pkg/x.A","project_alias":"proj-1"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}

	if _, hasQuery := gotArgs["query"]; hasQuery {
		t.Errorf("ImpactREST still sends args[query]; want changed_symbols after J-11 repoint")
	}
	if _, hasCS := gotArgs["changed_symbols"]; !hasCS {
		t.Errorf("ImpactREST missing args[changed_symbols] after J-11 repoint; got %v", gotArgs)
	}
}

func TestWhyRESTMapsWhyAnswer(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader(`{"symbol":"pkg.M","project_alias":"p"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.WhyRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Subject != "pkg.M" || len(out.LinkedADRs) != 1 || out.LinkedADRs[0].ADRID != "ADR-0081" {
		t.Errorf("WhyRESTResponse = %+v", out)
	}
	if len(out.LoreTrailers) != 1 || out.LoreTrailers[0].TrailerKind != "constraint" {
		t.Errorf("LoreTrailers = %+v", out.LoreTrailers)
	}
}

func TestWhyRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d; want 400", rec.Code)
	}
}

func TestWhyRESTRequiresSymbol(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty symbol → status %d; want 400", rec.Code)
	}
}

func TestWhyRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/why", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d; want 405", rec.Code)
	}
}

func TestWhyRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader(`{"symbol":"pkg.M"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d; want 503", rec.Code)
	}
}

func TestRiskRESTMapsRiskScore(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader(`{"changed_symbols":["pkg.A"],"changed_files":["a/b.go"],"project_alias":"p"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.RiskRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Level != "high" || out.Score != 0.72 || len(out.TopAffected) != 2 {
		t.Errorf("RiskRESTResponse = %+v", out)
	}
}

func TestRiskRESTRequiresInput(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty input → status %d; want 400", rec.Code)
	}
}

func TestRiskRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/risk", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d; want 405", rec.Code)
	}
}

func TestRiskRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader(`{"changed_symbols":["pkg.A"]}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d; want 503", rec.Code)
	}
}

func TestCochangeRESTMapsPeers(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader(`{"file":"a.go","project_alias":"p"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.CoChangeRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Peers) != 1 || out.Peers[0].Path != "b.go" || out.Peers[0].CouplingPercent != 60 {
		t.Errorf("CoChangeRESTResponse = %+v", out)
	}
}

func TestCochangeRESTRequiresFile(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty file → status %d; want 400", rec.Code)
	}
}

func TestCochangeRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/cochange", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d; want 405", rec.Code)
	}
}

func TestCochangeRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader(`{"file":"a.go"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d; want 503", rec.Code)
	}
}

func TestImplRESTMapsImplementations(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader(`{"interface":"io.Writer","project_alias":"p"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.ImplRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Implementations) != 1 || out.Implementations[0].ImplID != "bytes.Buffer" {
		t.Errorf("ImplRESTResponse = %+v", out)
	}
}

func TestImplRESTRequiresInterface(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty interface → status %d; want 400", rec.Code)
	}
}

func TestImplRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/impl", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d; want 405", rec.Code)
	}
}

func TestImplRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader(`{"interface":"io.Writer"}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d; want 503", rec.Code)
	}
}

func TestHealthRESTMapsHealthReport(t *testing.T) {
	t.Parallel()
	h := handlers.HealthREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/health", strings.NewReader(`{"project_alias":"p"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out handlers.HealthRESTResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.NodeCount != 100 || out.LastIndexed != 1700000000 || out.ResolveMode != "vta" {
		t.Errorf("HealthRESTResponse = %+v", out)
	}
}

func TestHealthRESTMethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := handlers.HealthREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/mcpgateway/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d; want 405", rec.Code)
	}
}

func TestHealthRESTGatewayUnconfigured(t *testing.T) {
	t.Parallel()
	h := handlers.HealthREST(&fakeGwCtx{h: nil})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/health", strings.NewReader(`{}`)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d; want 503", rec.Code)
	}
}

func malformedGateway(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{"content": []map[string]any{{"type": "text", "text": "NOT-VALID-JSON{"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func TestWhyRESTBadGatewayDecode(t *testing.T) {
	t.Parallel()
	h := handlers.WhyREST(&fakeGwCtx{h: malformedGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader(`{"symbol":"pkg.M"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d; want 502 on malformed payload", rec.Code)
	}
}

func TestRiskRESTBadGatewayDecode(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: malformedGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader(`{"changed_symbols":["pkg.A"]}`)))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d; want 502 on malformed payload", rec.Code)
	}
}

func TestCochangeRESTBadGatewayDecode(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: malformedGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader(`{"file":"a.go"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d; want 502 on malformed payload", rec.Code)
	}
}

func TestImplRESTBadGatewayDecode(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: malformedGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader(`{"interface":"io.Writer"}`)))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d; want 502 on malformed payload", rec.Code)
	}
}

func TestHealthRESTBadGatewayDecode(t *testing.T) {
	t.Parallel()
	h := handlers.HealthREST(&fakeGwCtx{h: malformedGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/health", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status %d; want 502 on malformed payload", rec.Code)
	}
}

func TestRiskRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.RiskREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d; want 400", rec.Code)
	}
}

func TestCochangeRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.CochangeREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d; want 400", rec.Code)
	}
}

func TestImplRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.ImplREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d; want 400", rec.Code)
	}
}

func TestHealthRESTInvalidJSON(t *testing.T) {
	t.Parallel()
	h := handlers.HealthREST(&fakeGwCtx{h: fakeCaronteGateway(t)})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/health", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d; want 400", rec.Code)
	}
}

func TestWhyRESTGatewayError(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.WhyREST(&fakeGwCtx{h: errGw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/why", strings.NewReader(`{"symbol":"pkg.M"}`)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}

func TestRiskRESTGatewayError(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.RiskREST(&fakeGwCtx{h: errGw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/risk", strings.NewReader(`{"changed_symbols":["pkg.A"]}`)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}

func TestCochangeRESTGatewayError(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.CochangeREST(&fakeGwCtx{h: errGw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/cochange", strings.NewReader(`{"file":"a.go"}`)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}

func TestImplRESTGatewayError(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.ImplREST(&fakeGwCtx{h: errGw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impl", strings.NewReader(`{"interface":"io.Writer"}`)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}

func TestHealthRESTGatewayError(t *testing.T) {
	t.Parallel()
	errGw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "engine fault", http.StatusInternalServerError)
	})
	h := handlers.HealthREST(&fakeGwCtx{h: errGw})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/health", strings.NewReader(`{}`)))
	if rec.Code == http.StatusOK {
		t.Errorf("expected non-200 on gateway error, got 200")
	}
}
