package profiles_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var requiredProfiles = []string{
	"orchestrator.tmpl.md",
	"swarm-coder.tmpl.md",
	"audit-cross.tmpl.md",
	"research-cheap.tmpl.md",
	"meta-reviewer.tmpl.md",
	"agente-ejecutor.tmpl.md",
}

func TestRequiredWorkerProfilesPresent(t *testing.T) {
	for _, f := range requiredProfiles {
		path := filepath.Join(".", f)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("worker profile template missing: %s (err: %v)", f, err)
		}
	}
}

func TestWorkerProfileTemplatesNotEmpty(t *testing.T) {
	for _, f := range requiredProfiles {
		path := filepath.Join(".", f)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Logf("%s not present yet; skipping shape check", f)
			continue
		}
		s := strings.TrimSpace(string(body))
		if len(s) < 100 {
			t.Errorf("%s: body too short (%d bytes) — likely empty after move", f, len(s))
		}
	}
}

func TestNoPluginAgentsReferences(t *testing.T) {

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("repo root detection failed: %v", err)
	}
	legacyDir := filepath.Join(repoRoot, "plugin", "zen-swarm", "agents")
	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("legacy plugin/zen-swarm/agents/ directory still exists; should be removed in H'-9")
	}
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
