package semantic

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"golang.org/x/tools/go/packages"
)

func TestBuildCHACallGraphOverBroken(t *testing.T) {
	res, err := loadGoPackages(context.Background(), "testdata/broken")
	if err != nil {
		t.Fatalf("loadGoPackages(broken): %v", err)
	}
	in, err := buildSSA(res.Packages)
	if err != nil {
		t.Fatalf("buildSSA(broken): %v", err)
	}
	g := buildCHACallGraph(in)
	if g == nil {
		t.Fatal("buildCHACallGraph returned nil over broken fixture")
	}
	edges := callEdgesFromCHA(g, in.modulePrefix)

	found := false
	for _, e := range edges {
		if e.SourceID == "Caller" && e.TargetID == "Callee" {
			found = true
		}
	}
	if !found {
		t.Error("CHA missed Caller→Callee in the build-broken fixture (must over-approximate, not fail)")
	}
}

func TestCHAEdgesAreExactCHAWithNullReachable(t *testing.T) {
	res, _ := loadGoPackages(context.Background(), "testdata/buildable")
	in, _ := buildSSA(res.Packages)
	edges := callEdgesFromCHA(buildCHACallGraph(in), in.modulePrefix)
	if len(edges) == 0 {
		t.Fatal("no CHA edges produced")
	}
	for _, e := range edges {
		if e.Confidence != store.ConfExactCHA {
			t.Errorf("CHA edge %s→%s confidence = %q; want exact_cha", e.SourceID, e.TargetID, e.Confidence)
		}
		if !e.Confidence.Valid() {
			t.Errorf("CHA edge confidence !Valid (inv-zen-233): %s→%s", e.SourceID, e.TargetID)
		}
		if e.Reachable != nil {
			t.Errorf("CHA edge %s→%s Reachable must be nil (NULL); got %v", e.SourceID, e.TargetID, *e.Reachable)
		}
	}
}

func TestInterfaceImplementsEdges(t *testing.T) {
	res, _ := loadGoPackages(context.Background(), "testdata/buildable")
	in, _ := buildSSA(res.Packages)
	reach := reachableNodeIDs(in)
	edges := interfaceImplementsEdges(res.Packages, store.ConfExactVTA, reach)
	gotCircle, gotSquare := false, false
	for _, e := range edges {
		if e.Kind != string(store.EdgeImplements) {
			t.Errorf("fan-out edge kind = %q; want implements", e.Kind)
		}

		if e.SourceID != "Shape" {
			continue
		}
		switch e.TargetID {
		case "Circle":
			gotCircle = true
		case "Square":
			gotSquare = true
		}
	}
	if !gotCircle || !gotSquare {
		t.Errorf("Implements fan-out missed an impl: circle=%v square=%v", gotCircle, gotSquare)
	}
}

func TestBuildCHACallGraphNilInput(t *testing.T) {
	if g := buildCHACallGraph(nil); g != nil {
		t.Errorf("buildCHACallGraph(nil) = %v; want nil", g)
	}
	if g := buildCHACallGraph(&vtaInputs{}); g != nil {
		t.Errorf("buildCHACallGraph(vtaInputs{prog:nil}) = %v; want nil", g)
	}
}

func TestReachableNodeIDsNil(t *testing.T) {
	if got := reachableNodeIDs(nil); got != nil {
		t.Errorf("reachableNodeIDs(nil) = %v; want nil (CHA sentinel)", got)
	}
}

func TestInterfaceImplementsEdgesNilReachSet(t *testing.T) {
	res, _ := loadGoPackages(context.Background(), "testdata/buildable")
	edges := interfaceImplementsEdges(res.Packages, store.ConfExactCHA, nil)
	if len(edges) == 0 {
		t.Fatal("nil-reachSet interfaceImplementsEdges returned no edges")
	}
	for _, e := range edges {
		if e.Reachable != nil {
			t.Errorf("nil-reachSet edge %s→%s Reachable must be nil; got %v",
				e.SourceID, e.TargetID, *e.Reachable)
		}
		if e.Confidence != store.ConfExactCHA {
			t.Errorf("nil-reachSet edge %s→%s confidence = %q; want exact_cha",
				e.SourceID, e.TargetID, e.Confidence)
		}
	}
}

func TestInterfaceImplementsEdgesEmptyPackages(t *testing.T) {
	edges := interfaceImplementsEdges([]*packages.Package{}, store.ConfExactVTA, nil)
	if len(edges) != 0 {
		t.Errorf("interfaceImplementsEdges(empty) len = %d; want 0", len(edges))
	}
}

func TestImplementsEdgesValidConfidence(t *testing.T) {
	res, _ := loadGoPackages(context.Background(), "testdata/buildable")
	in, _ := buildSSA(res.Packages)
	a := interfaceImplementsEdges(res.Packages, store.ConfExactVTA, reachableNodeIDs(in))
	b := interfaceImplementsEdges(res.Packages, store.ConfExactVTA, reachableNodeIDs(in))
	if len(a) != len(b) {
		t.Fatalf("non-deterministic implements edge count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !edgesValueEqual(a[i], b[i]) {
			t.Errorf("implements edge[%d] differs across runs", i)
		}
		if !a[i].Confidence.Valid() {
			t.Errorf("implements edge %s→%s !Valid", a[i].SourceID, a[i].TargetID)
		}
	}
}
