// tests/compliance/inv_zen_173_adr_coverage_test.go
//
// Compliance test for invariant: alternative trigger
// conditions (Tree-sitter + LLM-augmented in-house code-graph) MUST be
// captured in ADR-0082 at architecture records
//
// Spec §8.2 row 173. Triple-anchor:
// - compile-check: file existence grep at build time
// - runtime test: doctor check ADR coverage
// - compliance test (this file)
package compliance

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func findRepoRootInvZen173(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("findRepoRootInvZen173: could not locate go.mod from %v", dir)
	return ""
}

func TestInvZen173_ADR0082Exists(t *testing.T) {
	repoRoot := findRepoRootInvZen173(t)
	pattern := filepath.Join(repoRoot, "docs/decisions/0082-*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	if len(matches) == 0 {
		t.Fatalf("inv-zen-173 violation: ADR-0082 missing — pattern %s yielded no files", pattern)
	}
}

func TestInvZen173_ADR0082CoversTriggerCriteria(t *testing.T) {
	repoRoot := findRepoRootInvZen173(t)
	matches, _ := filepath.Glob(filepath.Join(repoRoot, "docs/decisions/0082-*.md"))
	if len(matches) == 0 {
		t.Skip("ADR-0082 missing (covered by sibling test)")
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read %s: %v", matches[0], err)
	}
	contents := string(data)

	required := []string{
		"Tree-sitter",
		"LLM",
		"trigger",
		"engraph",
	}
	missing := []string{}
	for _, kw := range required {
		if !strings.Contains(contents, kw) {
			missing = append(missing, kw)
		}
	}
	if len(missing) > 0 {
		t.Errorf("inv-zen-173 violation: ADR-0082 missing required keywords: %v", missing)
	}

	statusRE := regexp.MustCompile(`(?im)^\*?\*?Status:\*?\*?\s*(Tracked|Accepted|Superseded)`)
	if !statusRE.MatchString(contents) {
		t.Error("inv-zen-173 violation: ADR-0082 missing Status: Tracked|Accepted|Superseded line")
	}
}

func TestInvZen173_TriggerCriteriaDocumented(t *testing.T) {
	repoRoot := findRepoRootInvZen173(t)
	matches, _ := filepath.Glob(filepath.Join(repoRoot, "docs/decisions/0082-*.md"))
	if len(matches) == 0 {
		t.Skip("ADR-0082 missing (covered by sibling test)")
	}
	data, _ := os.ReadFile(matches[0])
	contents := strings.ToLower(string(data))

	triggerPhrases := []string{
		"trigger criteria",
		"trigger condition",
		"escape hatch",
		"alternative trigger",
		"escape-hatch",
	}
	found := false
	for _, p := range triggerPhrases {
		if strings.Contains(contents, p) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("inv-zen-173 violation: ADR-0082 missing trigger criteria language; expected one of %v", triggerPhrases)
	}
}
