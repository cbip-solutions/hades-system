// SPDX-License-Identifier: MIT
// Package eval — evaluator.go ships the RuntimeEvaluator: the concrete
// Evaluator interface impl that produces a CallDecision from (mcpName,
// toolName, params) + active doctrine bundle.
//
// inv-hades-184 enforcement: every EvaluateCall invocation MUST emit
// exactly one audit event via the configured Emitter. The runtime
// guarantee is the emit call below; tests assert via recordingEmitter
// count.
//
// Decision matrix per Q10=D + spec §2.10 3-profile baseline:
//
// profile == "max-scope" → CallAllow regardless of tier
// profile == "default":
// - low → CallAllow
// - medium → CallAllowWithAudit
// - high → CallAllowWithConfirm
// - unknown→ CallAllowWithAudit (conservative)
// profile == "capa-firewall":
// - mcpName.toolName in DenyList → CallDeny
// - mcpName.toolName in AllowList → CallAllow (explicit)
// - high (and not in AllowList) → CallDeny (deny-default)
// - medium → CallAllowWithConfirm
// - low → CallAllowWithAudit
// profile == "imported-from-claude-code" or other custom → treat as
// default-equivalent + apply allow/deny lists strictly.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
)

type Evaluator interface {
	EvaluateCall(ctx context.Context, mcpName, toolName string, params any) (decision CallDecision, evidence string, err error)
}

type Emitter interface {
	Emit(ctx context.Context, eventType string, payload []byte) (auditHash string, err error)
}

type Config struct {
	Policy TierPolicy

	Emitter Emitter
}

type RuntimeEvaluator struct {
	policy  TierPolicy
	emitter Emitter
}

// NewRuntimeEvaluator constructs the production evaluator. cfg.Policy +
// cfg.Emitter MUST both be non-nil; callers MUST validate at composition
// root.
func NewRuntimeEvaluator(cfg Config) *RuntimeEvaluator {
	return &RuntimeEvaluator{policy: cfg.Policy, emitter: cfg.Emitter}
}

func (r *RuntimeEvaluator) EvaluateCall(ctx context.Context, mcpName, toolName string, params any) (CallDecision, string, error) {
	decision, evidence := r.resolve(mcpName, toolName)

	paramKeys, sensitive := extractParamShape(params)
	if sensitive && r.policy.ActiveProfile() == "capa-firewall" {
		newDecision, newEvidence := escalateForSensitiveParams(decision, evidence, sensitive)
		decision = newDecision
		evidence = newEvidence
	}

	payload, jerr := json.Marshal(map[string]any{
		"mcpName":   mcpName,
		"toolName":  toolName,
		"decision":  decision.String(),
		"evidence":  evidence,
		"profile":   r.policy.ActiveProfile(),
		"paramKeys": paramKeys,
	})
	if jerr != nil {
		payload = []byte(fmt.Sprintf(`{"error":%q}`, jerr.Error()))
	}
	_, err := r.emitter.Emit(ctx, decision.EventTypeFor(), payload)
	return decision, evidence, err
}

// sensitiveParamKeys is the canonical taxonomy of parameter keys whose
// presence triggers param-aware policy refinement. Names match the
// most common MCP tool argument conventions (path, file_path, target,
// secret, token, api_key, etc). Ordering does not matter (the check
// is set-membership).
//
// SECURITY this slice is doctrine-canonical. Additions require ADR
// amendment + matching tests in evaluator_test.go.
var sensitiveParamKeys = map[string]bool{
	"path":      true,
	"file_path": true,
	"target":    true,
	"directory": true,
	"dir":       true,
	"secret":    true,
	"token":     true,
	"api_key":   true,
	"password":  true,
	"command":   true,
	"script":    true,
}

// extractParamShape walks the params arg (expected map[string]any or any
// JSON-decodable shape) and returns:
//
// - keys: sorted []string of top-level keys present in params, for
// audit forensic trace. Never includes values (security: values
// may carry secrets).
// - sensitive: true if any key in the params is a sensitive-shape
// key per sensitiveParamKeys taxonomy.
//
// Non-map params (e.g., a bare string or nil) return ([], false). The
// function is defensive: any unexpected shape falls through to (nil, false)
// rather than panic.
func extractParamShape(params any) (keys []string, sensitive bool) {
	if params == nil {
		return nil, false
	}
	m, ok := params.(map[string]any)
	if !ok {
		return nil, false
	}
	keys = make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
		if sensitiveParamKeys[k] {
			sensitive = true
		}
	}

	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys, sensitive
}

func escalateForSensitiveParams(decision CallDecision, evidence string, _ bool) (CallDecision, string) {
	next := decision
	switch decision {
	case CallAllow:
		next = CallAllowWithAudit
	case CallAllowWithAudit:
		next = CallAllowWithConfirm
	case CallAllowWithConfirm:
		next = CallDeny
	case CallDeny:
		next = CallDeny
	}
	return next, evidence + "; sensitive-param: present (capa-firewall escalation)"
}

func (r *RuntimeEvaluator) resolve(mcpName, toolName string) (CallDecision, string) {
	profile := r.policy.ActiveProfile()
	tier := r.policy.RiskTierFor(mcpName, toolName)
	toolKey := fmt.Sprintf("%s.%s", mcpName, toolName)

	switch profile {
	case "max-scope":
		return CallAllow, fmt.Sprintf("max-scope profile: allow-all (tier=%s)", tier)
	case "default":
		return resolveDefaultProfile(tier)
	case "capa-firewall":
		return r.resolveCapaFirewall(tier, mcpName, toolKey)
	default:

		return r.resolveCustomProfile(profile, tier, mcpName, toolKey)
	}
}

func resolveDefaultProfile(tier string) (CallDecision, string) {
	switch tier {
	case "low":
		return CallAllow, "default profile + low risk tier"
	case "medium":
		return CallAllowWithAudit, "default profile + medium risk tier (allow-with-audit)"
	case "high":
		return CallAllowWithConfirm, "default profile + high risk tier (require operator confirm)"
	default:
		return CallAllowWithAudit, fmt.Sprintf("default profile + unknown tier %q (conservative allow-with-audit)", tier)
	}
}

func (r *RuntimeEvaluator) resolveCapaFirewall(tier, mcpName, toolKey string) (CallDecision, string) {
	for _, denyKey := range r.policy.DenyList() {
		if denyKey == toolKey || denyKey == mcpName {
			return CallDeny, fmt.Sprintf("capa-firewall profile: %s in deny list", denyKey)
		}
	}
	for _, allowKey := range r.policy.AllowList() {
		if allowKey == toolKey || allowKey == mcpName {
			return CallAllow, fmt.Sprintf("capa-firewall profile: %s in allow list", allowKey)
		}
	}
	switch tier {
	case "low":
		return CallAllowWithAudit, "capa-firewall profile + low risk tier (allow-with-audit; not in allow list)"
	case "medium":
		return CallAllowWithConfirm, "capa-firewall profile + medium risk tier (require confirm; not in allow list)"
	case "high", "unknown":
		return CallDeny, fmt.Sprintf("capa-firewall profile: high/unknown tier %q deny-default (not in allow list)", tier)
	default:
		return CallDeny, fmt.Sprintf("capa-firewall profile: unrecognized tier %q deny-default", tier)
	}
}

func (r *RuntimeEvaluator) resolveCustomProfile(profile, tier, mcpName, toolKey string) (CallDecision, string) {
	for _, denyKey := range r.policy.DenyList() {
		if denyKey == toolKey || denyKey == mcpName {
			return CallDeny, fmt.Sprintf("custom profile %q: %s in deny list", profile, denyKey)
		}
	}
	for _, allowKey := range r.policy.AllowList() {
		if allowKey == toolKey || allowKey == mcpName {
			return CallAllow, fmt.Sprintf("custom profile %q: %s in allow list", profile, allowKey)
		}
	}
	switch tier {
	case "low":
		return CallAllowWithAudit, fmt.Sprintf("custom profile %q + low tier: allow-with-audit", profile)
	case "medium":
		return CallAllowWithConfirm, fmt.Sprintf("custom profile %q + medium tier: allow-with-confirm", profile)
	case "high":
		return CallDeny, fmt.Sprintf("custom profile %q + high tier (conservative deny)", profile)
	default:
		return CallAllowWithAudit, fmt.Sprintf("custom profile %q + unknown tier: conservative allow-with-audit", profile)
	}
}
