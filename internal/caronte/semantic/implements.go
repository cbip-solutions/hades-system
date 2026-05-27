// SPDX-License-Identifier: MIT
package semantic

import (
	"sort"
	"strings"

	"go/types"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
)

// canonicalNodeID maps a go/types object to the REPO-RELATIVE node_id Phase
// B's parser wrote into graph_nodes (spec §4.2 + lang_go.go
// `qualifiedNodeID`): "<dir>.[Type.]Symbol", where <dir> is the package's
// directory RELATIVE TO THE REPO ROOT (the Go import-path tail). This is THE
// single source of truth for the parse↔resolve join — resolver edges
// reference node_ids this function produces, and they MUST match BYTE-FOR-BYTE
// the strings lang_go extraction emits for the same symbols.
//
// go/types reports the FULL import path on pkg.Path()
// ("github.com/cbip-solutions/hades-system/internal/widget"), but keys on the
// file's directory relative to the repo root ("internal/widget"). So
// canonicalNodeID STRIPS the module prefix (modulePrefix, read from go.mod via
// packages.Module.Path) from pkg.Path() to recover the dir-relative form that
// matches :
//
// module github.com/cbip-solutions/hades-system (from go.mod)
// import path github.com/cbip-solutions/hades-system/internal/widget (from go/types)
// dir prefix internal/widget (module stripped)
//
// func Run in internal/widget → "internal/widget.Run"
// method (T) Beta in internal/widget → "internal/widget.T.Beta" (receiver named-type, no '*')
// named type Reader in internal/widget → "internal/widget.Reader"
// main-module-root pkg / cmd:
// import path == module → dir prefix "" → "Run" / "Server.Serve" (no leading dot — exactly
// as goPackagePathFromFile drops the prefix for a repo-root file)
// github.com/.../cmd/hades → "cmd/hades" → "cmd/hades.main"
//
// Pointer and value receivers collapse to the same node_id: keys a
// method on its receiver's NAMED type, not on pointer-ness, so (T).M and
// (*T).M are one node (matching go/types method identity). A nil object — a
// synthetic SSA wrapper with no source object — yields "" so callers skip it
// rather than panicking.
//
// Pre obj is a *types.Func, *types.TypeName, or nil; modulePrefix is the
// module path from go.mod ("" tolerated — then no stripping, the import path
// is used verbatim, which only happens when the loader could not resolve a
// module and is the same fallback never hits in a real repo).
// Post "" iff obj is nil or has no enclosing package; otherwise a dotted
//
// repo-relative id BYTE-EQUAL to qualifiedNodeID for the same
// symbol, stable across runs (deterministic — feeds the inv-hades-232
// byte-stable structure downstream).
func canonicalNodeID(obj types.Object, modulePrefix string) string {
	if obj == nil {
		return ""
	}
	pkg := obj.Pkg()
	if pkg == nil {

		return ""
	}
	prefix := dirRelativePrefix(pkg.Path(), modulePrefix)
	switch o := obj.(type) {
	case *types.Func:
		sig, ok := o.Type().(*types.Signature)
		if ok && sig.Recv() != nil {
			if recv := receiverNamedTypeName(sig.Recv().Type()); recv != "" {
				return joinNodeID(prefix, recv, o.Name())
			}
		}
		return joinNodeID(prefix, "", o.Name())
	case *types.TypeName:
		return joinNodeID(prefix, "", o.Name())
	default:

		return joinNodeID(prefix, "", obj.Name())
	}
}

func dirRelativePrefix(importPath, modulePrefix string) string {
	if modulePrefix == "" {
		return importPath
	}
	if importPath == modulePrefix {
		return ""
	}
	if rest, ok := strings.CutPrefix(importPath, modulePrefix+"/"); ok {
		return rest
	}

	return importPath
}

func joinNodeID(prefix, receiver, name string) string {
	switch {
	case prefix == "" && receiver == "":
		return name
	case prefix == "":
		return receiver + "." + name
	case receiver == "":
		return prefix + "." + name
	default:
		return prefix + "." + receiver + "." + name
	}
}

func modulePathOf(pkgs []*packages.Package) string {
	for _, p := range pkgs {
		if p != nil && p.Module != nil && p.Module.Path != "" {
			return p.Module.Path
		}
	}
	return ""
}

func receiverNamedTypeName(recv types.Type) string {
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	if named, ok := recv.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

func reachableNodeIDs(in *vtaInputs) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in.funcs))
	for fn := range in.funcs {
		if id := ssaFuncNodeID(fn, in.modulePrefix); id != "" {
			out[id] = true
		}
	}
	return out
}

func interfaceImplementsEdges(pkgs []*packages.Package, conf store.Confidence, reachSet map[string]bool) []store.Edge {
	modulePrefix := modulePathOf(pkgs)
	interfaces, concretes := collectNamedTypes(pkgs, modulePrefix)
	out := []store.Edge{}
	for _, iface := range interfaces {
		ifaceUnderlying, ok := iface.Type().Underlying().(*types.Interface)
		if !ok || ifaceUnderlying.NumMethods() == 0 {

			continue
		}
		ifaceID := canonicalNodeID(iface, modulePrefix)
		if ifaceID == "" {
			continue
		}
		for _, c := range concretes {
			implID := canonicalNodeID(c, modulePrefix)
			if implID == "" || implID == ifaceID {
				continue
			}
			ct := c.Type()

			if !types.Implements(ct, ifaceUnderlying) && !types.Implements(types.NewPointer(ct), ifaceUnderlying) {
				continue
			}
			edge := store.Edge{
				SourceID:   ifaceID,
				TargetID:   implID,
				Kind:       string(store.EdgeImplements),
				Confidence: conf,
			}
			if reachSet != nil {
				if typeMethodsReached(c, reachSet, modulePrefix) {
					r := true
					edge.Reachable = &r
				} else {
					r := false
					edge.Reachable = &r
				}
			}
			out = append(out, edge)
		}
	}
	sortEdges(out)
	return out
}

func collectNamedTypes(pkgs []*packages.Package, modulePrefix string) (interfaces, concretes []types.Object) {
	seen := map[*types.Package]bool{}
	for _, p := range pkgs {
		if p.Types == nil || seen[p.Types] {
			continue
		}
		seen[p.Types] = true
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			switch tn.Type().Underlying().(type) {
			case *types.Interface:
				interfaces = append(interfaces, tn)
			default:
				concretes = append(concretes, tn)
			}
		}
	}

	sort.Slice(interfaces, func(i, j int) bool {
		return canonicalNodeID(interfaces[i], modulePrefix) < canonicalNodeID(interfaces[j], modulePrefix)
	})
	sort.Slice(concretes, func(i, j int) bool {
		return canonicalNodeID(concretes[i], modulePrefix) < canonicalNodeID(concretes[j], modulePrefix)
	})
	return interfaces, concretes
}

func typeMethodsReached(tn types.Object, reachSet map[string]bool, modulePrefix string) bool {
	for _, t := range []types.Type{tn.Type(), types.NewPointer(tn.Type())} {
		ms := types.NewMethodSet(t)
		for i := 0; i < ms.Len(); i++ {
			if reachSet[canonicalNodeID(ms.At(i).Obj(), modulePrefix)] {
				return true
			}
		}
	}
	return false
}

var _ = ssa.InstantiateGenerics
