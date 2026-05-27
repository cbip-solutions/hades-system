// go:build cgo
//go:build cgo
// +build cgo

package evolution

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/structure"
)

func seedNode(t *testing.T, s *store.Store, id, file string, coreness int) {
	t.Helper()
	if err := s.UpsertNode(context.Background(), store.Node{
		NodeID: id, Name: id, Kind: string(store.KindFunction), Language: "go",
		FilePath: file, Coreness: coreness, ContentHash: "h",
	}); err != nil {
		t.Fatalf("seedNode %s: %v", id, err)
	}
}

func seedEdge(t *testing.T, s *store.Store, caller, callee string) {
	t.Helper()
	if err := s.UpsertEdge(context.Background(), store.Edge{
		SourceID: caller, TargetID: callee, Kind: string(store.EdgeCalls),
		Confidence: store.ConfExactStatic, SiteFile: "x.go", SiteLine: 1,
	}); err != nil {
		t.Fatalf("seedEdge %s→%s: %v", caller, callee, err)
	}
}

func TestReverseConeCountsTransitiveCallers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"pkg.A", "pkg.B", "pkg.C"} {
		seedNode(t, s, id, "x.go", 0)
	}
	seedEdge(t, s, "pkg.A", "pkg.B")
	seedEdge(t, s, "pkg.B", "pkg.C")
	size, top, err := reverseCone(ctx, s, []string{"pkg.C"})
	if err != nil {
		t.Fatalf("reverseCone: %v", err)
	}
	if size != 2 {
		t.Errorf("cone size = %d; want 2 (B and A are transitive callers of C)", size)
	}

	if len(top) != 2 {
		t.Errorf("TopAffected = %v; want 2 entries", top)
	}
}

func TestReverseConeCycleGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.A", "x.go", 0)
	seedNode(t, s, "pkg.B", "x.go", 0)
	seedEdge(t, s, "pkg.A", "pkg.B")
	seedEdge(t, s, "pkg.B", "pkg.A")
	size, _, err := reverseCone(ctx, s, []string{"pkg.A"})
	if err != nil {
		t.Fatalf("reverseCone: %v", err)
	}
	if size != 1 {
		t.Errorf("cyclic cone size = %d; want 1 (B is A's only caller; A not counted as its own)", size)
	}
}

func TestReverseConeInvokeEdges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.Iface.M", "x.go", 0)
	seedNode(t, s, "pkg.Caller", "x.go", 0)
	if err := s.UpsertEdge(ctx, store.Edge{
		SourceID: "pkg.Caller", TargetID: "pkg.Iface.M", Kind: string(store.EdgeInvoke),
		Confidence: store.ConfExactVTA, SiteFile: "x.go", SiteLine: 1,
	}); err != nil {
		t.Fatalf("seed invoke edge: %v", err)
	}
	size, _, err := reverseCone(ctx, s, []string{"pkg.Iface.M"})
	if err != nil {
		t.Fatalf("reverseCone: %v", err)
	}
	if size != 1 {
		t.Errorf("cone over EdgeInvoke = %d; want 1 (the dynamic-dispatch caller is in the radius)", size)
	}
}

func TestMaxCorenessAndChurnHelpers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.Hub", "hub.go", 7)
	seedNode(t, s, "pkg.Leaf", "leaf.go", 1)
	mc, err := maxCoreness(ctx, s, structure.Decomposition{})
	if err != nil {
		t.Fatalf("maxCoreness: %v", err)
	}
	if mc != 7 {
		t.Errorf("maxCoreness = %d; want 7 (the hub)", mc)
	}
	if err := s.UpsertChurn(ctx, store.Churn{Path: "hub.go", WindowDays: 90, TouchCount: 42, AuthorCount: 3, UpdatedAt: 1}); err != nil {
		t.Fatalf("seed churn: %v", err)
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	ch, err := maxChurn(ctx, b, []string{"hub.go", "absent.go"}, 90)
	if err != nil {
		t.Fatalf("maxChurn: %v", err)
	}
	if ch != 42 {
		t.Errorf("maxChurn = %d; want 42 (hub.go; absent.go contributes 0)", ch)
	}
}

func TestReverseConeDeterminismOrdering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"pkg.A", "pkg.B", "pkg.C", "pkg.D"} {
		seedNode(t, s, id, "x.go", 0)
	}
	seedEdge(t, s, "pkg.A", "pkg.D")
	seedEdge(t, s, "pkg.B", "pkg.D")
	seedEdge(t, s, "pkg.C", "pkg.D")

	first := func() []string {
		_, top, err := reverseCone(ctx, s, []string{"pkg.D"})
		if err != nil {
			t.Fatalf("reverseCone: %v", err)
		}
		return top
	}
	a := first()
	b := first()
	if len(a) != 3 {
		t.Fatalf("cone size = %d; want 3", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("non-deterministic: call %d returned %v; call %d returned %v", 1, a, 2, b)
			break
		}
	}
}

func TestP95ConeEmptyGraph(t *testing.T) {
	s := newTestStore(t)
	got, err := p95Cone(context.Background(), s)
	if err != nil {
		t.Fatalf("p95Cone empty: %v", err)
	}
	if got != 0 {
		t.Errorf("p95Cone(empty) = %v; want 0", got)
	}
}

func TestP95ConeLinearChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"pkg.A", "pkg.B", "pkg.C", "pkg.D"} {
		seedNode(t, s, id, "x.go", 0)
	}
	seedEdge(t, s, "pkg.A", "pkg.B")
	seedEdge(t, s, "pkg.B", "pkg.C")
	seedEdge(t, s, "pkg.C", "pkg.D")

	got, err := p95Cone(ctx, s)
	if err != nil {
		t.Fatalf("p95Cone: %v", err)
	}

	if got != 3 {
		t.Errorf("p95Cone(A→B→C→D) = %v; want 3", got)
	}
}

func TestCouplingFanoutDeduped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, row := range []store.CoChange{
		{FileA: "a.go", FileB: "b.go", SharedRevs: 6, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1},
		{FileA: "a.go", FileB: "c.go", SharedRevs: 4, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1},
		{FileA: "b.go", FileB: "d.go", SharedRevs: 5, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1},
	} {
		if err := s.UpsertCoChange(ctx, row); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})

	got, err := couplingFanout(ctx, b, []string{"a.go", "d.go"}, 90)
	if err != nil {
		t.Fatalf("couplingFanout: %v", err)
	}
	if got != 2 {
		t.Errorf("couplingFanout({a.go, d.go}) = %d; want 2 (b.go+c.go deduped)", got)
	}
}

func TestMaxCorenessDecompositionFastPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.X", "x.go", 3)

	d := structure.Decomposition{Coreness: map[string]int{"pkg.X": 9}}
	mc, err := maxCoreness(ctx, s, d)
	if err != nil {
		t.Fatalf("maxCoreness: %v", err)
	}
	if mc != 9 {
		t.Errorf("maxCoreness with decomp = %d; want 9 (decomp fast-path peak)", mc)
	}
}

func TestP95FanoutEmptyAndCoupled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	got, err := p95Fanout(ctx, s, b, 90)
	if err != nil {
		t.Fatalf("p95Fanout(empty): %v", err)
	}
	if got != 0 {
		t.Errorf("p95Fanout(empty) = %v; want 0", got)
	}

	seedNode(t, s, "pkg.A", "a.go", 0)
	seedNode(t, s, "pkg.B", "b.go", 0)
	seedNode(t, s, "pkg.C", "c.go", 0)

	if err := s.UpsertCoChange(ctx, store.CoChange{
		FileA: "a.go", FileB: "b.go", SharedRevs: 5, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed cochange: %v", err)
	}

	got, err = p95Fanout(ctx, s, b, 90)
	if err != nil {
		t.Fatalf("p95Fanout(seeded): %v", err)
	}
	if got != 1 {
		t.Errorf("p95Fanout(a+b coupled, c alone) = %v; want 1", got)
	}
}

func TestCouplingFanoutBelowThreshold(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertCoChange(ctx, store.CoChange{
		FileA: "a.go", FileB: "b.go", SharedRevs: 1, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	got, err := couplingFanout(ctx, b, []string{"a.go"}, 90)
	if err != nil {
		t.Fatalf("couplingFanout: %v", err)
	}
	if got != 0 {
		t.Errorf("couplingFanout (below threshold) = %d; want 0 (10%% < 30%% threshold)", got)
	}
}

func TestP95SingleElement(t *testing.T) {
	got := p95([]int{7})
	if got != 7 {
		t.Errorf("p95([7]) = %v; want 7 (single element → itself)", got)
	}
}

func TestReverseConeSortedTopAffected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"pkg.Alpha", "pkg.Zulu", "pkg.Z"} {
		seedNode(t, s, id, "x.go", 0)
	}
	seedEdge(t, s, "pkg.Alpha", "pkg.Z")
	seedEdge(t, s, "pkg.Zulu", "pkg.Z")
	size, top, err := reverseCone(ctx, s, []string{"pkg.Z"})
	if err != nil {
		t.Fatalf("reverseCone: %v", err)
	}
	if size != 2 {
		t.Fatalf("cone size = %d; want 2", size)
	}

	if top[0] != "pkg.Alpha" || top[1] != "pkg.Zulu" {
		t.Errorf("TopAffected = %v; want [pkg.Alpha, pkg.Zulu] (lex tie-break)", top)
	}
}

func TestBlastRadiusHighRiskHub(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 9)
	for i := 0; i < 8; i++ {
		caller := "pkg.C" + string(rune('0'+i))
		seedNode(t, s, caller, "c.go", 1)
		seedEdge(t, s, caller, "pkg.Hub")
	}

	seedNode(t, s, "pkg.Leaf", "leaf.go", 1)

	if err := s.UpsertChurn(ctx, store.Churn{Path: "hub.go", WindowDays: 90, TouchCount: 80, AuthorCount: 6, UpdatedAt: 1}); err != nil {
		t.Fatalf("seed churn: %v", err)
	}
	if err := s.UpsertChurn(ctx, store.Churn{Path: "leaf.go", WindowDays: 90, TouchCount: 1, AuthorCount: 1, UpdatedAt: 1}); err != nil {
		t.Fatalf("seed churn leaf: %v", err)
	}

	for _, peer := range []string{"p1.go", "p2.go", "p3.go"} {
		if err := s.UpsertCoChange(ctx, store.CoChange{FileA: "hub.go", FileB: peer, SharedRevs: 8, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1}); err != nil {
			t.Fatalf("seed cochange: %v", err)
		}
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Hub"}, []string{"hub.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if rs.Level != "high" {
		t.Errorf("Level = %q (score %.3f); want high — terms cone=%.2f coreness=%.2f churn=%.2f coupling=%.2f",
			rs.Level, rs.Score, rs.Cone, rs.Coreness, rs.Churn, rs.Coupling)
	}
	if rs.Score < 0 || rs.Score > 1 {
		t.Errorf("Score = %v; want ∈ [0,1]", rs.Score)
	}

	if !approxEqual(rs.Coreness, 1.0) {
		t.Errorf("Coreness term = %v; want 1.0 (Hub is the max-coreness node)", rs.Coreness)
	}

	if len(rs.TopAffected) == 0 {
		t.Error("TopAffected empty; want the impacted callers of Hub")
	}
	for _, ta := range rs.TopAffected {
		if ta == "pkg.Hub" {
			t.Error("TopAffected must not contain the seed symbol itself")
		}
	}
}

func TestBlastRadiusLowRiskLeaf(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 9)
	seedNode(t, s, "pkg.Leaf", "leaf.go", 0)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Leaf"}, []string{"leaf.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if rs.Level != "low" {
		t.Errorf("Level = %q (score %.3f); want low for an isolated leaf", rs.Level, rs.Score)
	}
	if rs.Cone != 0 {
		t.Errorf("Cone term = %v; want 0 (no callers)", rs.Cone)
	}
}

func TestBlastRadiusEmptyChangeIsZero(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(context.Background(), s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(), "proj", nil, nil)
	if err != nil {
		t.Fatalf("BlastRadius(empty): %v", err)
	}
	if rs.Score != 0 || rs.Level != "low" {
		t.Errorf("empty change = {score %v, level %q}; want {0, low}", rs.Score, rs.Level)
	}
}

func TestBlastRadiusInvalidWeightsRejected(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	bad := RiskWeights{Cone: 0.5, Coreness: 0.5, Churn: 0.5, Coupling: 0.5}
	_, err := BlastRadius(context.Background(), s, b, structure.Decomposition{},
		bad, DefaultRiskThresholds(), "proj", []string{"x"}, []string{"x.go"})
	if err == nil {
		t.Error("BlastRadius with sum=2.0 weights = nil error; want a validation rejection")
	}
}

func TestBlastRadiusScoreIsWeightedSum(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 5)
	seedNode(t, s, "pkg.Caller", "caller.go", 5)
	seedEdge(t, s, "pkg.Caller", "pkg.Hub")
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Hub"}, []string{"hub.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if !approxEqual(rs.Cone, 1.0) || !approxEqual(rs.Coreness, 1.0) {
		t.Fatalf("terms cone=%v coreness=%v; want both 1.0 (precondition for the sum check)", rs.Cone, rs.Coreness)
	}
	if rs.Churn != 0 || rs.Coupling != 0 {
		t.Fatalf("terms churn=%v coupling=%v; want both 0", rs.Churn, rs.Coupling)
	}
	want := 0.35*rs.Cone + 0.30*rs.Coreness + 0.20*rs.Churn + 0.15*rs.Coupling
	if !approxEqual(rs.Score, want) {
		t.Errorf("Score = %v; want weighted-sum %v", rs.Score, want)
	}
	if !approxEqual(rs.Score, 0.65) {
		t.Errorf("Score = %v; want 0.65 (0.35+0.30)", rs.Score)
	}
}

func TestBlastRadiusDeterminism(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 7)
	for i := 0; i < 4; i++ {
		id := "pkg.X" + string(rune('A'+i))
		seedNode(t, s, id, "x.go", 1)
		seedEdge(t, s, id, "pkg.Hub")
	}
	if err := s.UpsertChurn(ctx, store.Churn{Path: "hub.go", WindowDays: 90, TouchCount: 30, AuthorCount: 2, UpdatedAt: 1}); err != nil {
		t.Fatalf("seed churn: %v", err)
	}
	if err := s.UpsertCoChange(ctx, store.CoChange{FileA: "hub.go", FileB: "peer.go", SharedRevs: 5, RevsA: 10, RevsB: 10, WindowDays: 90, UpdatedAt: 1}); err != nil {
		t.Fatalf("seed cochange: %v", err)
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	call := func() RiskScore {
		rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
			DefaultRiskWeights(), DefaultRiskThresholds(),
			"proj", []string{"pkg.Hub"}, []string{"hub.go"})
		if err != nil {
			t.Fatalf("BlastRadius: %v", err)
		}
		return rs
	}
	a, bv := call(), call()
	if a.Score != bv.Score || a.Level != bv.Level ||
		a.Cone != bv.Cone || a.Coreness != bv.Coreness ||
		a.Churn != bv.Churn || a.Coupling != bv.Coupling {
		t.Errorf("non-deterministic score: call1=%+v call2=%+v", a, bv)
	}
	if len(a.TopAffected) != len(bv.TopAffected) {
		t.Fatalf("TopAffected length mismatch: %d vs %d", len(a.TopAffected), len(bv.TopAffected))
	}
	for i := range a.TopAffected {
		if a.TopAffected[i] != bv.TopAffected[i] {
			t.Errorf("TopAffected[%d]: %q vs %q", i, a.TopAffected[i], bv.TopAffected[i])
		}
	}
}

func newBrokenStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	s := newTestStore(t)
	return s, func() { _ = s.DB().Close() }
}

func TestBlastRadiusColdStartDegrades(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 5)
	for i := 0; i < 3; i++ {
		caller := "pkg.C" + string(rune('0'+i))
		seedNode(t, s, caller, "c.go", 1)
		seedEdge(t, s, caller, "pkg.Hub")
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Hub"}, []string{"hub.go"})
	if err != nil {
		t.Fatalf("BlastRadius cold-start = %v; want nil (degrade, not fail)", err)
	}
	if rs.Churn != 0 || rs.Coupling != 0 {
		t.Errorf("cold-start churn=%v coupling=%v; want both 0 (no history)", rs.Churn, rs.Coupling)
	}

	if rs.Score <= 0 {
		t.Errorf("cold-start score = %v; want > 0 (structural terms still score)", rs.Score)
	}
	if rs.Coreness <= 0 {
		t.Errorf("cold-start coreness term = %v; want > 0 (Hub has coreness)", rs.Coreness)
	}
}

func TestBlastRadiusSingleNodeGraphNoPanic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.Only", "only.go", 0)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Only"}, []string{"only.go"})
	if err != nil {
		t.Fatalf("BlastRadius(single node): %v", err)
	}
	if math.IsNaN(rs.Score) || math.IsInf(rs.Score, 0) {
		t.Errorf("Score = %v; want a finite number (guards must hold on degenerate graph)", rs.Score)
	}
	if rs.Level != "low" {
		t.Errorf("single-node Level = %q; want low", rs.Level)
	}
}

func TestBlastRadiusDecompositionFastPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedNode(t, s, "pkg.Hub", "hub.go", 3)
	seedNode(t, s, "pkg.Other", "o.go", 10)
	d := structure.Decomposition{Coreness: map[string]int{"pkg.Hub": 8}}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, d,
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Hub"}, []string{"hub.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}

	if !approxEqual(rs.Coreness, 0.8) {
		t.Errorf("Coreness term = %v; want 0.8 (max(store 3, decomp 8)/max_coreness 10)", rs.Coreness)
	}
}

func TestBlastRadiusInvalidThresholdsRejected(t *testing.T) {
	s := newTestStore(t)
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	inverted := RiskThresholds{MediumAt: 0.70, HighAt: 0.30}
	_, err := BlastRadius(context.Background(), s, b, structure.Decomposition{},
		DefaultRiskWeights(), inverted, "proj", []string{"x"}, []string{"x.go"})
	if err == nil {
		t.Error("BlastRadius with inverted thresholds = nil; want a validation rejection")
	}
}

func TestBlastRadiusTopAffectedCapped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seedNode(t, s, "pkg.Hub", "hub.go", 5)
	for i := 0; i < 25; i++ {
		caller := fmt.Sprintf("pkg.Caller%02d", i)
		seedNode(t, s, caller, "c.go", 1)
		seedEdge(t, s, caller, "pkg.Hub")
	}
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.Hub"}, []string{"hub.go"})
	if err != nil {
		t.Fatalf("BlastRadius: %v", err)
	}
	if len(rs.TopAffected) > maxTopAffected {
		t.Errorf("TopAffected length = %d; want ≤ %d (capped)", len(rs.TopAffected), maxTopAffected)
	}
	if len(rs.TopAffected) != maxTopAffected {
		t.Errorf("TopAffected length = %d; want exactly %d (25 callers → cap)", len(rs.TopAffected), maxTopAffected)
	}
}

func TestBlastRadiusStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()

	if err := s.UpsertNode(ctx, store.Node{
		NodeID: "pkg.X", Name: "X", Kind: string(store.KindFunction),
		Language: "go", FilePath: "x.go", ContentHash: "h",
	}); err != nil {
		t.Fatalf("seed before close: %v", err)
	}
	closeDB()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	rs, err := BlastRadius(ctx, s, b, structure.Decomposition{},
		DefaultRiskWeights(), DefaultRiskThresholds(),
		"proj", []string{"pkg.X"}, []string{"x.go"})
	if err == nil {
		t.Error("BlastRadius on closed store = nil error; want store-read error propagation")
	}

	if math.IsNaN(rs.Score) || math.IsInf(rs.Score, 0) {
		t.Errorf("error-path Score = %v; want finite zero (no NaN/Inf on error)", rs.Score)
	}
}

func TestReverseConeStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()
	if err := s.UpsertNode(ctx, store.Node{
		NodeID: "pkg.A", Name: "A", Kind: string(store.KindFunction),
		Language: "go", FilePath: "a.go", ContentHash: "h",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	closeDB()
	_, _, err := reverseCone(ctx, s, []string{"pkg.A"})
	if err == nil {
		t.Error("reverseCone on closed store = nil error; want ListEdgesByTarget error propagation")
	}
}

func TestEnumerateAllNodesStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	closeDB()
	_, err := enumerateAllNodes(context.Background(), s)
	if err == nil {
		t.Error("enumerateAllNodes on closed store = nil error; want ListNodesByKind error propagation")
	}
}

func TestMaxChurnNonErrNotFoundPropagates(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()

	if err := s.UpsertChurn(ctx, store.Churn{
		Path: "f.go", WindowDays: 90, TouchCount: 5, AuthorCount: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed churn: %v", err)
	}
	closeDB()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	_, err := maxChurn(ctx, b, []string{"f.go"}, 90)
	if err == nil {
		t.Error("maxChurn on closed store = nil error; want GetChurn error propagation")
	}
}

func TestChurnPopulationNonErrNotFoundPropagates(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()
	if err := s.UpsertNode(ctx, store.Node{
		NodeID: "pkg.F", Name: "F", Kind: string(store.KindFunction),
		Language: "go", FilePath: "f.go", ContentHash: "h",
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	if err := s.UpsertChurn(ctx, store.Churn{
		Path: "f.go", WindowDays: 90, TouchCount: 3, AuthorCount: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed churn: %v", err)
	}
	closeDB()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	_, err := churnPopulation(ctx, s, b, 90)
	if err == nil {
		t.Error("churnPopulation on closed store = nil error; want GetChurn error propagation")
	}
}

func TestP95ConeStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()

	if err := s.UpsertNode(ctx, store.Node{
		NodeID: "pkg.A", Name: "A", Kind: string(store.KindFunction),
		Language: "go", FilePath: "a.go", ContentHash: "h",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	closeDB()
	_, err := p95Cone(ctx, s)
	if err == nil {
		t.Error("p95Cone on closed store = nil error; want reverseCone error propagation")
	}
}

func TestMaxCorenessStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	closeDB()
	_, err := maxCoreness(context.Background(), s, structure.Decomposition{})
	if err == nil {
		t.Error("maxCoreness on closed store = nil error; want enumerateAllNodes error propagation")
	}
}

func TestCouplingFanoutStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	ctx := context.Background()

	if err := s.UpsertCoChange(ctx, store.CoChange{
		FileA: "a.go", FileB: "b.go", SharedRevs: 5, RevsA: 10, RevsB: 10,
		WindowDays: 90, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed cochange: %v", err)
	}
	closeDB()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	_, err := couplingFanout(ctx, b, []string{"a.go"}, 90)
	if err == nil {
		t.Error("couplingFanout on closed store = nil error; want ListCoupling error propagation")
	}
}

func TestP95FanoutStorePropagatesError(t *testing.T) {
	s, closeDB := newBrokenStore(t)
	closeDB()
	b := NewBuilder(s, fakeRunner{}, fixedParams{p: DefaultParams()})
	_, err := p95Fanout(context.Background(), s, b, 90)
	if err == nil {
		t.Error("p95Fanout on closed store = nil error; want enumerateAllNodes error propagation")
	}
}
