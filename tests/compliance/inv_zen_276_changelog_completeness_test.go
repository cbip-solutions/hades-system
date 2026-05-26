// SPDX-License-Identifier: MIT

//go:build !race
// +build !race

// Package compliance — Plan 15 Phase A task A-12 compliance test for
// inv-zen-276.
//
// Asserts the verify-changelog-completeness gate passes against the live
// repo. The gate enforces:
//
//	(a) every `v*` git tag has either a `## [vN.M.K]` heading in
//	    CHANGELOG.md OR a row in configs/changelog-omission-allowlist.yaml
//	    with a non-empty rationale.
//	(b) flip-aware semantics — allowlist rows for tags >= v1.0.0 are
//	    rejected (post-v1.0 every release tag MUST carry CHANGELOG
//	    narrative per Plan-15 v1.0 release decisión 8).
//
// Why this test exists: even with the bats shell tests in
// tests/scripts/test_verify_changelog_completeness.bats, the Go test
// gate ensures `make test` + CI catch regressions where (a) a new tag
// is pushed without a CHANGELOG entry and without an allowlist row, or
// (b) an operator inadvertently adds a >=v1.0.0 row to the allowlist.
//
// Composes into the verify-release-gates Makefile composite (A-8 owns
// the composite). The 4 deliberate pre-v1.0 omissions (v0.10.0,
// test asserts the script returns exit 0 against the live repo.
package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvZen276_ChangelogCompleteness(t *testing.T) {
	root := findRepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "verify_changelog_completeness.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("inv-zen-276: script not found at %s: %v", scriptPath, err)
	}

	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf(
			"inv-zen-276: verify-changelog-completeness FAIL "+
				"(every v* git tag MUST have a `## [vN.M.K]` CHANGELOG.md "+
				"entry OR a row in configs/changelog-omission-allowlist.yaml "+
				"with non-empty rationale; allowlist rejects >=v1.0.0 per "+
				"Plan-15 decisión 8):\n%s\nerror: %v",
			out, err)
	}

	got := string(out)
	if !strings.Contains(got, "PASS: CHANGELOG completeness") {
		t.Errorf("inv-zen-276: expected PASS banner; got:\n%s", got)
	}

	if !strings.Contains(got, "inv-zen-276") {
		t.Errorf("inv-zen-276: expected banner to mention 'inv-zen-276'; got:\n%s", got)
	}
}

func TestInvZen276_AllowlistHasFourDeliberateOmissions(t *testing.T) {
	root := findRepoRoot(t)
	allowlistPath := filepath.Join(root, "configs", "changelog-omission-allowlist.yaml")
	body, err := os.ReadFile(allowlistPath)
	if err != nil {
		t.Fatalf("inv-zen-276: read allowlist at %s: %v", allowlistPath, err)
	}
	bs := string(body)

	expected := []string{"v0.10.0", "v0.16.0", "v0.18.0", "v0.19.0"}
	for _, tag := range expected {

		marker := "- tag: " + tag
		if !strings.Contains(bs, marker) {
			t.Errorf(
				"inv-zen-276: allowlist missing deliberate omission %q "+
					"(expected `%s` line). The 4 pre-v1.0 omissions "+
					"v0.10.0/v0.16.0/v0.18.0/v0.19.0 are load-bearing per "+
					"Plan-15 decisión 8 — to remove, add the `## [%s]` heading "+
					"to CHANGELOG.md first.",
				tag, marker, tag)
		}
	}
}

func TestInvZen276_AllowlistRejectsV1Plus(t *testing.T) {
	root := findRepoRoot(t)
	allowlistPath := filepath.Join(root, "configs", "changelog-omission-allowlist.yaml")
	body, err := os.ReadFile(allowlistPath)
	if err != nil {
		t.Fatalf("inv-zen-276: read allowlist at %s: %v", allowlistPath, err)
	}

	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)

		if !strings.HasPrefix(trimmed, "- tag:") {
			continue
		}

		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "- tag:"))
		rest = strings.Trim(rest, `"' `)
		if !strings.HasPrefix(rest, "v") {
			continue
		}
		ver := strings.TrimPrefix(rest, "v")

		dotIdx := strings.Index(ver, ".")
		if dotIdx <= 0 {
			continue
		}
		majorStr := ver[:dotIdx]

		if majorStr == "0" {
			continue
		}
		t.Errorf(
			"inv-zen-276: allowlist contains v1.0+ entry %q — flip-aware "+
				"policy rejects (Plan-15 decisión 8: post-v1.0 every "+
				"release tag MUST carry CHANGELOG narrative in the "+
				"public repo).",
			rest)
	}
}
