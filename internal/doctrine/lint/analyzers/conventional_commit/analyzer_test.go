package conventional_commit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	cc "github.com/cbip-solutions/hades-system/internal/doctrine/lint/analyzers/conventional_commit"
)

func readSubjects(t *testing.T, dir string) []string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "analysistest", "testdata", "src", "conventional-commit", dir, "subjects.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func initRepoWithSubjects(t *testing.T, subjects []string) string {
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

		commitArgs := []string{"commit", "-q", "--allow-empty-message"}
		if subj != "" {
			commitArgs = append(commitArgs, "-m", subj)
		} else {
			commitArgs = append(commitArgs, "-m", "")
		}
		for _, args := range [][]string{
			{"add", "f.txt"},
			commitArgs,
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v (subj=%q): %v\n%s", args, subj, err, out)
			}
		}
	}
	return dir
}

func initRepoWithBranch(t *testing.T, mainSubjects, branchSubjects []string, branch string) string {
	t.Helper()
	if len(mainSubjects) == 0 {
		t.Fatal("initRepoWithBranch: mainSubjects must have at least 1 entry (root commit)")
	}
	dir := initRepoWithSubjects(t, mainSubjects)

	for _, args := range [][]string{
		{"checkout", "-q", "-b", branch},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	for i, subj := range branchSubjects {
		fname := filepath.Join(dir, "f.txt")
		sz := len(mainSubjects) + i + 1
		if err := os.WriteFile(fname, []byte(strings.Repeat("x", sz)), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		commitArgs := []string{"commit", "-q", "--allow-empty-message"}
		if subj != "" {
			commitArgs = append(commitArgs, "-m", subj)
		} else {
			commitArgs = append(commitArgs, "-m", "")
		}
		for _, args := range [][]string{
			{"add", "f.txt"},
			commitArgs,
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v (subj=%q): %v\n%s", args, subj, err, out)
			}
		}
	}
	return dir
}

func TestRunWithGitDir_GoodFixturesProduceZeroDiagnostics(t *testing.T) {
	subjects := readSubjects(t, "good")
	dir := initRepoWithSubjects(t, subjects)
	diags, err := cc.RunWithGitDir(dir, len(subjects), "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("good fixtures produced %d diagnostics; want 0:\n%s", len(diags), strings.Join(diagsAsStrings(diags), "\n"))
	}
}

func TestRunWithGitDir_BadFixturesProduceExpectedDiagnostics(t *testing.T) {
	subjects := readSubjects(t, "bad")
	dir := initRepoWithSubjects(t, subjects)
	diags, err := cc.RunWithGitDir(dir, len(subjects), "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}

	expectedDiagnosticIDs := map[string]bool{
		"cc-bad-type":           false,
		"cc-missing-scope":      false,
		"cc-bad-scope":          false,
		"cc-bad-subject":        false,
		"cc-trailing-dot":       false,
		"cc-claude-attribution": false,
	}
	for _, d := range diags {
		for id := range expectedDiagnosticIDs {
			if strings.Contains(d.Message, id) {
				expectedDiagnosticIDs[id] = true
			}
		}
	}
	for id, fired := range expectedDiagnosticIDs {
		if !fired {
			t.Errorf("bad fixtures did NOT trigger diagnostic %q (extend bad fixtures or fix analyzer regex)", id)
		}
	}
}

func TestRunWithGitDir_DepthLimitsScan(t *testing.T) {
	subjects := []string{
		"feat(lint): valid first",
		"feat(lint): valid second",
		"BAD subject 3",
	}
	dir := initRepoWithSubjects(t, subjects)

	diags, err := cc.RunWithGitDir(dir, 1, "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}

	diagsAll, err := cc.RunWithGitDir(dir, 99, "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir all: %v", err)
	}
	if len(diagsAll) < len(diags) {
		t.Errorf("depth=99 produced %d diagnostics, depth=1 produced %d; expected depth=99 >= depth=1",
			len(diagsAll), len(diags))
	}
}

func TestRunWithGitDir_AllowedScopesFlag(t *testing.T) {
	subjects := []string{
		"feat(allowed-scope): valid",
		"feat(forbidden-scope): valid by default but forbidden by allowlist",
	}
	dir := initRepoWithSubjects(t, subjects)
	diags, err := cc.RunWithGitDir(dir, 2, "allowed-scope,other-allowed", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}

	foundForbidden := false
	for _, d := range diags {
		if strings.Contains(d.Subject, "forbidden-scope") {
			foundForbidden = true
			break
		}
	}
	if !foundForbidden {
		t.Errorf("allowlist did not reject 'forbidden-scope' subject; diagnostics=%v",
			diagsAsStrings(diags))
	}
}

func TestRunWithGitDir_NoGitDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	_, err := cc.RunWithGitDir(dir, 50, "", "", "")
	if err == nil {
		t.Error("RunWithGitDir on non-git dir returned nil error; want error")
	}
}

func TestRunWithGitDir_DepthZeroClamped(t *testing.T) {
	subjects := []string{"feat(lint): one"}
	dir := initRepoWithSubjects(t, subjects)
	_, err := cc.RunWithGitDir(dir, 0, "", "", "")
	if err != nil {
		t.Errorf("RunWithGitDir depth=0 returned error: %v; want clamped success", err)
	}
}

func TestAnalyzerName(t *testing.T) {
	if got := cc.Analyzer.Name; got != "conventional_commit" {
		t.Errorf("Analyzer.Name = %q; want %q", got, "conventional_commit")
	}
}

func TestAnalyzerDocMentionsRegex(t *testing.T) {
	doc := cc.Analyzer.Doc
	if doc == "" {
		t.Fatal("Analyzer.Doc is empty")
	}
	for _, want := range []string{"feat", "fix", "scope", "git log"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Analyzer.Doc does not mention %q", want)
		}
	}
}

func TestRunWithGitDir_BaseRefScansOnlyBranchLocalCommits(t *testing.T) {

	mainSubjects := []string{
		"feat(lint): seed main",
		"BAD UPPERCASE SUBJECT ON MAIN — IGNORE",
	}
	branchSubjects := []string{
		"feat(cli): branch commit one",
		"test(cli): branch commit two",
		"docs(readme): branch commit three",
		"BAD UPPERCASE SUBJECT ON BRANCH — MUST FIRE", // bad, branch-local → MUST fire
		"refactor(daemon): branch commit five",
	}
	dir := initRepoWithBranch(t, mainSubjects, branchSubjects, "feature")

	diags, err := cc.RunWithGitDir(dir, 99, "", "", "main")
	if err != nil {
		t.Fatalf("RunWithGitDir baseRef=main: %v", err)
	}

	wantBranchLocalBad := "BAD UPPERCASE SUBJECT ON BRANCH"
	wantMainBad := "BAD UPPERCASE SUBJECT ON MAIN"
	foundBranchLocalBad := 0
	foundMainBad := 0
	for _, d := range diags {
		if strings.Contains(d.Subject, wantBranchLocalBad) {
			foundBranchLocalBad++
		}
		if strings.Contains(d.Subject, wantMainBad) {
			foundMainBad++
		}
	}
	if foundBranchLocalBad == 0 {
		t.Errorf("base-ref scan did NOT fire on branch-local bad subject; got %d diagnostics:\n%s",
			len(diags), strings.Join(diagsAsStrings(diags), "\n"))
	}
	if foundMainBad != 0 {
		t.Errorf("base-ref scan FIRED on main-only bad subject (must be excluded); got %d hits",
			foundMainBad)
	}
}

func TestRunWithGitDir_BaseRefEmptyFallsBackToDepth(t *testing.T) {
	mainSubjects := []string{
		"feat(lint): seed main",
		"BAD UPPERCASE SUBJECT ON MAIN",
	}
	branchSubjects := []string{
		"feat(cli): one",
	}
	dir := initRepoWithBranch(t, mainSubjects, branchSubjects, "feature")

	diags, err := cc.RunWithGitDir(dir, 99, "", "", "")
	if err != nil {
		t.Fatalf("RunWithGitDir baseRef=\"\": %v", err)
	}

	found := false
	for _, d := range diags {
		if strings.Contains(d.Subject, "BAD UPPERCASE SUBJECT ON MAIN") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("baseRef=\"\" depth-scan did NOT see main commits; backward-compat broken:\n%s",
			strings.Join(diagsAsStrings(diags), "\n"))
	}
}

func TestRunWithGitDir_BaseRefAllGoodNoFindings(t *testing.T) {
	mainSubjects := []string{
		"feat(lint): seed main",
		"BAD UPPERCASE SUBJECT ON MAIN — ignored under base-ref",
	}
	branchSubjects := []string{
		"feat(cli): clean branch commit one",
		"test(cli): clean branch commit two",
		"docs(readme): clean branch commit three",
		"refactor(daemon): clean branch commit four",
		"fix(store): clean branch commit five",
	}
	dir := initRepoWithBranch(t, mainSubjects, branchSubjects, "feature")

	diags, err := cc.RunWithGitDir(dir, 99, "", "", "main")
	if err != nil {
		t.Fatalf("RunWithGitDir: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("all-good branch produced %d diagnostics under base-ref=main:\n%s",
			len(diags), strings.Join(diagsAsStrings(diags), "\n"))
	}
}

func TestRunWithGitDir_BaseRefUnknownRefReturnsError(t *testing.T) {
	dir := initRepoWithSubjects(t, []string{"feat(lint): one"})
	_, err := cc.RunWithGitDir(dir, 99, "", "", "nonexistent-ref")
	if err == nil {
		t.Error("RunWithGitDir with unknown base-ref returned nil error; want wrapped error")
	}
}

func TestAnalyzerHasBaseRefFlag(t *testing.T) {
	f := cc.Analyzer.Flags.Lookup("base-ref")
	if f == nil {
		t.Fatal("Analyzer.Flags does not expose -base-ref flag")
	}
	if !strings.Contains(f.Usage, "base-ref") && !strings.Contains(f.Usage, "HEAD") {
		t.Errorf("Analyzer.Flags -base-ref usage does not document scope; got %q", f.Usage)
	}
}

func diagsAsStrings(ds []cc.Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Message + " | subject=" + d.Subject
	}
	sort.Strings(out)
	return out
}
