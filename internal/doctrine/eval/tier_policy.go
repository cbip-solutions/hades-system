// SPDX-License-Identifier: MIT
// Package eval — tier_policy.go ships the TierPolicy interface consumed
// by RuntimeEvaluator to resolve per-MCP risk tiers + active doctrine
// profile + allow/deny lists.
//
// Production impl wraps Plan 8 doctrine.Active accessor; tests substitute
// stubPolicy for deterministic state. This indirection (vs reaching
// into internal/doctrine directly) keeps the eval package boundary
// minimal — the caller decides how doctrine state is sourced (file,
// daemon HTTP, in-memory test fixture).
package eval

type TierPolicy interface {
	// RiskTierFor returns "high" | "medium" | "low" | "unknown" for the
	// given MCP name + tool name. The (mcpName, toolName) tuple disambiguates
	// per-tool-granularity overrides per Q10=D dynamic eval.
	//
	// Implementation MUST default to "unknown" for MCPs not in the
	// curated catalog so the evaluator can apply the conservative
	// fallback (CallAllowWithAudit under default profile).
	RiskTierFor(mcpName, toolName string) string

	ActiveProfile() string

	AllowList() []string

	DenyList() []string
}
