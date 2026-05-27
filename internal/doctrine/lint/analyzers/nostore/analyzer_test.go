package nostore_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/nostore"
)

func testDataDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "analysistest", "testdata"))
}

func TestAnalyzerBadFixturesTriggerForbidden(t *testing.T) {
	// Bad package's import path: github.com/cbip-solutions/hades-system/no-store-import-bad
	// — NOT on the allowlist, so internal/store imports MUST be reported.
	analysistest.Run(t, testDataDir(t), nostore.Analyzer, "github.com/cbip-solutions/hades-system/no-store-import-bad")
}

func TestAnalyzerGoodFixturesPassWhenAllowlisted(t *testing.T) {

	const goodPkg = "github.com/cbip-solutions/hades-system/no-store-import-good"
	if err := nostore.Analyzer.Flags.Set("allowlist", goodPkg); err != nil {
		t.Fatalf("set allowlist flag: %v", err)
	}
	defer func() {

		_ = nostore.Analyzer.Flags.Set("allowlist", "")
	}()
	analysistest.Run(t, testDataDir(t), nostore.Analyzer, goodPkg)
}

func TestAnalyzerName(t *testing.T) {
	if got := nostore.Analyzer.Name; got != "nostore" {
		t.Errorf("Analyzer.Name = %q; want %q", got, "nostore")
	}
}

func TestAnalyzerDocMentionsBoundary(t *testing.T) {
	doc := nostore.Analyzer.Doc
	if doc == "" {
		t.Fatal("Analyzer.Doc is empty")
	}
	for _, want := range []string{"internal/store", "inv-zen-031", "inv-zen-133", "adapter"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Analyzer.Doc does not mention %q", want)
		}
	}
}

// TestDefaultAllowlistContainsCanonicalAdapters asserts the compile-baked
// default allowlist includes every known adapter package. If a future plan
// adds a new adapter (e.g., internal/daemon/eventlogadapter), the allowlist
// MUST be extended HERE — silent allowlist gap = silent invariant violation.
//
// IMPORTANT #2: widened from 4 to
// 13 entries to cover every legitimate store-consumer surfaced by `git grep`
// over production code. See nostore/analyzer.go DefaultAllowlist() doc-comment
// for the categorization (bridge adapters / top-level daemon / cmd entry /
// test helpers).
func TestDefaultAllowlistContainsCanonicalAdapters(t *testing.T) {
	want := []string{

		"github.com/cbip-solutions/hades-system/internal/daemon/bypassadapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/dispatcheradapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/doctrineadapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/inboxadapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/orchestratoradapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/projectctxadapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/quotaadapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter",
		"github.com/cbip-solutions/hades-system/internal/daemon/workforceadapter",

		"github.com/cbip-solutions/hades-system/internal/daemon",
		"github.com/cbip-solutions/hades-system/internal/daemon/handlers",

		"github.com/cbip-solutions/hades-system/cmd/zen-swarm-ctld",

		"github.com/cbip-solutions/hades-system/tests/testhelpers",
	}
	got := nostore.DefaultAllowlist()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DefaultAllowlist missing %q", w)
		}
	}
}

// TestDefaultAllowlistReturnsCopy asserts the returned slice is a copy, not
// a reference. Mutating the returned slice MUST NOT affect future calls.
func TestDefaultAllowlistReturnsCopy(t *testing.T) {
	a := nostore.DefaultAllowlist()
	if len(a) == 0 {
		t.Fatal("DefaultAllowlist returned empty slice")
	}
	a[0] = "MUTATED"
	b := nostore.DefaultAllowlist()
	if b[0] == "MUTATED" {
		t.Error("DefaultAllowlist returned shared slice; expected fresh copy each call")
	}
}
