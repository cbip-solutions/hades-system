package orchestrator_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/orchestrator"
)

func TestContractFixAutonomyOracleMethodSet(t *testing.T) {
	typ := reflect.TypeOf((*orchestrator.ContractFixAutonomyOracle)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("ContractFixAutonomyOracle: want interface kind, got %v", typ.Kind())
	}
	if typ.NumMethod() != 1 {
		t.Fatalf("ContractFixAutonomyOracle: want 1 method, got %d", typ.NumMethod())
	}
	m := typ.Method(0)
	if m.Name != "Decision" {
		t.Errorf("method[0].Name: want Decision, got %q", m.Name)
	}

	if m.Type.NumIn() != 1 {
		t.Fatalf("Decision: want 1 input, got %d", m.Type.NumIn())
	}
	if m.Type.NumOut() != 1 {
		t.Fatalf("Decision: want 1 output, got %d", m.Type.NumOut())
	}
	wantIn := reflect.TypeOf(coordinated.ContractBreakage{})
	if got := m.Type.In(0); got != wantIn {
		t.Errorf("Decision input: want %v, got %v", wantIn, got)
	}
	wantOut := reflect.TypeOf(coordinated.ModeSurface)
	if got := m.Type.Out(0); got != wantOut {
		t.Errorf("Decision output: want %v, got %v", wantOut, got)
	}
}

func TestSeamContractFixFileIsPureInterface(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "seam_contractfix.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse seam_contractfix.go: %v", err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !ts.Name.IsExported() {
			return true
		}
		if _, isStruct := ts.Type.(*ast.StructType); isStruct {
			t.Errorf("seam_contractfix.go: exported struct %q forbidden (seam must stay pure interface)", ts.Name.Name)
		}
		return true
	})
}

func TestSeamContractFixFileImports(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "seam_contractfix.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse seam_contractfix.go imports: %v", err)
	}
	var got []string
	for _, imp := range file.Imports {
		got = append(got, imp.Path.Value)
	}
	const wantCoordinated = `"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"`
	var sawCoordinated bool
	for _, p := range got {
		if p == wantCoordinated {
			sawCoordinated = true
		}

		forbidden := []string{
			`"github.com/cbip-solutions/hades-system/internal/orchestrator/hra"`,
			`"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"`,
			`"github.com/cbip-solutions/hades-system/internal/orchestrator/confirmation_policy"`,
		}
		for _, f := range forbidden {
			if p == f {
				t.Errorf("seam_contractfix.go: forbidden import %s (inv-zen-270 boundary)", p)
			}
		}
	}
	if !sawCoordinated {
		t.Errorf("seam_contractfix.go: missing required import %s; got %v", wantCoordinated, got)
	}
}
