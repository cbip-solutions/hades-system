// SPDX-License-Identifier: MIT
package audit

import "sort"

func defaultTemplates() map[string]string {
	return map[string]string{
		"default": `You are an expert code reviewer. Review the following unified diff and return ONLY a JSON object (no markdown, no prose outside JSON) with exactly these fields:

{
  "classification": "<one of: clean, minor, major, reject>",
  "concerns": ["<concern 1>", "..."],
  "suggestions": ["<suggestion 1>", "..."]
}

Classification guide:
- clean: no issues; diff is correct, safe, and well-structured
- minor: low-severity style, naming, or documentation issues that do not block merge
- major: correctness, performance, or maintainability issues that must be addressed before merge
- reject: security vulnerability, data loss risk, broken invariant, or architectural violation

Concerns should be specific and actionable (include file/line when identifiable).
Suggestions should be concrete remediation steps.

Diff to review:
`,

		"security": `You are a security-focused code reviewer. Review the following unified diff for security vulnerabilities, unsafe patterns, and policy violations. Return ONLY a JSON object with exactly these fields:

{
  "classification": "<one of: clean, minor, major, reject>",
  "concerns": ["<security concern 1>", "..."],
  "suggestions": ["<remediation 1>", "..."]
}

Classification guide:
- clean: no security issues found
- minor: low-severity hardening gaps (e.g. missing input sanitisation on non-critical path)
- major: exploitable vulnerability, weak crypto, secret exposure, or missing auth check
- reject: critical vulnerability, RCE vector, data exfiltration path, or direct secret leak

Check specifically for: injection (SQL/cmd/LDAP), credential handling, cryptographic misuse,
path traversal, privilege escalation, missing rate limiting, unsafe deserialization,
missing TLS pinning, timing side-channels, and audit log suppression.

Diff to review:
`,

		"performance": `You are a performance-focused code reviewer. Review the following unified diff for performance regressions, algorithmic inefficiencies, and resource leaks. Return ONLY a JSON object with exactly these fields:

{
  "classification": "<one of: clean, minor, major, reject>",
  "concerns": ["<performance concern 1>", "..."],
  "suggestions": ["<optimisation suggestion 1>", "..."]
}

Classification guide:
- clean: no performance concerns
- minor: micro-optimisation opportunities that have negligible real-world impact
- major: O(n²) or worse where O(n log n) is feasible, goroutine leak, memory leak, or unnecessary allocation in hot path
- reject: known catastrophic regression (e.g. unbounded growth, infinite loop risk, deadlock)

Check specifically for: algorithmic complexity, allocations per request, channel/goroutine leaks,
lock contention, N+1 queries, unnecessary serialization, blocking calls in hot path.

Diff to review:
`,

		"doctrine-violation": `You are a doctrine compliance reviewer for the zen-swarm project. Review the following unified diff for violations of the project's max-scope doctrine, invariants, and architectural boundaries. Return ONLY a JSON object with exactly these fields:

{
  "classification": "<one of: clean, minor, major, reject>",
  "concerns": ["<doctrine concern 1>", "..."],
  "suggestions": ["<remediation 1>", "..."]
}

Classification guide:
- clean: no doctrine violations; code is complete, correct, and boundary-respecting
- minor: style inconsistency (naming, comment missing why) that doesn't break invariants
- major: stub code (ErrNotImplementedPlanN, panic("not implemented"), TODO implement later),
  missing test coverage for documented behaviour, or inv-zen-031 boundary leakage
- reject: architectural boundary violation (internal/store imported from bypass/providers/mcp),
  inv-zen-004 Claude attribution, skipped gate (--no-verify), or max-scope regression

Check specifically for: incomplete method bodies, missing error propagation, missing WHY comments,
inv-zen-031 boundary leakage, Claude attribution in commits, missing invariant tests,
single-egress bypass, and Plan N boundary violations.

Diff to review:
`,
	}
}

type CriteriaRegistry struct {
	templates map[string]string
}

func NewCriteriaRegistry(custom map[string]string) *CriteriaRegistry {
	builtins := defaultTemplates()
	merged := make(map[string]string, len(builtins)+len(custom))
	for k, v := range builtins {
		merged[k] = v
	}
	for k, v := range custom {
		if v != "" {
			merged[k] = v
		}
	}
	return &CriteriaRegistry{templates: merged}
}

func (r *CriteriaRegistry) Get(name string) (string, bool) {
	if tmpl, ok := r.templates[name]; ok {
		return tmpl, true
	}
	return r.templates["default"], false
}

func (r *CriteriaRegistry) Names() []string {
	names := make([]string, 0, len(r.templates))
	for k := range r.templates {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// DefaultCriteriaTemplateNames returns the canonical set of built-in
// criteria template names in sorted order. This is the SOURCE OF TRUTH
// for the four templates the audit MCP CriteriaRegistry recognises:
// `default`, `security`, `performance`, `doctrine-violation`.
//
// Exported for downstream consumers (CLI client catalog, drift tests)
// to assert their copies stay in sync. Pre-fix the CLI catalog drift
// test (internal/client/audit_test.go::TestAuditCriteria_MatchesAuditMCPRegistry)
// hardcoded the four names instead of consulting this function — if a
// new template was added or one was renamed here, both the CLI catalog
// AND the test would continue to "pass" with stale names (review C-3).
//
// Returning a freshly-constructed sorted slice prevents accidental
// mutation by callers (mirrors the defaultTemplates() S-3 fix).
func DefaultCriteriaTemplateNames() []string {
	tmpls := defaultTemplates()
	names := make([]string, 0, len(tmpls))
	for k := range tmpls {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
