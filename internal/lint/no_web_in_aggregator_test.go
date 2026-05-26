package lint_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/lint"
)

func TestNoWebInAggregatorAnalyzerOnAggregatorViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInAggregatorAnalyzer, "no_web_in_aggregator/aggregator_violation")
}

func TestNoWebInAggregatorAnalyzerOnAggregatorClean(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInAggregatorAnalyzer, "no_web_in_aggregator/aggregator_clean")
}

func TestNoWebInAggregatorAnalyzerOnCacheRevalidator(t *testing.T) {

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInAggregatorAnalyzer, "no_web_in_aggregator/cache_revalidator")
}

func TestNoWebInAggregatorAnalyzerOnCacheViolation(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lint.NoWebInAggregatorAnalyzer, "no_web_in_aggregator/cache_violation")
}

func TestNoWebInAggregatorAnalyzerMetadata(t *testing.T) {
	a := lint.NoWebInAggregatorAnalyzer
	if a.Name != "noWebInAggregator" {
		t.Errorf("Name = %q; want %q", a.Name, "noWebInAggregator")
	}
	if a.Doc == "" {
		t.Error("Doc must not be empty")
	}
}
