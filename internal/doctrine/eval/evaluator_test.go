// Package eval_test covers the dynamic per-call doctrine evaluator
// .
//
// Coverage targets per spec §6.2 + ≥95% security-critical (invariant
// emission gate is the load-bearing MCP-call boundary check; the eval
// matrix MUST be exhaustively covered for the 3-profile baseline +
// custom profile fallback).
package eval_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/eval"
)

func TestEvaluatorInterfaceShape(t *testing.T) {
	var _ eval.Evaluator = (*eval.RuntimeEvaluator)(nil)
}

func TestEvaluateCallAllowsLowRiskDefault(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"sequential-thinking": "low"},
			profile: "default",
		},
		Emitter: emitter,
	})
	decision, evidence, err := e.EvaluateCall(context.Background(), "sequential-thinking", "think", nil)
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if decision != eval.CallAllow {
		t.Errorf("decision = %v, want CallAllow", decision)
	}
	if !strings.Contains(evidence, "low") {
		t.Errorf("evidence = %q, want substring 'low'", evidence)
	}
	if len(emitter.events) != 1 {
		t.Errorf("emitted events = %d, want 1 (inv-zen-184)", len(emitter.events))
	}
	if emitter.events[0].Type != "evt.doctrine.eval.allow" {
		t.Errorf("event type = %q, want evt.doctrine.eval.allow", emitter.events[0].Type)
	}
}

func TestEvaluateCallAllowsWithAuditMediumRiskDefault(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"playwright": "medium"}, profile: "default"},
		Emitter: emitter,
	})
	decision, _, err := e.EvaluateCall(context.Background(), "playwright", "browse", map[string]string{"url": "example.com"})
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if decision != eval.CallAllowWithAudit {
		t.Errorf("decision = %v, want CallAllowWithAudit", decision)
	}
	if emitter.events[0].Type != "evt.doctrine.eval.allow_with_audit" {
		t.Errorf("event type = %q, want evt.doctrine.eval.allow_with_audit", emitter.events[0].Type)
	}
}

func TestEvaluateCallConfirmHighRiskDefault(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"filesystem-write": "high"}, profile: "default"},
		Emitter: emitter,
	})
	decision, _, err := e.EvaluateCall(context.Background(), "filesystem-write", "write", nil)
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if decision != eval.CallAllowWithConfirm {
		t.Errorf("decision = %v, want CallAllowWithConfirm", decision)
	}
}

func TestEvaluateCallDefaultUnknownTierConservative(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{}, profile: "default"},
		Emitter: emitter,
	})
	decision, evidence, _ := e.EvaluateCall(context.Background(), "novel-mcp", "novel-tool", nil)
	if decision != eval.CallAllowWithAudit {
		t.Errorf("decision = %v, want CallAllowWithAudit (conservative)", decision)
	}
	if !strings.Contains(evidence, "unknown") {
		t.Errorf("evidence = %q, want substring 'unknown'", evidence)
	}
}

func TestEvaluateCallCapaFirewallDenyListWins(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"filesystem-write": "low"},
			profile: "capa-firewall",
			deny:    []string{"filesystem-write.write"},
		},
		Emitter: emitter,
	})
	decision, evidence, _ := e.EvaluateCall(context.Background(), "filesystem-write", "write", nil)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v, want CallDeny", decision)
	}
	if !strings.Contains(evidence, "deny list") {
		t.Errorf("evidence = %q, want substring 'deny list'", evidence)
	}
	if emitter.events[0].Type != "evt.doctrine.eval.deny" {
		t.Errorf("event type = %q, want evt.doctrine.eval.deny", emitter.events[0].Type)
	}
}

func TestEvaluateCallCapaFirewallAllowListGrants(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"filesystem-write": "high"},
			profile: "capa-firewall",
			allow:   []string{"filesystem-write.write"},
		},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "filesystem-write", "write", nil)
	if decision != eval.CallAllow {
		t.Errorf("decision = %v, want CallAllow", decision)
	}
}

func TestEvaluateCallCapaFirewallHighDenyDefault(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"filesystem-write": "high"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "filesystem-write", "write", nil)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v, want CallDeny", decision)
	}
}

func TestEvaluateCallCapaFirewallMediumConfirm(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"playwright": "medium"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "playwright", "browse", nil)
	if decision != eval.CallAllowWithConfirm {
		t.Errorf("decision = %v, want CallAllowWithConfirm", decision)
	}
}

func TestEvaluateCallCapaFirewallLowAudit(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"st": "low"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "st", "think", nil)
	if decision != eval.CallAllowWithAudit {
		t.Errorf("decision = %v, want CallAllowWithAudit", decision)
	}
}

func TestEvaluateCallCapaFirewallMCPWideDenyMatches(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"forbidden-mcp": "low"},
			profile: "capa-firewall",
			deny:    []string{"forbidden-mcp"},
		},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "forbidden-mcp", "any-tool", nil)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v, want CallDeny (mcp-wide deny)", decision)
	}
}

func TestEvaluateCallMaxScopeAllowsAll(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"filesystem-write": "high"}, profile: "max-scope"},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "filesystem-write", "write", nil)
	if decision != eval.CallAllow {
		t.Errorf("max-scope filesystem-write = %v, want CallAllow", decision)
	}
}

func TestExactlyOneEmissionPerEvaluateCall(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"st": "low"}, profile: "default"},
		Emitter: emitter,
	})
	for i := 0; i < 10; i++ {
		_, _, _ = e.EvaluateCall(context.Background(), "st", "think", nil)
	}
	if len(emitter.events) != 10 {
		t.Errorf("inv-zen-184: emitted = %d for 10 calls, want exactly 10", len(emitter.events))
	}
}

func TestEmitterErrorDoesNotBlockDecision(t *testing.T) {
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"x": "low"}, profile: "default"},
		Emitter: &failingEmitter{},
	})
	decision, _, err := e.EvaluateCall(context.Background(), "x", "y", nil)
	if decision != eval.CallAllow {
		t.Errorf("decision = %v, want CallAllow even with failing emitter", decision)
	}
	if err == nil {
		t.Errorf("err = nil, want non-nil (emit failure surfaced)")
	}
	if !errors.Is(err, errStubFailure) {
		t.Errorf("err = %v, want wrapping errStubFailure", err)
	}
}

func TestEvaluateCallCustomProfileDenyList(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"x": "low"},
			profile: "imported-from-claude-code",
			deny:    []string{"x.y"},
		},
		Emitter: emitter,
	})
	decision, evidence, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v, want CallDeny (custom profile deny list)", decision)
	}
	if !strings.Contains(evidence, "imported-from-claude-code") {
		t.Errorf("evidence = %q, want substring 'imported-from-claude-code'", evidence)
	}
}

func TestEvaluateCallCustomProfileAllowList(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"x": "high"},
			profile: "imported-from-claude-code",
			allow:   []string{"x.y"},
		},
		Emitter: emitter,
	})
	decision, _, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
	if decision != eval.CallAllow {
		t.Errorf("decision = %v, want CallAllow (custom profile allow list)", decision)
	}
}

func TestEvaluateCallCustomProfileTierFallback(t *testing.T) {
	tests := []struct {
		tier     string
		expected eval.CallDecision
	}{
		{"low", eval.CallAllowWithAudit},
		{"medium", eval.CallAllowWithConfirm},
		{"high", eval.CallDeny},
		{"unknown", eval.CallAllowWithAudit},
	}
	for _, tc := range tests {
		t.Run(tc.tier, func(t *testing.T) {
			emitter := &recordingEmitter{}
			e := eval.NewRuntimeEvaluator(eval.Config{
				Policy:  stubPolicy{tiers: map[string]string{"x": tc.tier}, profile: "custom-test"},
				Emitter: emitter,
			})
			decision, _, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
			if decision != tc.expected {
				t.Errorf("custom profile + %s tier: decision = %v, want %v", tc.tier, decision, tc.expected)
			}
		})
	}
}

func TestCallDecisionStringStable(t *testing.T) {
	cases := []struct {
		decision eval.CallDecision
		label    string
	}{
		{eval.CallAllow, "allow"},
		{eval.CallAllowWithAudit, "allow_with_audit"},
		{eval.CallAllowWithConfirm, "allow_with_confirm"},
		{eval.CallDeny, "deny"},
	}
	for _, tc := range cases {
		if got := tc.decision.String(); got != tc.label {
			t.Errorf("CallDecision(%d).String() = %q, want %q", tc.decision, got, tc.label)
		}
	}

	if got := eval.CallDecision(99).String(); got != "unknown" {
		t.Errorf("OOR.String() = %q, want unknown", got)
	}
}

func TestEventTypeForStable(t *testing.T) {
	if got := eval.CallAllow.EventTypeFor(); got != "evt.doctrine.eval.allow" {
		t.Errorf("EventTypeFor = %q, want evt.doctrine.eval.allow", got)
	}
	if got := eval.CallDeny.EventTypeFor(); got != "evt.doctrine.eval.deny" {
		t.Errorf("EventTypeFor = %q, want evt.doctrine.eval.deny", got)
	}
}

func TestEvaluateCall_ParamKeysSurfacedInAuditPayload(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"playwright": "low"}, profile: "default"},
		Emitter: emitter,
	})
	params := map[string]any{
		"url":     "https://example.com",
		"timeout": 30,
	}
	_, _, _ = e.EvaluateCall(context.Background(), "playwright", "browse", params)
	if len(emitter.events) != 1 {
		t.Fatalf("emit count = %d; want 1", len(emitter.events))
	}
	payload := string(emitter.events[0].Payload)
	if !strings.Contains(payload, `"paramKeys"`) {
		t.Errorf("payload missing paramKeys field; got %q", payload)
	}
	if !strings.Contains(payload, `"url"`) || !strings.Contains(payload, `"timeout"`) {
		t.Errorf("payload should include keys url+timeout; got %q", payload)
	}
	// Values MUST NOT appear in payload (security).
	if strings.Contains(payload, "example.com") {
		t.Errorf("payload should NOT include values; leaked example.com: %q", payload)
	}
}

func TestEvaluateCall_CapaFirewallEscalatesOnSensitiveParam(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"filesystem-read": "low"}, profile: "capa-firewall"},
		Emitter: emitter,
	})

	params := map[string]any{"path": "/etc/passwd"}
	decision, evidence, _ := e.EvaluateCall(context.Background(), "filesystem-read", "read", params)
	if decision != eval.CallAllowWithConfirm {
		t.Errorf("decision = %v; want CallAllowWithConfirm (escalated from low+capa-firewall by sensitive 'path' param)", decision)
	}
	if !strings.Contains(evidence, "sensitive-param") {
		t.Errorf("evidence = %q; want substring 'sensitive-param'", evidence)
	}
}

func TestEvaluateCall_DefaultProfileDoesNotEscalateOnSensitiveParam(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"filesystem-read": "low"}, profile: "default"},
		Emitter: emitter,
	})
	params := map[string]any{"path": "/etc/passwd"}
	decision, _, _ := e.EvaluateCall(context.Background(), "filesystem-read", "read", params)
	if decision != eval.CallAllow {
		t.Errorf("decision = %v; want CallAllow (default profile + low tier; no escalation)", decision)
	}
}

func TestEvaluateCall_NilParamsTolerated(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"x": "low"}, profile: "default"},
		Emitter: emitter,
	})

	decision, _, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
	if decision != eval.CallAllow {
		t.Errorf("decision (nil params) = %v; want CallAllow", decision)
	}

	decision, _, _ = e.EvaluateCall(context.Background(), "x", "y", "bare-string")
	if decision != eval.CallAllow {
		t.Errorf("decision (bare-string params) = %v; want CallAllow", decision)
	}
}

func TestEscalateForSensitiveParams_AllowToAudit(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy: stubPolicy{
			tiers:   map[string]string{"approved-mcp": "low"},
			profile: "capa-firewall",
			allow:   []string{"approved-mcp.read"},
		},
		Emitter: emitter,
	})

	params := map[string]any{"path": "/var/log/whatever"}
	decision, evidence, _ := e.EvaluateCall(context.Background(), "approved-mcp", "read", params)
	if decision != eval.CallAllowWithAudit {
		t.Errorf("decision = %v; want CallAllowWithAudit (escalated CallAllow on sensitive param)", decision)
	}
	if !strings.Contains(evidence, "sensitive-param") {
		t.Errorf("evidence = %q; want substring 'sensitive-param'", evidence)
	}
}

func TestEscalateForSensitiveParams_ConfirmToDeny(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"caller-mcp": "medium"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	params := map[string]any{"token": "redacted-secret-shape"}
	decision, evidence, _ := e.EvaluateCall(context.Background(), "caller-mcp", "do", params)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v; want CallDeny (escalated CallAllowWithConfirm on sensitive param)", decision)
	}
	if !strings.Contains(evidence, "sensitive-param") {
		t.Errorf("evidence = %q; want substring 'sensitive-param'", evidence)
	}
}

func TestEscalateForSensitiveParams_DenyStaysDeny(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"high-risk-mcp": "high"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	params := map[string]any{"path": "/etc/passwd"}
	decision, _, _ := e.EvaluateCall(context.Background(), "high-risk-mcp", "read", params)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v; want CallDeny (idempotent ladder top for already-deny)", decision)
	}
}

func TestEvaluateCall_CapaFirewallUnrecognizedTierDenyDefault(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"future-mcp": "critical"}, profile: "capa-firewall"},
		Emitter: emitter,
	})
	decision, evidence, _ := e.EvaluateCall(context.Background(), "future-mcp", "act", nil)
	if decision != eval.CallDeny {
		t.Errorf("decision = %v; want CallDeny (unrecognized tier deny-default)", decision)
	}
	if !strings.Contains(evidence, "unrecognized") {
		t.Errorf("evidence = %q; want substring 'unrecognized'", evidence)
	}
	if !strings.Contains(evidence, "critical") {
		t.Errorf("evidence = %q; want substring with offending tier 'critical'", evidence)
	}
}

func TestEvaluateCall_JSONMarshalErrorFallback(t *testing.T) {

	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"x": "low"}, profile: "default"},
		Emitter: emitter,
	})

	_, _, err := e.EvaluateCall(context.Background(), "x", "y", make(chan int))
	if err != nil {
		t.Fatalf("EvaluateCall: %v", err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("emit count = %d; want 1", len(emitter.events))
	}

	if !strings.Contains(string(emitter.events[0].Payload), `"decision":"allow"`) {
		t.Errorf("payload = %q; want canonical JSON", string(emitter.events[0].Payload))
	}
}

func TestEvaluateCall_ParamKeysDeterministicOrder(t *testing.T) {
	emitter := &recordingEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tiers: map[string]string{"x": "low"}, profile: "default"},
		Emitter: emitter,
	})
	params := map[string]any{"z": 1, "a": 2, "m": 3}
	_, _, _ = e.EvaluateCall(context.Background(), "x", "y", params)
	payload := string(emitter.events[0].Payload)

	ai := strings.Index(payload, `"a"`)
	mi := strings.Index(payload, `"m"`)
	zi := strings.Index(payload, `"z"`)
	if ai < 0 || mi < 0 || zi < 0 {
		t.Fatalf("payload missing one of a/m/z keys: %q", payload)
	}
	if !(ai < mi && mi < zi) {
		t.Errorf("paramKeys order non-deterministic: a=%d m=%d z=%d in %q", ai, mi, zi, payload)
	}
}

type stubPolicy struct {
	tiers   map[string]string
	profile string
	allow   []string
	deny    []string
}

func (s stubPolicy) RiskTierFor(mcpName, _ string) string {
	if tier, ok := s.tiers[mcpName]; ok {
		return tier
	}
	return "unknown"
}
func (s stubPolicy) ActiveProfile() string { return s.profile }
func (s stubPolicy) AllowList() []string   { return s.allow }
func (s stubPolicy) DenyList() []string    { return s.deny }

type recordingEmitter struct {
	events []recordedEvent
}

type recordedEvent struct {
	Type    string
	Payload []byte
}

func (r *recordingEmitter) Emit(_ context.Context, eventType string, payload []byte) (string, error) {
	r.events = append(r.events, recordedEvent{Type: eventType, Payload: payload})
	return "stub-hash", nil
}

type failingEmitter struct{}

func (f *failingEmitter) Emit(_ context.Context, _ string, _ []byte) (string, error) {
	return "", errStubFailure
}

var errStubFailure = errors.New("emit failed")
