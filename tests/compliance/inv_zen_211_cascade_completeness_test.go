// tests/compliance/inv_zen_211_cascade_completeness_test.go
//
// Compliance gate for inv-zen-211 (cascade completeness — daemon-startup
// check). The daemon's main.go runs verifyCascadeCompleteness AFTER
// buildOrchestrator: for every OPERATOR-supplied cascade (profiles.toml
// via ProfileResolver.OperatorProfileNames(), plus projects.toml
// [orchestrator].fallback_chain via OperatorOrchestratorProjects()),
// every cascade name MUST resolve via providers.Registry.Get. A miss
// aborts daemon boot naming the offending profile/project + provider.
//
// Built-in roster defaults (BuiltinProfileDefaults) are EXCLUDED — they
// are aspirational stubs of the v1.0 OSS roster and the operator wires
// them on demand; gating them would block the out-of-box empty-config
// boot. Plan 16 Phase B T22 hot-fix.
//
// Three-place triple:
//
//	(1) spec §8 inv-zen-211 text
//	(2) this compliance test (source-level marker grep + helper-presence check)
//	(3) Makefile verify-inv-zen-211 target
package compliance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (no go.mod found in ancestors)")
		}
		dir = parent
	}
	t.Fatalf("repo root not found within 8 levels of %s", dir)
	return ""
}

func readSource(t *testing.T, relPath string) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInvZen211_CascadeCompletenessStartupCheck(t *testing.T) {

	mainStr := readSource(t, filepath.Join("cmd", "zen-swarm-ctld", "main.go"))
	if !strings.Contains(mainStr, "verifyCascadeCompleteness") {
		t.Error("inv-zen-211: main.go must call verifyCascadeCompleteness at startup")
	}
	if !strings.Contains(mainStr, "inv-zen-211") {
		t.Error("inv-zen-211: main.go startup-check must cite inv-zen-211 in a comment")
	}

	wiringStr := readSource(t, filepath.Join("cmd", "zen-swarm-ctld", "orchestrator_wiring.go"))
	if !strings.Contains(wiringStr, "func verifyCascadeCompleteness") {
		t.Error("inv-zen-211: orchestrator_wiring.go must define verifyCascadeCompleteness")
	}
	if !strings.Contains(wiringStr, "OperatorProfileNames()") {
		t.Error("inv-zen-211: verifyCascadeCompleteness must iterate OperatorProfileNames() (profiles.toml entries)")
	}
	if !strings.Contains(wiringStr, "OperatorOrchestratorProjects()") {
		t.Error("inv-zen-211: verifyCascadeCompleteness must iterate OperatorOrchestratorProjects() (projects.toml [orchestrator].fallback_chain)")
	}
	if !strings.Contains(wiringStr, "inv-zen-211") {
		t.Error("inv-zen-211: orchestrator_wiring.go verifyCascadeCompleteness must cite inv-zen-211")
	}
}
