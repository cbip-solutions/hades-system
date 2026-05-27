// go:build !race
//go:build !race
// +build !race

package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCHANGELOGOmitsV019Narrative(t *testing.T) {
	root := findRepoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	bs := string(body)

	// (a) The narrative MUST NOT exist — no `## [v0.19.0]` header line.
	if strings.Contains(bs, "## [v0.19.0]") {
		t.Errorf("CHANGELOG.md contains a `## [v0.19.0]` header — v0.19.0 narrative was added; " +
			"the deliberate omission per Plan-15 v1.0 release decision was violated. " +
			"Remove the section; the narrative consolidates at v1.0 (Plan 15).")
	}

	// (b) The omitted-versions comment block MUST explicitly include v0.19.0.
	// Look for "v0.19.0" near "omitted" / "deliberately" / "intentionally" — accept any
	// of those phrasings within the top-of-file comment block (first 30 lines).
	lines := strings.SplitN(bs, "\n", 32)
	topLines := lines
	if len(topLines) > 30 {
		topLines = topLines[:30]
	}
	topBlock := strings.Join(topLines, "\n")
	if !strings.Contains(topBlock, "v0.19.0") {
		t.Errorf("CHANGELOG.md top-of-file comment block does not list v0.19.0 as an omitted version. " +
			"Add v0.19.0 to the existing omitted-versions comment so a future writer doesn't restore the narrative as an oversight.")
	}
	if !(strings.Contains(topBlock, "omitted") || strings.Contains(topBlock, "OMITTED")) {
		t.Errorf("CHANGELOG.md top-of-file comment block missing the 'omitted' marker for the omitted-versions list; " +
			"a future writer needs the explicit signal.")
	}
}
