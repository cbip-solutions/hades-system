package lint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestNoAutoPromoteAnalyzerOnEmptyReason(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoAutoPromoteAnalyzer, "no_auto_promote/promote_empty_reason")
}

func TestNoAutoPromoteAnalyzerOnMissingReason(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoAutoPromoteAnalyzer, "no_auto_promote/promote_missing_reason")
}

func TestNoAutoPromoteAnalyzerOnGoodCallsite(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoAutoPromoteAnalyzer, "no_auto_promote/promote_with_reason")
}

func TestNoAutoPromoteAnalyzerMetadata(t *testing.T) {
	a := lint.NoAutoPromoteAnalyzer
	if a.Name != "noAutoPromote" {
		t.Errorf("Name = %q; want %q", a.Name, "noAutoPromote")
	}
	if a.Doc == "" {
		t.Error("Doc must not be empty")
	}
}
