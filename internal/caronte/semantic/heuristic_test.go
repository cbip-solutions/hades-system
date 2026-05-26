package semantic

import (
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestHeuristicImplementsEdges(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "m.Renderer", Name: "Renderer", Kind: string(store.KindInterface), Language: "typescript"},
	}
	structs := []store.Node{
		{NodeID: "m.Widget", Name: "Widget", Kind: string(store.KindStruct), Language: "typescript"},
		{NodeID: "m.Stub", Name: "Stub", Kind: string(store.KindStruct), Language: "typescript"},
	}
	methods := []store.Node{
		{NodeID: "m.Renderer.render", Name: "render", Kind: string(store.KindField), Language: "typescript"},
		{NodeID: "m.Widget.render", Name: "render", Kind: string(store.KindMethod), Language: "typescript"},
		{NodeID: "m.Widget.extra", Name: "extra", Kind: string(store.KindMethod), Language: "typescript"},
		{NodeID: "m.Stub.other", Name: "other", Kind: string(store.KindMethod), Language: "typescript"},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	var linked bool
	for _, e := range edges {
		if !e.Confidence.Valid() || e.Confidence != store.ConfHeuristicName {
			t.Errorf("edge %s→%s confidence = %q; want heuristic_name", e.SourceID, e.TargetID, e.Confidence)
		}
		if e.Kind != string(store.EdgeImplements) {
			t.Errorf("edge kind = %q; want implements", e.Kind)
		}
		if e.SourceID == "m.Widget" && e.TargetID == "m.Renderer" {
			linked = true
		}
		if e.SourceID == "m.Stub" {
			t.Errorf("Stub (no render method) wrongly linked to an interface: %s→%s", e.SourceID, e.TargetID)
		}
	}
	if !linked {
		t.Error("Widget (covers Renderer.render) was not linked to Renderer")
	}
}

func TestHeuristicNoInterfaces(t *testing.T) {
	edges := heuristicImplementsEdges(nil, []store.Node{{NodeID: "m.W", Name: "W", Kind: string(store.KindStruct)}}, nil)
	if len(edges) != 0 {
		t.Errorf("heuristicImplementsEdges(no interfaces) = %d; want 0", len(edges))
	}
}

func TestHeuristicDeterministicOrder(t *testing.T) {
	ifaces := []store.Node{
		{NodeID: "m.A", Name: "A", Kind: string(store.KindInterface)},
		{NodeID: "m.B", Name: "B", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{{NodeID: "m.Impl", Name: "Impl", Kind: string(store.KindStruct)}}
	methods := []store.Node{
		{NodeID: "m.A.f", Name: "f", Kind: string(store.KindField)},
		{NodeID: "m.B.f", Name: "f", Kind: string(store.KindField)},
		{NodeID: "m.Impl.f", Name: "f", Kind: string(store.KindMethod)},
	}
	first := heuristicImplementsEdges(ifaces, structs, methods)
	second := heuristicImplementsEdges(ifaces, structs, methods)
	if len(first) != len(second) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("edge[%d] differs across runs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestHeuristicNoMatchEmitsNoEdge(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "m.Srv", Name: "Srv", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "m.Concrete", Name: "Concrete", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{

		{NodeID: "m.Srv.process", Name: "process", Kind: string(store.KindField)},
		{NodeID: "m.Concrete.handle", Name: "handle", Kind: string(store.KindMethod)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 0 {
		t.Errorf("heuristicImplementsEdges(no match) = %d edges; want 0 (no dangling)", len(edges))
		for _, e := range edges {
			t.Logf("  spurious edge: %s→%s", e.SourceID, e.TargetID)
		}
	}
}

// TestHeuristicConfidenceTier is the confidence-tier bite-check: edges MUST
// carry ConfHeuristicName. Swapping the tier in the implementation makes this
// test fail, proving the assertion is load-bearing. The assertion here mirrors
// what the multi-language resolver (E-8) depends on when filtering by tier.
func TestHeuristicConfidenceTier(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "pkg.I", Name: "I", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "pkg.S", Name: "S", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{
		{NodeID: "pkg.I.do", Name: "do", Kind: string(store.KindField)},
		{NodeID: "pkg.S.do", Name: "do", Kind: string(store.KindMethod)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge; got %d", len(edges))
	}
	if edges[0].Confidence != store.ConfHeuristicName {
		t.Errorf("Confidence = %q; want %q (ConfHeuristicName)", edges[0].Confidence, store.ConfHeuristicName)
	}
	if !edges[0].Confidence.Valid() {
		t.Errorf("Confidence %q is not Valid() — inv-zen-233 would reject this edge", edges[0].Confidence)
	}
}

func TestHeuristicRustDoubleColonSeparator(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "crate::traits::Runner", Name: "Runner", Kind: string(store.KindInterface), Language: "rust"},
	}
	structs := []store.Node{
		{NodeID: "crate::engine::Core", Name: "Core", Kind: string(store.KindStruct), Language: "rust"},
	}
	methods := []store.Node{
		{NodeID: "crate::traits::Runner::run", Name: "run", Kind: string(store.KindField), Language: "rust"},
		{NodeID: "crate::engine::Core::run", Name: "run", Kind: string(store.KindMethod), Language: "rust"},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 1 {
		t.Fatalf("Rust :: separator: want 1 edge; got %d", len(edges))
	}
	if edges[0].SourceID != "crate::engine::Core" || edges[0].TargetID != "crate::traits::Runner" {
		t.Errorf("wrong endpoints: got %s→%s", edges[0].SourceID, edges[0].TargetID)
	}
}

// TestHeuristicTopLevelMethodsSkipped asserts that method nodes with no owner
// (top-level symbols with no "." or "::" in their node_id) are silently skipped
// and do not cause a panic or spurious match. This exercises the
// ownerOfMember→"" → continue branch.
func TestHeuristicTopLevelMethodsSkipped(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "pkg.I", Name: "I", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "pkg.S", Name: "S", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{

		{NodeID: "topLevelFunc", Name: "topLevelFunc", Kind: string(store.KindMethod)},

		{NodeID: "pkg.I.do", Name: "do", Kind: string(store.KindField)},

		{NodeID: "pkg.S.do", Name: "do", Kind: string(store.KindMethod)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge despite top-level node; got %d", len(edges))
	}
}

func TestHeuristicInterfaceWithNoMembers(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "pkg.Empty", Name: "Empty", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "pkg.S", Name: "S", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{

		{NodeID: "pkg.S.do", Name: "do", Kind: string(store.KindMethod)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 0 {
		t.Errorf("interface with no members should yield 0 edges; got %d", len(edges))
	}
}

func TestHeuristicStructWithNoMethods(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "pkg.I", Name: "I", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "pkg.NoMethods", Name: "NoMethods", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{
		{NodeID: "pkg.I.req", Name: "req", Kind: string(store.KindField)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 0 {
		t.Errorf("struct with no methods should yield 0 edges; got %d", len(edges))
	}
}

func TestHeuristicMultipleMethodsOnOwner(t *testing.T) {
	interfaces := []store.Node{
		{NodeID: "pkg.I", Name: "I", Kind: string(store.KindInterface)},
	}
	structs := []store.Node{
		{NodeID: "pkg.S", Name: "S", Kind: string(store.KindStruct)},
	}
	methods := []store.Node{
		{NodeID: "pkg.I.a", Name: "a", Kind: string(store.KindField)},
		{NodeID: "pkg.I.b", Name: "b", Kind: string(store.KindField)},

		{NodeID: "pkg.S.a", Name: "a", Kind: string(store.KindMethod)},
		{NodeID: "pkg.S.b", Name: "b", Kind: string(store.KindMethod)},
	}
	edges := heuristicImplementsEdges(interfaces, structs, methods)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge (S covers I via both a+b); got %d", len(edges))
	}
	if edges[0].SourceID != "pkg.S" || edges[0].TargetID != "pkg.I" {
		t.Errorf("wrong endpoints: %s→%s", edges[0].SourceID, edges[0].TargetID)
	}
}

func TestOwnerOfMember(t *testing.T) {
	cases := []struct {
		nodeID string
		want   string
	}{
		{"m.Widget.render", "m.Widget"},
		{"crate::m::Widget::render", "crate::m::Widget"},
		{"topLevel", ""},
		{"a.b.c.d", "a.b.c"},
	}
	for _, c := range cases {
		got := ownerOfMember(c.nodeID)
		if got != c.want {
			t.Errorf("ownerOfMember(%q) = %q; want %q", c.nodeID, got, c.want)
		}
	}
}
