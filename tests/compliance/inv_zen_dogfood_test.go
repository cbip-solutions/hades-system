package compliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootForDogfood(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestDogfoodPlan8_LintZeroViolations(t *testing.T) {
	repo := repoRootForDogfood(t)
	if _, err := os.Stat(filepath.Join(repo, "cmd", "zen-doctrine-lint")); err != nil {
		t.Skip("cmd/zen-doctrine-lint not yet present; Phase L dependency. Skipping dogfood test.")
	}
	if _, err := os.Stat(filepath.Join(repo, "lints")); err != nil {
		t.Skip("lints/ not yet present; Phase L dependency. Skipping dogfood test.")
	}
	cmd := exec.Command("make", "lint")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make lint FAILED (dogfood pass blocked); output:\n%s\nerr: %v", out, err)
	}
	s := string(out)

	forbiddenPatterns := []string{
		"nostub-panic:",
		"nostub-errnotimpl:",
		"nostub-todo:",
		"nostub-empty-method:",
		"nostore-forbidden:",
		"cc-bad-subject:",
		"cc-bad-scope:",
		"cc-claude-attribution:",
	}
	for _, pat := range forbiddenPatterns {
		if strings.Contains(s, pat) {
			t.Errorf("dogfood pass surfaced violation pattern %q in make lint output:\n%s", pat, s)
		}
	}
}

func TestDogfoodPlan8_AggregateTargetExitsZero(t *testing.T) {
	repo := repoRootForDogfood(t)
	if _, err := os.Stat(filepath.Join(repo, "cmd", "zen-doctrine-lint")); err != nil {
		t.Skip("cmd/zen-doctrine-lint not yet present; Phase L dependency. Skipping aggregate test.")
	}
	cmd := exec.Command("make", "dogfood-plan-8")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make dogfood-plan-8 FAILED; output:\n%s\nerr: %v", out, err)
	}
	if !strings.Contains(string(out), "ALL CHECKS PASS") {
		t.Errorf("make dogfood-plan-8 did not emit 'ALL CHECKS PASS' line; output:\n%s", out)
	}
}
