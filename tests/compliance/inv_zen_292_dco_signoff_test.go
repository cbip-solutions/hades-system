// SPDX-License-Identifier: MIT

// Package compliance — Plan 15 Phase C task C-14 compliance gates for DCO
// (Developer Certificate of Origin) sign-off enforcement.
//
// inv-zen-292 (decisión 15-2; 2026-05-24): every public-repo commit MUST
// carry a Signed-off-by: trailer. Cascades from decisión 14 sub-c (community
// engagement strategy: best-effort no-SLA + dual-path PR triage + DCO).
//
// Three layers gate the invariant:
//  1. Client-side: `.githooks/pre-commit-dco` aborts unsigned commits.
//  2. Sync: operator installs via `scripts/install_git_hooks.sh` (Makefile
//     target `install-git-hooks` invokes it).
//  3. Server-side: `.github/workflows/dco-check.yml` rejects PRs whose
//     commits lack the trailer (authoritative for the public repo).
//
// These tests are intentionally surface-level (file presence + executable bit
// + load-bearing markers); deep behavioral checks live in
// `tests/scripts/test_pre_commit_dco.bats`. The separation lets the Go
// compliance suite stay hermetic (no `bash` dependency) while still failing
// loudly if any of the three surfaces drift away.
package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot292(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-292: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-292: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen292_PreCommitDCOHookPresent(t *testing.T) {
	root := repoRoot292(t)
	hookPath := filepath.Join(root, ".githooks", "pre-commit-dco")

	info, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("inv-zen-292: DCO pre-commit hook missing at %s: %v", hookPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-292: DCO pre-commit hook not executable; mode=%v (want any +x bit set)", info.Mode())
	}

	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("inv-zen-292: reading %s: %v", hookPath, err)
	}
	body := string(data)
	if !strings.Contains(body, "Signed-off-by") {
		t.Errorf("inv-zen-292: DCO hook does not reference 'Signed-off-by' marker")
	}
	if !strings.Contains(body, "inv-zen-292") {
		t.Errorf("inv-zen-292: DCO hook does not reference invariant ID 'inv-zen-292'")
	}
}

func TestInvZen292_CommitMsgDispatcherAndHookPresent(t *testing.T) {
	root := repoRoot292(t)

	dispatcherPath := filepath.Join(root, ".githooks", "commit-msg")
	dispatcherInfo, err := os.Stat(dispatcherPath)
	if err != nil {
		t.Fatalf("inv-zen-292: commit-msg dispatcher missing at %s: %v", dispatcherPath, err)
	}
	if dispatcherInfo.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-292: commit-msg dispatcher not executable; mode=%v", dispatcherInfo.Mode())
	}
	dispatcherSrc, err := os.ReadFile(dispatcherPath)
	if err != nil {
		t.Fatalf("inv-zen-292: reading %s: %v", dispatcherPath, err)
	}
	if !strings.Contains(string(dispatcherSrc), "commit-msg-*") {
		t.Errorf("inv-zen-292: commit-msg dispatcher does not iterate commit-msg-* glob")
	}
	if !strings.Contains(string(dispatcherSrc), `"$@"`) {
		t.Errorf("inv-zen-292: commit-msg dispatcher does not forward \"$@\" (would lose message-file path)")
	}

	hookPath := filepath.Join(root, ".githooks", "commit-msg-dco")
	hookInfo, err := os.Stat(hookPath)
	if err != nil {
		t.Fatalf("inv-zen-292: commit-msg-dco hook missing at %s: %v", hookPath, err)
	}
	if hookInfo.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-292: commit-msg-dco hook not executable; mode=%v", hookInfo.Mode())
	}
}

func TestInvZen292_InstallGitHooksScriptPresent(t *testing.T) {
	root := repoRoot292(t)
	scriptPath := filepath.Join(root, "scripts", "install_git_hooks.sh")

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("inv-zen-292: install_git_hooks.sh missing at %s: %v", scriptPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-292: install_git_hooks.sh not executable; mode=%v (want any +x bit set)", info.Mode())
	}

	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("inv-zen-292: reading %s: %v", scriptPath, err)
	}
	body := string(data)

	if !strings.Contains(body, "pre-commit-dco") {
		t.Errorf("inv-zen-292: install script does not reference 'pre-commit-dco' source hook")
	}
}

func TestInvZen292_DCOWorkflowPresent(t *testing.T) {
	root := repoRoot292(t)
	wfPath := filepath.Join(root, ".github", "workflows", "dco-check.yml")

	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("inv-zen-292: DCO CI workflow missing at %s: %v", wfPath, err)
	}
	content := string(data)

	requiredMarkers := []string{
		"name: dco-check",
		"on:",
		"pull_request:",
		"Signed-off-by:",
	}
	for _, m := range requiredMarkers {
		if !strings.Contains(content, m) {
			t.Errorf("inv-zen-292: DCO workflow missing required marker %q", m)
		}
	}
}
