// tests/compliance/inv_zen_270_d5_boundary_test.go
//
// Compliance test for inv-zen-270 (master Plan 20):
//
//	"The L10 Coordinator subsystem internal/caronte/coordinated/ is
//	 INDEPENDENT of F.7 hook daemon-wiring (D5); Coordinator code MUST
//	 NOT import or construct HRA / ConfirmationPolicy / MergeEngine
//	 directly. Compliance: AST scan over
//	 internal/caronte/coordinated/'s import set forbids those
//	 packages; capability-detection of WorktreePool is the ONLY
//	 sanctioned bridge."
//
// Enforcement: full-package AST scan via
// golang.org/x/tools/go/packages over internal/caronte/coordinated/
// — load the package + its dependencies (Mode=NeedImports|NeedName|
// NeedFiles|NeedDeps) and walk the recursive Import() set looking
// for forbidden package paths. The forbidden set is the three F.7
// hook package paths; the SOLE allowed orchestrator packages are
// internal/orchestrator (for the ContractFixAutonomyOracle seam) +
// internal/orchestrator/worktreepool (per the master
// "capability-detection of WorktreePool is the ONLY sanctioned
// bridge" carve-out).

package compliance

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

var forbiddenForCoordinated = []string{
	"github.com/cbip-solutions/hades-system/internal/orchestrator/hra",
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge",
	"github.com/cbip-solutions/hades-system/internal/orchestrator/confirmation_policy",
}

var allowedOrchestratorPaths = map[string]bool{
	"github.com/cbip-solutions/hades-system/internal/orchestrator":              true,
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool": true,
}

func TestInvZen270CoordinatedNoF7Imports(t *testing.T) {
	cfg := &packages.Config{
		Mode: packages.NeedImports | packages.NeedName | packages.NeedFiles | packages.NeedDeps,
		Dir:  repoRoot(t),

		BuildFlags: []string{"-tags=sqlite_fts5"},
	}
	pkgs, err := packages.Load(cfg, "github.com/cbip-solutions/hades-system/internal/caronte/coordinated/...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatalf("packages.Load returned 0 packages for internal/caronte/coordinated/...; expected at least 1")
	}

	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("packages.Load error on %s: %v", p.PkgPath, e)
		}
	}

	transitive := make(map[string]bool)
	var walk func(p *packages.Package)
	walk = func(p *packages.Package) {
		for path, dep := range p.Imports {
			if transitive[path] {
				continue
			}
			transitive[path] = true
			walk(dep)
		}
	}
	for _, p := range pkgs {
		walk(p)
	}

	for _, forbidden := range forbiddenForCoordinated {
		if transitive[forbidden] {
			t.Errorf("inv-zen-270: internal/caronte/coordinated MUST NOT import (transitively) %s; D5 boundary violation", forbidden)
		}
	}
}

func TestInvZen270CoordinatedDirectOrchestratorImports(t *testing.T) {
	cfg := &packages.Config{
		Mode:       packages.NeedImports | packages.NeedName | packages.NeedFiles,
		Dir:        repoRoot(t),
		BuildFlags: []string{"-tags=sqlite_fts5"},
	}
	pkgs, err := packages.Load(cfg, "github.com/cbip-solutions/hades-system/internal/caronte/coordinated")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("packages.Load returned 0 for internal/caronte/coordinated")
	}
	for _, p := range pkgs {
		for _, e := range p.Errors {
			t.Errorf("packages.Load error on %s: %v", p.PkgPath, e)
		}
	}
	const orchestratorPrefix = "github.com/cbip-solutions/hades-system/internal/orchestrator"
	for _, p := range pkgs {
		for path := range p.Imports {
			if !strings.HasPrefix(path, orchestratorPrefix) {
				continue
			}
			if allowedOrchestratorPaths[path] {
				continue
			}
			t.Errorf("inv-zen-270: internal/caronte/coordinated MUST NOT directly import orchestrator subpackage %s (not in carve-out); D5 boundary violation",
				path)
		}
	}
}
