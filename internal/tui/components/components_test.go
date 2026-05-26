package components

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTableRendersRows(t *testing.T) {
	tab := Table{
		Headers: []string{"task", "phase"},
		Rows:    [][]string{{"001", "codegen"}, {"002", "tests"}},
	}
	out := tab.Render()
	if !strings.Contains(out, "task") || !strings.Contains(out, "001") {
		t.Errorf("Render missing headers/rows: %s", out)
	}
}

func TestTableEmptyReturnsEmpty(t *testing.T) {
	if got := (Table{}).Render(); got != "" {
		t.Errorf("empty Table.Render() should return empty, got %q", got)
	}
	if got := (Table{Headers: []string{"h"}}).Render(); got != "" {
		t.Errorf("Table with headers but no rows should return empty, got %q", got)
	}
}

func TestTableWidthCap(t *testing.T) {
	tab := Table{
		Headers: []string{"LONG_HEADER_A", "B"},
		Rows:    [][]string{{"verylongcell000", "x"}},
		Width:   20,
	}
	out := tab.Render()
	if len(out) == 0 {
		t.Error("Table.Render with Width cap returned empty")
	}
}

func TestTableExtraCellsInRow(t *testing.T) {
	tab := Table{
		Headers: []string{"A"},
		Rows:    [][]string{{"val1", "extra_cell_beyond_headers"}},
	}

	out := tab.Render()
	if !strings.Contains(out, "val1") {
		t.Errorf("Table extra-cell row missing expected cell: %q", out)
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	if got := truncate("hello", 0); got != "" {
		t.Errorf("truncate w=0 want empty, got %q", got)
	}
	if got := truncate("hi", 1); got != "…" {
		t.Errorf("truncate w=1 want '…', got %q", got)
	}
	if got := truncate("short", 20); got != "short" {
		t.Errorf("truncate w>len want unchanged, got %q", got)
	}
	if got := truncate("longer", 4); got != "lon…" {
		t.Errorf("truncate normal want 'lon…', got %q", got)
	}
}

func TestSparklineRenders(t *testing.T) {
	s := Sparkline{Values: []float64{1, 2, 3, 4, 5}, Width: 5}
	out := s.Render()
	if len(out) == 0 {
		t.Error("expected non-empty sparkline")
	}
}

func TestSparklineEmpty(t *testing.T) {
	s := Sparkline{}
	if got := s.Render(); got != "" {
		t.Errorf("empty Sparkline.Render() should return empty, got %q", got)
	}
}

func TestSparklineDefaultWidth(t *testing.T) {
	s := Sparkline{Values: []float64{1, 2, 3}}
	out := s.Render()
	if len(out) == 0 {
		t.Error("Sparkline default-width returned empty")
	}
}

func TestSparklineDecimation(t *testing.T) {

	vals := make([]float64, 20)
	for i := range vals {
		vals[i] = float64(i)
	}
	s := Sparkline{Values: vals, Width: 5}
	out := s.Render()
	if len([]rune(out)) > 5+1 {
		t.Errorf("decimated sparkline width > %d, got %d runes: %q", 5, len([]rune(out)), out)
	}
	if len(out) == 0 {
		t.Error("decimated Sparkline.Render() returned empty")
	}
}

func TestSparklineZeroRange(t *testing.T) {
	s := Sparkline{Values: []float64{5, 5, 5, 5}, Width: 4}
	out := s.Render()
	for _, r := range out {
		if r != '▁' {
			t.Errorf("zero-range sparkline expected all '▁', got %q in output %q", r, out)
		}
	}
}

func TestProseStripsFrontmatter(t *testing.T) {
	md := "---\nname: x\n---\n# Heading\nbody"
	stripped := stripFrontmatter(md)
	if strings.Contains(stripped, "name: x") {
		t.Errorf("frontmatter not stripped: %s", stripped)
	}
}

func TestIsBoxDrawing(t *testing.T) {
	if !IsBoxDrawing("┌─┐") {
		t.Error("expected box-drawing detected")
	}
	if IsBoxDrawing("hello") {
		t.Error("expected no box-drawing")
	}
}

func TestComponentsNoHardcodedHex(t *testing.T) {
	files := []string{
		"header.go",
		"ascii_diagram.go",
		"table.go",
		"prose.go",
		"sparkline.go",
	}
	hexPattern := regexp.MustCompile(`lipgloss\.Color\("#[0-9A-Fa-f]{6}"\)`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if loc := hexPattern.FindIndex(data); loc != nil {
			line := bytes.Count(data[:loc[0]], []byte{'\n'}) + 1
			t.Errorf("%s:%d hardcoded hex literal found — must reference palette constant from tui/styles.go", f, line)
		}
	}
}

func TestHeaderRender(t *testing.T) {
	out := HeaderRender("HADES dashboard", "status: ok")
	if len(out) == 0 {
		t.Fatal("HeaderRender returned empty string")
	}
	if !strings.Contains(out, "HADES dashboard") {
		t.Errorf("HeaderRender output missing title:\n%q", out)
	}
	if !strings.Contains(out, "status: ok") {
		t.Errorf("HeaderRender output missing statusLine:\n%q", out)
	}
}

func TestHeaderRenderEmptyInputs(t *testing.T) {
	out := HeaderRender("", "")

	if out == "" {
		t.Error("HeaderRender(\"\", \"\") must not return empty string (border styling)")
	}
}

func TestASCIIDiagramRender(t *testing.T) {
	d := ASCIIDiagram{Lines: []string{"┌─┐", "│x│", "└─┘"}}
	out := d.Render()
	if len(out) == 0 {
		t.Fatal("ASCIIDiagram.Render returned empty string")
	}
	if !strings.Contains(out, "┌─┐") {
		t.Errorf("ASCIIDiagram.Render output missing first line:\n%q", out)
	}
}

func TestASCIIDiagramRenderEmpty(t *testing.T) {
	d := ASCIIDiagram{}

	_ = d.Render()
}

func TestProseRenderSmoke(t *testing.T) {
	p := ProseRender{Width: 80}
	out, err := p.Render("# Hello\n\nBody text.\n")
	if err != nil {
		t.Fatalf("ProseRender.Render error: %v", err)
	}
	if len(out) == 0 {
		t.Error("ProseRender.Render returned empty output")
	}
}

func TestProseRenderDefaultWidth(t *testing.T) {
	p := ProseRender{}
	out, err := p.Render("plain text\n")
	if err != nil {
		t.Fatalf("ProseRender.Render default-width error: %v", err)
	}
	_ = out
}

func TestBidentCornerGlyph(t *testing.T) {
	g := BidentCornerGlyph()
	if len(g) == 0 {
		t.Fatal("BidentCornerGlyph returned empty string")
	}

	if !strings.Contains(g, "\n") {
		t.Errorf("BidentCornerGlyph expected multi-row braille, got single line:\n%q", g)
	}

	hasBraille := false
	for _, r := range g {
		if r >= 0x2800 && r <= 0x28FF {
			hasBraille = true
			break
		}
	}
	if !hasBraille {
		t.Errorf("BidentCornerGlyph expected at least one braille character, got:\n%q", g)
	}
}
