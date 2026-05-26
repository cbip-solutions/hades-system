//go:build integration

package crossplan

import (
	"context"
	"go/build"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
)

func TestPlan20F7SeamBoundaryAndAsBuiltEmitState(t *testing.T) {
	disableKeychain(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()

	pkg, err := build.Default.Import("github.com/cbip-solutions/hades-system/internal/caronte/coordinated", repoRoot(t), 0)
	if err != nil {
		t.Fatalf("build.Default.Import(coordinated): %v", err)
	}
	forbidden := []string{
		"github.com/cbip-solutions/hades-system/internal/orchestrator/hra",
		"github.com/cbip-solutions/hades-system/internal/orchestrator/merge",
		"github.com/cbip-solutions/hades-system/internal/orchestrator/confirmation_policy",
	}
	seen := map[string]bool{}
	var walk func(pkgPath string)
	walk = func(pkgPath string) {
		if seen[pkgPath] {
			return
		}
		seen[pkgPath] = true
		p, err := build.Default.Import(pkgPath, repoRoot(t), 0)
		if err != nil {
			return
		}
		for _, ip := range p.Imports {
			if strings.HasPrefix(ip, "github.com/cbip-solutions/hades-system/") {
				walk(ip)
			}
		}
	}
	for _, ip := range pkg.Imports {
		walk(ip)
	}
	for _, f := range forbidden {
		if seen[f] {
			t.Errorf("Part A: inv-zen-270 violation — internal/caronte/coordinated/ transitively imports %s; L10 MUST stay independent of F.7 wiring per D5 (cross-checks tests/compliance/inv_zen_270_d5_boundary_test.go)", f)
		}
	}

	recorder := &recordingBlastRadiusProvider{}
	if recorder.count() != 0 {
		t.Fatalf("recorder pre-dispatch count = %d; want 0 (test bug)", recorder.count())
	}

	const (
		workspaceID = "k7-emit"
		owningRepo  = "repo-owning"
	)
	ws := newWorkspace(t, workspaceID, permissivePolicy{}, owningRepo)
	breakage := newBreakageOwning(workspaceID, owningRepo)
	breakage.Workspace = ws

	audit := newTesseraAdapter(t, ctx, "k7-emit-itest", tmp)
	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: allowOracle{},
		Pool:     nil,
		Audit:    audit,
	}
	if _, err := coord.Dispatch(ctx, breakage); err != nil {
		t.Fatalf("Coordinator.Dispatch: %v", err)
	}

	if got := recorder.count(); got != 0 {
		t.Errorf("Part B as-built: recorder count = %d; want 0 (the as-built Coordinator does NOT wire BlastRadiusProvider emission per Phase H ship state — see K-7 doc-comment for the future-wiring flip semantics)", got)
	}

}
