// SPDX-License-Identifier: MIT
package structure

import (
	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// degeneracyCores runs gonum's DegeneracyOrdering over the undirected
// projection and returns ONLY the k-core layering (the `cores` return). The
// degeneracy `order` is discarded (this layer needs coreness, not the
// coloring order). Wrapped here so callers (and tests) do not depend on the
// topo import directly.
//
// gonum: func DegeneracyOrdering(g graph.Undirected) (order []graph.Node,
// cores [][]graph.Node). cores is the k-core layering; a node's coreness is
// the largest index k for which the node appears in cores[k] (see
// corenessByNode).
func degeneracyCores(u *simple.UndirectedGraph) [][]graph.Node {
	_, cores := topo.DegeneracyOrdering(u)
	return cores
}

func corenessByNode(cores [][]graph.Node, m *idMap) map[string]int {
	out := make(map[string]int, len(m.sortedNames()))
	for _, name := range m.sortedNames() {
		out[name] = 0
	}
	for k, layer := range cores {
		for _, gnode := range layer {
			name := m.name(gnode.ID())
			if name == "" {
				continue
			}
			if k > out[name] {
				out[name] = k
			}
		}
	}
	return out
}
