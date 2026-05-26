package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func makeScannerFixture(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()

	mem := filepath.Join(root, "claude-projects", "internal-platform-x", "memory")
	if err := os.MkdirAll(mem, 0o755); err != nil {
		t.Fatalf("mkdir mem: %v", err)
	}
	for _, n := range []string{"feedback_a.md", "reference_b.md", "project_c.md"} {
		writeFile(t, filepath.Join(mem, n), "# Memory "+n+"\n\nbody")
	}
	writeFile(t, filepath.Join(mem, ".hidden.md"), "should be excluded")
	writeFile(t, filepath.Join(mem, "MEMORY.md"), "# index file")

	adr := filepath.Join(root, "internal-platform-x", "docs", "decisions")
	if err := os.MkdirAll(adr, 0o755); err != nil {
		t.Fatalf("mkdir adr: %v", err)
	}
	writeFile(t, filepath.Join(adr, "0001-foo.md"), "# ADR 0001")
	writeFile(t, filepath.Join(adr, "0002-bar.md"), "# ADR 0002")

	spec := filepath.Join(root, "internal-platform-x", "docs", "superpowers", "specs")
	plan := filepath.Join(root, "internal-platform-x", "docs", "superpowers", "plans")
	for _, d := range []string{spec, plan} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", d, err)
		}
	}
	writeFile(t, filepath.Join(spec, "design.md"), "# Spec")
	writeFile(t, filepath.Join(plan, "phase-a.md"), "# Plan A")

	writeFile(t, filepath.Join(root, "internal-platform-x", "HANDOFF.md"), "# Handoff")

	research := filepath.Join(root, "research-cache", "global")
	if err := os.MkdirAll(research, 0o755); err != nil {
		t.Fatalf("mkdir research: %v", err)
	}
	writeFile(t, filepath.Join(research, "topic-1.md"), "# Research")

	return root
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestScanMemoryDir(t *testing.T) {
	root := makeScannerFixture(t)
	src := ScannerSource{
		Kind:         FileTypeMemory,
		Root:         filepath.Join(root, "claude-projects", "internal-platform-x", "memory"),
		ProjectID:    "abc123",
		ProjectAlias: "internal-platform-x",
		Recursive:    true,
	}
	s := NewScanner(MaxIndexableBytes)
	files, errs, err := s.Scan([]ScannerSource{src})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(errs) != 0 {
		t.Errorf("ScannerErrors = %v, want 0", errs)
	}
	if len(files) != 4 {
		t.Errorf("files = %d, want 4 (excluding .hidden.md): %v", len(files), files)
	}
	for _, f := range files {
		if strings.Contains(f.Path, ".hidden") {
			t.Errorf("scan included hidden file %s", f.Path)
		}
		if f.Kind != FileTypeMemory {
			t.Errorf("kind = %v, want FileTypeMemory", f.Kind)
		}
		if f.ProjectID != "abc123" {
			t.Errorf("ProjectID = %q, want abc123", f.ProjectID)
		}
		if f.ProjectAlias != "internal-platform-x" {
			t.Errorf("ProjectAlias = %q, want internal-platform-x", f.ProjectAlias)
		}
		if f.Size <= 0 {
			t.Errorf("Size = %d, want > 0", f.Size)
		}
		if f.ModTime <= 0 {
			t.Errorf("ModTime = %d, want > 0", f.ModTime)
		}
	}
}

func TestScanIsLexSortedDeterministic(t *testing.T) {
	root := makeScannerFixture(t)
	src := ScannerSource{
		Kind:      FileTypeMemory,
		Root:      filepath.Join(root, "claude-projects", "internal-platform-x", "memory"),
		ProjectID: "abc", Recursive: true,
	}
	s := NewScanner(MaxIndexableBytes)
	got1, _, _ := s.Scan([]ScannerSource{src})
	got2, _, _ := s.Scan([]ScannerSource{src})
	if len(got1) != len(got2) {
		t.Fatalf("non-deterministic count: %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if got1[i].Path != got2[i].Path {
			t.Errorf("non-deterministic order at %d: %q vs %q", i, got1[i].Path, got2[i].Path)
		}
	}

	sorted := make([]string, len(got1))
	for i, f := range got1 {
		sorted[i] = f.Path
	}
	prev := ""
	for _, p := range sorted {
		if prev != "" && !sort.StringsAreSorted([]string{prev, p}) {
			t.Errorf("paths not lex-sorted: %q before %q", prev, p)
		}
		prev = p
	}
}

// TestScanExcludesOverSizedFiles verifies oversize files are reported via
// ScannerError + ErrFileTooLarge but do NOT abort the scan.
func TestScanExcludesOverSizedFiles(t *testing.T) {
	root := t.TempDir()
	memDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(memDir, "small.md"), "ok")
	big := strings.Repeat("x", 10*1024*1024)
	writeFile(t, filepath.Join(memDir, "big.md"), big)

	s := NewScanner(5 * 1024 * 1024)
	files, errs, _ := s.Scan([]ScannerSource{{
		Kind: FileTypeMemory, Root: memDir, ProjectID: "p", Recursive: true,
	}})
	if len(files) != 1 || !strings.HasSuffix(files[0].Path, "small.md") {
		t.Errorf("expected only small.md, got %v", files)
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 oversize error, got %d: %v", len(errs), errs)
	}
	if !errors.Is(errs[0].Err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got %v", errs[0].Err)
	}
}

func TestScanHandoffSingleFile(t *testing.T) {
	root := makeScannerFixture(t)
	src := ScannerSource{
		Kind:      FileTypeHandoff,
		Root:      filepath.Join(root, "internal-platform-x", "HANDOFF.md"),
		ProjectID: "abc", ProjectAlias: "internal-platform-x",
		Recursive: false,
	}
	s := NewScanner(MaxIndexableBytes)
	files, _, err := s.Scan([]ScannerSource{src})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0].Path, "HANDOFF.md") {
		t.Errorf("HANDOFF.md scan = %v, want 1 file", files)
	}
	if files[0].Kind != FileTypeHandoff {
		t.Errorf("kind = %v, want FileTypeHandoff", files[0].Kind)
	}
}

func TestScanResearchCacheGlobalNoProject(t *testing.T) {
	root := makeScannerFixture(t)
	src := ScannerSource{
		Kind:         FileTypeResearch,
		Root:         filepath.Join(root, "research-cache", "global"),
		ProjectID:    "",
		ProjectAlias: "",
		Recursive:    true,
	}
	s := NewScanner(MaxIndexableBytes)
	files, _, _ := s.Scan([]ScannerSource{src})
	if len(files) != 1 {
		t.Fatalf("research files = %d, want 1", len(files))
	}
	if files[0].ProjectID != "" {
		t.Errorf("global research has ProjectID = %q, want empty", files[0].ProjectID)
	}
	if files[0].Kind != FileTypeResearch {
		t.Errorf("kind = %v, want FileTypeResearch", files[0].Kind)
	}
}

func TestScanMissingRootReturnsError(t *testing.T) {
	src := ScannerSource{
		Kind:      FileTypeMemory,
		Root:      "/nonexistent/path/abcdef",
		Recursive: true,
	}
	s := NewScanner(MaxIndexableBytes)
	_, errs, err := s.Scan([]ScannerSource{src})
	if err != nil {
		t.Fatalf("Scan returned hard error %v; should soft-error per source", err)
	}
	if len(errs) == 0 {
		t.Errorf("expected ≥1 ScannerError for missing root, got 0")
	}
}

func TestScanMultipleSourcesAggregates(t *testing.T) {
	root := makeScannerFixture(t)
	srcs := []ScannerSource{
		{Kind: FileTypeMemory, Root: filepath.Join(root, "claude-projects", "internal-platform-x", "memory"), ProjectID: "p", Recursive: true},
		{Kind: FileTypeADR, Root: filepath.Join(root, "internal-platform-x", "docs", "decisions"), ProjectID: "p", Recursive: true},
		{Kind: FileTypeSpec, Root: filepath.Join(root, "internal-platform-x", "docs", "superpowers", "specs"), ProjectID: "p", Recursive: true},
		{Kind: FileTypePlan, Root: filepath.Join(root, "internal-platform-x", "docs", "superpowers", "plans"), ProjectID: "p", Recursive: true},
		{Kind: FileTypeHandoff, Root: filepath.Join(root, "internal-platform-x", "HANDOFF.md"), ProjectID: "p", Recursive: false},
		{Kind: FileTypeResearch, Root: filepath.Join(root, "research-cache", "global"), Recursive: true},
	}
	s := NewScanner(MaxIndexableBytes)
	files, errs, _ := s.Scan(srcs)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	counts := map[FileType]int{}
	for _, f := range files {
		counts[f.Kind]++
	}
	if counts[FileTypeMemory] != 4 {
		t.Errorf("memory count = %d, want 4", counts[FileTypeMemory])
	}
	if counts[FileTypeADR] != 2 {
		t.Errorf("adr count = %d, want 2", counts[FileTypeADR])
	}
	if counts[FileTypeSpec] != 1 {
		t.Errorf("spec count = %d, want 1", counts[FileTypeSpec])
	}
	if counts[FileTypePlan] != 1 {
		t.Errorf("plan count = %d, want 1", counts[FileTypePlan])
	}
	if counts[FileTypeHandoff] != 1 {
		t.Errorf("handoff count = %d, want 1", counts[FileTypeHandoff])
	}
	if counts[FileTypeResearch] != 1 {
		t.Errorf("research count = %d, want 1", counts[FileTypeResearch])
	}
	if len(files) != 10 {
		t.Errorf("total files = %d, want 10", len(files))
	}
}

func TestScannerSourceOrderPreserved(t *testing.T) {
	root := makeScannerFixture(t)
	srcs := []ScannerSource{

		{Kind: FileTypePlan, Root: filepath.Join(root, "internal-platform-x", "docs", "superpowers", "plans"), ProjectID: "p", Recursive: true},
		{Kind: FileTypeMemory, Root: filepath.Join(root, "claude-projects", "internal-platform-x", "memory"), ProjectID: "p", Recursive: true},
	}
	s := NewScanner(MaxIndexableBytes)
	files, _, _ := s.Scan(srcs)
	if len(files) != 5 {
		t.Fatalf("files = %d, want 5", len(files))
	}

	if files[0].Kind != FileTypePlan {
		t.Errorf("files[0].Kind = %v, want FileTypePlan (per-source order)", files[0].Kind)
	}
	for i := 1; i < 5; i++ {
		if files[i].Kind != FileTypeMemory {
			t.Errorf("files[%d].Kind = %v, want FileTypeMemory (per-source order)", i, files[i].Kind)
		}
	}
}

func TestScanZeroMaxBytesUsesDefault(t *testing.T) {
	s := NewScanner(0)
	if s.maxBytes != MaxIndexableBytes {
		t.Errorf("NewScanner(0).maxBytes = %d, want %d (default)", s.maxBytes, MaxIndexableBytes)
	}
	s2 := NewScanner(-1)
	if s2.maxBytes != MaxIndexableBytes {
		t.Errorf("NewScanner(-1).maxBytes = %d, want %d (default)", s2.maxBytes, MaxIndexableBytes)
	}
}

func TestScanSingleFileOversize(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", 10*1024*1024)
	bigPath := filepath.Join(root, "HANDOFF.md")
	writeFile(t, bigPath, big)

	s := NewScanner(5 * 1024 * 1024)
	files, errs, _ := s.Scan([]ScannerSource{{
		Kind: FileTypeHandoff, Root: bigPath, Recursive: false,
	}})
	if len(files) != 0 {
		t.Errorf("expected no files (oversize handoff), got %d", len(files))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !errors.Is(errs[0].Err, ErrFileTooLarge) {
		t.Errorf("expected ErrFileTooLarge, got %v", errs[0].Err)
	}
}

func TestScanSingleFileExpectedFileGotDir(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	s := NewScanner(MaxIndexableBytes)
	files, errs, _ := s.Scan([]ScannerSource{{
		Kind: FileTypeHandoff, Root: subdir, Recursive: false,
	}})
	if len(files) != 0 {
		t.Errorf("expected no files when Recursive=false on a dir, got %d", len(files))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if !strings.Contains(errs[0].Err.Error(), "expected file") {
		t.Errorf("error %q lacks 'expected file' anchor", errs[0].Err.Error())
	}
}

func TestScannerErrorErrorFormat(t *testing.T) {
	se := ScannerError{
		Source: ScannerSource{Kind: FileTypeMemory, Root: "/some/root"},
		Path:   "/some/root/big.md",
		Err:    ErrFileTooLarge,
	}
	got := se.Error()
	if !strings.Contains(got, "memory") {
		t.Errorf("Error() = %q, missing source kind 'memory'", got)
	}
	if !strings.Contains(got, "/some/root/big.md") {
		t.Errorf("Error() = %q, missing path", got)
	}
	if !strings.Contains(got, "exceeds size cap") {
		t.Errorf("Error() = %q, missing wrapped err message", got)
	}
}

func TestScanIgnoresHiddenSubdirs(t *testing.T) {
	root := t.TempDir()
	visible := filepath.Join(root, "visible.md")
	writeFile(t, visible, "# visible")

	hiddenDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatalf("mkdir hidden: %v", err)
	}
	writeFile(t, filepath.Join(hiddenDir, "config.md"), "# inside .git")

	obsidianDir := filepath.Join(root, ".obsidian")
	if err := os.MkdirAll(obsidianDir, 0o755); err != nil {
		t.Fatalf("mkdir obsidian: %v", err)
	}
	writeFile(t, filepath.Join(obsidianDir, "workspace.md"), "# inside .obsidian")

	s := NewScanner(MaxIndexableBytes)
	files, _, _ := s.Scan([]ScannerSource{{
		Kind: FileTypeMemory, Root: root, ProjectID: "p", Recursive: true,
	}})
	if len(files) != 1 {
		t.Errorf("expected only visible.md, got %d files: %v", len(files), files)
	}
	for _, f := range files {
		if strings.Contains(f.Path, ".git") || strings.Contains(f.Path, ".obsidian") {
			t.Errorf("scan included file from hidden subdir: %s", f.Path)
		}
	}
}

func TestScanOnlyMarkdownFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "doc.md"), "# md")
	writeFile(t, filepath.Join(root, "script.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(root, "readme.txt"), "txt")
	writeFile(t, filepath.Join(root, "config.json"), "{}")

	s := NewScanner(MaxIndexableBytes)
	files, _, _ := s.Scan([]ScannerSource{{
		Kind: FileTypeMemory, Root: root, ProjectID: "p", Recursive: true,
	}})
	if len(files) != 1 {
		t.Errorf("expected 1 .md file, got %d: %v", len(files), files)
	}
	if !strings.HasSuffix(files[0].Path, "doc.md") {
		t.Errorf("only doc.md should be indexed, got %s", files[0].Path)
	}
}

func TestScanSingleFileMissingPath(t *testing.T) {
	src := ScannerSource{
		Kind:      FileTypeHandoff,
		Root:      "/nonexistent/path/handoff.md",
		Recursive: false,
	}
	s := NewScanner(MaxIndexableBytes)
	files, errs, err := s.Scan([]ScannerSource{src})
	if err != nil {
		t.Fatalf("Scan returned hard error %v; should soft-error per source", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no files for missing single-file source, got %d", len(files))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 stat error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Err.Error(), "stat") {
		t.Errorf("expected stat-prefixed error, got %v", errs[0].Err)
	}
}

func TestScannerErrorUnwrap(t *testing.T) {
	se := ScannerError{
		Source: ScannerSource{Kind: FileTypeMemory, Root: "/r"},
		Path:   "/r/big.md",
		Err:    ErrFileTooLarge,
	}
	if got := se.Unwrap(); !errors.Is(got, ErrFileTooLarge) {
		t.Errorf("Unwrap() = %v, want ErrFileTooLarge", got)
	}

	var asErr error = se
	if !errors.Is(asErr, ErrFileTooLarge) {
		t.Errorf("errors.Is(ScannerError, ErrFileTooLarge) = false, want true (Unwrap chain broken)")
	}
}

func TestScanWalkErrPropagated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 does not block WalkDir; skip walk-err propagation test")
	}
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "sibling.md"), "ok")

	forbidden := filepath.Join(root, "forbidden")
	if err := os.MkdirAll(forbidden, 0o755); err != nil {
		t.Fatalf("mkdir forbidden: %v", err)
	}
	writeFile(t, filepath.Join(forbidden, "secret.md"), "secret")
	if err := os.Chmod(forbidden, 0o000); err != nil {
		t.Fatalf("chmod forbidden: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(forbidden, 0o755) })

	s := NewScanner(MaxIndexableBytes)
	files, errs, err := s.Scan([]ScannerSource{{
		Kind: FileTypeMemory, Root: root, ProjectID: "p", Recursive: true,
	}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	foundSibling := false
	for _, f := range files {
		if strings.HasSuffix(f.Path, "sibling.md") {
			foundSibling = true
		}
	}
	if !foundSibling {
		t.Errorf("walk aborted instead of continuing past forbidden subtree; files=%v", files)
	}

	if len(errs) == 0 {
		t.Errorf("expected ≥1 ScannerError for permission-denied subdir, got 0")
	}
}

func TestScanDirEntryInfoErrorPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "doc.md"), "# md")

	orig := dirEntryInfoFn
	t.Cleanup(func() { dirEntryInfoFn = orig })
	stubErr := errors.New("simulated race-window dirent removal")
	dirEntryInfoFn = func(d os.DirEntry) (os.FileInfo, error) {
		return nil, stubErr
	}

	s := NewScanner(MaxIndexableBytes)
	files, errs, err := s.Scan([]ScannerSource{{
		Kind: FileTypeMemory, Root: root, ProjectID: "p", Recursive: true,
	}})
	if err != nil {
		t.Fatalf("Scan returned hard error %v; should soft-error per source", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files (Info() failed for the only candidate), got %d", len(files))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 ScannerError from Info() failure, got %d: %v", len(errs), errs)
	}
	if !errors.Is(errs[0].Err, stubErr) {
		t.Errorf("expected wrapped stubErr, got %v", errs[0].Err)
	}
	if !strings.Contains(errs[0].Err.Error(), "info:") {
		t.Errorf("expected 'info:' prefix on wrapped error, got %v", errs[0].Err)
	}
}

func TestScanEmptySourcesReturnsEmpty(t *testing.T) {
	s := NewScanner(MaxIndexableBytes)
	files, errs, err := s.Scan(nil)
	if err != nil {
		t.Errorf("Scan(nil): err = %v, want nil", err)
	}
	if len(files) != 0 {
		t.Errorf("Scan(nil): files = %v, want empty", files)
	}
	if len(errs) != 0 {
		t.Errorf("Scan(nil): errs = %v, want empty", errs)
	}

	files2, errs2, err2 := s.Scan([]ScannerSource{})
	if err2 != nil {
		t.Errorf("Scan([]): err = %v, want nil", err2)
	}
	if len(files2) != 0 {
		t.Errorf("Scan([]): files = %v, want empty", files2)
	}
	if len(errs2) != 0 {
		t.Errorf("Scan([]): errs = %v, want empty", errs2)
	}
}
