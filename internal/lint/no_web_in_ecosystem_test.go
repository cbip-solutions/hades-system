package lint_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestNoWebInEcosystemAnalyzerOnViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInEcosystemAnalyzer,
		"no_web_in_ecosystem/ecosystem_violation")
}

func TestNoWebInEcosystemAnalyzerOnClean(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInEcosystemAnalyzer,
		"no_web_in_ecosystem/ecosystem_clean")
}

func TestNoWebInEcosystemAnalyzerOnSourceViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInEcosystemAnalyzer,
		"no_web_in_ecosystem/ecosystem_source_violation")
}

func TestNoWebInEcosystemAnalyzerOnTLSViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInEcosystemAnalyzer,
		"no_web_in_ecosystem/ecosystem_tls_violation")
}

func TestNoWebInEcosystemAnalyzerMetadata(t *testing.T) {
	a := lint.NoWebInEcosystemAnalyzer
	if a.Name != "noWebInEcosystem" {
		t.Errorf("Name = %q; want %q", a.Name, "noWebInEcosystem")
	}
	if a.Doc == "" {
		t.Error("Doc must not be empty")
	}
	if a.Run == nil {
		t.Error("Run must not be nil")
	}
}

func TestNoWebInEcosystemAnalyzerNotTriggerOnAggregator(t *testing.T) {

	fset := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("aggregator")}
	pkg := types.NewPackage(
		"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator",
		"aggregator",
	)

	var diags []string
	pass := &analysis.Pass{
		Analyzer: lint.NoWebInEcosystemAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := lint.NoWebInEcosystemAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics on aggregator pkg, got %d: %v",
			len(diags), diags)
	}
}

func TestNoWebInEcosystemAnalyzerNotTriggerOnDaemon(t *testing.T) {
	fset := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("daemon")}
	pkg := types.NewPackage(
		"github.com/cbip-solutions/hades-system/internal/daemon",
		"daemon",
	)

	var diags []string
	pass := &analysis.Pass{
		Analyzer: lint.NoWebInEcosystemAnalyzer,
		Fset:     fset,
		Files:    []*ast.File{file},
		Pkg:      pkg,
		Report: func(d analysis.Diagnostic) {
			diags = append(diags, d.Message)
		},
	}
	if _, err := lint.NoWebInEcosystemAnalyzer.Run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics on daemon pkg, got %d: %v",
			len(diags), diags)
	}
}
