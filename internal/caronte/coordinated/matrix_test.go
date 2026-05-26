package coordinated

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type matrixCell struct {
	name            string
	poolPresent     bool
	oracleMode      DispatchMode
	workspaceLocked bool
	crossProject    bool

	wantMode       DispatchMode
	wantDispatched []string
	wantAuditEvent federation.EventType
	wantErr        error
}

func TestDispatchMatrix(t *testing.T) {
	cells := []matrixCell{

		{
			name:        "1_pool_autonomy_unlocked_own",
			poolPresent: true, oracleMode: ModeAutonomy,
			workspaceLocked: false, crossProject: false,
			wantMode: ModeAutonomy, wantDispatched: []string{"owning"},
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "2_pool_autonomy_unlocked_cross",
			poolPresent: true, oracleMode: ModeAutonomy,
			workspaceLocked: false, crossProject: true,
			wantMode: ModeAutonomy, wantDispatched: []string{"client-a", "owning"},
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "3_pool_autonomy_locked_own",
			poolPresent: true, oracleMode: ModeAutonomy,
			workspaceLocked: true, crossProject: false,
			wantMode: ModeAutonomy, wantDispatched: []string{"owning"},
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "4_pool_autonomy_locked_cross_DENY",
			poolPresent: true, oracleMode: ModeAutonomy,
			workspaceLocked: true, crossProject: true,
			wantAuditEvent: federation.EvtFederatedQueryDenied,
			wantErr:        store.ErrCrossProjectDenied,
		},

		{
			name:        "5_pool_surface_unlocked_own",
			poolPresent: true, oracleMode: ModeSurface,
			workspaceLocked: false, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "6_pool_surface_unlocked_cross",
			poolPresent: true, oracleMode: ModeSurface,
			workspaceLocked: false, crossProject: true,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "7_pool_surface_locked_own",
			poolPresent: true, oracleMode: ModeSurface,
			workspaceLocked: true, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "8_pool_surface_locked_cross_DENY",
			poolPresent: true, oracleMode: ModeSurface,
			workspaceLocked: true, crossProject: true,
			wantAuditEvent: federation.EvtFederatedQueryDenied,
			wantErr:        store.ErrCrossProjectDenied,
		},

		{
			name:        "9_nopool_autonomy_unlocked_own_DOWNGRADE",
			poolPresent: false, oracleMode: ModeAutonomy,
			workspaceLocked: false, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "10_nopool_autonomy_unlocked_cross_DOWNGRADE",
			poolPresent: false, oracleMode: ModeAutonomy,
			workspaceLocked: false, crossProject: true,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "11_nopool_autonomy_locked_own_DOWNGRADE",
			poolPresent: false, oracleMode: ModeAutonomy,
			workspaceLocked: true, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "12_nopool_autonomy_locked_cross_DENY",
			poolPresent: false, oracleMode: ModeAutonomy,
			workspaceLocked: true, crossProject: true,
			wantAuditEvent: federation.EvtFederatedQueryDenied,
			wantErr:        store.ErrCrossProjectDenied,
		},

		{
			name:        "13_nopool_surface_unlocked_own",
			poolPresent: false, oracleMode: ModeSurface,
			workspaceLocked: false, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "14_nopool_surface_unlocked_cross",
			poolPresent: false, oracleMode: ModeSurface,
			workspaceLocked: false, crossProject: true,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "15_nopool_surface_locked_own",
			poolPresent: false, oracleMode: ModeSurface,
			workspaceLocked: true, crossProject: false,
			wantMode: ModeSurface, wantDispatched: nil,
			wantAuditEvent: federation.EvtCoordinatedDispatch,
		},

		{
			name:        "16_nopool_surface_locked_cross_DENY",
			poolPresent: false, oracleMode: ModeSurface,
			workspaceLocked: true, crossProject: true,
			wantAuditEvent: federation.EvtFederatedQueryDenied,
			wantErr:        store.ErrCrossProjectDenied,
		},
	}

	if len(cells) != 16 {
		t.Fatalf("matrix size drift: want 16 cells, got %d", len(cells))
	}

	for _, cell := range cells {
		cell := cell
		t.Run(cell.name, func(t *testing.T) {

			audit := newFakeAudit(t)
			audit.installEmitAuditFn(t)
			oracle := stubOracle(cell.oracleMode)

			ws := stubWorkspace(t, cell.workspaceLocked, "owning", "client-a")

			var pool worktreepool.Pool
			if cell.poolPresent {
				pool = &stubPool{}
			}

			coord := &OrchestratorCoordinator{
				Autonomy: oracle,
				Pool:     pool,
				Audit:    audit.Adapter(),
			}

			consumers := []ConsumerRef{
				{Repo: "owning", File: "owning.go", Line: 1},
			}
			if cell.crossProject {
				consumers = append(consumers,
					ConsumerRef{Repo: "client-a", File: "client.go", Line: 1},
				)
			}
			b := ContractBreakage{
				Change: store.BreakingChange{
					ChangeID:     "ch-" + cell.name,
					EndpointRepo: "owning",
					Kind:         "removed_field",
					WorkspaceID:  "ws-test",
				},
				AffectedConsumers: consumers,
				Workspace:         ws,
			}

			got, err := coord.Dispatch(context.Background(), b)

			if cell.wantErr != nil {
				if err == nil {
					t.Fatalf("Dispatch: want error wrapping %v, got nil", cell.wantErr)
				}
				if !errors.Is(err, cell.wantErr) {
					t.Errorf("Dispatch error: want wraps %v, got %v", cell.wantErr, err)
				}

				if audit.Count() != 1 {
					t.Errorf("audit count (deny path): want 1, got %d", audit.Count())
				}
				if evt := audit.LastEvent(); evt.Type != cell.wantAuditEvent {
					t.Errorf("audit event type: want %q, got %q", cell.wantAuditEvent, evt.Type)
				}

				ring, rerr := coord.RecentDispatches(context.Background(), 0)
				if rerr != nil {
					t.Fatalf("RecentDispatches (deny): %v", rerr)
				}
				if len(ring) != 0 {
					t.Errorf("ring entries (deny path): want 0, got %d", len(ring))
				}
				return
			}

			if err != nil {
				t.Fatalf("Dispatch: unexpected error: %v", err)
			}
			if got.Mode != cell.wantMode {
				t.Errorf("Mode: want %q, got %q", cell.wantMode, got.Mode)
			}
			if !slicesEq(got.DispatchedRepos, cell.wantDispatched) {
				t.Errorf("DispatchedRepos: want %v, got %v", cell.wantDispatched, got.DispatchedRepos)
			}
			if got.SurfaceMessage == "" {
				t.Errorf("SurfaceMessage: want non-empty, got empty")
			}
			if got.AuditID == "" {
				t.Errorf("AuditID: want non-empty, got empty")
			}

			wantPhrase := strings.ToUpper(string(cell.wantMode))
			if !strings.Contains(got.SurfaceMessage, wantPhrase) {
				t.Errorf("SurfaceMessage missing mode phrase %q; full: %s", wantPhrase, got.SurfaceMessage)
			}

			if audit.Count() != 1 {
				t.Errorf("audit count: want 1, got %d", audit.Count())
			}
			if evt := audit.LastEvent(); evt.Type != cell.wantAuditEvent {
				t.Errorf("audit event type: want %q, got %q", cell.wantAuditEvent, evt.Type)
			}

			ring, rerr := coord.RecentDispatches(context.Background(), 0)
			if rerr != nil {
				t.Fatalf("RecentDispatches: %v", rerr)
			}
			if len(ring) != 1 {
				t.Fatalf("ring entries: want 1 (success-path append), got %d", len(ring))
			}
			entry := ring[0]
			if entry.ChangeID != b.Change.ChangeID {
				t.Errorf("ring[0].ChangeID: want %q, got %q", b.Change.ChangeID, entry.ChangeID)
			}
			if entry.Mode != got.Mode {
				t.Errorf("ring[0].Mode: want %q, got %q", got.Mode, entry.Mode)
			}
			if !slicesEq(entry.DispatchedRepos, got.DispatchedRepos) {
				t.Errorf("ring[0].DispatchedRepos: want %v, got %v", got.DispatchedRepos, entry.DispatchedRepos)
			}
			if entry.AuditID != got.AuditID {
				t.Errorf("ring[0].AuditID: want %q, got %q", got.AuditID, entry.AuditID)
			}
			if entry.DecidedAt.IsZero() {
				t.Errorf("ring[0].DecidedAt: want non-zero, got zero time")
			}
		})
	}
}

func slicesEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
