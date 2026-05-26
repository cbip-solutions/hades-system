// tests/compliance/inv_zen_228_bident_inline_glyph_test.go
//
// inv-zen-260 (HADES UI gaps / Task 2) — bident inline glyph.
//
// Root cause of GAP 4: the initial palette.toml draft used the fleur-de-lis
// (⚜, U+269C) for inline prompt and response glyphs — visually clashing with
// the box-drawing bident (╨, U+2568) used in the HADES ASCII banner.
//
// inv-zen-260 pins the fix: the [branding] table in
// plugin/hades/skins/palette.toml MUST
//  1. contain NO occurrence of the fleur-de-lis character (⚜, U+269C), and
//  2. set prompt_symbol to exactly the bident (╨, U+2568).
//
// This is a source-level compliance guard (read the TOML file as text;
// assert the two properties above). The behavioral companion is the
// skin-level pytest in plugin/hades/skins/tests/test_hades.py.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fleurDeLis260 = "⚜"

	bidentGlyph260 = "╨"

	paletteFile260 = "plugin/hades/skins/palette.toml"
)

func repoRoot260(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-260: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-260: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen260_NoFleurDeLis(t *testing.T) {
	root := repoRoot260(t)
	path := filepath.Join(root, paletteFile260)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-260: read %s: %v", paletteFile260, err)
	}
	if strings.Contains(string(body), fleurDeLis260) {
		t.Errorf(
			"inv-zen-260: %s contains the fleur-de-lis glyph %q (U+269C). "+
				"Inline glyphs must be the bident family (╨, U+2568). "+
				"Replace every ⚜ occurrence with ╨.",
			paletteFile260, fleurDeLis260,
		)
	}
}

func TestInvZen260_PromptSymbolIsBident(t *testing.T) {
	root := repoRoot260(t)
	path := filepath.Join(root, paletteFile260)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("inv-zen-260: read %s: %v", paletteFile260, err)
	}

	want := `prompt_symbol`
	if !strings.Contains(string(body), want) {
		t.Fatalf("inv-zen-260: %s has no %q key in [branding]; palette schema changed?", paletteFile260, want)
	}

	for _, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "prompt_symbol") {
			if !strings.Contains(line, bidentGlyph260) {
				t.Errorf(
					"inv-zen-260: %s: prompt_symbol line does not contain the bident glyph %q (U+2568). Got: %q",
					paletteFile260, bidentGlyph260, strings.TrimSpace(line),
				)
			}
			return
		}
	}
	t.Fatalf("inv-zen-260: %s: prompt_symbol key found but no matching line — file corrupt?", paletteFile260)
}
