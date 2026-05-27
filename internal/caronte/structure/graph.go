// SPDX-License-Identifier: MIT
// Package structure is Caronte's L3 layer: the deterministic architectural
// decomposition of the call graph — k-core coreness (DegeneracyOrdering over
// the undirected projection), Tarjan SCC (cycles + condensation), and
// package/directory labels. 100% deterministic by design: the same
// (node-set, edge-set) yields byte-identical coreness/scc_id/package_id, the
// invariant contract asserted by the determinism golden. Only deterministic
// graph algorithms are used — no stochastic community detection.
//
// Boundary this package reads/writes the graph ONLY through the injected
// *store.Store and NEVER imports internal/store
// . It performs no I/O of its own beyond the
// store calls, no LLM, no network, no embeddings — pure CPU graph logic.
package structure

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func structuralEdgeKinds() []store.EdgeKind {
	return []store.EdgeKind{store.EdgeCalls, store.EdgeInvoke, store.EdgeEmbeds}
}

type idMap struct {
	toID   map[string]int64
	toName []string
}

func newIDMap(nodes []store.Node) *idMap {
	names := make([]string, 0, len(nodes))
	seen := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if _, dup := seen[n.NodeID]; dup {
			continue
		}
		seen[n.NodeID] = struct{}{}
		names = append(names, n.NodeID)
	}
	sort.Strings(names)
	m := &idMap{toID: make(map[string]int64, len(names)), toName: names}
	for i, name := range names {
		m.toID[name] = int64(i)
	}
	return m
}

func (m *idMap) id(nodeID string) int64 {
	if id, ok := m.toID[nodeID]; ok {
		return id
	}
	return -1
}

func (m *idMap) name(id int64) string {
	if id < 0 || int(id) >= len(m.toName) {
		return ""
	}
	return m.toName[id]
}

func (m *idMap) sortedNames() []string { return m.toName }

func buildDirected(nodes []store.Node, edges []store.Edge, m *idMap) *simple.DirectedGraph {
	g := simple.NewDirectedGraph()
	for _, name := range m.sortedNames() {
		g.AddNode(simple.Node(m.id(name)))
	}
	_ = nodes
	for _, e := range edges {
		s, t := m.id(e.SourceID), m.id(e.TargetID)
		if s < 0 || t < 0 || s == t {
			continue
		}
		g.SetEdge(simple.Edge{F: simple.Node(s), T: simple.Node(t)})
	}
	return g
}

func buildUndirected(d *simple.DirectedGraph, m *idMap) *simple.UndirectedGraph {
	u := simple.NewUndirectedGraph()
	for _, name := range m.sortedNames() {
		u.AddNode(simple.Node(m.id(name)))
	}

	for _, name := range m.sortedNames() {
		from := m.id(name)
		var succ []int64
		it := d.From(from)
		for it.Next() {
			succ = append(succ, it.Node().ID())
		}
		sort.Slice(succ, func(i, j int) bool { return succ[i] < succ[j] })
		for _, to := range succ {
			if u.Edge(from, to) == nil {
				u.SetEdge(simple.Edge{F: simple.Node(from), T: simple.Node(to)})
			}
		}
	}
	return u
}

func canonicalSerialization(nodes []store.Node, edges []store.Edge) []byte {
	m := newIDMap(nodes)
	var b []byte
	b = append(b, 'N')
	for _, name := range m.sortedNames() {
		b = append(b, '\n')
		b = append(b, name...)
	}

	structural := map[store.EdgeKind]bool{}
	for _, k := range structuralEdgeKinds() {
		structural[k] = true
	}
	seen := map[string]struct{}{}
	var lines []string
	for _, e := range edges {
		if !structural[store.EdgeKind(e.Kind)] {
			continue
		}
		if m.id(e.SourceID) < 0 || m.id(e.TargetID) < 0 || e.SourceID == e.TargetID {
			continue
		}
		line := e.SourceID + "\t" + e.TargetID
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	b = append(b, '\n', 'E')
	for _, line := range lines {
		b = append(b, '\n')
		b = append(b, line...)
	}
	b = append(b, '\n', 'C')
	b = append(b, []byte(strconv.Itoa(len(lines)))...)
	return b
}

func HashKey(nodes []store.Node, edges []store.Edge) string {
	sum := sha256.Sum256(canonicalSerialization(nodes, edges))
	return hex.EncodeToString(sum[:])
}

var _ graph.Node = simple.Node(0)
