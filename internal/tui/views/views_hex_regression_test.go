package views

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestViewsNoHardcodedHex(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	hexPattern := regexp.MustCompile(`lipgloss\.Color\("#[0-9A-Fa-f]{6}"\)`)
	for _, f := range matches {

		if filepath.Ext(f) == ".go" && len(f) > 8 && f[len(f)-8:] == "_test.go" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := hexPattern.FindIndex(data); loc != nil {
			line := bytes.Count(data[:loc[0]], []byte{'\n'}) + 1
			t.Errorf("%s:%d hardcoded hex literal found — must reference palette constant from internal/tui/palette (e.g., palette.ColorOK, palette.ColorSeverityLow)", f, line)
		}
	}
}
