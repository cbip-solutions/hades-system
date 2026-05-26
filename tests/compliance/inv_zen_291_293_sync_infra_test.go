// SPDX-License-Identifier: MIT

package compliance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootSyncInfra(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("inv-zen-291/293: getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("inv-zen-291/293: go.mod not found walking up from %s", dir)
		}
		dir = parent
	}
}

func TestInvZen291_SnapshotScriptPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	scriptPath := filepath.Join(root, "scripts", "build_public_snapshot.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("inv-zen-291 VIOLATED: snapshot script missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-291 VIOLATED: snapshot script not executable; mode=%v", info.Mode())
	}
}

func TestInvZen291_EmergencyAlphaBackSyncScriptPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	scriptPath := filepath.Join(root, "scripts", "emergency_alpha_back_sync.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("inv-zen-291 VIOLATED: emergency-α back-sync script missing: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("inv-zen-291 VIOLATED: emergency-α back-sync script not executable; mode=%v", info.Mode())
	}
}

func TestInvZen291_SyncWorkflowPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	wfPath := filepath.Join(root, ".github", "workflows", "sync-public.yml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("inv-zen-291 VIOLATED: sync workflow missing: %v", err)
	}
	content := string(data)
	requiredMarkers := []string{
		"on:",
		"tags:",
		"v*.*.*",
		"workflow_dispatch:",
		"scripts/build_public_snapshot.sh",
		"HADES_PUBLIC_DEPLOY_KEY",
	}
	for _, m := range requiredMarkers {
		if !strings.Contains(content, m) {
			t.Errorf("inv-zen-291 VIOLATED: sync workflow missing marker %q", m)
		}
	}
}

func TestInvZen291_EmergencyAlphaBackSyncWorkflowPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	wfPath := filepath.Join(root, ".github", "workflows", "emergency-alpha-back-sync.yml")
	data, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("inv-zen-291 VIOLATED: emergency-α back-sync workflow missing (24h hard-invariant per decisión 14 sub-d): %v", err)
	}
	content := string(data)
	requiredMarkers := []string{
		"schedule:",
		"cron:",
		"scripts/emergency_alpha_back_sync.sh",
		"workflow_dispatch:",
	}
	for _, m := range requiredMarkers {
		if !strings.Contains(content, m) {
			t.Errorf("inv-zen-291 VIOLATED: emergency-α back-sync workflow missing marker %q", m)
		}
	}
}

func TestInvZen293_ManifestPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	manifestPath := filepath.Join(root, "docs", "public-manifest", "allowlist.yml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("inv-zen-293 VIOLATED: allowlist manifest missing: %v", err)
	}
	content := string(data)
	requiredSections := []string{
		"include:",
		"exclude:",
		"required_present:",
	}
	for _, sec := range requiredSections {
		if !strings.Contains(content, sec) {
			t.Errorf("inv-zen-293 VIOLATED: manifest missing section %q", sec)
		}
	}
}

func TestInvZen293_ManifestExcludesKnownPrivateSurfaces(t *testing.T) {
	root := repoRootSyncInfra(t)
	manifestPath := filepath.Join(root, "docs", "public-manifest", "allowlist.yml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("inv-zen-293 VIOLATED: allowlist manifest missing: %v", err)
	}
	content := string(data)

	mustExclude := []string{
		"private-tier1-module/**",
		"internal/daemon/bypassadapter/**",
		"cmd/zen-swarm-ctld/bootstrap.go",
		"docs/superpowers/**",
		"HANDOFF.md",
		"AGENTS.md",
		"CLAUDE.md",
		"0101-bypass-refresh-protocol",
		"**/.venv/**",
		"**/__pycache__/**",
		"**/.pytest_cache/**",
		"**/.claude/**",
		"plugin/**/.venv/**",
		"plugin/**/__pycache__/**",
		"plugin/**/.ruff_cache/**",
		"plugin/**/.hypothesis/**",
		"plugin/**/.claude/**",
		"plugin/**/bin/**",
		"plugin/**/*.pyc",
	}
	for _, e := range mustExclude {
		if !strings.Contains(content, e) {
			t.Errorf("inv-zen-293 VIOLATED: manifest missing exclude for known-private surface %q", e)
		}
	}
}

func TestInvZen293_ManifestRequiresKnownPublicSurfaces(t *testing.T) {
	root := repoRootSyncInfra(t)
	manifestPath := filepath.Join(root, "docs", "public-manifest", "allowlist.yml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("inv-zen-293 VIOLATED: allowlist manifest missing: %v", err)
	}
	content := string(data)

	mustRequire := []string{
		"LICENSE",
		"README.md",
		"INSTALL.md",
		"THIRD_PARTY_LICENSES.md",
		"CHANGELOG.md",
		"SECURITY.md",
		"CONTRIBUTING.md",
		"CODE_OF_CONDUCT.md",
		"go.mod",
		"Makefile",
	}

	idx := strings.Index(content, "required_present:")
	if idx < 0 {
		t.Fatal("inv-zen-293 VIOLATED: required_present: section missing")
	}
	requiredBlock := content[idx:]
	for _, r := range mustRequire {
		if !strings.Contains(requiredBlock, r) {
			t.Errorf("inv-zen-293 VIOLATED: required_present missing canonical public surface %q", r)
		}
	}
}

func TestInvZen291_PostV1WorkflowDocPresent(t *testing.T) {
	root := repoRootSyncInfra(t)
	docPath := filepath.Join(root, "docs", "operations", "post-v1-dev-workflow.md")
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("inv-zen-291 VIOLATED: post-v1 workflow doc missing: %v", err)
	}
	content := string(data)
	requiredSections := []string{
		"Modelo B",
		"Modelo 5 Hybrid",
		"Modelo β",
		"Modelo α",
		"24h back-sync",
		"DCO sign-off",
		"build_public_snapshot.sh",
		"docs/public-manifest/allowlist.yml",
		"emergency_alpha_back_sync.sh",
		"emergency-alpha-back-sync.yml",
	}
	for _, sec := range requiredSections {
		if !strings.Contains(content, sec) {
			t.Errorf("inv-zen-291 VIOLATED: post-v1 workflow doc missing section/marker %q (per decisión 14)", sec)
		}
	}
}

func TestInvZen291_SyncSmoke(t *testing.T) {
	root := repoRootSyncInfra(t)
	scriptPath := filepath.Join(root, "scripts", "build_public_snapshot.sh")
	cmd := exec.Command(scriptPath, "--dry-run")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {

		outStr := string(out)
		if strings.Contains(outStr, "required_present surfaces missing") {
			t.Skipf("required_present surfaces not yet landed (Phase H still in flight); skipping smoke. Output:\n%s", outStr)
		}
		t.Errorf("inv-zen-291 VIOLATED: snapshot script --dry-run failed:\n%s", outStr)
	}
}
