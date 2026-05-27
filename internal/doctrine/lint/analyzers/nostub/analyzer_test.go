// Package nostub_test verifies noStubAnalyzer per spec §1 Q4 B + §5.1 tier 10
// (analysistest). Uses analysistest.Run over testdata/src/no-stub/{good,bad}.
//
// Fixture convention:
//
// - testdata/src/no-stub/bad/*.go — files that MUST trigger the analyzer;
// each line that should report carries a `// want "regex"` annotation
// - testdata/src/no-stub/good/*.go — files that MUST NOT trigger; absence
// of `// want "..."` comments means analysistest expects ZERO diagnostics
//
// analysistest reads the diagnostics emitted by the Analyzer.Run for each
// package and cross-references them with `// want` annotations; mismatches
// (missing wants OR unwanted diagnostics) fail the test.
package nostub_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostub"
)

func testDataDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "analysistest", "testdata"))
}

func TestAnalyzerBadFixturesTriggerDiagnostics(t *testing.T) {
	analysistest.Run(t, testDataDir(t), nostub.Analyzer, "no-stub/bad")
}

func TestAnalyzerGoodFixturesDoNotTrigger(t *testing.T) {
	analysistest.Run(t, testDataDir(t), nostub.Analyzer, "no-stub/good")
}

func TestAnalyzerName(t *testing.T) {
	if got := nostub.Analyzer.Name; got != "nostub" {
		t.Errorf("Analyzer.Name = %q; want %q", got, "nostub")
	}
}

func TestAnalyzerDocNonEmpty(t *testing.T) {
	if nostub.Analyzer.Doc == "" {
		t.Error("Analyzer.Doc is empty")
	}
}
