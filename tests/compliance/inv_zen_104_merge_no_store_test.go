// tests/compliance/inv_zen_104_merge_no_store_test.go
//
// Compliance gate for inv-zen-104: every package under
// internal/orchestrator/merge/... MUST NOT depend on internal/store,
// neither directly nor transitively. The store boundary is bridged via
// the Plan 5 Phase N adapter at internal/daemon/orchestratoradapter/
// which adapts the merge engine's RawEmitter / EventEmitter onto the
// store-backed audit_events_raw table.
//
// Two checks at the same compile-time substrate:
//  1. `go list -deps -json` against ./internal/orchestrator/merge/...
//     decoded into Imports + transitive Deps closure. Either route
//     to internal/store fails the build. The Deps closure is the
//     load-bearing check — a transitive store dep is just as much an
//     inv-zen-104 violation as a direct one because it pulls the SQL
//     surface into the merge engine's compile-time graph.
//  2. The doc.go substrateSeparated() compile-time marker still builds.
//     If a future contributor accidentally adds a forbidden import,
//     `go build ./internal/orchestrator/merge/...` would either succeed
//     anyway (caught by check 1) or fail to build (caught by check 2),
//     so the two checks are belt-and-suspenders defense-in-depth.
//
// Mirrors Plan 5 Phase A inv-zen-089 / inv-zen-090 import-graph scan
// pattern (tests/compliance/inv_zen_089_orchestrator_no_store_test.go,
// tests/compliance/inv_zen_090_substrate_separation_test.go) — same
// helpers (repoRoot, isUnderPrefix), same go-list-deps approach, same
// stderr-capture-on-ExitError, same scanned == 0 sentinel.
package compliance

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const (
	merge104Prefix = "github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
	store104Prefix = "github.com/cbip-solutions/hades-system/internal/store"
)

// TestInvZen104MergeMustNotImportStore — internal/orchestrator/merge/*
// MUST NOT import internal/store directly or transitively. Bridge via
// internal/daemon/orchestratoradapter (Plan 5 Phase N).
func TestInvZen104MergeMustNotImportStore(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-deps", "-json", "./internal/orchestrator/merge/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {

		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps -json ./internal/orchestrator/merge/...: %v\nstderr: %s", err, stderr)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))

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
		if !isUnderPrefix(pkg.ImportPath, merge104Prefix) {

			continue
		}
		scanned++
		for _, d := range pkg.Imports {
			if isUnderPrefix(d, store104Prefix) {
				t.Errorf("inv-zen-104 VIOLATED: %s directly imports %s (must bridge via internal/daemon/orchestratoradapter/)",
					pkg.ImportPath, d)
			}
		}
		for _, d := range pkg.Deps {
			if isUnderPrefix(d, store104Prefix) {
				t.Errorf("inv-zen-104 VIOLATED: %s transitively imports %s (full Deps closure; must bridge via internal/daemon/orchestratoradapter/)",
					pkg.ImportPath, d)
			}
		}
	}
	if scanned == 0 {

		t.Fatalf("inv-zen-104 scan inconclusive: 0 merge packages observed by go list (sentinel — confirm package layout)")
	}
}

func TestInvZen104CompileMarkerPresent(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "build", "./internal/orchestrator/merge/...")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("internal/orchestrator/merge/ failed to build (substrateSeparated marker may be missing or a forbidden import broke the package): %v\n%s",
			err, string(out))
	}
}
