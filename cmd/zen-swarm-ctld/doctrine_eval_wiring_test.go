package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/doctrine/eval"
)

func TestActiveTierPolicy_RiskTierFor_ZenSwarmCtldReturnsLow(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("zen-swarm-ctld", "any-tool")
	if got != "low" {
		t.Errorf("RiskTierFor(zen-swarm-ctld, *) = %q; want %q (catalog Phase A)", got, "low")
	}
}

func TestActiveTierPolicy_RiskTierFor_PlaywrightReturnsMedium(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("playwright", "browse")
	if got != "medium" {
		t.Errorf("RiskTierFor(playwright, *) = %q; want %q (catalog Phase A)", got, "medium")
	}
}

func TestActiveTierPolicy_RiskTierFor_FilesystemReturnsHigh(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("filesystem", "write")
	if got != "high" {
		t.Errorf("RiskTierFor(filesystem, *) = %q; want %q (catalog Phase A)", got, "high")
	}
}

func TestActiveTierPolicy_RiskTierFor_GitHubReturnsHigh(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("github", "create-pr")
	if got != "high" {
		t.Errorf("RiskTierFor(github, *) = %q; want %q (catalog Phase A)", got, "high")
	}
}

func TestActiveTierPolicy_RiskTierFor_SequentialThinkingReturnsLow(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("sequential-thinking", "think")
	if got != "low" {
		t.Errorf("RiskTierFor(sequential-thinking, *) = %q; want %q (catalog Phase A)", got, "low")
	}
}

func TestActiveTierPolicy_RiskTierFor_UnknownMCPReturnsUnknown(t *testing.T) {
	p := newActiveTierPolicy()
	got := p.RiskTierFor("non-existent-mcp", "any-tool")
	if got != "unknown" {
		t.Errorf("RiskTierFor(unknown-mcp, *) = %q; want %q (catalog miss fallback)", got, "unknown")
	}
}

func TestActiveTierPolicy_RiskTierFor_AllCatalogEntriesPopulated(t *testing.T) {
	p := newActiveTierPolicy()

	canonicalMCPs := []string{
		"zen-swarm-ctld",
		"playwright", "filesystem", "github",
		"prisma-postgres", "sentry", "linear", "memory", "sequential-thinking",
		"sqlite", "graphql", "mysql", "openapi",
	}
	for _, mcp := range canonicalMCPs {
		t.Run(mcp, func(t *testing.T) {
			got := p.RiskTierFor(mcp, "any-tool")
			if got == "unknown" {
				t.Errorf("RiskTierFor(%q, *) = %q; want one of low/medium/high (inv-zen-181 substrate)", mcp, got)
			}
			if got != "low" && got != "medium" && got != "high" {
				t.Errorf("RiskTierFor(%q, *) = %q; want low/medium/high (catalog data drift)", mcp, got)
			}
		})
	}
}

func TestActiveTierPolicy_ActiveProfile_RegistryWiredReturnsMaxScope(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
	p := newActiveTierPolicy()
	got := p.ActiveProfile()
	if got != "max-scope" {
		t.Errorf("ActiveProfile() = %q; want %q (built-in default per active.Active fallback)", got, "max-scope")
	}
}

func TestActiveTierPolicy_ActiveProfile_DefaultProfileReturnsDefault(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
	if err := active.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault: %v", err)
	}
	p := newActiveTierPolicy()
	got := p.ActiveProfile()
	if got != "default" {
		t.Errorf("ActiveProfile() = %q; want %q (operator override)", got, "default")
	}
}

func TestActiveTierPolicy_ActiveProfile_NilResolverFallback(t *testing.T) {
	p := &activeTierPolicy{}
	got := p.RiskTierFor("anything", "any")
	if got != "unknown" {
		t.Errorf("RiskTierFor(*) on nil-resolver policy = %q; want %q (defensive)", got, "unknown")
	}
}

func TestActiveTierPolicy_AllowDenyListsEmpty(t *testing.T) {
	p := newActiveTierPolicy()
	if len(p.AllowList()) != 0 {
		t.Errorf("AllowList = %v; want empty (Plan 14+ schema extension pending)", p.AllowList())
	}
	if len(p.DenyList()) != 0 {
		t.Errorf("DenyList = %v; want empty (Plan 14+ schema extension pending)", p.DenyList())
	}
}

func TestBuildDoctrineEvaluator_NilEmitterReturnsNil(t *testing.T) {
	got := buildDoctrineEvaluator(nil)
	if got != nil {
		t.Errorf("buildDoctrineEvaluator(nil) = %#v; want nil (defensive short-circuit)", got)
	}
}

func withDefaultDoctrine(t *testing.T) {
	t.Helper()
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
	if err := active.SetUserDefault("default"); err != nil {
		t.Fatalf("SetUserDefault(default): %v", err)
	}
}

func TestBuildDoctrineEvaluator_ProducesFunctionalEvaluator(t *testing.T) {
	withDefaultDoctrine(t)
	emitter := &fakeEvalEmitter{}
	evaluator := buildDoctrineEvaluator(emitter)
	if evaluator == nil {
		t.Fatalf("buildDoctrineEvaluator(emitter) = nil; want non-nil")
	}

	label, evidence, err := evaluator.EvaluateCall(context.Background(), "zen-swarm-ctld", "status", map[string]any{})
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if label != "allow" {
		t.Errorf("decision label = %q; want %q (zen-swarm-ctld is catalog-low + default profile)", label, "allow")
	}
	if !strings.Contains(evidence, "low") {
		t.Errorf("evidence = %q; want substring %q", evidence, "low")
	}
	if len(emitter.events) != 1 {
		t.Errorf("audit emit count = %d; want 1 (inv-zen-184)", len(emitter.events))
	}
}

func TestBuildDoctrineEvaluator_HighTierProducesConfirm(t *testing.T) {
	withDefaultDoctrine(t)
	emitter := &fakeEvalEmitter{}
	evaluator := buildDoctrineEvaluator(emitter)
	label, _, err := evaluator.EvaluateCall(context.Background(), "filesystem", "write", map[string]any{})
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if label != "allow_with_confirm" {
		t.Errorf("decision label = %q; want %q (filesystem is catalog-high + default profile)", label, "allow_with_confirm")
	}
}

func TestBuildDoctrineEvaluator_MediumTierProducesAudit(t *testing.T) {
	withDefaultDoctrine(t)
	emitter := &fakeEvalEmitter{}
	evaluator := buildDoctrineEvaluator(emitter)
	label, _, err := evaluator.EvaluateCall(context.Background(), "playwright", "browse", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if label != "allow_with_audit" {
		t.Errorf("decision label = %q; want %q (playwright is catalog-medium + default profile)", label, "allow_with_audit")
	}
}

func TestBuildDoctrineEvaluator_UnknownMCPProducesAudit(t *testing.T) {
	withDefaultDoctrine(t)
	emitter := &fakeEvalEmitter{}
	evaluator := buildDoctrineEvaluator(emitter)
	label, evidence, err := evaluator.EvaluateCall(context.Background(), "novel-mcp-not-catalogued", "tool", nil)
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if label != "allow_with_audit" {
		t.Errorf("decision label = %q; want %q (unknown MCP fallback)", label, "allow_with_audit")
	}
	if !strings.Contains(evidence, "unknown") {
		t.Errorf("evidence = %q; want substring %q", evidence, "unknown")
	}
}

func TestAuditEmitterAdapter_NilForwardingTolerated(t *testing.T) {

	a := newAuditEmitterAdapter(nil)
	hash, err := a.Emit(context.Background(), "evt.test", []byte("{}"))
	if err != nil {
		t.Errorf("Emit on nil-wrapped adapter = %v; want nil err", err)
	}
	if hash != "" {
		t.Errorf("Emit hash = %q; want empty (defensive fallback)", hash)
	}
}

func TestAuditEmitterAdapter_ForwardsToUnderlying(t *testing.T) {
	rec := &fakeMcpgatewayAuditEmitter{}
	a := newAuditEmitterAdapter(rec)
	hash, err := a.Emit(context.Background(), "evt.doctrine.eval.allow", []byte(`{"x":1}`))
	if err != nil {
		t.Errorf("Emit = %v; want nil err", err)
	}
	if hash != "" {
		t.Errorf("synthesised hash = %q; want empty per godoc", hash)
	}
	if rec.emitCount != 1 {
		t.Errorf("underlying Emit count = %d; want 1", rec.emitCount)
	}
	if rec.lastEventType != "evt.doctrine.eval.allow" {
		t.Errorf("eventType = %q; want %q", rec.lastEventType, "evt.doctrine.eval.allow")
	}
}

type fakeEvalEmitter struct {
	events []fakeEvalEvent
}

type fakeEvalEvent struct {
	eventType string
	payload   []byte
}

func (f *fakeEvalEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	f.events = append(f.events, fakeEvalEvent{eventType: eventType, payload: payload})
	return "fake-hash", nil
}

type fakeMcpgatewayAuditEmitter struct {
	emitCount     int
	lastEventType string
	lastPayload   []byte
}

func (f *fakeMcpgatewayAuditEmitter) Emit(eventType string, payload []byte) {
	f.emitCount++
	f.lastEventType = eventType
	f.lastPayload = payload
}

var _ eval.Emitter = (*fakeEvalEmitter)(nil)
