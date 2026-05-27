// go:build cgo
package coordinated

import (
	"context"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func TestSisterClaim_DispatchResult_SurfaceMessage_AlwaysPopulated(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	ws := stubWorkspace(t, false, "owning", "client-a")
	audit := newDummyTesseraPtr(t)
	pool := &stubPool{}
	coord := &OrchestratorCoordinator{
		Autonomy: alwaysAutonomyOracle{},
		Pool:     pool,
		Audit:    audit,
	}

	res, err := coord.Dispatch(context.Background(), ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-sister-msg", WorkspaceID: "ws-test", EndpointRepo: "owning", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "client-a", File: "x.go", Line: 1},
		},
		Workspace: ws,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.SurfaceMessage == "" {
		t.Errorf("SurfaceMessage empty on Mode=%s; doc-comment claim 'ALWAYS populated' violated", res.Mode)
	}
	// Also exercise the Surface path (Pool=nil): SurfaceMessage MUST
	// still be populated.
	coord.Pool = nil
	res2, err := coord.Dispatch(context.Background(), ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-sister-msg-2", WorkspaceID: "ws-test", EndpointRepo: "owning", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "client-a", File: "x.go", Line: 2},
		},
		Workspace: ws,
	})
	if err != nil {
		t.Fatalf("Dispatch (surface): %v", err)
	}
	if res2.SurfaceMessage == "" {
		t.Errorf("SurfaceMessage empty on Mode=%s (Pool=nil surface branch); doc-comment claim 'ALWAYS populated' violated", res2.Mode)
	}
}

func TestSisterClaim_RecentDispatches_AuditID_Nonempty(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	ws := stubWorkspace(t, false, "owning", "client-a")
	audit := newDummyTesseraPtr(t)
	coord := &OrchestratorCoordinator{
		Autonomy: alwaysAutonomyOracle{},
		Pool:     &stubPool{},
		Audit:    audit,
	}

	_, err := coord.Dispatch(context.Background(), ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-sister-auditid", WorkspaceID: "ws-test", EndpointRepo: "owning", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "client-a", File: "x.go", Line: 1},
		},
		Workspace: ws,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	recent, err := coord.RecentDispatches(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentDispatches: %v", err)
	}
	if len(recent) == 0 {
		t.Fatalf("RecentDispatches returned empty; want ≥1 entry")
	}
	for _, dec := range recent {
		if dec.AuditID == "" {
			t.Errorf("DispatchDecision.AuditID empty for change_id=%s; defense-in-depth claim violated (chokepoint bypass surface)", dec.ChangeID)
		}
	}
}

func TestSisterClaim_Dispatch_NilWorkspace_ReturnsErrCoordinatorNoWorkspace(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: alwaysAutonomyOracle{},
		Pool:     &stubPool{},
		Audit:    audit.Adapter(),
	}
	_, err := coord.Dispatch(context.Background(), ContractBreakage{
		Change:            store.BreakingChange{ChangeID: "ch-sister-nilws", WorkspaceID: "ws-test", EndpointRepo: "owning", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{{Repo: "x", File: "x.go", Line: 1}},
	})
	if err == nil {
		t.Fatal("Dispatch with nil Workspace returned nil err; want ErrCoordinatorNoWorkspace")
	}
	if err != ErrCoordinatorNoWorkspace {
		t.Errorf("Dispatch err = %v; want ErrCoordinatorNoWorkspace exactly", err)
	}

	if got := audit.Count(); got != 0 {
		t.Errorf("audit emit count on nil-Workspace guard return = %d; want 0 (the wiring-bug guard MUST short-circuit BEFORE the deny-path emitAuditFn)", got)
	}
}

func TestSisterClaim_BuildSurfaceMessage_AutonomyMentionsDispatched(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	ws := stubWorkspace(t, false, "owning", "client-a", "client-b")
	audit := newDummyTesseraPtr(t)
	coord := &OrchestratorCoordinator{
		Autonomy: alwaysAutonomyOracle{},
		Pool:     &stubPool{},
		Audit:    audit,
	}
	res, err := coord.Dispatch(context.Background(), ContractBreakage{
		Change: store.BreakingChange{ChangeID: "ch-sister-surface-content", WorkspaceID: "ws-test", EndpointRepo: "owning", Kind: "removed_field"},
		AffectedConsumers: []ConsumerRef{
			{Repo: "client-a", File: "x.go", Line: 1},
			{Repo: "client-b", File: "y.go", Line: 2},
		},
		Workspace: ws,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	// Surface message MUST contain something — empty surface message
	// would defeat the §10 MCP surface contract.
	if res.SurfaceMessage == "" {
		t.Fatalf("SurfaceMessage empty; surface-content sister-test cannot pin further")
	}

	if !strings.Contains(res.SurfaceMessage, "ch-sister-surface-content") {
		t.Errorf("SurfaceMessage = %q; expected to contain change_id 'ch-sister-surface-content' (surface-content drift)",
			res.SurfaceMessage)
	}
}

type alwaysAutonomyOracle struct{}

func (alwaysAutonomyOracle) Decision(_ ContractBreakage) DispatchMode {
	return ModeAutonomy
}
