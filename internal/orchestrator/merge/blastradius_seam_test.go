package merge_test

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type fakeBlastScorer struct{ v merge.Verdict }

func (f fakeBlastScorer) BlastRadius(_ context.Context, _ string, _, _ []string) (merge.Verdict, error) {
	return f.v, nil
}

func TestMergeBlastRadiusScorerSeam(t *testing.T) {
	var sc merge.BlastRadiusScorer = fakeBlastScorer{v: merge.Verdict{Level: "high", Score: 0.8}}
	v, err := sc.BlastRadius(context.Background(), "proj", []string{"pkg.X"}, []string{"x.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if v.Score != 0.8 || v.Level != "high" {
		t.Errorf("verdict = %+v; want {high, 0.8}", v)
	}
}
