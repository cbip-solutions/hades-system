// go:build integration
package crossplan

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func TestPlan5CapabilityDetection(t *testing.T) {
	disableKeychain(t)

	type cell struct {
		name         string
		usePool      bool
		oracle       coordinated.AutonomyOracle
		policy       caronte_store.WorkspacePolicy
		crossProject bool

		wantMode coordinated.DispatchMode
		wantErr  error
	}

	cases := []cell{
		{
			name:    "nil_pool_oracle_allow_permissive_OWNING",
			usePool: false, oracle: allowOracle{}, policy: permissivePolicy{}, crossProject: false,
			wantMode: coordinated.ModeSurface,
		},
		{
			name:    "nil_pool_oracle_deny_permissive_OWNING",
			usePool: false, oracle: denyOracle{}, policy: permissivePolicy{}, crossProject: false,
			wantMode: coordinated.ModeSurface,
		},
		{
			name:    "pool_oracle_allow_permissive_OWNING",
			usePool: true, oracle: allowOracle{}, policy: permissivePolicy{}, crossProject: false,
			wantMode: coordinated.ModeAutonomy,
		},
		{
			name:    "pool_oracle_deny_permissive_OWNING",
			usePool: true, oracle: denyOracle{}, policy: permissivePolicy{}, crossProject: false,
			wantMode: coordinated.ModeSurface,
		},
		{
			name:    "pool_oracle_allow_locked_CROSSPROJECT_DENY",
			usePool: true, oracle: allowOracle{}, policy: lockedPolicy{}, crossProject: true,
			wantErr: caronte_store.ErrCrossProjectDenied,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			tmp := t.TempDir()

			const (
				workspaceID = "k9-cap"
				owningRepo  = "repo-owning"
				clientRepo  = "repo-client"
			)
			ws := newWorkspace(t, workspaceID, tc.policy, owningRepo, clientRepo)

			breakage := newBreakageOwning(workspaceID, owningRepo)
			breakage.Workspace = ws
			if tc.crossProject {

				breakage.AffectedConsumers = append(breakage.AffectedConsumers, coordinated.ConsumerRef{
					Repo:   clientRepo,
					CallID: clientRepo + ":call:1",
					NodeID: clientRepo + ":node:1",
				})
			}

			var pool worktreepool.Pool
			if tc.usePool {
				pool = &mockWorktreePool{}
			}

			audit := newTesseraAdapter(t, ctx, "k9-cap-"+tc.name, tmp)
			coord := &coordinated.OrchestratorCoordinator{
				Autonomy: tc.oracle,
				Pool:     pool,
				Audit:    audit,
			}

			res, err := coord.Dispatch(ctx, breakage)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Dispatch err = %v; want %v wrapped", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Dispatch unexpected err: %v", err)
			}
			if res.Mode != tc.wantMode {
				t.Errorf("Mode = %q; want %q (cell %s)", res.Mode, tc.wantMode, tc.name)
			}
			if res.AuditID == "" {
				t.Errorf("AuditID empty; every dispatch emits audit per C-11 (cell %s)", tc.name)
			}
		})
	}
}
