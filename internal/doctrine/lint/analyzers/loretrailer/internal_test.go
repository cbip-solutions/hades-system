package loretrailer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func initRepoForRun(t *testing.T, msg string) string {
	t.Helper()
	dir := t.TempDir()
	gitOK := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitOK("init", "-q", "-b", "main")
	gitOK("config", "user.email", "test@example.com")
	gitOK("config", "user.name", "test")
	gitOK("config", "commit.gpgsign", "false")
	if err := os.MkdirAll(filepath.Join(dir, "internal", "core"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "core", "hub.go"), []byte("package core\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitOK("add", "internal/core/hub.go")
	gitOK("commit", "-q", "-m", msg)
	return dir
}

func pseudoPass(t *testing.T) *analysis.Pass {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package p\n", parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var reports []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:     fset,
		Files:    []*ast.File{f},
		Analyzer: Analyzer,
		Report:   func(d analysis.Diagnostic) { reports = append(reports, d) },
	}
	t.Cleanup(func() { _ = reports })
	return pass
}

func TestRunOnceGuardSkipsSecondInvocation(t *testing.T) {
	ResetOnceForTest()
	dir := initRepoForRun(t, "feat(core): no lore")
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	pass1 := pseudoPass(t)
	pass2 := pseudoPass(t)
	_, err1 := run(pass1)
	_, err2 := run(pass2)
	if err1 != nil {
		t.Errorf("first run returned error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("second run (should be no-op) returned error: %v", err2)
	}
}

func TestRunDisabledIsNoOpViaPass(t *testing.T) {
	ResetOnceForTest()

	prev := enabledFlag
	enabledFlag = false
	defer func() { enabledFlag = prev }()

	dir := initRepoForRun(t, "feat(core): hub no lore")
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "x.go", "package p\n", parser.ParseComments)
	var reports []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:     fset,
		Files:    []*ast.File{f},
		Analyzer: Analyzer,
		Report:   func(d analysis.Diagnostic) { reports = append(reports, d) },
	}
	if _, err := run(pass); err != nil {
		t.Errorf("run disabled returned error: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("run disabled emitted %d reports; want 0", len(reports))
	}
}

func TestRunEnabledWithViolationEmitsReport(t *testing.T) {
	ResetOnceForTest()
	prev := enabledFlag
	enabledFlag = true
	defer func() { enabledFlag = prev }()

	prevHRF := highRiskFilesFlag
	highRiskFilesFlag = "internal/core/*.go"
	defer func() { highRiskFilesFlag = prevHRF }()

	dir := initRepoForRun(t, "feat(core): hub no lore")
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "x.go", "package p\n", parser.ParseComments)
	var reports []analysis.Diagnostic
	pass := &analysis.Pass{
		Fset:     fset,
		Files:    []*ast.File{f},
		Analyzer: Analyzer,
		Report:   func(d analysis.Diagnostic) { reports = append(reports, d) },
	}
	if _, err := run(pass); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(reports) == 0 {
		t.Error("run enabled with violation emitted 0 reports; want >= 1")
	}
}

func TestRunNoFilesAndDiagnosticsReturnsError(t *testing.T) {
	ResetOnceForTest()
	prev := enabledFlag
	enabledFlag = true
	defer func() { enabledFlag = prev }()

	prevHRF := highRiskFilesFlag
	highRiskFilesFlag = "internal/core/*.go"
	defer func() { highRiskFilesFlag = prevHRF }()

	dir := initRepoForRun(t, "feat(core): no lore")
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	pass := &analysis.Pass{
		Fset:     token.NewFileSet(),
		Files:    nil,
		Analyzer: Analyzer,
	}
	_, err := run(pass)
	if err == nil {
		t.Error("run with diagnostics + nil Files returned nil; want wrapped error")
	}
}

func TestRunNoFilesCleanHistoryReturnsNil(t *testing.T) {
	ResetOnceForTest()
	prev := enabledFlag
	enabledFlag = true
	defer func() { enabledFlag = prev }()

	prevHRF := highRiskFilesFlag
	highRiskFilesFlag = "internal/core/*.go"
	defer func() { highRiskFilesFlag = prevHRF }()

	dir := initRepoForRun(t, "feat(core): hub\n\nLore-Constraint: stays lock-free\n")
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	pass := &analysis.Pass{
		Fset:     token.NewFileSet(),
		Files:    nil,
		Analyzer: Analyzer,
	}
	_, err := run(pass)
	if err != nil {
		t.Errorf("run with clean history + nil Files returned err: %v; want nil", err)
	}
}

func TestRunResetOnceForTest(t *testing.T) {
	runOnce.Store(true)
	ResetOnceForTest()
	if runOnce.Load() {
		t.Error("ResetOnceForTest did not clear runOnce")
	}
}

func TestSplitBodyAndFiles(t *testing.T) {
	blob := "feat(x): subject\n\nLore-Constraint: c1\n\na/b.go\nc/d.go\n"
	body, files := splitBodyAndFiles(blob)
	if !strings.Contains(body, "Lore-Constraint") {
		t.Errorf("body %q should contain Lore-Constraint", body)
	}
	if len(files) != 2 || files[0] != "a/b.go" || files[1] != "c/d.go" {
		t.Errorf("files = %v; want [a/b.go c/d.go]", files)
	}
}

func TestSplitBodyAndFilesNoBlankSeparator(t *testing.T) {

	blob := "just-a-filename.go"
	body, files := splitBodyAndFiles(blob)
	if len(files) != 1 || files[0] != "just-a-filename.go" {
		t.Errorf("files = %v; want [just-a-filename.go]", files)
	}
	if body != "" {
		t.Errorf("body = %q; want empty (no separator → file run consumes entire blob)", body)
	}
}

func TestSplitCSVEmpty(t *testing.T) {
	if got := splitCSV(""); got != nil {
		t.Errorf("splitCSV(\"\") = %v; want nil", got)
	}
}

func TestSplitCSVMultiple(t *testing.T) {
	got := splitCSV("a/b.go , c/d.go, ,e/f.go")
	want := []string{"a/b.go", "c/d.go", "e/f.go"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV returned %d; want %d (%v)", len(got), len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("splitCSV[%d] = %q; want %q", i, got[i], w)
		}
	}
}

func TestMatchPrefixGlobSingleStar(t *testing.T) {
	if !matchPrefixGlob("internal/core/*", "internal/core/hub.go") {
		t.Error("matchPrefixGlob internal/core/* hub.go: want true")
	}
	if matchPrefixGlob("internal/core/*", "internal/core/sub/deep.go") {
		t.Error("matchPrefixGlob internal/core/* sub/deep.go: want false (/* does not recurse)")
	}
}

func TestMatchPrefixGlobDoubleStarFallthrough(t *testing.T) {
	if matchPrefixGlob("internal/core/hub.go", "internal/core/hub.go") {
		t.Error("matchPrefixGlob exact path (no trailing *): want false (not a glob suffix)")
	}
}

func TestTrailingFooterEmptyBody(t *testing.T) {
	if got := trailingFooter("   \n  \n"); got != nil {
		t.Errorf("trailingFooter empty: %v; want nil", got)
	}
}

func TestTrailingFooterFoldedContinuation(t *testing.T) {
	body := "feat(x): t\n\nLore-Constraint: stay pure\n  on one line\n"
	got := trailingFooter(body)
	if len(got) != 1 {
		t.Fatalf("trailingFooter returned %d lines; want 1", len(got))
	}
	if !strings.Contains(got[0], "on one line") {
		t.Errorf("continuation not folded; got %q", got[0])
	}
}

func TestIsFooterLineColonAtZero(t *testing.T) {
	if isFooterLine(": value") {
		t.Error("isFooterLine(': value') = true; want false (empty key)")
	}
}

func TestIsFooterLineInvalidKeyChar(t *testing.T) {
	if isFooterLine("Bad Key: value") {
		t.Error("isFooterLine('Bad Key: value') = true; want false (space in key)")
	}
}

func TestParseLoreCommitsSingleCommit(t *testing.T) {
	out := "abc123" + fieldSep + "feat(x): test" + fieldSep + "feat(x): test\n\nLore-Constraint: c1\n" + recSep + "\nsome/file.go\n"
	commits := parseLoreCommits(out)
	if len(commits) != 1 {
		t.Fatalf("parseLoreCommits: got %d commits; want 1", len(commits))
	}
	if commits[0].hash != "abc123" {
		t.Errorf("hash = %q; want abc123", commits[0].hash)
	}
	if len(commits[0].files) != 1 || commits[0].files[0] != "some/file.go" {
		t.Errorf("files = %v; want [some/file.go]", commits[0].files)
	}
	if !strings.Contains(commits[0].body, "Lore-Constraint") {
		t.Errorf("body %q should contain Lore-Constraint", commits[0].body)
	}
}

func TestParseLoreCommitsTwoCommits(t *testing.T) {
	out := "sha2" + fieldSep + "feat(b): b" + fieldSep + "feat(b): b\n" + recSep +
		"\nb.go\n\n" +
		"sha1" + fieldSep + "feat(a): a" + fieldSep + "feat(a): a\n" + recSep +
		"\na.go\n"
	commits := parseLoreCommits(out)
	if len(commits) != 2 {
		t.Fatalf("parseLoreCommits: got %d; want 2", len(commits))
	}
	if commits[0].hash != "sha2" || len(commits[0].files) == 0 || commits[0].files[0] != "b.go" {
		t.Errorf("commit[0] = %+v; want hash=sha2 files=[b.go]", commits[0])
	}
	if commits[1].hash != "sha1" || len(commits[1].files) == 0 || commits[1].files[0] != "a.go" {
		t.Errorf("commit[1] = %+v; want hash=sha1 files=[a.go]", commits[1])
	}
}

func TestParseLoreCommitsEmptyOutput(t *testing.T) {
	if got := parseLoreCommits(""); len(got) != 0 {
		t.Errorf("parseLoreCommits(\"\") = %d commits; want 0", len(got))
	}
	if got := parseLoreCommits("   \n  \n"); len(got) != 0 {
		t.Errorf("parseLoreCommits(blank) = %d commits; want 0", len(got))
	}
}
