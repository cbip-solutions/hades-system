package commenthygiene

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ClassifierDecision
	}{
		{"rot plan-n", "// Plan 5 dispatcher", DecisionDelete},
		{"rot v-version", "// v0.17.8 fix for refresh", DecisionDelete},
		{"rot todo no-owner", "// TODO fix this", DecisionDelete},
		{"rot ai-attribution", "// added by claude session abc", DecisionDelete},
		{"rot private-manifest pointer", "// per spec docs/superpowers/x.md", DecisionDelete},
		{"load-bearing invariant", "// inv-zen-031: bypass MUST NOT import store", DecisionKeep},
		{"load-bearing race", "// MUST hold mu before reading; race in chaos run", DecisionKeep},
		{"load-bearing workaround", "// workaround: hermes-agent#42 returns nil", DecisionKeep},
		{"todo with owner", "// TODO(testuser 2026-06-01): refactor", DecisionKeep},
		{"neutral wins keep", "// LegitFunction performs the work", DecisionKeep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.in)
			if got != tc.want {
				t.Errorf("Classify(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestClassifierDecisionString(t *testing.T) {
	cases := []struct {
		d    ClassifierDecision
		want string
	}{
		{DecisionKeep, "KEEP"},
		{DecisionDelete, "DELETE"},
		{DecisionRewrite, "REWRITE"},
		{ClassifierDecision(99), "UNKNOWN"},
	}
	for _, tc := range cases {
		if got := tc.d.String(); got != tc.want {
			t.Errorf("ClassifierDecision(%d).String() = %q, want %q", int(tc.d), got, tc.want)
		}
	}
}
