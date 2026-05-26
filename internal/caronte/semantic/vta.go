// SPDX-License-Identifier: MIT
package semantic

import (
	"fmt"
	"go/token"
	"sort"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

type vtaInputs struct {
	prog         *ssa.Program
	funcs        map[*ssa.Function]bool
	fset         *token.FileSet
	modulePrefix string
}

func buildSSA(pkgs []*packages.Package) (*vtaInputs, error) {
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("caronte/semantic: buildSSA: no packages")
	}
	prog, _ := ssautil.Packages(pkgs, ssa.InstantiateGenerics)
	if prog == nil {
		return nil, fmt.Errorf("caronte/semantic: ssautil.Packages returned nil program")
	}
	prog.Build()
	return &vtaInputs{
		prog:         prog,
		funcs:        ssautil.AllFunctions(prog),
		fset:         prog.Fset,
		modulePrefix: modulePathOf(pkgs),
	}, nil
}

func buildVTACallGraph(in *vtaInputs) *callgraph.Graph {
	if in == nil {
		return nil
	}

	chaG := cha.CallGraph(in.prog)
	vtaG := vta.CallGraph(in.funcs, chaG)

	mergeCallGraphEdges(vtaG, chaG, in.funcs)
	return vtaG
}

func mergeCallGraphEdges(dst, src *callgraph.Graph, funcs map[*ssa.Function]bool) {
	if dst == nil || src == nil {
		return
	}
	for fn, srcNode := range src.Nodes {
		if fn == nil || !funcs[fn] {
			continue
		}
		for _, e := range srcNode.Out {
			if e.Callee == nil || e.Callee.Func == nil {
				continue
			}
			calleeF := e.Callee.Func

			if calleeF.Synthetic != "" {
				calleeF = resolveSyntheticCallee(src, calleeF)
				if calleeF == nil {
					continue
				}
			}
			if !funcs[calleeF] {
				continue
			}

			callerNode := dst.CreateNode(fn)
			calleeNode := dst.CreateNode(calleeF)

			if !edgeExists(callerNode, calleeNode, e.Site) {
				callgraph.AddEdge(callerNode, e.Site, calleeNode)
			}
		}
	}
}

func resolveSyntheticCallee(g *callgraph.Graph, wrapper *ssa.Function) *ssa.Function {
	node, ok := g.Nodes[wrapper]
	if !ok || node == nil {
		return nil
	}
	for _, we := range node.Out {
		if we.Callee != nil && we.Callee.Func != nil && we.Callee.Func.Synthetic == "" {
			return we.Callee.Func
		}
	}
	return nil
}

func edgeExists(callerNode, calleeNode *callgraph.Node, site ssa.CallInstruction) bool {
	for _, e := range callerNode.Out {
		if e.Callee == calleeNode && e.Site == site {
			return true
		}
	}
	return false
}

func callEdgesFromGraph(g *callgraph.Graph, modulePrefix string) []store.Edge {
	return collectCallEdges(g, store.ConfExactVTA, boolPtrTrue(), modulePrefix)
}

func collectCallEdges(g *callgraph.Graph, conf store.Confidence, reachable *bool, modulePrefix string) []store.Edge {
	out := []store.Edge{}
	if g == nil {
		return out
	}
	for fn, node := range g.Nodes {
		srcID := ssaFuncNodeID(fn, modulePrefix)
		if srcID == "" {
			continue
		}
		for _, e := range node.Out {
			if e.Callee == nil || e.Callee.Func == nil {
				continue
			}
			dstID := ssaFuncNodeID(e.Callee.Func, modulePrefix)
			if dstID == "" {
				continue
			}
			kind := store.EdgeCalls
			if e.Site != nil && e.Site.Common().IsInvoke() {
				kind = store.EdgeInvoke
			}
			file, line := siteFileLine(g, e)
			edge := store.Edge{
				SourceID:   srcID,
				TargetID:   dstID,
				Kind:       string(kind),
				Confidence: conf,
				SiteFile:   file,
				SiteLine:   line,
			}
			if reachable != nil {
				r := *reachable
				edge.Reachable = &r
			}
			out = append(out, edge)
		}
	}
	sortEdges(out)
	return out
}

func ssaFuncNodeID(fn *ssa.Function, modulePrefix string) string {
	if fn == nil || fn.Synthetic != "" {
		return ""
	}
	return canonicalNodeID(fn.Object(), modulePrefix)
}

func siteFileLine(g *callgraph.Graph, e *callgraph.Edge) (string, int) {
	if e.Site == nil {
		return "", 0
	}
	pos := e.Site.Pos()
	if !pos.IsValid() {
		return "", 0
	}

	if e.Caller != nil && e.Caller.Func != nil && e.Caller.Func.Prog != nil {
		p := e.Caller.Func.Prog.Fset.Position(pos)
		return p.Filename, p.Line
	}
	_ = g
	return "", 0
}

func sortEdges(edges []store.Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.TargetID != b.TargetID {
			return a.TargetID < b.TargetID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SiteLine < b.SiteLine
	})
}

func boolPtrTrue() *bool { b := true; return &b }
