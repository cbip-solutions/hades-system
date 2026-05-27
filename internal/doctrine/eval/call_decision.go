// SPDX-License-Identifier: MIT
// Package eval ships the release dynamic per-call MCP-boundary
// doctrine evaluator per Q10=D + spec §3.3 + §3.7 + §8.6 invariant.
//
// The evaluator consumes the active doctrine bundle (release
// doctrine.Active accessor) + the per-MCP risk-tier classification
// (Q10=D) + parameters of an outbound MCP call; produces (CallDecision,
// evidence). release dispatcher consumes at the MCP-call boundary; the
// single-egress invariant invariant stays intact (eval runs at the
// existing dispatcher seam, no new egress).
//
// Boundary: eval package consumes ONLY internal/doctrine
// (active doctrine accessor; for now via the TierPolicy injection seam,
// so this package's static imports are stdlib-only) + a caller-injected
// Emitter; MUST NOT import internal/store.
//
// Wiring note:
// this package ships the complete evaluator + lint extension as a
// consumable library; daemon-wiring (cmd/zen-swarm-ctld/main.go) is
// forward-additive release+ scope. The release spec §3.7 documents the
// MCP-call boundary as the canonical seam; release wires it. The eval
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
