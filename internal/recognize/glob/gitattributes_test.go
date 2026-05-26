package glob

import (
	"testing"
	"testing/fstest"
)

func TestParseGitAttributes_LanguageOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("*.weird linguist-language=Lua\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := o.LanguageOverride("x.weird"); got != "Lua" {
		t.Errorf("LanguageOverride = %q; want Lua", got)
	}
	if got := o.LanguageOverride("x.go"); got != "" {
		t.Errorf("LanguageOverride x.go = %q; want \"\"", got)
	}
}

func TestParseGitAttributes_VendoredOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("legacy/* linguist-vendored=true\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !o.IsVendored("legacy/old.py") {
		t.Error("IsVendored(legacy/old.py) = false; want true")
	}
	if o.IsVendored("src/new.go") {
		t.Error("IsVendored(src/new.go) = true; want false")
	}
}

func TestParseGitAttributes_GeneratedOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("gen/* linguist-generated=true\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !o.IsGenerated("gen/auto.go") {
		t.Error("IsGenerated(gen/auto.go) = false; want true")
	}
}

func TestParseGitAttributes_DocumentationOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("manual/* linguist-documentation=true\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !o.IsDocumentation("manual/intro.md") {
		t.Error("IsDocumentation(manual/intro.md) = false; want true")
	}
}

func TestParseGitAttributes_NoAttributesFile(t *testing.T) {
	fsys := fstest.MapFS{}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if o.LanguageOverride("any.go") != "" {
		t.Error("absent gitattributes leaked override")
	}
	if o.IsVendored("any.go") {
		t.Error("absent gitattributes leaked vendored flag")
	}
}

func TestParseGitAttributes_MultipleAttributesPerLine(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("*.foo linguist-language=Bar linguist-vendored=true\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if o.LanguageOverride("x.foo") != "Bar" {
		t.Errorf("LanguageOverride = %q; want Bar", o.LanguageOverride("x.foo"))
	}
	if !o.IsVendored("x.foo") {
		t.Error("IsVendored = false; want true")
	}
}

func TestParseGitAttributes_CommentsAndBlankLines(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("# this is a comment\n\n*.weird linguist-language=Lua\n# another comment\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if o.LanguageOverride("x.weird") != "Lua" {
		t.Errorf("LanguageOverride miss after comments + blanks")
	}
}

func TestMatch_VariousPatterns(t *testing.T) {
	tests := []struct {
		pattern, p string
		want       bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "subdir/main.go", true},
		{"vendor/*", "vendor/foo.go", true},
		{"vendor/*", "vendor/inner/foo.go", true},
		{"docs/*.md", "docs/x.md", true},
		{"docs/*.md", "src/x.md", false},
		{"exact.go", "exact.go", true},
		{"exact.go", "other.go", false},
		{"NoStar", "OtherFile", false},
	}
	for _, tc := range tests {
		got := match(tc.pattern, tc.p)
		if got != tc.want {
			t.Errorf("match(%q, %q) = %v; want %v", tc.pattern, tc.p, got, tc.want)
		}
	}
}

func TestParseGitAttributes_NoTrailingAttributes(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("*.foo text\n*.bar binary\n")},
	}
	o, err := parseGitAttributes(fsys)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if o.LanguageOverride("x.foo") != "" {
		t.Error("unexpected override on text-only attribute")
	}
}
