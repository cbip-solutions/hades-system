package evolution

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestDefaultRiskWeightsMatchSpec(t *testing.T) {
	w := DefaultRiskWeights()
	if w.Cone != 0.35 {
		t.Errorf("Cone = %v; want 0.35", w.Cone)
	}
	if w.Coreness != 0.30 {
		t.Errorf("Coreness = %v; want 0.30", w.Coreness)
	}
	if w.Churn != 0.20 {
		t.Errorf("Churn = %v; want 0.20", w.Churn)
	}
	if w.Coupling != 0.15 {
		t.Errorf("Coupling = %v; want 0.15", w.Coupling)
	}
}

func TestRiskWeightsValidateSumsToOne(t *testing.T) {
	if err := DefaultRiskWeights().Validate(); err != nil {
		t.Errorf("DefaultRiskWeights().Validate() = %v; want nil", err)
	}
	sum := DefaultRiskWeights().Cone + DefaultRiskWeights().Coreness +
		DefaultRiskWeights().Churn + DefaultRiskWeights().Coupling
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("weights sum = %v; want 1.0", sum)
	}
}

func TestRiskWeightsValidateRejectsBadSum(t *testing.T) {
	bad := RiskWeights{Cone: 0.25, Coreness: 0.30, Churn: 0.20, Coupling: 0.15}
	if err := bad.Validate(); err == nil {
		t.Error("Validate() on sum=0.90 = nil; want a sum-violation error")
	} else if !errors.Is(err, ErrRiskWeightsInvalid) {
		t.Errorf("Validate() = %v; want ErrRiskWeightsInvalid", err)
	}
	neg := RiskWeights{Cone: -0.05, Coreness: 0.40, Churn: 0.30, Coupling: 0.35}
	if err := neg.Validate(); err == nil {
		t.Error("Validate() on a negative weight = nil; want rejection")
	}
}

func TestDefaultRiskThresholdsMatchSpec(t *testing.T) {
	tr := DefaultRiskThresholds()
	if tr.MediumAt != 0.30 {
		t.Errorf("MediumAt = %v; want 0.30", tr.MediumAt)
	}
	if tr.HighAt != 0.60 {
		t.Errorf("HighAt = %v; want 0.60", tr.HighAt)
	}
}

func TestRiskThresholdsLevelBoundaries(t *testing.T) {
	tr := DefaultRiskThresholds()
	cases := []struct {
		score float64
		want  string
	}{
		{0.0, "low"}, {0.2999, "low"},
		{0.30, "medium"}, {0.45, "medium"}, {0.5999, "medium"},
		{0.60, "high"}, {0.85, "high"}, {1.0, "high"},
	}
	for _, c := range cases {
		if got := tr.Level(c.score); got != c.want {
			t.Errorf("Level(%v) = %q; want %q", c.score, got, c.want)
		}
	}
}

func TestRiskThresholdsValidate(t *testing.T) {
	if err := DefaultRiskThresholds().Validate(); err != nil {
		t.Errorf("DefaultRiskThresholds().Validate() = %v; want nil", err)
	}
	inverted := RiskThresholds{MediumAt: 0.60, HighAt: 0.30}
	if err := inverted.Validate(); err == nil {
		t.Error("Validate() on inverted bands = nil; want rejection")
	}
}

func TestPercentileRank(t *testing.T) {
	pop := []int{1, 2, 2, 5, 10}

	if got := percentileRank(10, pop); got != 1.0 {
		t.Errorf("percentileRank(10) = %v; want 1.0", got)
	}

	if got := percentileRank(2, pop); got != 0.6 {
		t.Errorf("percentileRank(2) = %v; want 0.6", got)
	}

	if got := percentileRank(0, pop); got != 0.0 {
		t.Errorf("percentileRank(0) = %v; want 0.0", got)
	}

	if got := percentileRank(5, nil); got != 0.0 {
		t.Errorf("percentileRank(_, empty) = %v; want 0.0", got)
	}
}

func TestNormLog1p(t *testing.T) {

	if got := normLog1p(50, 50); !approxEqual(got, 1.0) {
		t.Errorf("normLog1p(50,50) = %v; want 1.0", got)
	}

	if got := normLog1p(0, 50); got != 0.0 {
		t.Errorf("normLog1p(0,50) = %v; want 0.0", got)
	}

	if got := normLog1p(500, 50); got != 1.0 {
		t.Errorf("normLog1p(500,50) = %v; want clamp 1.0", got)
	}

	if got := normLog1p(10, 0); got != 0.0 {
		t.Errorf("normLog1p(10,0) = %v; want 0.0 (guarded)", got)
	}
}

func TestClamp01(t *testing.T) {
	if clamp01(-0.5) != 0 || clamp01(1.5) != 1 || clamp01(0.4) != 0.4 {
		t.Errorf("clamp01 wrong: got %v %v %v; want 0 1 0.4", clamp01(-0.5), clamp01(1.5), clamp01(0.4))
	}
}

// TestEnumerateAllNodesIncludesKindType is the bite-check for the recurring
// "six NodeKinds" bug. enumerateAllNodes MUST include store.KindType nodes so
// that type-definition nodes are not silently excluded from the blast-radius
// percentile baselines (p95Cone, maxCoreness, p95Fanout). If enumerateAllNodes
// is reverted to a 6-kind list without KindType, this test will fail because
// the seeded KindType node will be absent from the returned set.
func TestEnumerateAllNodesIncludesKindType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	nodes := []struct {
		id   string
		kind store.NodeKind
	}{
		{"pkg.F", store.KindFunction},
		{"pkg.M", store.KindMethod},
		{"pkg.S", store.KindStruct},
		{"pkg.I", store.KindInterface},
		{"pkg.T", store.KindType},
		{"pkg.Fi", store.KindField},
		{"pkg.P", store.KindPackage},
	}
	for _, n := range nodes {
		if err := s.UpsertNode(ctx, store.Node{
			NodeID: n.id, Name: n.id, Kind: string(n.kind),
			Language: "go", FilePath: "x.go", ContentHash: "h",
		}); err != nil {
			t.Fatalf("UpsertNode %s: %v", n.id, err)
		}
	}

	all, err := enumerateAllNodes(ctx, s)
	if err != nil {
		t.Fatalf("enumerateAllNodes: %v", err)
	}

	ids := make(map[string]bool, len(all))
	for _, n := range all {
		ids[n.NodeID] = true
	}

	if !ids["pkg.T"] {
		t.Error("enumerateAllNodes omitted the KindType node \"pkg.T\"; all 7 NodeKind values must be enumerated")
	}

	for _, n := range nodes {
		if !ids[n.id] {
			t.Errorf("enumerateAllNodes omitted node %q (kind %s)", n.id, n.kind)
		}
	}
	if len(all) != 7 {
		t.Errorf("enumerateAllNodes returned %d nodes; want 7 (one per NodeKind)", len(all))
	}
}
