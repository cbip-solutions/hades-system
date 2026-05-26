// SPDX-License-Identifier: MIT
package structure

import (
	"sort"

	"gonum.org/v1/gonum/graph"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// topoSCC runs gonum's TarjanSCC over the directed graph and returns the
// strongly-connected components. Wrapped here so callers/tests do not depend
// on the topo import directly.
//
// gonum: func TarjanSCC(g graph.Directed) [][]graph.Node. The returned order
// is gonum's (reverse-topological of the condensation); we do NOT depend on
// it — sccByNode re-labels deterministically.
func topoSCC(d *simple.DirectedGraph) [][]graph.Node {
	return topo.TarjanSCC(d)
}

func sccByNode(sccs [][]graph.Node, m *idMap) (map[string]int, map[int]int) {

	type comp struct {
		members []string
		key     string
	}
	comps := make([]comp, 0, len(sccs))
	for _, scc := range sccs {
		members := make([]string, 0, len(scc))
		for _, gnode := range scc {
			if name := m.name(gnode.ID()); name != "" {
				members = append(members, name)
			}
		}
		if len(members) == 0 {
			continue
		}
		sort.Strings(members)
		comps = append(comps, comp{members: members, key: members[0]})
	}

	sort.Slice(comps, func(i, j int) bool { return comps[i].key < comps[j].key })
	sccID := make(map[string]int, len(m.sortedNames()))
	size := make(map[int]int, len(comps))
	for id, c := range comps {
		size[id] = len(c.members)
		for _, name := range c.members {
			sccID[name] = id
		}
	}
	return sccID, size
}

func condensationLayers(d *simple.DirectedGraph, sccID map[string]int, m *idMap, sccCount int) [][]int {

	adj := make(map[int]map[int]struct{}, sccCount)
	indeg := make(map[int]int, sccCount)
	for i := 0; i < sccCount; i++ {
		adj[i] = map[int]struct{}{}
		indeg[i] = 0
	}
	for _, name := range m.sortedNames() {
		from := m.id(name)
		su := sccID[name]
		it := d.From(from)
		for it.Next() {
			tv := m.name(it.Node().ID())
			if tv == "" {
				continue
			}
			sv := sccID[tv]
			if su == sv {
				continue
			}
			if _, ok := adj[su][sv]; !ok {
				adj[su][sv] = struct{}{}
				indeg[sv]++
			}
		}
	}

	var layers [][]int
	remaining := sccCount
	done := make(map[int]bool, sccCount)
	for remaining > 0 {
		var layer []int
		for i := 0; i < sccCount; i++ {
			if !done[i] && indeg[i] == 0 {
				layer = append(layer, i)
			}
		}
		if len(layer) == 0 {
			break
		}
		sort.Ints(layer)
		for _, u := range layer {
			done[u] = true
			remaining--
			for v := range adj[u] {
				indeg[v]--
			}
		}
		layers = append(layers, layer)
	}
	return layers
}
