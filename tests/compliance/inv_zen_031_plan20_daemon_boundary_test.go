// tests/compliance/inv_zen_031_plan20_daemon_boundary_test.go
//
// Compliance gate for inv-zen-031 (Plan 20 Phase J extension): the
// internal/daemon package MUST NOT transitively import the Plan 20
// concrete substrate packages (internal/caronte/store/federation +
// internal/caronte/coordinated + internal/caronte/contract/bcdetect).
// The cmd/zen-swarm-ctld composition root (contract_federation_wiring.go)
// is the ONLY layer that bridges the daemon's narrow
// ContractFederationForDaemon + ContractCoordinatorForDaemon interfaces
// with the Plan 20 concretes — verified via the wiring adapters below.
//
// Mirrors the existing inv-zen-089 (orchestrator no store) +
// inv-zen-090 (substrate separation) pattern: shells out to
// `go list -deps -json` against ./internal/daemon/... and walks the
// transitive closure asserting the forbidden imports are absent.
package compliance

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// plan20DaemonBoundaryForbidden is the SHARED forbidden-set used by
// both the live scan TestInvZen031Plan20DaemonNoFederationOrCoordinator
// Import AND the sister-test TestInvZen031Plan20DaemonBoundary_
// ForbiddenSetIsComplete. Defense-in-depth: the bcdetect / extract /
// link entries below mirror the OUTBOUND inv-zen-271 boundary
// (master spec §"Risk register" — Plan-20 packages the daemon MUST
// NOT consume from). Today's go list closure is CLEAN of all five
// entries (verified by the live scan); the defensive entries prevent
// future drift where a daemon handler accidentally consumes a
// Plan-20 value type by direct import. The sister-test below pins
// the slice contents so a future shrink fires a compliance error
// rather than silently shrinking the boundary surface.
var plan20DaemonBoundaryForbidden = []string{
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation",
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated",

	"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect",
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract",
	"github.com/cbip-solutions/hades-system/internal/caronte/contract/link",
}

// TestInvZen031Plan20DaemonBoundary_ForbiddenSetIsComplete is the
// sister-test (Fix 2 of J fix-up) pinning the LITERAL contents of
// plan20DaemonBoundaryForbidden. Per
// [[feedback_sister_test_pattern]]: a documented invariant (the
// 5-entry defense-in-depth set) MUST have a gating assertion so a
// future code-level shrink (e.g., removing the defensive bcdetect/
// extract/link entries) fires a compliance error rather than
// silently weakening the boundary scan. Bite-check: drop any entry
// from the slice → this test fires with a mismatch on len() and the
// specific missing entry.
func TestInvZen031Plan20DaemonBoundary_ForbiddenSetIsComplete(t *testing.T) {
	wantEntries := []string{
		"github.com/cbip-solutions/hades-system/internal/caronte/store/federation",
		"github.com/cbip-solutions/hades-system/internal/caronte/coordinated",
		"github.com/cbip-solutions/hades-system/internal/caronte/contract/bcdetect",
		"github.com/cbip-solutions/hades-system/internal/caronte/contract/extract",
		"github.com/cbip-solutions/hades-system/internal/caronte/contract/link",
	}
	if len(plan20DaemonBoundaryForbidden) != len(wantEntries) {
		t.Fatalf("plan20DaemonBoundaryForbidden has %d entries; want %d (defense-in-depth set: federation+coordinated+bcdetect+extract+link)",
			len(plan20DaemonBoundaryForbidden), len(wantEntries))
	}
	have := make(map[string]bool, len(plan20DaemonBoundaryForbidden))
	for _, e := range plan20DaemonBoundaryForbidden {
		have[e] = true
	}
	for _, want := range wantEntries {
		if !have[want] {
			t.Errorf("plan20DaemonBoundaryForbidden missing required entry %q (defense-in-depth gate)", want)
		}
	}
}

// TestInvZen031Plan20DaemonNoFederationOrCoordinatorImport — Plan 20
// Phase J narrow-interface boundary: internal/daemon/* MUST NOT import
// internal/caronte/store/federation or internal/caronte/coordinated,
// directly or transitively. Bridge via the wiring adapters in
// cmd/zen-swarm-ctld/contract_federation_wiring.go.
func TestInvZen031Plan20DaemonNoFederationOrCoordinatorImport(t *testing.T) {
	root := repoRoot(t)

	cmd := exec.Command("go", "list", "-deps", "-json", "./internal/daemon/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
		}
		t.Fatalf("go list -deps -json ./internal/daemon/...: %v\nstderr: %s", err, stderr)
	}
	dec := json.NewDecoder(strings.NewReader(string(out)))
	const daemonPrefix = "github.com/cbip-solutions/hades-system/internal/daemon"
	forbidden := plan20DaemonBoundaryForbidden

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
		if !strings.HasPrefix(pkg.ImportPath, daemonPrefix) {
			continue
		}

		scanned++
		for _, d := range pkg.Imports {
			for _, fb := range forbidden {
				if d == fb || strings.HasPrefix(d, fb+"/") {
					t.Errorf("inv-zen-031 VIOLATED: %s directly imports %s", pkg.ImportPath, d)
				}
			}
		}
		for _, d := range pkg.Deps {
			for _, fb := range forbidden {
				if d == fb || strings.HasPrefix(d, fb+"/") {
					t.Errorf("inv-zen-031 VIOLATED: %s transitively imports %s (full Deps closure)", pkg.ImportPath, d)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatalf("inv-zen-031 Plan 20 boundary scan inconclusive: 0 daemon packages observed by go list (sentinel — confirm package layout)")
	}
}
