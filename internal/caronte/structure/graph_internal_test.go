package structure

import (
	"testing"

	"gonum.org/v1/gonum/graph"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func node(id, file string) store.Node {
	return store.Node{NodeID: id, Name: id, Kind: string(store.KindFunction), Language: "go", FilePath: file, ContentHash: "h"}
}

func edge(src, tgt string) store.Edge {
	return store.Edge{SourceID: src, TargetID: tgt, Kind: string(store.EdgeCalls), Confidence: store.ConfExactStatic, SiteFile: "x.go", SiteLine: 1}
}

func TestIDMapIsDeterministicBijection(t *testing.T) {
	nodes := []store.Node{node("pkg/z.C", "z.go"), node("pkg/a.A", "a.go"), node("pkg/a.B", "a.go")}
	m := newIDMap(nodes)

	want := map[string]int64{"pkg/a.A": 0, "pkg/a.B": 1, "pkg/z.C": 2}
	for name, id := range want {
		if got := m.id(name); got != id {
			t.Errorf("id(%q) = %d; want %d", name, got, id)
		}
		if got := m.name(id); got != name {
			t.Errorf("name(%d) = %q; want %q", id, got, name)
		}
	}

	m2 := newIDMap([]store.Node{node("pkg/a.B", "a.go"), node("pkg/z.C", "z.go"), node("pkg/a.A", "a.go")})
	for name, id := range want {
		if got := m2.id(name); got != id {
			t.Errorf("re-ordered build: id(%q) = %d; want %d (must be order-independent)", name, got, id)
		}
	}
	if got := m.sortedNames(); len(got) != 3 || got[0] != "pkg/a.A" || got[2] != "pkg/z.C" {
		t.Errorf("sortedNames() = %v; want [pkg/a.A pkg/a.B pkg/z.C]", got)
	}
}

func TestBuildDirectedSkipsSelfLoops(t *testing.T) {
	nodes := []store.Node{node("pkg/x.Rec", "x.go"), node("pkg/x.Other", "x.go")}
	edges := []store.Edge{
		edge("pkg/x.Rec", "pkg/x.Rec"),
		edge("pkg/x.Rec", "pkg/x.Other"),
	}
	m := newIDMap(nodes)
	g := buildDirected(nodes, edges, m)
	if g.Edge(m.id("pkg/x.Rec"), m.id("pkg/x.Other")) == nil {
		t.Error("non-self edge Rec→Other missing from directed graph")
	}
	if g.Node(m.id("pkg/x.Rec")) == nil || g.Node(m.id("pkg/x.Other")) == nil {
		t.Error("both nodes must be present (AddNode before SetEdge)")
	}
}

func TestBuildDirectedAddsIsolatedNodes(t *testing.T) {
	nodes := []store.Node{node("pkg/x.Lonely", "x.go")}
	g := buildDirected(nodes, nil, newIDMap(nodes))
	if g.Node(newIDMap(nodes).id("pkg/x.Lonely")) == nil {
		t.Error("isolated node missing from graph")
	}
}

func TestBuildDirectedDeduplicatesParallelEdges(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go")}
	e1 := edge("pkg/x.A", "pkg/x.B")
	e2 := edge("pkg/x.A", "pkg/x.B")
	e2.SiteLine = 99
	g := buildDirected(nodes, []store.Edge{e1, e2}, newIDMap(nodes))
	m := newIDMap(nodes)
	if g.Edge(m.id("pkg/x.A"), m.id("pkg/x.B")) == nil {
		t.Error("collapsed A→B edge missing")
	}
}

func TestBuildUndirectedProjection(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go")}
	m := newIDMap(nodes)
	d := buildDirected(nodes, []store.Edge{edge("pkg/x.A", "pkg/x.B")}, m)
	u := buildUndirected(d, m)
	if u.Edge(m.id("pkg/x.A"), m.id("pkg/x.B")) == nil && u.Edge(m.id("pkg/x.B"), m.id("pkg/x.A")) == nil {
		t.Error("undirected projection missing the {A,B} edge")
	}
}

func TestHashKeyStableAndSensitive(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "a.go"), node("pkg/x.B", "b.go")}
	edges := []store.Edge{edge("pkg/x.A", "pkg/x.B")}
	h1 := HashKey(nodes, edges)

	h2 := HashKey([]store.Node{nodes[1], nodes[0]}, edges)
	if h1 != h2 {
		t.Errorf("HashKey not order-independent: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("HashKey len = %d; want 64 (hex sha256)", len(h1))
	}

	h3 := HashKey(nodes, append([]store.Edge{}, edge("pkg/x.B", "pkg/x.A")))
	if h3 == h1 {
		t.Error("HashKey unchanged after edge-set change")
	}

	h4 := HashKey(nodes[:1], nil)
	if h4 == h1 {
		t.Error("HashKey unchanged after node-set change")
	}
}

func TestHashKeyIgnoresNonStructuralFields(t *testing.T) {
	a := node("pkg/x.A", "a.go")
	b := node("pkg/x.B", "b.go")
	edges := []store.Edge{edge("pkg/x.A", "pkg/x.B")}
	h1 := HashKey([]store.Node{a, b}, edges)
	a.Doc = "changed doc"
	a.Signature = "func A(x int)"
	a.ContentHash = "different"
	h2 := HashKey([]store.Node{a, b}, edges)
	if h1 != h2 {
		t.Error("HashKey changed on a non-structural (doc/signature/hash) edit; must depend only on topology")
	}
}

func TestCorenessTriangleVsTail(t *testing.T) {
	nodes := []store.Node{
		node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go"),
		node("pkg/x.C", "x.go"), node("pkg/x.D", "x.go"),
	}

	edges := []store.Edge{
		edge("pkg/x.A", "pkg/x.B"), edge("pkg/x.B", "pkg/x.C"),
		edge("pkg/x.C", "pkg/x.A"), edge("pkg/x.A", "pkg/x.D"),
	}
	m := newIDMap(nodes)
	d := buildDirected(nodes, edges, m)
	u := buildUndirected(d, m)
	cor := corenessByNode(degeneracyCores(u), m)
	for _, n := range []string{"pkg/x.A", "pkg/x.B", "pkg/x.C"} {
		if cor[n] != 2 {
			t.Errorf("coreness[%s] = %d; want 2 (triangle member)", n, cor[n])
		}
	}
	if cor["pkg/x.D"] != 1 {
		t.Errorf("coreness[pkg/x.D] = %d; want 1 (pendant)", cor["pkg/x.D"])
	}
}

func TestCorenessIsolatedNodeIsZero(t *testing.T) {
	nodes := []store.Node{node("pkg/x.Lonely", "x.go")}
	m := newIDMap(nodes)
	d := buildDirected(nodes, nil, m)
	u := buildUndirected(d, m)
	cor := corenessByNode(degeneracyCores(u), m)
	if v, ok := cor["pkg/x.Lonely"]; !ok || v != 0 {
		t.Errorf("coreness[Lonely] = %d (present=%v); want 0, present", v, ok)
	}
}

func TestCorenessByNodeMaxKIdiom(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go")}
	m := newIDMap(nodes)

	cores := [][]graphNode{
		{gn(m.id("pkg/x.A")), gn(m.id("pkg/x.B"))},
		{gn(m.id("pkg/x.A"))},
		{gn(m.id("pkg/x.A"))},
	}
	cor := corenessByNode(toGonum(cores), m)
	if cor["pkg/x.A"] != 2 {
		t.Errorf("coreness[A] = %d; want 2 (max k it appears in)", cor["pkg/x.A"])
	}
	if cor["pkg/x.B"] != 0 {
		t.Errorf("coreness[B] = %d; want 0 (only in cores[0])", cor["pkg/x.B"])
	}
}

func TestSCCSeparatesCycleFromAcyclic(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go"), node("pkg/x.C", "x.go")}
	edges := []store.Edge{
		edge("pkg/x.A", "pkg/x.B"), edge("pkg/x.B", "pkg/x.A"),
		edge("pkg/x.A", "pkg/x.C"),
	}
	m := newIDMap(nodes)
	d := buildDirected(nodes, edges, m)
	sccID, size := sccByNode(topoSCC(d), m)
	if sccID["pkg/x.A"] != sccID["pkg/x.B"] {
		t.Errorf("A and B must share an scc_id (mutual cycle): %d vs %d", sccID["pkg/x.A"], sccID["pkg/x.B"])
	}
	if sccID["pkg/x.C"] == sccID["pkg/x.A"] {
		t.Error("C must NOT share the cycle's scc_id")
	}
	if size[sccID["pkg/x.A"]] != 2 {
		t.Errorf("cycle SCC size = %d; want 2", size[sccID["pkg/x.A"]])
	}
	if size[sccID["pkg/x.C"]] != 1 {
		t.Errorf("singleton SCC size = %d; want 1", size[sccID["pkg/x.C"]])
	}
}

func TestSCCLabellingIsDeterministic(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go")}
	m := newIDMap(nodes)
	d := buildDirected(nodes, nil, m)
	sccID, _ := sccByNode(topoSCC(d), m)

	if sccID["pkg/x.A"] != 0 {
		t.Errorf("scc_id[A] = %d; want 0 (smallest-member rank)", sccID["pkg/x.A"])
	}
	if sccID["pkg/x.B"] != 1 {
		t.Errorf("scc_id[B] = %d; want 1", sccID["pkg/x.B"])
	}
}

func TestIsCyclicViaSize(t *testing.T) {
	nodes := []store.Node{node("pkg/x.A", "x.go"), node("pkg/x.B", "x.go"), node("pkg/x.C", "x.go")}
	edges := []store.Edge{edge("pkg/x.A", "pkg/x.B"), edge("pkg/x.B", "pkg/x.C"), edge("pkg/x.C", "pkg/x.A")}
	m := newIDMap(nodes)
	d := buildDirected(nodes, edges, m)
	_, size := sccByNode(topoSCC(d), m)
	var any3 bool
	for _, s := range size {
		if s == 3 {
			any3 = true
		}
	}
	if !any3 {
		t.Errorf("3-cycle must yield an SCC of size 3; sizes=%v", size)
	}
}

func TestPackageIDFromFilePath(t *testing.T) {
	cases := []struct {
		node store.Node
		want string
	}{
		{store.Node{NodeID: "a", FilePath: "internal/caronte/store/store.go", Language: "go"}, "internal/caronte/store"},
		{store.Node{NodeID: "b", FilePath: "internal/caronte/store/types.go", Language: "go"}, "internal/caronte/store"},
		{store.Node{NodeID: "c", FilePath: "cmd/zen/main.go", Language: "go"}, "cmd/zen"},
		{store.Node{NodeID: "d", FilePath: "main.go", Language: "go"}, "."},
	}
	for _, c := range cases {
		if got := packageID(c.node); got != c.want {
			t.Errorf("packageID(%q) = %q; want %q", c.node.FilePath, got, c.want)
		}
	}
}

func TestPackageIDPrefersExplicit(t *testing.T) {
	n := store.Node{NodeID: "e", FilePath: "internal/x/x.go", Language: "go", PackageID: "github.com/cbip-solutions/hades-system/internal/x"}
	if got := packageID(n); got != "github.com/cbip-solutions/hades-system/internal/x" {
		t.Errorf("packageID preserved = %q; want the explicit fully-qualified package", got)
	}
}

func TestPackageIDForwardSlashNormalization(t *testing.T) {
	n := store.Node{NodeID: "f", FilePath: `internal\caronte\store\x.go`, Language: "go"}
	if got := packageID(n); got != "internal/caronte/store" {
		t.Errorf("packageID(backslash) = %q; want internal/caronte/store", got)
	}
}

type graphNode int64

func (g graphNode) ID() int64 { return int64(g) }
func gn(id int64) graphNode   { return graphNode(id) }

func toGonum(in [][]graphNode) [][]graph.Node {
	out := make([][]graph.Node, len(in))
	for i, layer := range in {
		row := make([]graph.Node, len(layer))
		for j, n := range layer {
			row[j] = n
		}
		out[i] = row
	}
	return out
}
