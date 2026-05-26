package semantic

import (
	"context"
	"fmt"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
)

func loadAndBuildSSA(t *testing.T) (*vtaInputs, *loadResult) {
	t.Helper()
	res, err := loadGoPackages(context.Background(), "testdata/buildable")
	if err != nil {
		t.Fatalf("loadGoPackages: %v", err)
	}
	in, err := buildSSA(res.Packages)
	if err != nil {
		t.Fatalf("buildSSA: %v", err)
	}
	return in, &res
}

func TestBuildVTACallGraphReachesArea(t *testing.T) {
	in, _ := loadAndBuildSSA(t)
	g := buildVTACallGraph(in)
	if g == nil {
		t.Fatal("buildVTACallGraph returned nil")
	}
	edges := callEdgesFromGraph(g, in.modulePrefix)

	gotCircle, gotSquare, gotHelper := false, false, false
	for _, e := range edges {
		switch e.TargetID {
		case "Circle.Area":
			gotCircle = true
		case "Square.Area":
			gotSquare = true
		case "helper":
			gotHelper = true
		}
	}
	if !gotCircle || !gotSquare {
		t.Errorf("VTA did not resolve both Area impls: circle=%v square=%v", gotCircle, gotSquare)
	}
	if !gotHelper {
		t.Error("VTA missed the direct static call TotalArea→helper")
	}
}

func TestVTAEdgesAreExactVTAAndReachable(t *testing.T) {
	in, _ := loadAndBuildSSA(t)
	edges := callEdgesFromGraph(buildVTACallGraph(in), in.modulePrefix)
	if len(edges) == 0 {
		t.Fatal("no VTA edges produced")
	}
	for _, e := range edges {
		if e.Confidence != store.ConfExactVTA {
			t.Errorf("edge %s→%s confidence = %q; want exact_vta", e.SourceID, e.TargetID, e.Confidence)
		}
		if !e.Confidence.Valid() {
			t.Errorf("edge %s→%s confidence !Valid (inv-zen-233)", e.SourceID, e.TargetID)
		}
		if e.Reachable == nil || *e.Reachable != true {
			t.Errorf("VTA edge %s→%s Reachable must be &true; got %v", e.SourceID, e.TargetID, e.Reachable)
		}
	}
}

func TestVTAInvokeVsCallKind(t *testing.T) {
	in, _ := loadAndBuildSSA(t)
	edges := callEdgesFromGraph(buildVTACallGraph(in), in.modulePrefix)
	for _, e := range edges {
		if e.TargetID == "helper" && e.Kind != string(store.EdgeCalls) {
			t.Errorf("direct call to helper kind = %q; want calls", e.Kind)
		}
		if (e.TargetID == "Circle.Area" || e.TargetID == "Square.Area") &&
			e.Kind != string(store.EdgeInvoke) {
			t.Errorf("interface dispatch to %s kind = %q; want invoke", e.TargetID, e.Kind)
		}
	}
}

func edgesValueEqual(a, b store.Edge) bool {
	if a.SourceID != b.SourceID || a.TargetID != b.TargetID ||
		a.Kind != b.Kind || a.Confidence != b.Confidence ||
		a.SiteFile != b.SiteFile || a.SiteLine != b.SiteLine {
		return false
	}
	switch {
	case a.Reachable == nil && b.Reachable == nil:
		return true
	case a.Reachable == nil || b.Reachable == nil:
		return false
	default:
		return *a.Reachable == *b.Reachable
	}
}

func TestCallEdgesDeterministic(t *testing.T) {
	in, _ := loadAndBuildSSA(t)
	g := buildVTACallGraph(in)
	a := callEdgesFromGraph(g, in.modulePrefix)
	b := callEdgesFromGraph(g, in.modulePrefix)
	if len(a) != len(b) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !edgesValueEqual(a[i], b[i]) {
			t.Errorf("edge[%d] differs across runs:\n %+v\n %+v", i, a[i], b[i])
		}
	}
}

func TestBuildSSAEmptyPackages(t *testing.T) {
	_, err := buildSSA(nil)
	if err == nil {
		t.Fatal("buildSSA(nil) returned nil error; want error for empty package list")
	}
}

func TestBuildVTACallGraphNilInput(t *testing.T) {
	if g := buildVTACallGraph(nil); g != nil {
		t.Errorf("buildVTACallGraph(nil) = %v; want nil", g)
	}
}

func TestCallEdgesFromGraphNilGraph(t *testing.T) {
	edges := callEdgesFromGraph(nil, "")
	if len(edges) != 0 {
		t.Errorf("callEdgesFromGraph(nil) len = %d; want 0", len(edges))
	}
}

func TestMergeCallGraphEdgesNilInputs(t *testing.T) {
	in, _ := loadAndBuildSSA(t)
	real := buildVTACallGraph(in)

	mergeCallGraphEdges(nil, real, in.funcs)

	mergeCallGraphEdges(real, nil, in.funcs)
}

func TestResolveSyntheticCalleeNotInGraph(t *testing.T) {
	in, _ := loadAndBuildSSA(t)

	var synth *ssa.Function
	for fn := range in.funcs {
		if fn.Synthetic != "" {
			synth = fn
			break
		}
	}
	if synth == nil {
		t.Skip("no synthetic function found in buildable fixture (test requires one)")
	}

	emptyGraph := &callgraph.Graph{Nodes: make(map[*ssa.Function]*callgraph.Node)}
	got := resolveSyntheticCallee(emptyGraph, synth)
	if got != nil {
		t.Errorf("resolveSyntheticCallee(emptyGraph, synth) = %v; want nil (not in graph)", got)
	}
}

func TestSiteFileLineNilSite(t *testing.T) {
	e := &callgraph.Edge{}
	file, line := siteFileLine(nil, e)
	if file != "" || line != 0 {
		t.Errorf("siteFileLine(nil site) = (%q, %d); want (\"\", 0)", file, line)
	}
}

func TestSortEdgesMultiKey(t *testing.T) {
	reachTrue := boolPtrTrue()
	edges := []store.Edge{
		{SourceID: "B", TargetID: "X", Kind: "calls", SiteLine: 1, Confidence: store.ConfExactVTA, Reachable: reachTrue},
		{SourceID: "A", TargetID: "Z", Kind: "calls", SiteLine: 1, Confidence: store.ConfExactVTA, Reachable: reachTrue},
		{SourceID: "A", TargetID: "X", Kind: "invoke", SiteLine: 5, Confidence: store.ConfExactVTA, Reachable: reachTrue},
		{SourceID: "A", TargetID: "X", Kind: "calls", SiteLine: 10, Confidence: store.ConfExactVTA, Reachable: reachTrue},
		{SourceID: "A", TargetID: "X", Kind: "calls", SiteLine: 3, Confidence: store.ConfExactVTA, Reachable: reachTrue},
	}
	sortEdges(edges)
	wantOrder := []string{
		"A/X/calls/3",
		"A/X/calls/10",
		"A/X/invoke/5",
		"A/Z/calls/1",
		"B/X/calls/1",
	}
	for i, e := range edges {
		got := e.SourceID + "/" + e.TargetID + "/" + e.Kind + "/" + fmt.Sprintf("%d", e.SiteLine)
		if got != wantOrder[i] {
			t.Errorf("sortEdges[%d] = %q; want %q", i, got, wantOrder[i])
		}
	}
}
