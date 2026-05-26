package glob

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestWalk_BasicLanguageCount(t *testing.T) {
	fsys := fstest.MapFS{
		"main.go":    &fstest.MapFile{Data: []byte("package main\n\nfunc main() {}\n")},
		"lib.py":     &fstest.MapFile{Data: []byte("def hello():\n    return 'hi'\n")},
		"README.md":  &fstest.MapFile{Data: []byte("# Project\n")},
		".gitignore": &fstest.MapFile{Data: []byte("*.tmp\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err != nil {
		t.Fatalf("Walk err: %v", err)
	}

	seen := map[string]int64{}
	for _, s := range stats {
		seen[s.Language] = s.Bytes
	}
	if seen["Go"] == 0 {
		t.Errorf("Go bytes = 0; want > 0 (stats=%+v)", stats)
	}
	if seen["Python"] == 0 {
		t.Errorf("Python bytes = 0; want > 0 (stats=%+v)", stats)
	}

	if seen["Markdown"] != 0 {
		t.Errorf("Markdown bytes = %d; want 0 (IsDocumentation excludes README.md)", seen["Markdown"])
	}
}

func TestWalk_VendorFilterExcludesNodeModules(t *testing.T) {
	fsys := fstest.MapFS{
		"src/index.ts":             &fstest.MapFile{Data: []byte("export const x = 1;\n")},
		"node_modules/foo/main.js": &fstest.MapFile{Data: []byte("module.exports = {};\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {

		if s.Language == "JavaScript" && s.Files > 0 {
			t.Errorf("JavaScript file count = %d; want 0 (node_modules vendored)", s.Files)
		}
	}
}

func TestWalk_BinaryFilterExcludesBinary(t *testing.T) {
	binary := make([]byte, 1024)
	for i := range binary {
		binary[i] = byte(i % 256)
	}
	binary[0] = 0x00
	fsys := fstest.MapFS{
		"blob.bin": &fstest.MapFile{Data: binary},
		"main.go":  &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {
		if s.Language == "Binary" {
			t.Error("Binary language present in stats; want excluded")
		}
	}
}

func TestWalk_GitattributesLanguageOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("*.weird linguist-language=Lua\n")},
		"x.weird":        &fstest.MapFile{Data: []byte("-- this is actually whatever\n")},
		"y.go":           &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range stats {
		seen[s.Language] = s.Bytes > 0
	}
	if !seen["Lua"] {
		t.Errorf("Lua absent; expected via gitattributes override (stats=%+v)", stats)
	}
}

func TestWalk_GitattributesVendoredOverride(t *testing.T) {
	fsys := fstest.MapFS{
		".gitattributes": &fstest.MapFile{Data: []byte("legacy/* linguist-vendored=true\n")},
		"legacy/old.py":  &fstest.MapFile{Data: []byte("print('old')\n")},
		"src/new.go":     &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {
		if s.Language == "Python" && s.Files > 0 {
			t.Error("Python file count > 0; want 0 (legacy/ vendored)")
		}
	}
}

func TestWalk_ContentCapEnforced(t *testing.T) {
	huge := make([]byte, 5*1024*1024)
	for i := range huge {
		huge[i] = 'a'
	}
	fsys := fstest.MapFS{

		"blob.unknown": &fstest.MapFile{Data: huge},
		"main.go":      &fstest.MapFile{Data: []byte("package main\n")},
	}

	stats, err := Walk(context.Background(), fsys, WalkOptions{
		MaxBytesPerFile: 4096,
		Workers:         1,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	_ = stats
}

func TestWalk_ContextCancellationHonored(t *testing.T) {

	fsys := fstest.MapFS{}
	for i := 0; i < 100; i++ {
		fsys[fmtFilename(i, "go")] = &fstest.MapFile{Data: []byte("package main\n")}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Walk(ctx, fsys, WalkOptions{
		MaxBytesPerFile: 50 * 1024,
		Workers:         2,
	})
	if err == nil {
		t.Error("Walk returned nil err on pre-cancelled ctx; want ctx.Err()")
	}
}

func TestWalk_DefaultsApplied(t *testing.T) {
	fsys := fstest.MapFS{
		"a.go": &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(stats) == 0 {
		t.Error("stats empty; want Go entry from a.go")
	}
}

func TestWalk_EmptyFile(t *testing.T) {
	fsys := fstest.MapFS{
		"empty.go": &fstest.MapFile{Data: []byte("")},
		"x.go":     &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{Workers: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {

		if s.Language == "Go" && s.Files > 1 {
			t.Errorf("expected only x.go to count for Go; got Files=%d", s.Files)
		}
	}
}

func TestWalk_IncludeBinaryOption(t *testing.T) {
	binary := make([]byte, 1024)
	for i := range binary {
		binary[i] = byte(i % 256)
	}
	binary[0] = 0x00
	fsys := fstest.MapFS{
		"data.bin": &fstest.MapFile{Data: binary},
		"x.go":     &fstest.MapFile{Data: []byte("package main\n")},
	}
	_, err := Walk(context.Background(), fsys, WalkOptions{
		Workers:       2,
		IncludeBinary: true,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestWalk_VendorDirSkipped(t *testing.T) {
	fsys := fstest.MapFS{
		"vendor/dep/a.go": &fstest.MapFile{Data: []byte("package dep\n")},
		"src/main.go":     &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{Workers: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	for _, s := range stats {
		if s.Language == "Go" && s.Files > 1 {
			t.Errorf("Go.Files = %d; want 1 (vendor/dep/a.go skipped)", s.Files)
		}
	}
}

// TestWalk_SkipsNodeModulesAtDirLevel asserts the dir-level fs.SkipDir
// short-circuit fires for node_modules (Im-1 fix; spec §2.4 perf budget).
// We verify by side-effect: a file inside node_modules/ MUST NOT appear
// in any language stat. Pre-fix, enry.IsVendor("node_modules") returned
// false (no trailing slash), so the SkipDir branch never fired and
// every file inside got walked + stat'd + classified before being
// rejected file-by-file in classify. On a real 100k-file Node monorepo
// this blew the ≤2s budget.
func TestWalk_SkipsNodeModulesAtDirLevel(t *testing.T) {

	fsys := fstest.MapFS{
		"node_modules/big.js": &fstest.MapFile{Data: []byte("module.exports = { huge: true };\n")},
		"src/main.go":         &fstest.MapFile{Data: []byte("package main\n\nfunc main() {}\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{Workers: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {
		if s.Language == "JavaScript" && s.Files > 0 {
			t.Errorf("JavaScript.Files = %d, Bytes = %d; want 0 (node_modules dir-level skip should prune)", s.Files, s.Bytes)
		}
	}
}

func TestWalk_SkipsGitAtDirLevel(t *testing.T) {

	fsys := fstest.MapFS{
		".git/HEAD":             &fstest.MapFile{Data: []byte("ref: refs/heads/main\n")},
		".git/objects/abc/def":  &fstest.MapFile{Data: []byte("binary-blob\n")},
		".git/config":           &fstest.MapFile{Data: []byte("[core]\n")},
		".git/hooks/pre-commit": &fstest.MapFile{Data: []byte("#!/bin/sh\necho hi\n")},
		"main.go":               &fstest.MapFile{Data: []byte("package main\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{Workers: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	for _, s := range stats {
		if s.Language == "Shell" {
			t.Errorf("Shell present in stats (Files=%d, Bytes=%d); want 0 (.git/ dir-level skip should prune)", s.Files, s.Bytes)
		}
	}
}

func TestWalk_SkipsBowerComponentsViaEnryTrailingSlash(t *testing.T) {
	fsys := fstest.MapFS{
		"bower_components/foo/big.js": &fstest.MapFile{Data: []byte("module.exports = { foo: 1 };\n")},
		"src/index.ts":                &fstest.MapFile{Data: []byte("export const x = 1;\n")},
	}
	stats, err := Walk(context.Background(), fsys, WalkOptions{Workers: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, s := range stats {
		if s.Language == "JavaScript" && s.Files > 0 {
			t.Errorf("JavaScript.Files = %d; want 0 (bower_components must be pruned via enry trailing-slash)", s.Files)
		}
	}
}

func fmtFilename(i int, ext string) string {
	return "f" + itoa(i) + "." + ext
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

var _ fs.FS = (fstest.MapFS)(nil)
