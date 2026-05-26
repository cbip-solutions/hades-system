package compliance

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestInvZenB9_BrewFormulaServiceBlock(t *testing.T) {
	formulaPath := os.Getenv("HOMEBREW_PRIVATE_TAP_FORMULA")
	if formulaPath == "" {
		t.Skip("HOMEBREW_PRIVATE_TAP_FORMULA env var not set; this test runs in CI with the brew tap mounted")
	}
	body, err := os.ReadFile(formulaPath)
	if err != nil {
		t.Fatalf("read formula at %s: %v", formulaPath, err)
	}
	content := string(body)

	if !strings.Contains(content, "service do") {
		t.Error("Formula missing 'service do' block (inv-zen-283 anchor)")
	}
	for _, directive := range []string{"keep_alive true", "log_path", "error_log_path", "run ["} {
		if !strings.Contains(content, directive) {
			t.Errorf("Formula missing %q directive (inv-zen-283 service block contract)", directive)
		}
	}
}

func TestInvZenB9_BrewFormulaNoLicenseField(t *testing.T) {
	formulaPath := os.Getenv("HOMEBREW_PRIVATE_TAP_FORMULA")
	if formulaPath == "" {
		t.Skip("HOMEBREW_PRIVATE_TAP_FORMULA env var not set; this test runs in CI with the brew tap mounted")
	}
	body, err := os.ReadFile(formulaPath)
	if err != nil {
		t.Fatalf("read formula at %s: %v", formulaPath, err)
	}
	licenseLine := regexp.MustCompile(`(?m)^\s*license\s+`)
	if licenseLine.MatchString(string(body)) {
		t.Errorf("Formula MUST NOT contain a license DSL line per decisión 17-f; found: %q", licenseLine.FindString(string(body)))
	}
}
