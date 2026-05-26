package analysistest_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostore"
	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostub"
)

func testDataDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "testdata"))
}

func TestNoStubAndNoStoreCompose(t *testing.T) {
	dir := testDataDir(t)
	t.Run("nostub_then_nostore", func(t *testing.T) {
		analysistest.Run(t, dir, nostub.Analyzer, "no-stub/bad")
		analysistest.Run(t, dir, nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
	})
	t.Run("nostore_then_nostub", func(t *testing.T) {
		analysistest.Run(t, dir, nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
		analysistest.Run(t, dir, nostub.Analyzer, "no-stub/bad")
	})
}

func TestGoodFixturesQuietForBothAnalyzers(t *testing.T) {
	dir := testDataDir(t)

	analysistest.Run(t, dir, nostub.Analyzer, "no-stub/good")

	const goodPkg = "github.com/cbip-solutions/hades-system/no-store-import-good"
	if err := nostore.Analyzer.Flags.Set("allowlist", goodPkg); err != nil {
		t.Fatalf("set allowlist flag: %v", err)
	}
	defer func() { _ = nostore.Analyzer.Flags.Set("allowlist", "") }()
	analysistest.Run(t, dir, nostore.Analyzer, goodPkg)
}
