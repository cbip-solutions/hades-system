// tests/compliance/inv_zen_089_orchestrator_no_store_test.go
//
// Compliance gate for inv-zen-089: every package under
// internal/orchestrator/... MUST NOT depend on internal/store, neither
// directly nor transitively. The store boundary is bridged via the
// Phase N adapter at internal/daemon/orchestratoradapter/ which adapts
// the orchestrator's RawEmitter interface onto the store-backed
// audit_events_raw table.
//
// Phase A's contribution covers internal/orchestrator/clock and
// internal/orchestrator/eventlog. The scan is package-pattern based
// (./internal/orchestrator/...) so future Phase B-P subpackages
// (worktreepool/, state_machine, orchestrator/, etc.) inherit the
// same compliance gate automatically.
//
// Implementation: shells out to `go list -deps -json` against the
// orchestrator subtree, then walks each package's Imports +
// transitive Deps and asserts internal/store is absent. The Deps
// closure is the load-bearing check — a transitive store dep is just
// as much an inv-zen-089 violation as a direct one because it pulls
// the SQL surface into orchestrator's compile-time graph.
package compliance

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestInvZen089OrchestratorNoStoreImport — internal/orchestrator/* MUST
// NOT import internal/store directly or transitively. Bridge via
// internal/daemon/orchestratoradapter (Phase N).
func TestInvZen089OrchestratorNoStoreImport(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-deps", "-json", "./internal/orchestrator/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {

		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps -json ./internal/orchestrator/...: %v\nstderr: %s", err, stderr)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	const orchestratorPrefix = "github.com/cbip-solutions/hades-system/internal/orchestrator/"
	const storePrefix = "github.com/cbip-solutions/hades-system/internal/store"

	scanned := 0
	for dec.More() {
		var pkg struct {
			ImportPath string
			Imports    []string
			Deps       []string
		}
		if err := dec.Decode(&pkg); err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		if !strings.HasPrefix(pkg.ImportPath, orchestratorPrefix) {

			continue
		}
		scanned++
		for _, d := range pkg.Imports {
			if isStoreImport(d, storePrefix) {
				t.Errorf("inv-zen-089 VIOLATED: %s directly imports %s", pkg.ImportPath, d)
			}
		}
		for _, d := range pkg.Deps {
			if isStoreImport(d, storePrefix) {
				t.Errorf("inv-zen-089 VIOLATED: %s transitively imports %s (full Deps closure)", pkg.ImportPath, d)
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("inv-zen-089 scan inconclusive: 0 orchestrator packages observed by go list (sentinel — confirm package layout)")
	}
}

func isStoreImport(dep, storePrefix string) bool {
	if dep == storePrefix {
		return true
	}
	return strings.HasPrefix(dep, storePrefix+"/")
}
