//go:build cgo
// +build cgo

package semantic

import (
	"context"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"testing"

	caronteparser "github.com/cbip-solutions/hades-system/internal/caronte/parser"
)

// TestNodeIDMatchesPhaseBByteForByte is the parse↔resolve JOIN-KEY gate. node_id
// is the join between parsed nodes (Phase B writes them to graph_nodes) and
// resolved edges (Phase C writes SourceID/TargetID). They MUST be the SAME
// string for the SAME symbol or every C edge dangles. Here we drive BOTH layers
// off ONE Go source:
//
//   - Phase B: parser.ParseFile(ctx, "internal/widget/widget.go", src) — its
//     goPackagePathFromFile derives dir "internal/widget" and qualifiedNodeID
//     emits "internal/widget.Run" / "internal/widget.Server.Serve".
//   - Phase C: type-check the SAME src as import path
//     "github.com/cbip-solutions/hades-system/internal/widget", then canonicalNodeID
//     strips module "github.com/cbip-solutions/hades-system" to recover the SAME
//     "internal/widget.<...>".
//
// The dir Phase B sees ("internal/widget") and the module-stripped import-path
// tail Phase C sees MUST coincide — that is the contract, asserted byte-for-byte.
func TestNodeIDMatchesPhaseBByteForByte(t *testing.T) {
	const (
		modulePath = "github.com/cbip-solutions/hades-system"
		importPath = modulePath + "/internal/widget"
		// repoRelPath is the path Phase B's extractor receives; its directory
		// ("internal/widget") MUST equal importPath with modulePath stripped.
		repoRelPath = "internal/widget/widget.go"
		src         = `package widget

// Run does work.
func Run(id int) error { return nil }

// Server greets.
type Server struct{}

// Serve runs the server.
func (s Server) Serve() {}

// Ptr has a pointer receiver.
func (s *Server) Ptr() {}
`
	)

	p, err := caronteparser.NewParser()
	if err != nil {
		t.Fatalf("parser.NewParser: %v", err)
	}
	bRes, err := p.ParseFile(context.Background(), repoRelPath, []byte(src))
	if err != nil {
		t.Fatalf("Phase B ParseFile: %v", err)
	}
	bIDs := map[string]string{}
	for _, n := range bRes.Nodes {

		key := n.Name

		if rest, ok := trimDirPrefix(n.NodeID, "internal/widget"); ok {
			key = rest
		}
		bIDs[key] = n.NodeID
	}

	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "widget.go", src, 0)
	if err != nil {
		t.Fatalf("go/parser: %v", err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check(importPath, fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	cID := func(name string) string {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			t.Fatalf("symbol %q not in package scope", name)
		}
		return canonicalNodeID(obj, modulePath)
	}
	methodID := func(recv, name string) string {
		ms := types.NewMethodSet(types.NewPointer(pkg.Scope().Lookup(recv).Type()))
		for i := 0; i < ms.Len(); i++ {
			if ms.At(i).Obj().Name() == name {
				return canonicalNodeID(ms.At(i).Obj(), modulePath)
			}
		}
		t.Fatalf("method %s.%s not found", recv, name)
		return ""
	}

	cases := []struct {
		key  string
		cVal string
	}{
		{"Run", cID("Run")},
		{"Server", cID("Server")},
		{"Server.Serve", methodID("Server", "Serve")},
		{"Server.Ptr", methodID("Server", "Ptr")},
	}
	for _, c := range cases {
		bVal, ok := bIDs[c.key]
		if !ok {
			t.Errorf("Phase B did not emit a node for %q (got %v)", c.key, bIDs)
			continue
		}
		if bVal != c.cVal {
			t.Errorf("JOIN-KEY drift for %q: Phase B node_id %q != Phase C node_id %q", c.key, bVal, c.cVal)
		}
	}

	if got := cID("Run"); got != "internal/widget.Run" {
		t.Errorf("Phase C Run id = %q; want internal/widget.Run", got)
	}
	if got := methodID("Server", "Serve"); got != "internal/widget.Server.Serve" {
		t.Errorf("Phase C Serve id = %q; want internal/widget.Server.Serve", got)
	}
}

func trimDirPrefix(nodeID, dir string) (string, bool) {
	if dir == "" {
		return nodeID, true
	}
	p := dir + "."
	if len(nodeID) > len(p) && nodeID[:len(p)] == p {
		return nodeID[len(p):], true
	}
	return nodeID, false
}
