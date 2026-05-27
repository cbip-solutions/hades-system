// go:build integration
package plan18b_integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan18bJInt4_TUIPaletteAndHeaders(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	paletteBody, err := os.ReadFile(filepath.Join(root, "internal/tui/palette/palette.go"))
	if err != nil {
		t.Fatalf("read palette/palette.go: %v", err)
	}
	paletteStr := string(paletteBody)
	wantPaletteHex := []string{
		"#0d0d0d",
		"#c41e3a",
		"#e0e0e0",
		"#10b981",
	}
	for _, hex := range wantPaletteHex {
		if !strings.Contains(paletteStr, hex) {
			t.Errorf("J-int-4 palette/palette.go missing spec §Q2 palette color %q", hex)
		}
	}

	viewsHeaderBody, err := os.ReadFile(filepath.Join(root, "internal/tui/views/header.go"))
	if err != nil {
		t.Fatalf("read views/header.go: %v", err)
	}
	viewsHeaderStr := string(viewsHeaderBody)
	if !strings.Contains(viewsHeaderStr, "HADES") {
		t.Errorf("J-int-4 views/header.go missing HADES wordmark (single-source-of-truth for panel brand)")
	}
	if !strings.Contains(viewsHeaderStr, "panelHeader") {
		t.Errorf("J-int-4 views/header.go missing panelHeader function definition")
	}

	viewsDir := filepath.Join(root, "internal/tui/views")
	entries, err := os.ReadDir(viewsDir)
	if err != nil {
		t.Fatalf("read views dir: %v", err)
	}
	hadesOrPanelHeader := 0
	totalPanels := 0
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "header.go" {
			continue
		}

		if strings.HasSuffix(name, "_keys.go") {
			continue
		}
		totalPanels++
		body, err := os.ReadFile(filepath.Join(viewsDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		bodyStr := string(body)

		if strings.Contains(bodyStr, "panelHeader") || strings.Contains(bodyStr, "HADES") {
			hadesOrPanelHeader++
		}
	}

	if totalPanels < 12 {
		t.Errorf("J-int-4 expected ≥12 panel views (excl. header.go + _keys.go + test files), got %d", totalPanels)
	}
	if hadesOrPanelHeader < totalPanels {
		t.Errorf("J-int-4 expected all %d panel views to use panelHeader() or contain HADES; got %d", totalPanels, hadesOrPanelHeader)
	}

	dashboardBody, err := os.ReadFile(filepath.Join(root, "internal/tui/dashboard.go"))
	if err != nil {
		t.Fatalf("read dashboard.go: %v", err)
	}
	dashboardStr := string(dashboardBody)
	if !strings.Contains(dashboardStr, "HADES") {
		t.Errorf("J-int-4 dashboard.go missing HADES footer rebrand")
	}

	legacyFooterPhrase := "zen-swarm v0.12 — F1..F12 panels"
	if strings.Contains(dashboardStr, legacyFooterPhrase) {
		t.Errorf("J-int-4 dashboard.go still has legacy footer %q (Phase C miss)", legacyFooterPhrase)
	}

	headerBody, err := os.ReadFile(filepath.Join(root, "internal/tui/components/header.go"))
	if err != nil {
		t.Fatalf("read components/header.go: %v", err)
	}
	headerStr := string(headerBody)
	if !strings.Contains(headerStr, "HADES") {
		t.Errorf("J-int-4 components/header.go missing HADES corner glyph wire-up")
	}
}

// TestPlan18bJInt4_TUIPaletteGoNoLegacyZenSwarmInPalette asserts that the
// palette/palette.go declarations do NOT carry forward any legacy zen-swarm
// brand strings as comments. Defense-in-depth over the invariant AST scan
// (which catches string literals but not comments).
//
// Note: The package comment in styles.go says "zen-swarm dashboard TUI" —
// this is a package doc comment (not a palette declaration comment) and is
// allowlisted per spec §Q3 BORDERLINE (internal package doc, not
// operator-visible). The palette/ sub-package is what we scan here.
func TestPlan18bJInt4_TUIPaletteGoNoLegacyZenSwarmInPalette(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "internal/tui/palette/palette.go"))
	if err != nil {
		t.Fatalf("read palette/palette.go: %v", err)
	}

	violations := []string{}
	for lineno, line := range strings.Split(string(body), "\n") {
		stripped := strings.TrimSpace(line)
		if !strings.HasPrefix(stripped, "//") {
			continue
		}

		if strings.Contains(line, "(formerly zen-swarm)") {
			continue
		}
		if strings.Contains(line, "zen-swarm") || strings.Contains(line, "zen_swarm") {
			violations = append(violations, "  - line "+itoa_legacy(lineno+1)+": "+strings.TrimSpace(line))
		}
	}
	if len(violations) > 0 {
		t.Errorf("J-int-4 palette/palette.go has %d comment(s) still referencing legacy zen-swarm brand "+
			"(forensic trail signal — surface to operator for clean-up):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}

func itoa_legacy(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+(i%10))) + digits
		i /= 10
	}
	return digits
}
