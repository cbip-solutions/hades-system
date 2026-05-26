// SPDX-License-Identifier: MIT
package semantic

import (
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
)

func buildCHACallGraph(in *vtaInputs) *callgraph.Graph {
	if in == nil || in.prog == nil {
		return nil
	}
	return cha.CallGraph(in.prog)
}

func callEdgesFromCHA(g *callgraph.Graph, modulePrefix string) []store.Edge {
	return collectCallEdges(g, store.ConfExactCHA, nil, modulePrefix)
}
