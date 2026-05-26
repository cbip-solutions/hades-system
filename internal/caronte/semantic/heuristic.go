// SPDX-License-Identifier: MIT
package semantic

import (
	"strings"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func heuristicImplementsEdges(interfaces, structs, methods []store.Node) []store.Edge {
	if len(interfaces) == 0 || len(structs) == 0 {
		return nil
	}

	ownerMethods := map[string]map[string]bool{}
	for _, m := range methods {
		owner := ownerOfMember(m.NodeID)
		if owner == "" {
			continue
		}
		if ownerMethods[owner] == nil {
			ownerMethods[owner] = map[string]bool{}
		}
		ownerMethods[owner][m.Name] = true
	}

	var edges []store.Edge
	for _, iface := range interfaces {
		want := ownerMethods[iface.NodeID]
		if len(want) == 0 {
			continue
		}
		for _, s := range structs {
			have := ownerMethods[s.NodeID]
			if len(have) == 0 {
				continue
			}
			if covers(have, want) {
				edges = append(edges, store.Edge{
					SourceID:   s.NodeID,
					TargetID:   iface.NodeID,
					Kind:       string(store.EdgeImplements),
					Confidence: store.ConfHeuristicName,
				})
			}
		}
	}
	sortEdgesByKey(edges)
	return edges
}

func ownerOfMember(nodeID string) string {
	if i := strings.LastIndex(nodeID, "::"); i >= 0 {
		return nodeID[:i]
	}
	if i := strings.LastIndex(nodeID, "."); i >= 0 {
		return nodeID[:i]
	}
	return ""
}

func covers(have, want map[string]bool) bool {
	for name := range want {
		if !have[name] {
			return false
		}
	}
	return true
}
