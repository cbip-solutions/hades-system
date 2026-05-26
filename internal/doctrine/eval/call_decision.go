// SPDX-License-Identifier: MIT
// Package eval ships the Plan 13 Phase F6 dynamic per-call MCP-boundary
// doctrine evaluator per Q10=D + spec §3.3 + §3.7 + §8.6 inv-zen-184.
//
// The evaluator consumes the active doctrine bundle (Plan 8
// doctrine.Active accessor) + the per-MCP risk-tier classification
// (Q10=D) + parameters of an outbound MCP call; produces (CallDecision,
// evidence). Plan 3 dispatcher consumes at the MCP-call boundary; the
// single-egress invariant inv-zen-088 stays intact (eval runs at the
// existing dispatcher seam, no new egress).
//
// Boundary (inv-zen-031): eval package consumes ONLY internal/doctrine
// (active doctrine accessor; for now via the TierPolicy injection seam,
// so this package's static imports are stdlib-only) + a caller-injected
// Emitter; MUST NOT import internal/store.
//
// Wiring note (Plan 13 Phase F-tail F6 reality check):
// this package ships the complete evaluator + lint extension as a
// consumable library; daemon-wiring (cmd/zen-swarm-ctld/main.go) is
// forward-additive Plan 14+ scope. The Plan 13 spec §3.7 documents the
// MCP-call boundary as the canonical seam; Plan 14 wires it. The eval
// API is final-shape (Q10=D doctrine satisfied) and ready for
// consumption.
package eval

type CallDecision int

const (
	CallAllow CallDecision = iota

	CallAllowWithAudit

	CallAllowWithConfirm

	CallDeny
)

func (d CallDecision) String() string {
	switch d {
	case CallAllow:
		return "allow"
	case CallAllowWithAudit:
		return "allow_with_audit"
	case CallAllowWithConfirm:
		return "allow_with_confirm"
	case CallDeny:
		return "deny"
	default:
		return "unknown"
	}
}

func (d CallDecision) EventTypeFor() string {
	return "evt.doctrine.eval." + d.String()
}
