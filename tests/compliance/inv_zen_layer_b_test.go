// Verifies the .githooks/pre-commit dispatcher invokes both
// pre-commit-bypass-token-scan AND pre-commit-doctrine, that
// pre-commit-doctrine exists and is executable, and that the hook
// blocks a synthetic violating diff via the
// ZEN_DOCTRINE_HOOK_SELFTEST env var.
//
// These tests pin the Layer B contract: bypassing Layer A
// (operator forgets `make lint`) MUST be caught at commit time.
package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootForLayerB(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestLayerB_PreCommitDispatcherExists(t *testing.T) {
	repo := repoRootForLayerB(t)
	hook := filepath.Join(repo, ".githooks", "pre-commit")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf(".githooks/pre-commit does not exist: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf(".githooks/pre-commit exists but is not executable (mode=%v)", info.Mode())
	}
	body, err := os.ReadFile(hook)
	if err != nil {
		t.Fatalf("read .githooks/pre-commit failed: %v", err)
	}
	s := string(body)

	if !strings.Contains(s, "pre-commit-*") {
		t.Errorf(".githooks/pre-commit dispatcher does not iterate pre-commit-* glob;\nbody:\n%s", s)
	}
}

func TestLayerB_PreCommitDoctrineExecutable(t *testing.T) {
	repo := repoRootForLayerB(t)
	hook := filepath.Join(repo, ".githooks", "pre-commit-doctrine")
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatalf(".githooks/pre-commit-doctrine does not exist: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf(".githooks/pre-commit-doctrine exists but is not executable (mode=%v)", info.Mode())
	}
}

// TestLayerB_PreCommitDoctrineBlocksSyntheticViolation asserts that the
// pre-commit-doctrine hook blocks a commit when ZEN_DOCTRINE_HOOK_SELFTEST
// is set to a synthetic violating snippet. The hook treats this env var
// as a fake-diff trigger and exits non-zero with a forced "synthetic
// violation detected" diagnostic.
//
// This test EXEMPLIFIES the Layer B contract: bypassing Layer A
// (operator forgets `make lint`) MUST be caught at commit time. Tests
// the env-var escape hatch convention shared with
// pre-commit-bypass-token-scan's ZEN_BYPASS_HOOK_SELFTEST.
func TestLayerB_PreCommitDoctrineBlocksSyntheticViolation(t *testing.T) {
	repo := repoRootForLayerB(t)
	hook := filepath.Join(repo, ".githooks", "pre-commit-doctrine")
	cmd := exec.Command(hook)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "ZEN_DOCTRINE_HOOK_SELFTEST=panic-not-implemented")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("pre-commit-doctrine SHOULD have blocked the synthetic violation; got exit 0\noutput:\n%s", out)
	}
	if !strings.Contains(string(out), "synthetic violation detected") {
		t.Errorf("expected diagnostic 'synthetic violation detected' in output:\n%s", out)
	}
}

func TestLayerB_PreCommitDoctrineSkipFlag(t *testing.T) {
	repo := repoRootForLayerB(t)
	hook := filepath.Join(repo, ".githooks", "pre-commit-doctrine")
	cmd := exec.Command(hook)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "ZEN_SKIP_DOCTRINE_HOOK=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pre-commit-doctrine with ZEN_SKIP_DOCTRINE_HOOK=1 failed unexpectedly: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "SKIPPED") {
		t.Errorf("expected 'SKIPPED' diagnostic in output:\n%s", out)
	}
}
