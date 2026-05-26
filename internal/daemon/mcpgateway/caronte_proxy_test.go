package mcpgateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	caronte "github.com/cbip-solutions/hades-system/internal/caronte"
	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type fakeCaronteEngine struct {
	failAll bool
	closed  bool
}

func (f *fakeCaronteEngine) errOrNil() error {
	if f.failAll {
		return errors.New("engine boom")
	}
	return nil
}
func (f *fakeCaronteEngine) CodeGraph(_ context.Context, query, projectID string) (research.CodeGraphResult, error) {
	if err := f.errOrNil(); err != nil {
		return research.CodeGraphResult{ProjectID: projectID}, err
	}
	return research.CodeGraphResult{Hits: []research.CodeGraphHit{{Node: "pkg/x." + query, Score: 0.9}}, ProjectID: projectID}, nil
}
func (f *fakeCaronteEngine) Context(_ context.Context, symbol, _ string) (caronte.ContextResult, error) {
	return caronte.ContextResult{Symbol: symbol, Callers: []string{"pkg/y.Caller"}}, f.errOrNil()
}
func (f *fakeCaronteEngine) BlastRadius(_ context.Context, _ string, _, _ []string) (evolution.RiskScore, error) {
	return evolution.RiskScore{Score: 0.7, Level: "high"}, f.errOrNil()
}
func (f *fakeCaronteEngine) GetWhy(_ context.Context, _, subject string) (intent.WhyAnswer, error) {
	return intent.WhyAnswer{Subject: subject}, f.errOrNil()
}
func (f *fakeCaronteEngine) GetImplementations(_ context.Context, _, _ string) ([]semantic.Implementation, error) {
	return []semantic.Implementation{{InterfaceID: "I", ImplID: "T"}}, f.errOrNil()
}
func (f *fakeCaronteEngine) TraceCallPath(_ context.Context, _ string, _ int, _ string) ([]semantic.CallPathHop, error) {
	return []semantic.CallPathHop{{FromID: "A", ToID: "B", Depth: 1}}, f.errOrNil()
}
func (f *fakeCaronteEngine) GetCoChange(_ context.Context, _, _ string) ([]caronte.CoChangePeer, error) {
	return []caronte.CoChangePeer{{Path: "b.go", CouplingPercent: 42}}, f.errOrNil()
}
func (f *fakeCaronteEngine) GetHealth(_ context.Context, projectID string) (caronte.HealthReport, error) {
	return caronte.HealthReport{ProjectID: projectID, NodeCount: 3}, f.errOrNil()
}
func (f *fakeCaronteEngine) GetArchitecture(_ context.Context, _ string) (caronte.ArchitectureReport, error) {
	return caronte.ArchitectureReport{Packages: []caronte.PackageNode{{PackageID: "pkg/x"}}}, f.errOrNil()
}
func (f *fakeCaronteEngine) Wiki(_ context.Context, module, _ string) (caronte.WikiDoc, error) {
	return caronte.WikiDoc{Module: module, Markdown: "# " + module}, f.errOrNil()
}
func (f *fakeCaronteEngine) Close() error { f.closed = true; return nil }

func (f *fakeCaronteEngine) GetContract(_ context.Context, endpointID, _ string) (caronte.ContractPayload, error) {
	return caronte.ContractPayload{
		EndpointID:    endpointID,
		Repo:          "repo-a",
		Kind:          "http",
		Method:        "GET",
		PathTemplate:  "/users/{id}",
		HandlerNodeID: "node-1",
		ExtractedAt:   1700000000,
		ExtractorID:   "oasdiff",
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) GetConsumers(_ context.Context, endpointID, workspaceID string) (caronte.ConsumerList, error) {
	return caronte.ConsumerList{
		EndpointID:   endpointID,
		EndpointRepo: "repo-a",
		WorkspaceID:  workspaceID,
		Consumers: []caronte.ConsumerLink{
			{CallID: "call-1", Repo: "repo-b", Confidence: "spec_artifact", LinkMethod: "artifact"},
		},
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) GetBreakingChanges(_ context.Context, workspaceID string, _ int64) ([]caronte.BreakingChangePayload, error) {
	return []caronte.BreakingChangePayload{{
		ChangeID:     "chg-1",
		WorkspaceID:  workspaceID,
		EndpointID:   "endpoint-1",
		EndpointRepo: "repo-a",
		Kind:         "param_added_required",
		DetectedAt:   1700000000,
		DetectorID:   "oasdiff",
	}}, f.errOrNil()
}

func (f *fakeCaronteEngine) TraceAPICall(_ context.Context, callID, workspaceID string) (caronte.APICallTrace, error) {
	return caronte.APICallTrace{
		CallID:       callID,
		CallRepo:     "repo-b",
		WorkspaceID:  workspaceID,
		EndpointID:   "endpoint-1",
		EndpointRepo: "repo-a",
		Confidence:   "spec_artifact",
		LinkMethod:   "artifact",
		Unresolved:   false,
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) GetWorkspace(_ context.Context, workspaceID string) (caronte.WorkspaceSnapshot, error) {
	return caronte.WorkspaceSnapshot{
		WorkspaceID:   workspaceID,
		OwningProject: "proj-a",
		Members:       []string{"proj-a", "proj-b"},
		PolicyLocked:  false,
		CreatedAt:     1700000000,
		SchemaVersion: 1,
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) FederationHealth(_ context.Context, workspaceID string) (caronte.FederationHealthReport, error) {
	return caronte.FederationHealthReport{
		WorkspaceID:      workspaceID,
		Reachable:        true,
		GateLatencyP95Ms: 1.2,
		UnresolvedCount:  0,
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) ContractDiff(_ context.Context, endpointID string, sinceUnix int64) (caronte.ContractDiff, error) {
	return caronte.ContractDiff{
		EndpointID:   endpointID,
		EndpointRepo: "repo-a",
		SinceUnix:    sinceUnix,
		HeadUnix:     1700000100,
		DetectorID:   "oasdiff",
		Severity:     "BREAKING",
		Kind:         "param_added_required",
	}, f.errOrNil()
}

func (f *fakeCaronteEngine) GetWhyBreakingChange(_ context.Context, changeID string) (caronte.WhyBreakingChange, error) {
	return caronte.WhyBreakingChange{
		ChangeID:       changeID,
		WorkspaceID:    "ws-1",
		EndpointID:     "endpoint-1",
		EndpointRepo:   "repo-a",
		LoreAuthor:     "alice@example.com",
		LoreCommitSHA:  "abc1234",
		LoreADRRefs:    []string{"ADR-0114"},
		LoreSupersedes: []string{},
		DetectedAt:     1700000000,
	}, f.errOrNil()
}

func TestCaronteProxyExposesElevenPlan19Tools(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	tools := p.Tools()
	want := map[string]bool{
		"query": true, "context": true, "impact": true, "wiki": true,
		"get_risk": true, "get_why": true, "get_health": true,
		"trace_call_path": true, "get_cochange": true,
		"get_implementations": true, "get_architecture": true,
	}
	for _, tn := range tools {
		if tn.Subsystem() != "caronte" {
			t.Errorf("tool %q subsystem = %q; want caronte", tn.Tool(), tn.Subsystem())
		}
		delete(want, tn.Tool())
	}
	if len(want) != 0 {
		t.Errorf("missing Plan-19-K tools: %v", want)
	}
}

func TestCaronteProxyEnsureReachableNilEngine(t *testing.T) {
	p := NewCaronteProxy(nil, nil)
	if err := p.EnsureReachable(context.Background()); !errors.Is(err, ErrCaronteBootstrapRequired) {
		t.Errorf("EnsureReachable(nil engine) = %v; want ErrCaronteBootstrapRequired", err)
	}
}

func TestCaronteProxyQueryDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "query"),
		Args:      map[string]any{"query": "Widget"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(query): %v", err)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "pkg/x.Widget") {
		t.Errorf("query response = %+v; want hits containing pkg/x.Widget", resp.Content)
	}
	if resp.Subsystem != "caronte" {
		t.Errorf("response Subsystem = %q; want caronte", resp.Subsystem)
	}
}

func TestCaronteProxyImpactIsDistinct(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool: MustToolName("caronte", "impact"),
		Args: map[string]any{"symbol": "pkg/x.A"}, ProjectID: "proj-1", Mode: ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(impact): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "high") {
		t.Errorf("impact response = %+v; want blast-radius level high", resp.Content)
	}
}

func TestCaronteProxyEscalatePerMode(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, NopAuditEmitter())
	cases := []struct {
		mode Mode
		want string
	}{
		{ModeAutonomy, "escalate=WAITING_FOR_CONFIRMATION"},
		{ModeAFK, "degrade=cached_summary"},
		{ModeInteractive, "degrade=doctor_warning"},
		{ModeUnspecified, "degrade=doctor_warning"},
	}
	for _, c := range cases {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool: MustToolName("caronte", "get_health"), ProjectID: "p", Mode: c.mode,
		})
		if err == nil {
			t.Fatalf("mode %v: expected error, got nil", c.mode)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("mode %v escalation = %q; want substring %q", c.mode, err.Error(), c.want)
		}
		if !errors.Is(err, ErrCaronteUnreachable) {
			t.Errorf("mode %v: err not wrapping ErrCaronteUnreachable: %v", c.mode, err)
		}
	}
}

func TestCaronteProxyUnknownToolRejected(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_ = p

	bogus, _ := NewToolName("caronte", "no-such-op")
	if _, err := p.CallByTool(context.Background(), CallRequest{Tool: bogus, Mode: ModeInteractive}); !errors.Is(err, ErrToolNotRegistered) {
		t.Errorf("CallByTool(unknown) err = %v; want ErrToolNotRegistered", err)
	}
}

func TestCaronteProxyCloseDelegates(t *testing.T) {
	fe := &fakeCaronteEngine{}
	p := NewCaronteProxy(fe, NopAuditEmitter())
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fe.closed {
		t.Error("Close did not propagate to the engine")
	}
}

func TestCaronteProxyContextDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "context"),
		Args:      map[string]any{"symbol": "pkg/x.Widget"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(context): %v", err)
	}

	if !strings.Contains(resp.Content[0].Text, "Caller") {
		t.Errorf("context response = %+v; want callers field from ContextResult", resp.Content)
	}
}

func TestCaronteProxyWikiDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "wiki"),
		Args:      map[string]any{"module": "pkg/payments"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(wiki): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "pkg/payments") {
		t.Errorf("wiki response = %+v; want module in output", resp.Content)
	}
}

func TestCaronteProxyGetRiskDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_risk"),
		Args:      map[string]any{"changed_symbols": []any{"pkg/x.A"}},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_risk): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "high") {
		t.Errorf("get_risk response = %+v; want RiskScore level high", resp.Content)
	}
}

func TestCaronteProxyGetWhyDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_why"),
		Args:      map[string]any{"subject": "pkg/x.Widget"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_why): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "Widget") {
		t.Errorf("get_why response = %+v; want subject in output", resp.Content)
	}
}

func TestCaronteProxyGetImplementationsDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_implementations"),
		Args:      map[string]any{"interface": "I"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_implementations): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, `"I"`) {
		t.Errorf("get_implementations response = %+v; want InterfaceID I", resp.Content)
	}
}

func TestCaronteProxyTraceCallPathDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_call_path"),
		Args:      map[string]any{"symbol": "pkg/x.Widget", "depth": float64(3)},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(trace_call_path): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, `"A"`) {
		t.Errorf("trace_call_path response = %+v; want hops with A", resp.Content)
	}
}

func TestCaronteProxyGetCoChangeDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_cochange"),
		Args:      map[string]any{"file": "internal/x/x.go"},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_cochange): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "b.go") {
		t.Errorf("get_cochange response = %+v; want peer b.go", resp.Content)
	}
}

func TestCaronteProxyGetHealthDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_health"),
		Args:      nil,
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_health): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "proj-1") {
		t.Errorf("get_health response = %+v; want project_id proj-1", resp.Content)
	}
}

func TestCaronteProxyGetArchitectureDispatch(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_architecture"),
		Args:      nil,
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(get_architecture): %v", err)
	}
	if !strings.Contains(resp.Content[0].Text, "pkg/x") {
		t.Errorf("get_architecture response = %+v; want package pkg/x", resp.Content)
	}
}

func TestCaronteProxyNilEngineCallByTool(t *testing.T) {
	p := NewCaronteProxy(nil, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool: MustToolName("caronte", "query"),
		Mode: ModeInteractive,
	})
	if err == nil {
		t.Fatal("CallByTool on nil-engine proxy returned nil err")
	}
	if !errors.Is(err, ErrCaronteBootstrapRequired) {
		t.Errorf("err = %v; expected wrap of ErrCaronteBootstrapRequired", err)
	}
}

func TestCaronteProxyCloseNilEngine(t *testing.T) {
	p := NewCaronteProxy(nil, NopAuditEmitter())
	if err := p.Close(); err != nil {
		t.Errorf("Close on nil engine returned err: %v", err)
	}
}

func TestCaronteProxyEnsureReachableHealthy(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	if err := p.EnsureReachable(context.Background()); err != nil {
		t.Errorf("EnsureReachable(healthy engine) = %v; want nil", err)
	}
}

func TestCaronteProxyAuditEmitOnEngineError(t *testing.T) {
	rec := &recordingCaronteAudit{}
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, rec)
	_, _ = p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_health"),
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	found := false
	for _, e := range rec.events {
		if e == "CaronteUnreachable" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit events = %v; missing CaronteUnreachable", rec.events)
	}
}

func TestCaronteProxyNilAuditNormalises(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{failAll: true}, nil)
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_health"),
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err == nil {
		t.Fatal("nil err under fake failure")
	}
}

func TestCaronteProxyMissingRequiredArgs(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"query", nil},
		{"query", map[string]any{}},
		{"query", map[string]any{"query": ""}},
		{"context", map[string]any{}},
		{"get_why", map[string]any{}},
		{"get_implementations", map[string]any{}},
		{"trace_call_path", map[string]any{}},
		{"get_cochange", map[string]any{}},
	}
	for _, c := range cases {
		_, err := p.CallByTool(context.Background(), CallRequest{
			Tool:      MustToolName("caronte", c.tool),
			Args:      c.args,
			ProjectID: "p",
			Mode:      ModeInteractive,
		})
		if err == nil {
			t.Errorf("tool=%q args=%v: expected validation error, got nil", c.tool, c.args)
		}
	}
}

func TestCaronteProxyWikiNoModuleArg(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "wiki"),
		Args:      nil,
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(wiki, no module): %v", err)
	}

	if resp.Subsystem != "caronte" {
		t.Errorf("Subsystem = %q; want caronte", resp.Subsystem)
	}
}

func TestCaronteProxyStringArgOptWrongType(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())

	resp, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "wiki"),
		Args:      map[string]any{"module": 42},
		ProjectID: "proj-1",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("CallByTool(wiki, wrong-type module): %v", err)
	}
	if resp.Subsystem != "caronte" {
		t.Errorf("Subsystem = %q; want caronte", resp.Subsystem)
	}
}

func TestCaronteProxyIntArgVariants(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())

	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_call_path"),
		Args:      map[string]any{"symbol": "pkg/x.A"},
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("trace_call_path (no depth): %v", err)
	}

	_, err = p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "trace_call_path"),
		Args:      map[string]any{"symbol": "pkg/x.A", "depth": 3},
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("trace_call_path (int depth): %v", err)
	}
}

func TestCaronteProxyStringArgWrongType(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "query"),
		Args:      map[string]any{"query": 42},
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err == nil {
		t.Fatal("expected error for wrong-type query arg, got nil")
	}
}

func TestCaronteProxyStringSliceArgVariants(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())

	_, err := p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_risk"),
		Args:      map[string]any{"changed_symbols": []string{"pkg/x.A"}},
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("get_risk ([]string symbols): %v", err)
	}

	_, err = p.CallByTool(context.Background(), CallRequest{
		Tool:      MustToolName("caronte", "get_risk"),
		Args:      map[string]any{"changed_symbols": 42},
		ProjectID: "p",
		Mode:      ModeInteractive,
	})
	if err != nil {
		t.Fatalf("get_risk (wrong type symbols → nil): %v", err)
	}
}

type recordingCaronteAudit struct {
	events []string
}

func (r *recordingCaronteAudit) Emit(t string, _ []byte) { r.events = append(r.events, t) }

func TestCaronteProxyExposesNineteenTools(t *testing.T) {
	p := NewCaronteProxy(&fakeCaronteEngine{}, NopAuditEmitter())
	tools := p.Tools()

	const plan19Count, plan20Count = 11, 8
	if got, want := len(tools), plan19Count+plan20Count; got < want {
		t.Fatalf("Tools() len = %d; want >= %d (Plan 19's %d + Plan 20's %d)",
			got, want, plan19Count, plan20Count)
	}

	// (b) Set-membership: every Plan-20 tool name MUST be present.
	//     This pins Phase I's contribution and remains stable as Plan 21+ add more.
	expectedPlan20Tools := map[string]bool{
		"get_contract":            true,
		"get_consumers":           true,
		"get_breaking_changes":    true,
		"trace_api_call":          true,
		"get_workspace":           true,
		"federation_health":       true,
		"contract_diff":           true,
		"get_why_breaking_change": true,
	}
	for _, tn := range tools {
		if tn.Subsystem() != "caronte" {
			t.Errorf("tool %q subsystem = %q; want caronte", tn.Tool(), tn.Subsystem())
		}
		delete(expectedPlan20Tools, tn.Tool())
	}
	if len(expectedPlan20Tools) != 0 {
		t.Errorf("missing Plan-20 tools: %v", expectedPlan20Tools)
	}
}
