//go:build integration

package mcpgateway_integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	carontepkg "github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type fakeCaronteRESTE2E struct {
	mu     sync.Mutex
	calls  []string
	failOn map[string]error
}

func (f *fakeCaronteRESTE2E) CodeGraph(_ context.Context, query, projectID string) (research.CodeGraphResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, query)
	if err, ok := f.failOn[query]; ok {
		f.mu.Unlock()
		return research.CodeGraphResult{}, err
	}
	f.mu.Unlock()
	return research.CodeGraphResult{
		ProjectID: projectID,
		Hits: []research.CodeGraphHit{
			{
				Node:  "MergeEngine.SelectWinner",
				Score: 0.92,
				URL:   "caronte://" + projectID + "/internal/merge/engine.go:45?kind=func",
			},
			{
				Node:  "augment.Impact",
				Score: 0.85,
				URL:   "caronte://" + projectID + "/internal/augment/impact.go:12?kind=func",
			},
		},
	}, nil
}

func (f *fakeCaronteRESTE2E) Context(_ context.Context, _, _ string) (carontepkg.ContextResult, error) {
	return carontepkg.ContextResult{}, nil
}
func (f *fakeCaronteRESTE2E) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{Score: 0.92, Level: "high", TopAffected: []string{"internal/merge/engine.go"}}, nil
}
func (f *fakeCaronteRESTE2E) GetWhy(_ context.Context, _, _ string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{}, nil
}
func (f *fakeCaronteRESTE2E) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return nil, nil
}
func (f *fakeCaronteRESTE2E) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return nil, nil
}
func (f *fakeCaronteRESTE2E) GetCoChange(_ context.Context, _, _ string) ([]carontepkg.CoChangePeer, error) {
	return nil, nil
}
func (f *fakeCaronteRESTE2E) GetHealth(_ context.Context, _ string) (carontepkg.HealthReport, error) {
	return carontepkg.HealthReport{}, nil
}
func (f *fakeCaronteRESTE2E) GetArchitecture(_ context.Context, _ string) (carontepkg.ArchitectureReport, error) {
	return carontepkg.ArchitectureReport{}, nil
}
func (f *fakeCaronteRESTE2E) Wiki(_ context.Context, _, _ string) (carontepkg.WikiDoc, error) {
	return carontepkg.WikiDoc{}, nil
}
func (f *fakeCaronteRESTE2E) Close() error { return nil }

type restE2EMCPCtx struct{ h http.Handler }

func (c *restE2EMCPCtx) MCPGateway() http.Handler { return c.h }

func buildGatewayWithCaronte(t *testing.T, eng mcpgateway.CaronteEngine) http.Handler {
	t.Helper()
	proxy := mcpgateway.NewCaronteProxy(eng, mcpgateway.NopAuditEmitter())
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{
		Audit: mcpgateway.NopAuditEmitter(),
	})
	if err := d.RegisterSubsystem(&caronteSubsystemE2E{proxy: proxy}); err != nil {
		t.Fatalf("register caronte subsystem: %v", err)
	}
	return mcpgateway.NewServer(d)
}

type caronteSubsystemE2E struct {
	proxy *mcpgateway.CaronteProxy
}

func (s *caronteSubsystemE2E) Name() string { return "caronte" }
func (s *caronteSubsystemE2E) Tools() []mcpgateway.ToolEntry {
	out := make([]mcpgateway.ToolEntry, 0, 3)
	for _, name := range []string{"query", "context", "impact"} {
		tn := mcpgateway.MustToolName("caronte", name)
		out = append(out, mcpgateway.ToolEntry{
			Name: tn,
			Meta: mcpgateway.ToolMeta{Description: "caronte " + name},
			Handler: func(ctx context.Context, req mcpgateway.CallRequest) (mcpgateway.CallResponse, error) {
				return s.proxy.CallByTool(ctx, req)
			},
		})
	}
	return out
}

func TestCodegraphQuery_E2E_HappyPath(t *testing.T) {
	eng := &fakeCaronteRESTE2E{}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	body, _ := json.Marshal(handlers.CodegraphRESTRequest{
		Query: "MergeEngine", ProjectAlias: "internal-platform-x", Limit: 5,
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	r.Header.Set("X-Zen-Doctrine", "max-scope")
	w := httptest.NewRecorder()
	handlers.CodegraphQueryREST(mctx).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp handlers.CodegraphRESTResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(resp.Hits))
	}
	if resp.Hits[0].Symbol != "MergeEngine.SelectWinner" {
		t.Errorf("Symbol = %q", resp.Hits[0].Symbol)
	}
	if resp.Hits[0].File != "internal/merge/engine.go" {
		t.Errorf("File = %q", resp.Hits[0].File)
	}
	if resp.Hits[0].Line != 45 {
		t.Errorf("Line = %d", resp.Hits[0].Line)
	}
	if resp.Hits[0].Kind != "func" {
		t.Errorf("Kind = %q", resp.Hits[0].Kind)
	}
	if resp.Hits[0].Confidence != 92 {
		t.Errorf("Confidence = %d", resp.Hits[0].Confidence)
	}
	if len(eng.calls) != 1 || eng.calls[0] != "MergeEngine" {
		t.Errorf("caronte calls = %v, want [MergeEngine]", eng.calls)
	}
}

func TestImpact_E2E_HappyPath(t *testing.T) {
	eng := &fakeCaronteRESTE2E{}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	body, _ := json.Marshal(handlers.ImpactRESTRequest{
		Symbol: "MergeEngine.SelectWinner", ProjectAlias: "internal-platform-x",
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/impact", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.ImpactREST(mctx).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp handlers.ImpactRESTResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.BlastRadius != "high" {
		t.Errorf("BlastRadius = %q, want high (top score 0.92)", resp.BlastRadius)
	}
	if resp.Score != 92 {
		t.Errorf("Score = %d, want 92", resp.Score)
	}
	if len(resp.AffectedFiles) == 0 {
		t.Errorf("AffectedFiles empty; want at least 1 from fake fixture")
	}
}

func TestContext360_E2E_HappyPath(t *testing.T) {

	eng := &fakeCaronteContext{}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	body, _ := json.Marshal(handlers.Context360RESTRequest{
		Symbol: "Dispatcher",
	})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/context", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.Context360REST(mctx).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
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
	if resp.Community != "dispatch-subsystem" {
		t.Errorf("Community = %q", resp.Community)
	}
}

type fakeCaronteContext struct{}

func (fakeCaronteContext) CodeGraph(_ context.Context, _, projectID string) (research.CodeGraphResult, error) {
	return research.CodeGraphResult{
		ProjectID: projectID,
		Hits: []research.CodeGraphHit{
			{Node: "orchestrator.Run", Score: 0.9, URL: "caronte://" + projectID + "/callers/orch.go"},
			{Node: "provider.Call", Score: 0.85, URL: "caronte://" + projectID + "/callees/prov.go"},
			{Node: "dispatch-subsystem", Score: 0.7, URL: "caronte://" + projectID + "/community/cluster-1"},
		},
	}, nil
}
func (fakeCaronteContext) Context(_ context.Context, symbol, _ string) (carontepkg.ContextResult, error) {
	return carontepkg.ContextResult{
		Symbol:    symbol,
		Callers:   []string{"orchestrator.Run"},
		Callees:   []string{"provider.Call"},
		Community: "dispatch-subsystem",
	}, nil
}
func (fakeCaronteContext) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{}, nil
}
func (fakeCaronteContext) GetWhy(_ context.Context, _, _ string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{}, nil
}
func (fakeCaronteContext) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return nil, nil
}
func (fakeCaronteContext) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return nil, nil
}
func (fakeCaronteContext) GetCoChange(_ context.Context, _, _ string) ([]carontepkg.CoChangePeer, error) {
	return nil, nil
}
func (fakeCaronteContext) GetHealth(_ context.Context, _ string) (carontepkg.HealthReport, error) {
	return carontepkg.HealthReport{}, nil
}
func (fakeCaronteContext) GetArchitecture(_ context.Context, _ string) (carontepkg.ArchitectureReport, error) {
	return carontepkg.ArchitectureReport{}, nil
}
func (fakeCaronteContext) Wiki(_ context.Context, _, _ string) (carontepkg.WikiDoc, error) {
	return carontepkg.WikiDoc{}, nil
}
func (fakeCaronteContext) Close() error { return nil }

func TestWiki_E2E_NotRegisteredYet(t *testing.T) {

	eng := &fakeCaronteRESTE2E{}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	body, _ := json.Marshal(handlers.WikiRESTRequest{Module: "internal/daemon"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/wiki", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.WikiREST(mctx).ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (wiki tool not registered)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "wiki tool not registered") {
		t.Errorf("body = %q; expected stable 'wiki tool not registered' marker", w.Body.String())
	}
}

func TestCodegraphQuery_E2E_CaronteUnreachable(t *testing.T) {

	eng := &fakeCaronteRESTE2E{failOn: map[string]error{"FAIL": errors.New("caronte: rpc timeout")}}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "FAIL"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.CodegraphQueryREST(mctx).ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (caronte unreachable)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "rpc timeout") && !strings.Contains(w.Body.String(), "ErrCaronteUnreachable") && !strings.Contains(w.Body.String(), "caronte") {
		t.Errorf("body = %q; expected caronte-related diagnostic", w.Body.String())
	}
}

func TestCodegraphQuery_E2E_GatewayUnconfigured(t *testing.T) {

	mctx := &restE2EMCPCtx{h: nil}
	body, _ := json.Marshal(handlers.CodegraphRESTRequest{Query: "X"})
	r := httptest.NewRequest(http.MethodPost, "/v1/mcpgateway/codegraph", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handlers.CodegraphQueryREST(mctx).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestAllRESTAdapters_E2E_Smoke(t *testing.T) {
	eng := &fakeCaronteRESTE2E{}
	gw := buildGatewayWithCaronte(t, eng)
	mctx := &restE2EMCPCtx{h: gw}

	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		body    any
		expect  int
	}{
		{"codegraph", handlers.CodegraphQueryREST(mctx), "/v1/mcpgateway/codegraph",
			handlers.CodegraphRESTRequest{Query: "X"}, http.StatusOK},
		{"impact", handlers.ImpactREST(mctx), "/v1/mcpgateway/impact",
			handlers.ImpactRESTRequest{Symbol: "X"}, http.StatusOK},
		{"context", handlers.Context360REST(mctx), "/v1/mcpgateway/context",
			handlers.Context360RESTRequest{Symbol: "X"}, http.StatusOK},
		{"wiki", handlers.WikiREST(mctx), "/v1/mcpgateway/wiki",
			handlers.WikiRESTRequest{Module: "X"}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			r := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, r)
			if w.Code != tc.expect {
				t.Errorf("status = %d, want %d (body=%s)", w.Code, tc.expect, w.Body.String())
			}
		})
	}
}
