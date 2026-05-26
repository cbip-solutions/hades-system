package orchestrator_test

import (
	"context"
	"testing"

	orch "github.com/cbip-solutions/hades-system/internal/orchestrator"
)

type fakeProvider struct{ v orch.Verdict }

func (f fakeProvider) BlastRadius(_ context.Context, _ string, _, _ []string) (orch.Verdict, error) {
	return f.v, nil
}

func TestVerdictIsHigh(t *testing.T) {
	if !(orch.Verdict{Level: "high"}).IsHigh() {
		t.Error("Verdict{high}.IsHigh() = false; want true")
	}
	for _, lvl := range []string{"low", "medium", "", "HIGH"} {
		if (orch.Verdict{Level: lvl}).IsHigh() {
			t.Errorf("Verdict{%q}.IsHigh() = true; want false (only exact \"high\" fires)", lvl)
		}
	}
}

func TestBlastRadiusProviderSeam(t *testing.T) {
	var p orch.BlastRadiusProvider = fakeProvider{v: orch.Verdict{Level: "high", Score: 0.7, TopAffected: []string{"pkg.A"}}}
	v, err := p.BlastRadius(context.Background(), "proj", []string{"pkg.X"}, []string{"x.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if !v.IsHigh() || v.Score != 0.7 || len(v.TopAffected) != 1 {
		t.Errorf("verdict = %+v; want {high, 0.7, [pkg.A]}", v)
	}
}
