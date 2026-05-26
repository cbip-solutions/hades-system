package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	mcpaudit "github.com/cbip-solutions/hades-system/internal/mcp/audit"
	mcpbudget "github.com/cbip-solutions/hades-system/internal/mcp/budget"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
	mcpsshexec "github.com/cbip-solutions/hades-system/internal/mcp/sshexec"
)

func TestBuildDispatcherRegistersAllSubsystems(t *testing.T) {

	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	defer d.Close()
	tools := d.ListTools()

	caronteCount := 0
	for _, e := range tools {
		if e.Name.Subsystem() == "caronte" {
			caronteCount++
		}
	}
	const plan19Count, plan20Count = 11, 8
	if got, want := caronteCount, plan19Count+plan20Count; got < want {
		t.Errorf("caronte tool count = %d; want >= %d (Plan 19 K's %d + Plan 20 I's %d)",
			got, want, plan19Count, plan20Count)
	}
}

func TestBuildDispatcherRefusesNilCaronte(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: nil,
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	_, err := buildDispatcher(deps)
	if err == nil {
		t.Fatal("buildDispatcher nil caronte: nil err; expected ErrCaronteBootstrapRequired (Q5=A bootstrap-required)")
	}
}

func TestDefaultRBACConfigDeniesCaronteUnderCapaFirewall(t *testing.T) {
	cfg := defaultRBACConfig()
	denied, ok := cfg.DoctrineDisabled[mcpgateway.DoctrineCapaFirewall]
	if !ok {
		t.Fatal("DoctrineDisabled missing capa-firewall key")
	}
	wantPrefix := "mcp_zen-swarm_caronte_"
	found := false
	for _, name := range denied {
		if len(name) > len(wantPrefix) && name[:len(wantPrefix)] == wantPrefix {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("capa-firewall denials = %v; expected at least one mcp_zen-swarm_caronte_* entry", denied)
	}
}

func TestDefaultRBACConfigDeniesResearchAgenticUnderCapaFirewall(t *testing.T) {
	cfg := defaultRBACConfig()
	denied := cfg.DoctrineDisabled[mcpgateway.DoctrineCapaFirewall]
	want := "mcp_zen-swarm_research_agentic"
	found := false
	for _, name := range denied {
		if name == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("capa-firewall denials = %v; expected %s", denied, want)
	}
}

func TestDefaultRBACConfigDeniesPlan20FederationToolsUnderCapaFirewall(t *testing.T) {
	cfg := defaultRBACConfig()
	denied := cfg.DoctrineDisabled[mcpgateway.DoctrineCapaFirewall]
	want := map[string]bool{
		"mcp_zen-swarm_caronte_get_contract":            true,
		"mcp_zen-swarm_caronte_get_consumers":           true,
		"mcp_zen-swarm_caronte_get_breaking_changes":    true,
		"mcp_zen-swarm_caronte_trace_api_call":          true,
		"mcp_zen-swarm_caronte_get_workspace":           true,
		"mcp_zen-swarm_caronte_federation_health":       true,
		"mcp_zen-swarm_caronte_contract_diff":           true,
		"mcp_zen-swarm_caronte_get_why_breaking_change": true,
	}
	for _, name := range denied {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("capa-firewall denylist missing Plan-20 entries: %v", want)
	}
}

func TestNilAuditNormalisesInBuildDispatcher(t *testing.T) {

	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   nil,
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher with nil audit: %v", err)
	}
	defer d.Close()
}

func TestBuildDispatcherWithAllSubsystems(t *testing.T) {

	budgetServer := mcpbudget.NewServer(nil)
	auditServer, err := mcpaudit.NewServer(mcpaudit.ServerConfig{
		ReviewerFamilyPool: []string{"anthropic", "google", "openai"},
		MinPoolSize:        2,
	})
	if err != nil {
		t.Fatalf("mcpaudit.NewServer: %v", err)
	}
	resolver := func(_ string) (*mcpsshexec.Allowlist, error) {
		return &mcpsshexec.Allowlist{Project: "test"}, nil
	}
	sshexecServer := mcpsshexec.NewServer(mcpsshexec.ServerConfig{
		Component:         "ssh-exec",
		AllowlistResolver: resolver,
		Emitter:           mcpsshexec.NopAuditEmitter{},
	})

	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
		budget:  budgetServer,
		audit5:  auditServer,
		sshexec: sshexecServer,
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	defer d.Close()

	counts := map[string]int{}
	for _, e := range d.ListTools() {
		counts[e.Name.Subsystem()]++
	}

	{
		const plan19Count, plan20Count = 11, 8
		if got, want := counts["caronte"], plan19Count+plan20Count; got < want {
			t.Errorf("caronte count = %d, want >= %d (Plan 19 K's %d + Plan 20 I's %d)",
				got, want, plan19Count, plan20Count)
		}
	}
	if counts["budget"] == 0 {
		t.Errorf("budget count = 0; expected >= 1")
	}
	if counts["audit"] != 1 {
		t.Errorf("audit count = %d, want 1", counts["audit"])
	}
	if counts["sshexec"] != 3 {
		t.Errorf("sshexec count = %d, want 3", counts["sshexec"])
	}
}

func TestBuildDispatcherListCaronteToolNames(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	defer d.Close()
	tools := d.ListTools()
	seen := make(map[string]bool)
	for _, e := range tools {
		if e.Name.Subsystem() == "caronte" {
			seen[e.Name.Tool()] = true
		}
	}

	for _, want := range []string{
		"query", "context", "impact", "wiki",
		"get_risk", "get_why", "get_health",
		"trace_call_path", "get_cochange",
		"get_implementations", "get_architecture",
	} {
		if !seen[want] {
			t.Errorf("caronte tools missing %q after cutover", want)
		}
	}
}

func TestBuildInternalMCPSubsystemHappyPath(t *testing.T) {
	captured := struct {
		called bool
		name   string
	}{}
	invoke := func(_ context.Context, name string, _ map[string]any) (any, error) {
		captured.called = true
		captured.name = name
		return "tool-result", nil
	}
	sub := buildInternalMCPSubsystem("research", []string{"agentic"}, invoke)
	if sub.Name() != "research" {
		t.Errorf("Name = %q want research", sub.Name())
	}
	tools := sub.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools len = %d, want 1", len(tools))
	}
	tn := tools[0].Name
	if tn.Tool() != "agentic" {
		t.Errorf("tool = %q want agentic", tn.Tool())
	}
	if tools[0].Meta.Description != "research MCP — agentic" {
		t.Errorf("desc = %q", tools[0].Meta.Description)
	}

	resp, err := tools[0].Handler(context.Background(), mcpgateway.CallRequest{
		Tool: tn,
		Args: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !captured.called {
		t.Error("invoke was not called")
	}
	if captured.name != "agentic" {
		t.Errorf("captured name = %q want agentic", captured.name)
	}
	if resp.Subsystem != "research" {
		t.Errorf("Subsystem = %q want research", resp.Subsystem)
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "tool-result" {
		t.Errorf("Content = %v; want 'tool-result' text", resp.Content)
	}
}

func TestBuildInternalMCPSubsystemErrorPath(t *testing.T) {
	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return nil, errors.New("downstream rpc failed")
	}
	sub := buildInternalMCPSubsystem("audit", []string{"emit"}, invoke)
	resp, err := sub.Tools()[0].Handler(context.Background(), mcpgateway.CallRequest{
		Tool: sub.Tools()[0].Name,
	})
	if err != nil {
		t.Fatalf("Handler returned err: %v", err)
	}
	if !resp.IsError {
		t.Error("IsError = false; want true")
	}
	if !strings.Contains(resp.Content[0].Text, "downstream rpc failed") {
		t.Errorf("Content = %v; missing error message", resp.Content)
	}
}

// TestBuildInternalMCPSubsystemSkipsInvalidNames verifies the defensive
// skip when a tool name fails canonical validation. The buildInternal
// constructor MUST silently drop such names (lowercase ASCII only).
func TestBuildInternalMCPSubsystemSkipsInvalidNames(t *testing.T) {
	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return nil, nil
	}
	sub := buildInternalMCPSubsystem("research", []string{"Valid", "agentic"}, invoke)

	if len(sub.Tools()) != 1 {
		t.Errorf("Tools len = %d, want 1 (after skip)", len(sub.Tools()))
	}
}

func TestInvokeAdapterMarshalAnyVariants(t *testing.T) {
	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return json.RawMessage(`{"foo":"bar"}`), nil
	}
	h := invokeAdapter(invoke)
	resp, err := h(context.Background(), mcpgateway.CallRequest{
		Tool: mcpgateway.MustToolName("audit", "emit"),
	})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if resp.Content[0].Text != `{"foo":"bar"}` {
		t.Errorf("Content = %q", resp.Content[0].Text)
	}
}

func TestInvokeAdapterBytesPath(t *testing.T) {
	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return []byte(`hello`), nil
	}
	h := invokeAdapter(invoke)
	resp, err := h(context.Background(), mcpgateway.CallRequest{
		Tool: mcpgateway.MustToolName("audit", "emit"),
	})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if resp.Content[0].Text != "hello" {
		t.Errorf("Content = %q", resp.Content[0].Text)
	}
}

func TestInvokeAdapterStructPath(t *testing.T) {
	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return map[string]any{"k": 42}, nil
	}
	h := invokeAdapter(invoke)
	resp, err := h(context.Background(), mcpgateway.CallRequest{
		Tool: mcpgateway.MustToolName("audit", "emit"),
	})
	if err != nil {
		t.Fatalf("h: %v", err)
	}
	if resp.Content[0].Text != `{"k":42}` {
		t.Errorf("Content = %q", resp.Content[0].Text)
	}
}

func TestInvokeAdapterMarshalFailure(t *testing.T) {

	invoke := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return make(chan int), nil
	}
	h := invokeAdapter(invoke)
	_, err := h(context.Background(), mcpgateway.CallRequest{
		Tool: mcpgateway.MustToolName("audit", "emit"),
	})
	if err == nil {
		t.Fatal("nil err on marshal-impossible value")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("err = %v; expected 'marshal' mention", err)
	}
}

func TestMcpgwAuditAdapterNilServerNoop(t *testing.T) {
	a := mcpgwAuditAdapter{srv: nil}
	a.Emit("Test", []byte(`{}`))
}

func TestMcpgwAuditAdapterEmitForwardsToServer(t *testing.T) {
	rec := &fakeDaemonAuditServer{}
	a := mcpgwAuditAdapter{srv: rec}
	payload := []byte(`{"tool":"mcp_zen-swarm_research_query","ms":42}`)
	a.Emit("ToolDispatched", payload)
	if got := len(rec.received); got != 1 {
		t.Fatalf("AuditEmit called %d times; want 1", got)
	}
	ev := rec.received[0]
	wantType := "mcpgateway.ToolDispatched"
	if ev.Type != wantType {
		t.Errorf("event.Type = %q; want %q (mcpgateway. prefix)", ev.Type, wantType)
	}
	raw, ok := ev.Payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type = %T; want json.RawMessage (verbatim forward)", ev.Payload)
	}
	if !bytes.Equal(raw, payload) {
		t.Errorf("payload mutated; got=%s want=%s", raw, payload)
	}
}

func TestRegisterSubsystemNamedWrapsCollisionForEachSlot(t *testing.T) {

	const dupTool = "query"

	invokeNop := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return nil, nil
	}

	type row struct {
		slotLabel string

		build func() mcpgateway.Subsystem
	}

	rows := []row{
		{
			slotLabel: "caronte",
			build: func() mcpgateway.Subsystem {

				return buildInternalMCPSubsystem("caronte", []string{dupTool, dupTool}, invokeNop)
			},
		},
		{
			slotLabel: "research",
			build: func() mcpgateway.Subsystem {
				return buildInternalMCPSubsystem("research", []string{dupTool, dupTool}, invokeNop)
			},
		},
		{
			slotLabel: "budget",
			build: func() mcpgateway.Subsystem {
				return buildInternalMCPSubsystem("budget", []string{dupTool, dupTool}, invokeNop)
			},
		},
		{
			slotLabel: "audit",
			build: func() mcpgateway.Subsystem {
				return buildInternalMCPSubsystem("audit", []string{dupTool, dupTool}, invokeNop)
			},
		},
		{
			slotLabel: "sshexec",
			build: func() mcpgateway.Subsystem {
				return buildInternalMCPSubsystem("sshexec", []string{dupTool, dupTool}, invokeNop)
			},
		},
	}

	for _, r := range rows {
		t.Run(r.slotLabel, func(t *testing.T) {
			d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
			err := registerSubsystemNamed(d, r.slotLabel, r.build())
			if err == nil {
				t.Fatalf("slot %q: expected error on intra-subsystem dup; got nil", r.slotLabel)
			}
			wantPrefix := "register " + r.slotLabel + ":"
			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("slot %q: error = %q; want prefix %q", r.slotLabel, err.Error(), wantPrefix)
			}
			if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
				t.Errorf("slot %q: error chain missing ErrToolNameCollision; got %v", r.slotLabel, err)
			}
		})
	}
}

func TestRegisterSubsystemNamedPassThroughOnSuccess(t *testing.T) {
	d := mcpgateway.NewDispatcher(mcpgateway.DispatcherConfig{})
	invokeNop := func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return nil, nil
	}
	sub := buildInternalMCPSubsystem("research", []string{"query"}, invokeNop)
	if err := registerSubsystemNamed(d, "research", sub); err != nil {
		t.Fatalf("happy-path register: %v", err)
	}
	tools := d.ListTools()
	if len(tools) != 1 {
		t.Errorf("ListTools len = %d; want 1", len(tools))
	}
}

func TestMcpgwAuditAdapterEmitMultipleEvents(t *testing.T) {
	rec := &fakeDaemonAuditServer{}
	a := mcpgwAuditAdapter{srv: rec}
	a.Emit("ToolDispatched", []byte(`{"k":1}`))
	a.Emit("HandlerPanic", []byte(`{"k":2}`))
	a.Emit("CaronteUnreachable", []byte(`{"k":3}`))
	wantTypes := []string{
		"mcpgateway.ToolDispatched",
		"mcpgateway.HandlerPanic",
		"mcpgateway.CaronteUnreachable",
	}
	if len(rec.received) != len(wantTypes) {
		t.Fatalf("recorded %d events; want %d", len(rec.received), len(wantTypes))
	}
	for i, w := range wantTypes {
		if rec.received[i].Type != w {
			t.Errorf("event[%d].Type = %q; want %q", i, rec.received[i].Type, w)
		}
	}
}

func TestInternalMCPSubsystemNameAccessor(t *testing.T) {
	sub := buildInternalMCPSubsystem("budget", []string{}, func(_ context.Context, _ string, _ map[string]any) (any, error) {
		return nil, nil
	})
	if sub.Name() != "budget" {
		t.Errorf("Name = %q want budget", sub.Name())
	}
	if len(sub.Tools()) != 0 {
		t.Errorf("empty tools list expected; got %d", len(sub.Tools()))
	}
}

func TestNoGitnexusSubsystemCtor(t *testing.T) {
	deps := mcpgatewayDeps{
		caronte: fakeCaronteEngine{},
		audit:   mcpgateway.NopAuditEmitter(),
		rbacCfg: defaultRBACConfig(),
	}
	d, err := buildDispatcher(deps)
	if err != nil {
		t.Fatalf("buildDispatcher: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	sawCaronte := false
	for _, e := range d.ListTools() {
		switch e.Name.Subsystem() {
		case "gitnexus":
			t.Errorf("dispatcher registered a tool under the removed %q segment: %s", "gitnexus", e.Name)
		case "caronte":
			sawCaronte = true
		}
	}
	if !sawCaronte {
		t.Error("dispatcher registered no tools under the caronte segment")
	}
}

func TestPerMCPWrappersConstructValidSubsystems(t *testing.T) {
	t.Run("budget", func(t *testing.T) {
		sub := newBudgetSubsystemForTest(t)
		if sub.Name() != "budget" {
			t.Errorf("budget Name = %q", sub.Name())
		}
		if len(sub.Tools()) == 0 {
			t.Error("budget Tools empty")
		}
	})

	t.Run("audit", func(t *testing.T) {
		sub := newAuditSubsystemForTest(t)
		if sub.Name() != "audit" {
			t.Errorf("audit Name = %q", sub.Name())
		}
		if len(sub.Tools()) != 1 {
			t.Errorf("audit Tools len = %d, want 1 (audit_review)", len(sub.Tools()))
		}
	})

	t.Run("sshexec", func(t *testing.T) {
		sub := newSSHExecSubsystemForTest(t)
		if sub.Name() != "sshexec" {
			t.Errorf("sshexec Name = %q", sub.Name())
		}
		if len(sub.Tools()) != 3 {
			t.Errorf("sshexec Tools len = %d, want 3", len(sub.Tools()))
		}
	})

	t.Run("research", func(t *testing.T) {

		fake := fakeResearchServer{
			names: []string{"web_search", "arxiv", "agentic_deep"},
			invokeFn: func(_ context.Context, name string, _ map[string]any) (any, error) {
				return "ok:" + name, nil
			},
		}
		sub := newResearchSubsystem(fake)
		if sub.Name() != "research" {
			t.Errorf("research Name = %q", sub.Name())
		}
		if got := len(sub.Tools()); got != 3 {
			t.Errorf("research Tools len = %d, want 3", got)
		}

		resp, err := sub.Tools()[0].Handler(context.Background(), mcpgateway.CallRequest{
			Tool: sub.Tools()[0].Name,
			Args: map[string]any{"q": "test"},
		})
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		if resp.IsError {
			t.Errorf("IsError = true; want false (fake returns nil err)")
		}

		var _ researchServer = (*research.Server)(nil)
	})
}

func TestBuildDispatcherSurfacesRegisterErrorEndToEnd(t *testing.T) {

	dupResearch := fakeResearchServer{names: []string{"web_search", "web_search"}}
	deps := mcpgatewayDeps{
		caronte:  fakeCaronteEngine{},
		audit:    mcpgateway.NopAuditEmitter(),
		rbacCfg:  defaultRBACConfig(),
		research: dupResearch,
	}
	_, err := buildDispatcher(deps)
	if err == nil {
		t.Fatal("buildDispatcher with dup-research tools: expected error; got nil")
	}
	if !strings.HasPrefix(err.Error(), "register research:") {
		t.Errorf("error = %q; want prefix 'register research:'", err.Error())
	}
	if !errors.Is(err, mcpgateway.ErrToolNameCollision) {
		t.Errorf("error chain missing ErrToolNameCollision; got %v", err)
	}
}
