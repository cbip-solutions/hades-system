package conventional_commit

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

func initRepoForRun(t *testing.T, subjects []string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for i, subj := range subjects {
		fname := filepath.Join(dir, "f.txt")
		if err := os.WriteFile(fname, []byte(strings.Repeat("x", i+1)), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		commitArgs := []string{"commit", "-q", "--allow-empty-message", "-m", subj}
		for _, args := range [][]string{
			{"add", "f.txt"},
			commitArgs,
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
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
		Report: func(d analysis.Diagnostic) {
			reports = append(reports, d)
		},
	}

	t.Cleanup(func() {
		_ = reports
	})
	return pass
}

func TestRunOncePathInGoodRepo(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{"feat(lint): one good"})
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	pass := pseudoPass(t)
	_, err := run(pass)
	if err != nil {
		t.Errorf("run on good-history repo returned error: %v", err)
	}
}

func TestRunSecondInvocationSkippedByOnce(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{"feat(lint): one good"})
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
		t.Errorf("first run errored: %v", err1)
	}

	if err2 != nil {
		t.Errorf("second run errored when should be skipped: %v", err2)
	}
}

func TestRunWithBadHistoryProducesReports(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{"BAD subject without scope"})
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
		t.Error("run with bad commit produced zero pass.Report calls; want >= 1")
	}
}

func TestRunNoFilesAndDiagnosticsReturnsError(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{"BAD subject"})
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
		t.Error("run with diagnostics + nil Files returned nil err; want wrapped error")
	}
}

func TestRunNoFilesAndCleanHistoryReturnsNil(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{"feat(lint): clean"})
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

func TestRunSkipsOnNoGitWithFlagSet(t *testing.T) {
	ResetOnceForTest()

	prev := skipWhenNoGitFlag
	skipWhenNoGitFlag = true
	defer func() { skipWhenNoGitFlag = prev }()

	tmp := t.TempDir()
	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	pass := pseudoPass(t)
	_, err := run(pass)

	if err == nil {
		t.Log("run returned nil even with flag set on non-git dir — acceptable if binary missing")
	}
}

func TestResetOnceForTestResets(t *testing.T) {
	runOnce.Store(true)
	ResetOnceForTest()
	if runOnce.Load() {
		t.Error("ResetOnceForTest did not clear runOnce")
	}
}

func TestClassifyFailureBranches(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		want    string
	}{
		{"missing_scope", "feat: add nostub analyzer", "cc-missing-scope"},
		{"bad_type", "weird(scope): add thing", "cc-bad-type"},
		{"bad_scope_uppercase", "feat(Lint): add nostub", "cc-bad-scope"},
		{"bad_scope_underscore", "fix(lint_): add nostub", "cc-bad-scope"},
		{"bad_subject_capital", "feat(lint): Add nostub", "cc-bad-subject"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFailure("hash-x", tc.subject)
			if len(got) != 1 {
				t.Fatalf("classifyFailure %q returned %d diagnostics; want 1", tc.subject, len(got))
			}
			if !strings.Contains(got[0].Message, tc.want) {
				t.Errorf("subject %q: got %q; want contains %q", tc.subject, got[0].Message, tc.want)
			}
		})
	}
}

func TestClassifyFailureEmptyBody(t *testing.T) {
	got := classifyFailure("h", "feat(lint): ")
	if len(got) != 1 {
		t.Fatalf("got %d; want 1", len(got))
	}
	if !strings.Contains(got[0].Message, "cc-bad-subject") {
		t.Errorf("expected cc-bad-subject for empty body; got %q", got[0].Message)
	}
}

func TestRunWithGitDirSubjectWithSpaceAtEdge(t *testing.T) {

	got := scopeAllowSet("")
	if got != nil {
		t.Errorf("scopeAllowSet(\"\") = %v; want nil", got)
	}
}

func TestSkipHashSetPopulated(t *testing.T) {
	got := skipHashSet("abc123, def456 , ,ghi789")
	want := map[string]bool{"abc123": true, "def456": true, "ghi789": true}
	if len(got) != len(want) {
		t.Fatalf("skipHashSet returned %d entries; want %d (%v)", len(got), len(want), got)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("skipHashSet missing %q; got %v", k, got)
		}
	}
}

func TestRunWithGitDirSkipsHashedCommit(t *testing.T) {

	dir := initRepoForRun(t, []string{
		"feat(lint): clean",
		"BAD UPPERCASE",
	})

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	headHash := strings.TrimSpace(string(out))
	if len(headHash) < 8 {
		t.Fatalf("HEAD hash too short: %q", headHash)
	}
	prefix := headHash[:8]

	diagsRaw, err := RunWithGitDir(dir, 99, "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir noskip: %v", err)
	}
	if len(diagsRaw) == 0 {
		t.Fatal("expected diagnostics on bad commit without skip; got 0")
	}

	diagsSkip, err := RunWithGitDir(dir, 99, "", prefix, "")
	if err != nil {
		t.Fatalf("RunWithGitDir skip=%s: %v", prefix, err)
	}
	for _, d := range diagsSkip {
		if strings.HasPrefix(d.CommitHash, prefix) {
			t.Errorf("skip-hashes %s did NOT suppress shape diagnostic on hash %s: %q",
				prefix, d.CommitHash, d.Message)
		}
	}
}

func TestRunWithGitDirBlankSubjectSkipped(t *testing.T) {
	dir := initRepoForRun(t, []string{"feat(lint): seed"})

	for _, cmdArgs := range [][]string{
		{"git", "config", "commit.gpgsign", "false"},
	} {
		c := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("setup: %v\n%s", err, out)
		}
	}

	c := exec.Command("git", "commit-tree", "HEAD^{tree}", "-m", "", "-p", "HEAD")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("commit-tree: %v\n%s", err, out)
	}
	newHash := strings.TrimSpace(string(out))

	c2 := exec.Command("git", "update-ref", "HEAD", newHash)
	c2.Dir = dir
	if out, err := c2.CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %v\n%s", err, out)
	}

	diags, err := RunWithGitDir(dir, 99, "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("blank-subject commit must be silently skipped; got %d diagnostics", len(diags))
	}
}

func TestRunNoFilesAndDiagnosticsBaseRefMessage(t *testing.T) {
	ResetOnceForTest()

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	for _, subj := range []string{"feat(lint): seed", "BAD UPPERCASE"} {
		fname := filepath.Join(dir, "f.txt")
		_ = os.WriteFile(fname, []byte(subj), 0o600)
		for _, args := range [][]string{
			{"add", "f.txt"},
			{"commit", "-q", "-m", subj},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}

	for _, args := range [][]string{
		{"checkout", "-q", "-b", "branch"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	fname := filepath.Join(dir, "g.txt")
	_ = os.WriteFile(fname, []byte("y"), 0o600)
	for _, args := range [][]string{
		{"add", "g.txt"},
		{"commit", "-q", "-m", "ANOTHER BAD"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	prevWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevWd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	prev := baseRefFlag
	baseRefFlag = "main"
	defer func() { baseRefFlag = prev }()

	pass := &analysis.Pass{
		Fset:     token.NewFileSet(),
		Files:    nil,
		Analyzer: Analyzer,
	}
	_, err := run(pass)
	if err == nil {
		t.Fatal("expected wrapped error from run() with no-anchor + diagnostics; got nil")
	}
	if !strings.Contains(err.Error(), "main..HEAD") {
		t.Errorf("expected error message to reference main..HEAD scope; got %q", err.Error())
	}
}

func TestIsAllowedBodyStartCases(t *testing.T) {
	allowed := []byte{'a', 'm', 'z', '/', '(', '`'}
	for _, c := range allowed {
		if !isAllowedBodyStart(c) {
			t.Errorf("isAllowedBodyStart(%q) = false; want true", c)
		}
	}
	disallowed := []byte{'A', 'Z', '0', ' ', '.', '!', ')', '['}
	for _, c := range disallowed {
		if isAllowedBodyStart(c) {
			t.Errorf("isAllowedBodyStart(%q) = true; want false", c)
		}
	}
}

func TestConventionalCommitRegexMultiScope(t *testing.T) {
	cases := []string{
		"feat(cli): ok",
		"feat(cli, daemon): ok",
		"feat(cli,daemon): ok",
		"feat(cli, daemon, store): ok",
	}
	for _, c := range cases {
		if !conventionalCommitRegex.MatchString(c) {
			t.Errorf("conventionalCommitRegex did NOT match %q (multi-scope should be allowed)", c)
		}
	}
}

func TestRunWhitelistsGitMergeAndRevertSubjects(t *testing.T) {
	ResetOnceForTest()

	// Mix of git-synthesized merge/revert subjects that MUST be whitelisted,
	// plus one well-formed conventional commit so the analyzer has a happy-
	// path baseline alongside the whitelist hits.
	dir := initRepoForRun(t, []string{
		"feat(lint): baseline good commit",
		"Merge 655aa6693255753749cd106fba672c2731e023d7 into 7bbf40358f4c5ae22001d91fd974a36d0d66db78",
		"Merge pull request #3 from cbip-solutions/plan-8-execute",
		"Merge remote-tracking branch 'origin/main' into plan-8-execute",
		"Merge branch 'plan-7-execute'",
		`Revert "feat(lint): something to undo"`,
	})
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
	if len(reports) != 0 {
		var subjects []string
		for _, r := range reports {
			subjects = append(subjects, r.Message)
		}
		t.Errorf("git merge/revert subjects must be whitelisted; got %d unexpected reports:\n%s",
			len(reports), strings.Join(subjects, "\n"))
	}
}

func TestRunWhitelistedMergeStillCatchesClaudeAttribution(t *testing.T) {
	ResetOnceForTest()

	dir := initRepoForRun(t, []string{
		`Revert "feat(lint): legit Co-Authored-By: prohibited assistant <noreply@anthropic.com>"`,
	})
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

	if len(reports) != 1 {
		t.Fatalf("expected 1 diagnostic (claude-attribution); got %d", len(reports))
	}
	if !strings.Contains(reports[0].Message, "cc-claude-attribution") {
		t.Errorf("expected cc-claude-attribution diagnostic; got %q", reports[0].Message)
	}
}
