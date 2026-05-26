//go:build adversarial

// Package adversarial — p13_doctrine_eval_hostile_params_test.go (Plan
// 13 Phase F-tail F-imp IMPORTANT 7).
//
// Adversarial: the doctrine evaluator MUST handle hostile/malformed
// params without panic, never leak secret values via paramKeys, and
// never escalate decisions outside the capa-firewall escalation rules.
//
// Build tag `adversarial` excludes this file from default CI; opt-in
// via `go test -tags=adversarial ./tests/adversarial/...`.
package adversarial

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/doctrine/eval"
)

type stubPolicy struct {
	tier    string
	profile string
}

func (s stubPolicy) RiskTierFor(_, _ string) string { return s.tier }
func (s stubPolicy) ActiveProfile() string          { return s.profile }
func (s stubPolicy) AllowList() []string            { return nil }
func (s stubPolicy) DenyList() []string             { return nil }

type silentEmitter struct {
	lastPayload string
}

func (s *silentEmitter) Emit(_ context.Context, _ string, payload []byte) (string, error) {
	s.lastPayload = string(payload)
	return "hash", nil
}

func TestAdversarial_HostileParamsDoNotPanic(t *testing.T) {
	t.Parallel()
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tier: "low", profile: "default"},
		Emitter: &silentEmitter{},
	})
	hostile := []any{
		nil,
		map[string]any{},
		"bare-string",
		42,
		[]int{1, 2, 3},
		map[string]any{
			"nested": map[string]any{
				"deep": map[string]any{
					"deeper": "value",
				},
			},
		},

		largeMap(),
	}
	for i, params := range hostile {
		decision, _, err := e.EvaluateCall(context.Background(), "x", "y", params)
		if err != nil {
			t.Errorf("hostile params [%d] surfaced err: %v", i, err)
		}

		switch decision {
		case eval.CallAllow, eval.CallAllowWithAudit, eval.CallAllowWithConfirm, eval.CallDeny:

		default:
			t.Errorf("hostile params [%d]: decision %v not in canonical enum", i, decision)
		}
	}
}

// TestAdversarial_SecretValuesNeverInAuditPayload asserts the property:
// no matter what value-shape is in the params, the audit payload
// "paramKeys" field NEVER includes value content. Security: values may
// be secrets; only keys are forensic-traced.
func TestAdversarial_SecretValuesNeverInAuditPayload(t *testing.T) {
	t.Parallel()
	emitter := &silentEmitter{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tier: "low", profile: "default"},
		Emitter: emitter,
	})
	secrets := []string{
		"sk-anthropic-secret-token-12345",
		"ghp_abc123def456ghi789jkl012mno345",
		"password123!@#",
		"BEGIN-PRIVATE-KEY----END-KEY",
	}
	for _, secret := range secrets {
		params := map[string]any{
			"api_key":  secret,
			"password": secret,
			"token":    secret,
		}
		_, _, _ = e.EvaluateCall(context.Background(), "x", "y", params)
		// The payload MUST NOT contain any of the secret values.
		if strings.Contains(emitter.lastPayload, secret) {
			t.Errorf("audit payload leaked secret value %q in payload: %s",
				secret, emitter.lastPayload)
		}
	}
}

func TestAdversarial_EscalationOnlyInCapaFirewall(t *testing.T) {
	t.Parallel()
	params := map[string]any{
		"path":     "/etc/passwd",
		"command":  "rm -rf /",
		"token":    "secret",
		"secret":   "more-secret",
		"api_key":  "leaked",
		"password": "1234",
	}
	profiles := []string{"default", "max-scope", "imported-from-claude-code"}
	for _, profile := range profiles {
		emitter := &silentEmitter{}
		e := eval.NewRuntimeEvaluator(eval.Config{
			Policy:  stubPolicy{tier: "low", profile: profile},
			Emitter: emitter,
		})
		decision, evidence, _ := e.EvaluateCall(context.Background(), "x", "y", params)

		if strings.Contains(evidence, "sensitive-param") {
			t.Errorf("profile %q escalated on sensitive params (evidence=%q); only capa-firewall should escalate",
				profile, evidence)
		}

		switch profile {
		case "default":
			if decision != eval.CallAllow {
				t.Errorf("default+low decision = %v; want CallAllow (no escalation)", decision)
			}
		case "max-scope":
			if decision != eval.CallAllow {
				t.Errorf("max-scope decision = %v; want CallAllow", decision)
			}
		}
	}
}

func largeMap() map[string]any {
	m := make(map[string]any, 10000)
	for i := 0; i < 10000; i++ {
		m[fmtKey(i)] = i
	}
	return m
}

func fmtKey(i int) string {

	return "k" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
