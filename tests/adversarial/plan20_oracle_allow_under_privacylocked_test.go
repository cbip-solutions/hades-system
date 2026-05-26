// tests/adversarial/plan20_oracle_allow_under_privacylocked_test.go
//
// PrivacyLocked workspace still denies (double-gate).
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario (spec §13.4 eighth bullet + inv-zen-266 + master C-8/C-9
// §8.4): the autonomy oracle returns ModeAutonomy (the "Allow" case),
// but the workspace policy is PrivacyLocked. The Coordinator MUST
// defer to the capa-firewall (Workspace.AuthorizeProjects denies
// cross-project), NOT to the oracle's Allow.
//
// The compliance test inv_zen_266_integration_test.go already pins
// the single-shot scenario; this adversarial sibling stress-tests
// the double-gate ORDER (capa-firewall MUST run BEFORE oracle —
// inv-zen-266 ordering claim) under N concurrent dispatches with
// mixed roster + oracle setups.
//
// Adversarial corpus:
//   - 10 concurrent goroutines each fire a Dispatch with:
//       (a) PrivacyLocked workspace,
//       (b) ModeAutonomy oracle,
//       (c) cross-project consumers (both projects on roster);
//   - assert EVERY dispatch returns err wrapping
//     store.ErrCrossProjectDenied (NOT ModeAutonomy success);
//   - assert NO dispatch's DispatchedRepos is non-empty.
//
// Bite-check: temporarily flip the gate order in
// orchestrator.Dispatch (oracle BEFORE authorize, with oracle Allow
// winning) → the test must fail (ModeAutonomy result on a locked
// workspace). Restoring the canonical order returns the gate green.

//go:build adversarial

package adversarial

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type advLockedPolicy struct{}

func (advLockedPolicy) PrivacyLocked() bool { return true }

type advAllowOracle struct{}

func (advAllowOracle) Decision(_ coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

func TestPlan20AdversarialOracleAllowUnderPrivacyLocked(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	auditDir := filepath.Join(t.TempDir(), "audit-root")
	audit, err := tessera.NewProjectAdapter(ctx, "test-project", auditDir, tessera.Config{
		BatchMaxAge:         50_000_000,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	})
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	defer audit.Close()

	wsDir := t.TempDir()
	const (
		wsID   = "ws-adv-locked"
		owner  = "svc-owner"
		client = "svc-client"
	)
	storeOwner := openAdvStore(t, wsDir, owner)
	storeClient := openAdvStore(t, wsDir, client)
	ws, err := store.NewWorkspace(wsID, []store.WorkspaceMember{
		{ProjectID: owner, Store: storeOwner},
		{ProjectID: client, Store: storeClient},
	}, advLockedPolicy{})
	if err != nil {
		t.Fatalf("store.NewWorkspace: %v", err)
	}
	defer ws.Close()

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: advAllowOracle{},
		Pool:     nil,
		Audit:    audit,
	}

	const N = 10
	var (
		wg            sync.WaitGroup
		dispatchOK    atomic.Int64
		expectedDeny  atomic.Int64
		unexpectedErr atomic.Int64
		modeViolation atomic.Int64
	)

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(id int) {
			defer wg.Done()
			b := coordinated.ContractBreakage{
				Change: store.BreakingChange{
					ChangeID:     "ch-adv-locked-" + itoaAdv(id),
					EndpointRepo: owner,
					Kind:         "removed_field",
					WorkspaceID:  wsID,
				},
				AffectedConsumers: []coordinated.ConsumerRef{
					{Repo: owner, File: "owner.go", Line: 1},
					{Repo: client, File: "client.go", Line: 1},
				},
				Workspace: ws,
			}
			res, err := coord.Dispatch(ctx, b)
			if err == nil {
				dispatchOK.Add(1)
				t.Errorf("plan20 adv L-14 [goroutine %d]: Dispatch returned nil err; locked-cross-project must DENY", id)
				return
			}
			if errors.Is(err, store.ErrCrossProjectDenied) {
				expectedDeny.Add(1)
			} else {
				unexpectedErr.Add(1)
				t.Errorf("plan20 adv L-14 [goroutine %d]: err = %v; want errors.Is ErrCrossProjectDenied", id, err)
			}

			if res.Mode == coordinated.ModeAutonomy {
				modeViolation.Add(1)
				t.Errorf("plan20 adv L-14 [goroutine %d]: Mode = Autonomy (oracle Allow won over capa-firewall) — double-gate ORDER violated", id)
			}
			if len(res.DispatchedRepos) != 0 {
				t.Errorf("plan20 adv L-14 [goroutine %d]: DispatchedRepos = %v; want empty (denied path leaks no fan-out)",
					id, res.DispatchedRepos)
			}
		}(i)
	}
	wg.Wait()

	if got := expectedDeny.Load(); got != N {
		t.Errorf("plan20 adv L-14: %d/%d dispatches returned ErrCrossProjectDenied; want %d", got, N, N)
	}
	if dispatchOK.Load() != 0 {
		t.Errorf("plan20 adv L-14: %d/%d dispatches returned nil err; want 0 (capa-firewall must deny ALL)",
			dispatchOK.Load(), N)
	}
	if unexpectedErr.Load() != 0 {
		t.Errorf("plan20 adv L-14: %d/%d dispatches returned wrong error type; want 0", unexpectedErr.Load(), N)
	}
	if modeViolation.Load() != 0 {
		t.Errorf("plan20 adv L-14: %d/%d dispatches returned Mode=Autonomy on locked workspace; want 0 (double-gate order)",
			modeViolation.Load(), N)
	}
}
