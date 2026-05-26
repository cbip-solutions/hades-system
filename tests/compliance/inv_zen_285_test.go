// tests/compliance/inv_zen_285_test.go
//
// Compliance gate for inv-zen-285 (v0.20.2 fix): the caronte intent linker
// resolves ADR ids via YAML frontmatter (canonical) with a filename-
// derivation fallback ONLY when frontmatter is absent or malformed.
//
// Why: across the repo, ADR identity is declared in the frontmatter and
// the filename is a mnemonic — they can diverge legitimately
// (renumber-on-merge cycles update the frontmatter id but defer the file
// rename, or a deliberate decision keeps the original-author filename
// when the canonical id shifts). The pre-v0.20.2 form
// (`id := "ADR-" + e.Name()[:4]` as the sole assignment) silently
// dropped renumbered ADRs from coverage_manifest + code-citation links
// — surfaced by TestCoverageManifestLinks failing on origin/main.
//
// Source-regex anchors over internal/caronte/intent/adrlink.go:
//
//  1. `splitFrontmatter(string(data))` MUST appear inside adrPathIndex —
//     the canonical parser is invoked.
//  2. `fmID != ""` MUST appear — frontmatter id is preferred when
//     present (gates the canonical assignment; substring works for both
//     inline-if and standalone-if idioms).
//  3. The fallback line `id = "ADR-" + e.Name()[:4]` MUST appear — back-
//     compat for legacy ADRs without frontmatter is preserved.
//  4. The pre-v0.20.2 form `id := "ADR-" + e.Name()[:4]` (NOTE := short
//     declaration) MUST NOT appear — that is the regressed shape where
//     filename is the sole source of truth.
//
// Behavioural anchors (same source file's test file):
//
//  5. `TestADRPathIndexFrontmatterIDWins` MUST be declared — the load-
//     bearing assertion that filename/frontmatter mismatch resolves
//     via frontmatter (the exact case that broke TestCoverageManifestLinks).
//
// Sister-test bite check: revert any of the four anchors in adrlink.go
// (or delete TestADRPathIndexFrontmatterIDWins); this test MUST fail.
//
// inv-zen-285 (v0.20.2 fix).
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen285SourceRegex_SplitFrontmatterCalled(t *testing.T) {
	src := readADRLinkSource(t)
	const needle = `splitFrontmatter(string(data))`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-285 violated: %q invocation missing from adrlink.go; adrPathIndex may have reverted to filename-only derivation", needle)
	}
}

func TestInvZen285SourceRegex_FrontmatterIDPreferred(t *testing.T) {
	src := readADRLinkSource(t)
	const needle = `fmID != ""`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-285 violated: %q guard missing in adrPathIndex; frontmatter id may not be the canonical source", needle)
	}
}

func TestInvZen285SourceRegex_FilenameFallbackPreserved(t *testing.T) {
	src := readADRLinkSource(t)
	const needle = `id = "ADR-" + e.Name()[:4]`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-285 violated: %q filename-fallback missing; legacy ADRs without frontmatter would not be indexed", needle)
	}
}

// TestInvZen285SourceRegex_NoRegressedShortDecl is anchor 4: the
// pre-v0.20.2 regressed form (`id := "ADR-" + e.Name()[:4]` with `:=`
// short declaration) MUST NOT appear. The new code uses `var id string`
// + `id = ...` for both the frontmatter and fallback assignments; the
// `:=` short-decl form is the signal that the canonical path has been
// removed and filename-derivation is the sole source again.
func TestInvZen285SourceRegex_NoRegressedShortDecl(t *testing.T) {
	src := readADRLinkSource(t)
	const needle = `id := "ADR-" + e.Name()[:4]`
	if strings.Contains(src, needle) {
		t.Errorf("inv-zen-285 violated: regressed pre-v0.20.2 short-decl form %q reappeared in adrlink.go; the canonical frontmatter path has been removed", needle)
	}
}

func TestInvZen285Behavioural_FrontmatterWinsTestDeclared(t *testing.T) {
	src := readADRLinkTestSource(t)
	const needle = `func TestADRPathIndexFrontmatterIDWins(`
	if !strings.Contains(src, needle) {
		t.Errorf("inv-zen-285 violated: behavioural test %q missing from adrlink_test.go; the load-bearing assertion has been removed", needle)
	}
}

func readADRLinkSource(t *testing.T) string {
	t.Helper()
	return readRepoFile(t, filepath.Join("..", "..", "internal", "caronte", "intent", "adrlink.go"))
}

func readADRLinkTestSource(t *testing.T) string {
	t.Helper()
	return readRepoFile(t, filepath.Join("..", "..", "internal", "caronte", "intent", "adrlink_test.go"))
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve %s: %v", rel, err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read %s: %v", abs, err)
	}
	return string(b)
}
