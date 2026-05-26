//go:build integration

package coordinated

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
)

func TestCoordinatorAutonomyWithMockedPool(t *testing.T) {
	disableKeychain(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tmp := t.TempDir()
	pool := &mockWorktreePool{}
	audit := newTesseraAdapter(t, ctx, "coord-itest-autonomy", tmp)

	const (
		workspaceID = "coord-autonomy"
		owningRepo  = "repo-owning"
	)
	ws := newPermissiveWorkspace(t, workspaceID, owningRepo)
	breakage := newBreakage(t, ws, workspaceID, owningRepo)

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: allowOracle{},
		Pool:     pool,
		Audit:    audit,
	}
	res, err := coord.Dispatch(ctx, breakage)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if res.Mode != coordinated.ModeAutonomy {
		t.Errorf("Mode = %q; want %q (Pool present + oracle Allow + permissive policy → Autonomy per C-8 §8.3 step 4a)",
			res.Mode, coordinated.ModeAutonomy)
	}

	if len(res.DispatchedRepos) == 0 {
		t.Errorf("DispatchedRepos empty; want at least one repo (the autonomy branch leases per unique affected-consumer repo)")
	}
	var foundOwning bool
	for _, r := range res.DispatchedRepos {
		if r == owningRepo {
			foundOwning = true
		}
	}
	if !foundOwning {
		t.Errorf("DispatchedRepos = %+v; want to contain %q", res.DispatchedRepos, owningRepo)
	}

	if res.AuditID == "" {
		t.Errorf("AuditID empty; want non-empty (every dispatch emits a Plan 14 audit row per C-11 / inv-zen-269)")
	}

	if res.SurfaceMessage == "" {
		t.Errorf("SurfaceMessage empty; want non-empty even on Autonomy (the DispatchResult contract requires it per orchestrator.go:148-153)")
	}

	if got := pool.leases(); got == 0 {
		t.Errorf("mockWorktreePool.Lease never called; the Autonomy branch MUST drive the pool (≥1 lease for ≥1 consumer dispatched)")
	}
}
