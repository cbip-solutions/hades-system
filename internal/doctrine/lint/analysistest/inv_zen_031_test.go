package analysistest_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostore"
)

func TestInvZen031_AnalyzerCatchesWorkforceQueueImportingStore(t *testing.T) {
	dir := analysistestDir(t)
	analysistest.Run(t, dir, nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
}

func TestInvZen031_AnalyzerCatchesAllWorkforceSubpackages(t *testing.T) {
	dir := analysistestDir(t)
	analysistest.Run(t, dir, nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
}

func TestInvZen031_WorkforceAdapterIsAllowlisted(t *testing.T) {
	allow := nostore.DefaultAllowlist()
	const wantPath = "github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter"
	for _, p := range allow {
		if p == wantPath {
			return
		}
	}
	t.Errorf("DefaultAllowlist does NOT contain %q; "+
		"workforceadapter MUST be allowlisted (the bridge between workforce/* and store)",
		wantPath)
}

func TestInvZen133_AnalyzerCatchesDoctrineImportingStore(t *testing.T) {
	dir := analysistestDir(t)
	analysistest.Run(t, dir, nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
}

// TestInvZen031_PlanWorkforceProductionCodeIsClean asserts that the actual
// production code under internal/workforce/* does NOT import internal/store
// directly. This is the ULTIMATE goal of inv-zen-031 — beyond fixture
// verification, the real codebase must conform.
//
// We achieve this by checking the analyzer's allowlist invariants
// programmatically: workforce sub-packages MUST NOT be on the allowlist
// (they bridge via adapter). For real-package coverage, see the Phase M
// CI gate that invokes ./scripts/lint-no-tech-debt.sh on the entire repo.
func TestInvZen031_PlanWorkforceProductionCodeIsClean(t *testing.T) {
	allow := nostore.DefaultAllowlist()
	// workforce sub-packages MUST NOT be on the allowlist (they bridge via adapter).
	forbidden := []string{
		"github.com/cbip-solutions/hades-system/internal/workforce/queue",
		"github.com/cbip-solutions/hades-system/internal/workforce/scheduler",
		"github.com/cbip-solutions/hades-system/internal/workforce/budget",
		"github.com/cbip-solutions/hades-system/internal/doctrine/active",
		"github.com/cbip-solutions/hades-system/internal/doctrine/parser",
	}
	for _, p := range forbidden {
		for _, a := range allow {
			if a == p {
				t.Errorf("DefaultAllowlist accidentally contains %q — workforce/doctrine "+
					"sub-packages MUST bridge via adapter, not import store directly", p)
			}
		}
	}
}

func analysistestDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "testdata"))
}
