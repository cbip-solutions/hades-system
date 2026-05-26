package lint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestNoCrossProjectAtTesseraAnalyzerOnClean(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoCrossProjectAtTesseraAnalyzer, "no_cross_project_at_tessera/projectid_keyed")
}

func TestNoCrossProjectAtTesseraAnalyzerOnViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoCrossProjectAtTesseraAnalyzer, "no_cross_project_at_tessera/cross_project")
}

func TestNoCrossProjectAtTesseraAnalyzerMetadata(t *testing.T) {
	a := lint.NoCrossProjectAtTesseraAnalyzer
	if a.Name != "noCrossProjectAtTessera" {
		t.Errorf("Name = %q; want %q", a.Name, "noCrossProjectAtTessera")
	}
	if a.Doc == "" {
		t.Error("Doc must not be empty")
	}
}
