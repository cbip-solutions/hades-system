// SPDX-License-Identifier: MIT
package hermesplugin

type TestCitation struct {
	ID    string
	Body  string
	Cites []string
}

func NewTestCitation() *TestCitation {
	return &TestCitation{
		ID:    "cit-01",
		Body:  "test-citation: zen-swarm renders consistently across 7 renderers (Plan 15 A-3)",
		Cites: []string{"docs/superpowers/specs/2026-05-15-zen-swarm-plan-15-release-polish-design.md#1.6"},
	}
}
