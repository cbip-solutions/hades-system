// tests/compliance/inv_zen_184_eval_emission_exactly_once_test.go
//
// Spec §8.6 inv-zen-184 compliance test: every eval.RuntimeEvaluator
// EvaluateCall invocation MUST emit exactly one audit event via the
// configured Emitter. The runtime guarantee is the load-bearing
// forensic-trace contract per spec §3.7 + §8.6.
//
// location per spec §8.6. The pkg-internal test
// (internal/doctrine/eval/evaluator_test.go) covers the truth table;
// this compliance test asserts the inv-zen-184 contract holds across
// every decision branch + emitter failure path.
package compliance

import (
	"context"
	"errors"
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

type recordingEmitter184 struct {
	count    int
	failNext bool
}

func (r *recordingEmitter184) Emit(_ context.Context, _ string, _ []byte) (string, error) {
	r.count++
	if r.failNext {
		return "", errEmitFailure
	}
	return "hash", nil
}

var errEmitFailure = errors.New("emit failed")

func TestInvZen184_ExactlyOneEmitPerCall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		policy  stubPolicy
		mcpName string
	}{
		{"allow", stubPolicy{tier: "low", profile: "default"}, "x"},
		{"allow_with_audit", stubPolicy{tier: "medium", profile: "default"}, "x"},
		{"allow_with_confirm", stubPolicy{tier: "high", profile: "default"}, "x"},
		{"deny-capa", stubPolicy{tier: "high", profile: "capa-firewall"}, "x"},
		{"unknown-tier-conservative", stubPolicy{tier: "", profile: "default"}, "x"},
		{"max-scope-allow-all", stubPolicy{tier: "high", profile: "max-scope"}, "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emitter := &recordingEmitter184{}
			e := eval.NewRuntimeEvaluator(eval.Config{
				Policy:  tc.policy,
				Emitter: emitter,
			})
			_, _, _ = e.EvaluateCall(context.Background(), tc.mcpName, "y", nil)
			if emitter.count != 1 {
				t.Errorf("emit count = %d for %s; want exactly 1 (inv-zen-184)", emitter.count, tc.name)
			}
		})
	}
}

func TestInvZen184_ExactlyOneEmitAcrossManyCalls(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter184{}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tier: "low", profile: "default"},
		Emitter: emitter,
	})
	const N = 100
	for i := 0; i < N; i++ {
		_, _, _ = e.EvaluateCall(context.Background(), "x", "y", nil)
	}
	if emitter.count != N {
		t.Errorf("emit count = %d for %d calls; want exactly %d (inv-zen-184)", emitter.count, N, N)
	}
}

func TestInvZen184_EmitterFailureStillCountsAsOneEmit(t *testing.T) {
	t.Parallel()
	emitter := &recordingEmitter184{failNext: true}
	e := eval.NewRuntimeEvaluator(eval.Config{
		Policy:  stubPolicy{tier: "low", profile: "default"},
		Emitter: emitter,
	})
	_, _, err := e.EvaluateCall(context.Background(), "x", "y", nil)
	if err == nil {
		t.Errorf("err = nil; want emit failure surfaced")
	}
	if emitter.count != 1 {
		t.Errorf("emit count = %d; want 1 (attempt counted regardless of failure)", emitter.count)
	}
}

func TestInvZen184_DecisionEventTypesAreCanonical(t *testing.T) {
	t.Parallel()
	cases := []struct {
		decision eval.CallDecision
		want     string
	}{
		{eval.CallAllow, "evt.doctrine.eval.allow"},
		{eval.CallAllowWithAudit, "evt.doctrine.eval.allow_with_audit"},
		{eval.CallAllowWithConfirm, "evt.doctrine.eval.allow_with_confirm"},
		{eval.CallDeny, "evt.doctrine.eval.deny"},
	}
	for _, tc := range cases {
		if got := tc.decision.EventTypeFor(); got != tc.want {
			t.Errorf("%v.EventTypeFor() = %q; want %q (inv-zen-184 canonical)", tc.decision, got, tc.want)
		}
	}
}
