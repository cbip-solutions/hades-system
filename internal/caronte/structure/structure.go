// SPDX-License-Identifier: MIT
package structure

import (
	"context"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type Decomposition struct {
	Coreness  map[string]int
	SCCID     map[string]int
	PackageID map[string]string
	SCCSize   map[int]int
	Layers    [][]int
	HashKey   string
}

func (d Decomposition) CorenessOf(nodeID string) int { return d.Coreness[nodeID] }

func (d Decomposition) SCCOf(nodeID string) int { return d.SCCID[nodeID] }

func (d Decomposition) PackageOf(nodeID string) string { return d.PackageID[nodeID] }

func (d Decomposition) IsCyclic(nodeID string) bool {
	id, ok := d.SCCID[nodeID]
	if !ok {
		return false
	}
	return d.SCCSize[id] > 1
}

func Compute(nodes []store.Node, edges []store.Edge) Decomposition {
	m := newIDMap(nodes)

	structural := map[store.EdgeKind]bool{}
	for _, k := range structuralEdgeKinds() {
		structural[k] = true
	}
	var sEdges []store.Edge
	for _, e := range edges {
		if structural[store.EdgeKind(e.Kind)] {
			sEdges = append(sEdges, e)
		}
	}
	directed := buildDirected(nodes, sEdges, m)
	undirected := buildUndirected(directed, m)

	coreness := corenessByNode(degeneracyCores(undirected), m)
	sccID, sccSize := sccByNode(topoSCC(directed), m)
	layers := condensationLayers(directed, sccID, m, len(sccSize))

	pkg := make(map[string]string, len(nodes))
	for _, n := range nodes {
		pkg[n.NodeID] = packageID(n)
	}

	return Decomposition{
		Coreness:  coreness,
		SCCID:     sccID,
		PackageID: pkg,
		SCCSize:   sccSize,
		Layers:    layers,
		HashKey:   HashKey(nodes, edges),
	}
}

func Recompute(ctx context.Context, s *store.Store, lastHashKey string) (Decomposition, bool, error) {
	nodes, err := readAllNodes(ctx, s)
	if err != nil {
		return Decomposition{}, false, err
	}
	edges, err := readStructuralEdges(ctx, s, nodes)
	if err != nil {
		return Decomposition{}, false, err
	}
	dec := Compute(nodes, edges)
	if lastHashKey != "" && dec.HashKey == lastHashKey {
		return dec, false, nil
	}
	for _, n := range nodes {
		if err := s.UpdateNodeStructure(ctx, n.NodeID, dec.Coreness[n.NodeID], dec.SCCID[n.NodeID], dec.PackageID[n.NodeID]); err != nil {
			return Decomposition{}, false, fmt.Errorf("caronte/structure: writeback %q: %w", n.NodeID, err)
		}
	}
	return dec, true, nil
}

func readAllNodes(ctx context.Context, s *store.Store) ([]store.Node, error) {
	var out []store.Node
	for _, k := range store.AllNodeKinds() {
		ns, err := s.ListNodesByKind(ctx, k)
		if err != nil {
			return nil, fmt.Errorf("caronte/structure: ListNodesByKind %q: %w", k, err)
		}
		out = append(out, ns...)
	}
	return out, nil
}

func readStructuralEdges(ctx context.Context, s *store.Store, nodes []store.Node) ([]store.Edge, error) {
	var out []store.Edge
	for _, n := range nodes {
		for _, k := range structuralEdgeKinds() {
			es, err := s.ListEdgesBySource(ctx, n.NodeID, k)
			if err != nil {
				return nil, fmt.Errorf("caronte/structure: ListEdgesBySource %q/%q: %w", n.NodeID, k, err)
			}
			out = append(out, es...)
		}
	}
	return out, nil
}
