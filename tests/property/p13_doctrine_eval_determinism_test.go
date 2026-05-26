//go:build property

// Package property — p13_doctrine_eval_determinism_test.go (Plan 13
// Phase F-tail F-imp IMPORTANT 7).
//
// Property: identical inputs to eval.RuntimeEvaluator.EvaluateCall MUST
// produce identical (decision, evidence) tuples + identical audit
// event types. Determinism is the load-bearing forensic-trace contract
// — drift between identical calls would break reproducibility (operator
// can't reconcile audit events across daemon runs).
//
// Build tag `property` excludes this file from default CI; opt-in via
// `make test-property` or `go test -tags=property ./tests/property/...`.
package property

import (
	"context"
	"sync"
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

type recordingEmitter struct {
	mu        sync.Mutex
	calls     int
	lastEvent string
}

func (r *recordingEmitter) Emit(_ context.Context, eventType string, _ []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastEvent = eventType
	return "hash", nil
}

func TestP13_DoctrineEval_DeterministicAcrossCalls(t *testing.T) {
	t.Parallel()
	const N = 1000
	cases := []struct {
		policy stubPolicy
	}{
		{stubPolicy{tier: "low", profile: "default"}},
		{stubPolicy{tier: "medium", profile: "default"}},
		{stubPolicy{tier: "high", profile: "default"}},
		{stubPolicy{tier: "high", profile: "capa-firewall"}},
		{stubPolicy{tier: "low", profile: "max-scope"}},
	}
	for _, tc := range cases {
		emitter := &recordingEmitter{}
		e := eval.NewRuntimeEvaluator(eval.Config{
			Policy:  tc.policy,
			Emitter: emitter,
		})
		firstDecision, firstEvidence, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
		firstEvent := emitter.lastEvent
		for i := 1; i < N; i++ {
			d, ev, _ := e.EvaluateCall(context.Background(), "x", "y", nil)
			if d != firstDecision {
				t.Errorf("decision drift on call %d: %v vs first %v (profile=%s tier=%s)",
					i, d, firstDecision, tc.policy.profile, tc.policy.tier)
				break
			}
			if ev != firstEvidence {
				t.Errorf("evidence drift on call %d: %q vs first %q", i, ev, firstEvidence)
				break
			}
			if emitter.lastEvent != firstEvent {
				t.Errorf("event-type drift on call %d: %q vs first %q",
					i, emitter.lastEvent, firstEvent)
				break
			}
		}
		if emitter.calls != N {
			t.Errorf("emit count = %d for %d calls; want exactly %d (inv-zen-184)",
				emitter.calls, N, N)
		}
	}
}
