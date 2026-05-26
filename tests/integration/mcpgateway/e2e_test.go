//go:build integration

package mcpgateway_integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	caronte "github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type fakeCaronteE2E struct{}

func (fakeCaronteE2E) CodeGraph(_ context.Context, query, projectID string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{
		ProjectID: projectID,
		Hits: []research.CodeGraphHit{
			{Node: "Func." + query, Score: 0.9, URL: "caronte://" + projectID + "/" + query},
		},
	}, nil
}
func (fakeCaronteE2E) Context(_ context.Context, _, _ string) (caronte.ContextResult, error) {
	return caronte.ContextResult{}, nil
}
func (fakeCaronteE2E) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{}, nil
}
func (fakeCaronteE2E) GetWhy(_ context.Context, _, _ string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{}, nil
}
func (fakeCaronteE2E) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return nil, nil
}
func (fakeCaronteE2E) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return nil, nil
}
func (fakeCaronteE2E) GetCoChange(_ context.Context, _, _ string) ([]caronte.CoChangePeer, error) {
	return nil, nil
}
func (fakeCaronteE2E) GetHealth(_ context.Context, _ string) (caronte.HealthReport, error) {
	return caronte.HealthReport{}, nil
}
func (fakeCaronteE2E) GetArchitecture(_ context.Context, _ string) (caronte.ArchitectureReport, error) {
	return caronte.ArchitectureReport{}, nil
}
func (fakeCaronteE2E) Wiki(_ context.Context, _, _ string) (caronte.WikiDoc, error) {
	return caronte.WikiDoc{}, nil
}
func (fakeCaronteE2E) Close() error { return nil }

type fakeInProcessAudit struct{}

func (fakeInProcessAudit) Name() string { return "audit" }
func (fakeInProcessAudit) Tools() []mcpgateway.ToolEntry {
	tn := mcpgateway.MustToolName("audit", "emit")
	return []mcpgateway.ToolEntry{{
		Name: tn,
		Handler: func(_ context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
			body, _ := json.Marshal(map[string]any{
				"accepted":   true,
				"session_id": req.SessionID,
				"tool":       req.Tool.String(),
			})
			return mcpgateway.CallResponse{
				Content:   []mcpgateway.CallContentItem{{Type: "text", Text: string(body)}},
				Subsystem: "audit",
			}, nil
		},
		Meta: mcpgateway.ToolMeta{
			Description: "fake audit emit",
			InputSchema: map[string]any{"type": "object"},
		},
	}}
}

type caronteSubsystemForTest struct {
	proxy *mcpgateway.CaronteProxy
	tool  mcpgateway.ToolName
}

func (g *caronteSubsystemForTest) Name() string { return "caronte" }
func (g *caronteSubsystemForTest) Tools() []mcpgateway.ToolEntry {
	return []mcpgateway.ToolEntry{{
		Name:    g.tool,
		Handler: g.proxy.CallByTool,
		Meta:    mcpgateway.ToolMeta{Description: "fake caronte query"},
	}}
}

func mustPost(t *testing.T, ts *httptest.Server, body, doctrine string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/mcpgateway",
		bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if doctrine != "" {
		req.Header.Set("X-Zen-Doctrine", doctrine)
	}
	req.Header.Set("X-Zen-Mode", "interactive")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func newGatewayServer(t *testing.T) *httptest.Server {
	t.Helper()
	caronteProxy := mcpgateway.NewCaronteProxy(fakeCaronteE2E{}, mcpgateway.NopAuditEmitter())
	if err := caronteProxy.EnsureReachable(context.Background()); err != nil {
		t.Fatalf("EnsureReachable: %v", err)
	}
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
		RBACCfg: mcpgateway.RBACConfig{
			DoctrineDisabled: map[mcpgateway.Doctrine][]string{
				mcpgateway.DoctrineCapaFirewall: {
					"mcp_zen-swarm_caronte_query",
					"mcp_zen-swarm_caronte_context",
					"mcp_zen-swarm_caronte_impact",
				},
			},
		},
	})

	tn1 := mcpgateway.MustToolName("caronte", "query")
	if err := d.RegisterSubsystem(&caronteSubsystemForTest{
		proxy: caronteProxy,
		tool:  tn1,
	}); err != nil {
		t.Fatalf("RegisterSubsystem caronte: %v", err)
	}
	if err := d.RegisterSubsystem(fakeInProcessAudit{}); err != nil {
		t.Fatalf("RegisterSubsystem audit: %v", err)
	}
	mcpsrv := mcpgateway.NewServer(d)
	mux := http.NewServeMux()
	mux.Handle("/v1/mcpgateway", mcpsrv)
	return httptest.NewServer(mux)
}

func TestMCPGatewayToolsListRoundTrip(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	code, raw := mustPost(t, ts, body, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, raw)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Errorf("tools len = %d (want 2: caronte_query + audit_emit)", len(resp.Result.Tools))
	}
}

func TestMCPGatewayRoutesCaronteQuery(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()
	body := `{
		"jsonrpc": "2.0",
		"id": 2,
		"method": "tools/call",
		"params": {
			"name": "mcp_zen-swarm_caronte_query",
			"arguments": {"query": "MergeEngine", "project_id": "internal-platform-x"}
		}
	}`
	code, raw := mustPost(t, ts, body, "default")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, raw)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Result.Content) == 0 {
		t.Fatal("content empty")
	}
	if !strings.Contains(resp.Result.Content[0].Text, "Func.MergeEngine") {
		t.Errorf("content text = %q; expected fake hit", resp.Result.Content[0].Text)
	}
}

func TestMCPGatewayRoutesAuditEmit(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()
	body := `{
		"jsonrpc": "2.0",
		"id": 3,
		"method": "tools/call",
		"params": {
			"name": "mcp_zen-swarm_audit_emit",
			"arguments": {"type": "test"}
		}
	}`
	code, raw := mustPost(t, ts, body, "default")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, raw)
	}

	if !strings.Contains(string(raw), `\"accepted\":true`) {
		t.Errorf("body = %s; expected escaped accepted:true", raw)
	}
}

func TestMCPGatewayCapaFirewallRejectsCaronte(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()
	body := `{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {
			"name": "mcp_zen-swarm_caronte_query",
			"arguments": {"query": "x", "project_id": "p"}
		}
	}`
	code, raw := mustPost(t, ts, body, "capa-firewall")
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", code, raw)
	}
	if !strings.Contains(string(raw), `"error"`) {
		t.Errorf("body = %s; expected JSON-RPC error envelope", raw)
	}
	if !strings.Contains(string(raw), "rbac denied") {
		t.Errorf("body = %s; expected rbac-denied message", raw)
	}
}

func TestMCPGatewayMalformedToolName(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()
	body := `{
		"jsonrpc": "2.0",
		"id": 5,
		"method": "tools/call",
		"params": {"name": "not_canonical"}
	}`
	code, raw := mustPost(t, ts, body, "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(string(raw), `"code":-32602`) {
		t.Errorf("body = %s; expected -32602 invalid params", raw)
	}
}

func TestMCPGatewayConcurrentDispatch(t *testing.T) {
	ts := newGatewayServer(t)
	defer ts.Close()

	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"mcp_zen-swarm_audit_emit","arguments":{"x":1}}}`
			code, raw := mustPost(t, ts, body, "default")
			if code != http.StatusOK {
				errs <- fmt.Errorf("status %d body %s", code, raw)
				return
			}
			errs <- nil
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent: %v", err)
		}
	}
}
