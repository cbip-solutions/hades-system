//go:build chaos

package failpoints

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestSitesCount(t *testing.T) {
	if got := len(Sites()); got != CanonicalSiteCount {
		t.Fatalf("Sites() = %d, want %d (inv-zen-306)", got, CanonicalSiteCount)
	}
}

func TestSitesNamesUnique(t *testing.T) {
	seen := make(map[string]int, len(Sites()))
	for _, s := range Sites() {
		seen[s.Name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("duplicate Site.Name %q appears %d times", name, n)
		}
	}
}

func TestSitesLexicographicOrder(t *testing.T) {
	names := make([]string, len(Sites()))
	for i, s := range Sites() {
		names[i] = s.Name
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	if !slices.Equal(names, sorted) {
		t.Errorf("Sites() not lexicographic: got=%v want=%v", names, sorted)
	}
}

// TestSitesFileExistsAndCarriesGofailComment pins the catalogue ↔
// source-tree round-trip: every documented Site MUST resolve to a file
// that contains a `// gofail: var <Name>` comment. In the canonical
// committed (disabled) tree this is exact; in an enabled tree the
// rewriter has replaced the comment with `__fp_<Name>.Acquire(...)`
// (gofail v0.2.x mangled-identifier hot-path API) so we accept either
// form. The legacy `gofail.Inject(<Name>` form is retained for
// backwards-compat with older gofail versions a contributor may have
// installed locally.
//
// Sister-assertion: Plan 15 Phase F F-12 smoke surfaced that the
// previously-shipped enabled-form marker only covered the legacy
// `gofail.Inject` API; the v0.2.x rewriter (pinned by GOFAIL_VERSION
// in the Makefile) emits `__fp_<Name>.Acquire`. Without this fix the
// `make test-chaos-failpoint` enable→test→disable wrap fails 12/15
// sites during the enabled-window invariant check.
func TestSitesFileExistsAndCarriesGofailComment(t *testing.T) {
	root := repoRoot(t)
	for _, s := range Sites() {
		s := s
		t.Run(s.Name, func(t *testing.T) {
			abs := filepath.Join(root, s.File)
			data, err := os.ReadFile(abs)
			if err != nil {
				t.Fatalf("read %s: %v", abs, err)
			}
			disabled := "// gofail: var " + s.Name
			enabledLegacy := "gofail.Inject(\"" + s.Name + "\""
			enabledV02 := "__fp_" + s.Name + ".Acquire"
			content := string(data)
			if !strings.Contains(content, disabled) &&
				!strings.Contains(content, enabledLegacy) &&
				!strings.Contains(content, enabledV02) {
				t.Errorf("%s: file %s carries none of: disabled comment %q, legacy enabled call %q, v0.2.x enabled call %q",
					s.Name, s.File, disabled, enabledLegacy, enabledV02)
			}
		})
	}
}

func TestSiteByNameLookup(t *testing.T) {
	got := SiteByName("auditWALFsync")
	if got == nil {
		t.Fatal("SiteByName(auditWALFsync) returned nil")
	}
	if got.Package != "github.com/cbip-solutions/hades-system/internal/audit/chain" {
		t.Errorf("auditWALFsync.Package = %q", got.Package)
	}
	if SiteByName("does_not_exist") != nil {
		t.Error("SiteByName(does_not_exist) returned non-nil")
	}
}

func TestSitesAllPackagesUnderInternal(t *testing.T) {
	for _, s := range Sites() {
		if !strings.Contains(s.Package, "internal/") {
			t.Errorf("Site %q has Package %q outside internal/", s.Name, s.Package)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repoRoot: runtime.Caller failed")
	}
	root := strings.TrimSuffix(here, "tests/chaos/failpoints/sites_test.go")
	return root
}
