// go:build integration
package coordinated

import (
	"context"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
)

func TestCoordinatorSurfaceWithNilPool(t *testing.T) {
	disableKeychain(t)

	type subCase struct {
		name   string
		oracle coordinated.AutonomyOracle
	}
	cases := []subCase{
		{name: "allow_oracle_nil_pool_downgrades", oracle: allowOracle{}},
		{name: "deny_oracle_nil_pool_trivial_surface", oracle: denyOracle{}},
	}

	for _, sc := range cases {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			tmp := t.TempDir()
			audit := newTesseraAdapter(t, ctx, "coord-itest-surface-"+sc.name, tmp)

			const (
				workspaceID = "coord-surface"
				owningRepo  = "repo-owning"
			)
			ws := newPermissiveWorkspace(t, workspaceID, owningRepo)
			breakage := newBreakage(t, ws, workspaceID, owningRepo)

			coord := &coordinated.OrchestratorCoordinator{
				Autonomy: sc.oracle,
				Pool:     nil,
				Audit:    audit,
			}
			res, err := coord.Dispatch(ctx, breakage)
			if err != nil {
				t.Fatalf("Dispatch: %v", err)
			}

			if res.Mode != coordinated.ModeSurface {
				t.Errorf("Mode = %q; want %q (Pool=nil ALWAYS short-circuits to surface per C-8 §8.3 step 3)",
					res.Mode, coordinated.ModeSurface)
			}

			if len(res.DispatchedRepos) != 0 {
				t.Errorf("DispatchedRepos = %+v; want empty (the surface branch does not lease)", res.DispatchedRepos)
			}

			if res.SurfaceMessage == "" {
				t.Errorf("SurfaceMessage empty; want a structured recommendation (no-stub doctrine: ModeSurface is real production code)")
			}

			if res.AuditID == "" {
				t.Errorf("AuditID empty; want non-empty (surface dispatch ALSO emits audit per C-11)")
			}
		})
	}
}
