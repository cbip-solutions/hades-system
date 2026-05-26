package compliance_test

import (
	"os"
	"strings"
	"testing"
)

func TestInvZenG4_CheckWorkflowFreshnessScriptExists(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/check-workflow-freshness.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("inv-zen-314: scripts/release-gates/check-workflow-freshness.sh not found: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("inv-zen-314: scripts/release-gates/check-workflow-freshness.sh is not executable")
	}
}

func TestInvZenG4_CheckWorkflowFreshnessScriptDeclaresThresholds(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/check-workflow-freshness.sh")
	data := mustReadFile_g(t, path)
	content := string(data)

	expectedTokens := []string{
		"CHAOS_MAX_AGE_DAYS",
		"DOCTRINE_MAX_AGE_DAYS",
		"NIGHTLY_BYPASS_MAX_AGE_DAYS",
		"chaos.yml",
		"doctrine-pre-release.yml",
		"nightly-bypass-probe.yml",
	}
	for _, e := range expectedTokens {
		if !strings.Contains(content, e) {
			t.Errorf("inv-zen-314: check-workflow-freshness.sh missing expected token %q", e)
		}
	}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "CHAOS_MAX_AGE_DAYS="):
			if !strings.Contains(trimmed, "7") {
				t.Errorf("inv-zen-314: CHAOS_MAX_AGE_DAYS default should be 7 (per spec §7.5); got: %s", trimmed)
			}
		case strings.HasPrefix(trimmed, "DOCTRINE_MAX_AGE_DAYS="):
			if !strings.Contains(trimmed, "7") {
				t.Errorf("inv-zen-314: DOCTRINE_MAX_AGE_DAYS default should be 7 (per spec §7.5); got: %s", trimmed)
			}
		case strings.HasPrefix(trimmed, "NIGHTLY_BYPASS_MAX_AGE_DAYS="):
			if !strings.Contains(trimmed, "14") {
				t.Errorf("inv-zen-314: NIGHTLY_BYPASS_MAX_AGE_DAYS default should be 14 (per spec §7.5); got: %s", trimmed)
			}
		}
	}
}

func TestInvZenG4_CheckWorkflowFreshnessUsesGhApi(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, "scripts/release-gates/check-workflow-freshness.sh")
	data := mustReadFile_g(t, path)
	content := string(data)

	if !strings.Contains(content, "gh api") {
		t.Error("inv-zen-314: check-workflow-freshness.sh should invoke 'gh api' for workflow run queries")
	}
	if !strings.Contains(content, "/actions/workflows/") {
		t.Error("inv-zen-314: check-workflow-freshness.sh should query /actions/workflows/{name}/runs endpoint")
	}
}

func TestInvZenG4_MakefileVerifyCrossWorkflowFreshnessTarget(t *testing.T) {
	t.Parallel()

	makefilePath := repoPath_g(t, "Makefile")
	data := mustReadFile_g(t, makefilePath)
	content := string(data)

	if !strings.Contains(content, "verify-cross-workflow-freshness") {
		t.Error("inv-zen-314: Makefile missing verify-cross-workflow-freshness target")
	}
	if !strings.Contains(content, "scripts/release-gates/check-workflow-freshness.sh") {
		t.Error("inv-zen-314: Makefile verify-cross-workflow-freshness target should invoke check-workflow-freshness.sh")
	}
}

func TestInvZenG4_ReleaseGatesYamlIncludesFreshnessJob(t *testing.T) {
	t.Parallel()

	path := repoPath_g(t, ".github/workflows/release-gates.yml")
	data := mustReadFile_g(t, path)
	content := string(data)

	if !strings.Contains(content, "verify-cross-workflow-freshness") {
		t.Error("inv-zen-314: release-gates.yml missing verify-cross-workflow-freshness job (Phase G G-1 composition)")
	}
}
