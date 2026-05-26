package compliance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func inv180RepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestInvZen180SpikeArtifactPresent(t *testing.T) {
	repoRoot := inv180RepoRoot(t)
	specsDir := filepath.Join(repoRoot, "docs", "superpowers", "specs")
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("read specs dir: %v", err)
	}
	var found bool
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.Contains(e.Name(), "plan-13-spike-hermes-mcp-contract") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("inv-zen-180: spike artifact missing; expected file matching plan-13-spike-hermes-mcp-contract; got:\n%v", names)
	}
}

func TestInvZen180SpikeRegenerateBinaryRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("inv-zen-180: skip in -short (CLI compilation cost)")
	}
	repoRoot := inv180RepoRoot(t)
	cmd := exec.Command("go", "run", "./tests/spike", "--offline")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("spike --offline failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "spike artifact present") {
		t.Fatalf("inv-zen-180: spike --offline expected confirmation; got:\n%s", out)
	}
}

func TestInvZen180MakefileTargetPresent(t *testing.T) {
	repoRoot := inv180RepoRoot(t)
	body, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	matched, err := regexp.MatchString(`(?m)^verify-spike-current:`, string(body))
	if err != nil {
		t.Fatalf("regex compile: %v", err)
	}
	if !matched {
		t.Fatalf("inv-zen-180: verify-spike-current Makefile target missing")
	}
}
