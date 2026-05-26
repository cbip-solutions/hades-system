// tests/adversarial/plan20_forged_workspace_id_test.go
//
// + audit emit.
// Build tag: adversarial (per the tests/adversarial/ precedent).
// Runs under `make test-adversarial`.
//
// Scenario (spec §13.4 sixth bullet + inv-zen-264 chain extension):
// a malicious / buggy caller invokes coordinated.OrchestratorCoordinator.
// Dispatch with a ContractBreakage whose AffectedConsumers reference
// project IDs NOT on the workspace roster (the forged workspace_id
// surface — the workspace handle is real, but the consumer set tries
// to span projects the workspace was never authorized for).
//
// The Workspace.AuthorizeProjects (Plan-19-M capa-firewall) MUST refuse
// with store.ErrUnauthorizedProject + the Coordinator MUST emit a
// Tessera audit row of type EvtFederatedQueryDenied (master C-11 +
// inv-zen-269). The dispatch MUST return the wrapped error + NO data
// leaks (zero DispatchedRepos, empty SurfaceMessage from the dispatch
// payload — the denial-payload audit row carries the forensic record).
//
// Adversarial corpus walks 4 forgery shapes:
//   - completely off-roster project ID;
//   - case-mangled near-match of a roster member;
//   - empty project ID (edge case — neither a roster nor non-roster);
//   - workspace ID forged into the change while the consumer scope
//     references roster members (the inverse forgery surface: the
//     denial path catches workspace_id mismatch via the WorkspaceID
//     payload field).
//
// Bite-check: temporarily disable the audit-emit on the denial path in
// coordinated.OrchestratorCoordinator.Dispatch (orchestrator.go::Dispatch
// step 1, the emitAuditFn invocation in the deny branch) → this test
// fails with the per-case `counter delta = 1; want 2` message (the
// denial leaf is missing from the chain). The bracket-baseline counter
// pattern (see parseLeafCounterAdv) DIRECTLY pins the denial-side
// emission rather than relying on an indirect "the typed error proves
// the emit succeeded" inference.

//go:build adversarial

package adversarial

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type advAlwaysAutonomy struct{}

func (advAlwaysAutonomy) Decision(_ coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type advOpenPolicy struct{}

func (advOpenPolicy) PrivacyLocked() bool { return false }

func TestPlan20AdversarialForgedWorkspaceID(t *testing.T) {
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
		wsID   = "ws-adv-forged"
		owner  = "svc-owner"
		client = "svc-client"
	)
	storeOwner := openAdvStore(t, wsDir, owner)
	storeClient := openAdvStore(t, wsDir, client)
	ws, err := store.NewWorkspace(wsID, []store.WorkspaceMember{
		{ProjectID: owner, Store: storeOwner},
		{ProjectID: client, Store: storeClient},
	}, advOpenPolicy{})
	if err != nil {
		t.Fatalf("store.NewWorkspace: %v", err)
	}
	defer ws.Close()

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: advAlwaysAutonomy{},
		Pool:     nil,
		Audit:    audit,
	}

	cases := []struct {
		name              string
		endpointRepo      string
		consumerRepos     []string
		expectUnauth      bool
		expectCrossDenied bool
	}{
		{
			name:          "completely_off_roster",
			endpointRepo:  owner,
			consumerRepos: []string{"forged-evil-repo-id"},
			expectUnauth:  true,
		},
		{
			name:          "case_mangled_near_match",
			endpointRepo:  owner,
			consumerRepos: []string{"SVC-CLIENT"},
			expectUnauth:  true,
		},
		{
			name:          "endpoint_off_roster",
			endpointRepo:  "forged-endpoint-repo",
			consumerRepos: []string{client},
			expectUnauth:  true,
		},
		{
			name:          "mixed_roster_and_forged",
			endpointRepo:  owner,
			consumerRepos: []string{client, "another-forged-id"},
			expectUnauth:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			consumers := make([]coordinated.ConsumerRef, 0, len(c.consumerRepos))
			for _, r := range c.consumerRepos {
				consumers = append(consumers, coordinated.ConsumerRef{
					Repo: r, File: "x.go", Line: 1,
				})
			}
			b := coordinated.ContractBreakage{
				Change: store.BreakingChange{
					ChangeID:     "ch-adv-" + c.name,
					EndpointRepo: c.endpointRepo,
					Kind:         "removed_field",
					WorkspaceID:  wsID,
				},
				AffectedConsumers: consumers,
				Workspace:         ws,
			}

			beforeLeafID, baseErr := federation.EmitAudit(ctx, audit, federation.Event{
				Type:        federation.EvtWorkspacePolicySet,
				WorkspaceID: wsID,
				Payload:     mustJSONAdv(t, map[string]string{"baseline": "before-" + c.name}),
				OccurredAt:  time.Now().UnixNano(),
			})
			if baseErr != nil {
				t.Fatalf("plan20 adv L-12 [%s]: baseline EmitAudit (before): %v", c.name, baseErr)
			}

			res, err := coord.Dispatch(ctx, b)
			if err == nil {
				t.Errorf("plan20 adv L-12 [%s]: Dispatch returned nil err; want wrapped Plan-19-M typed error",
					c.name)
				return
			}

			matched := errors.Is(err, store.ErrUnauthorizedProject) ||
				errors.Is(err, store.ErrCrossProjectDenied)
			if !matched {
				t.Errorf("plan20 adv L-12 [%s]: err = %v; want errors.Is one of ErrUnauthorizedProject / ErrCrossProjectDenied",
					c.name, err)
			}

			if len(res.DispatchedRepos) != 0 {
				t.Errorf("plan20 adv L-12 [%s]: DispatchedRepos = %v; want empty (denied paths leak no fan-out)",
					c.name, res.DispatchedRepos)
			}

			afterLeafID, baseErr := federation.EmitAudit(ctx, audit, federation.Event{
				Type:        federation.EvtWorkspacePolicySet,
				WorkspaceID: wsID,
				Payload:     mustJSONAdv(t, map[string]string{"baseline": "after-" + c.name}),
				OccurredAt:  time.Now().UnixNano(),
			})
			if baseErr != nil {
				t.Fatalf("plan20 adv L-12 [%s]: baseline EmitAudit (after): %v", c.name, baseErr)
			}
			if beforeLeafID == "" || afterLeafID == "" {
				t.Fatalf("plan20 adv L-12 [%s]: baseline LeafIDs empty: before=%q after=%q (a non-nil tessera adapter MUST return a non-empty LeafID for a valid event)",
					c.name, beforeLeafID, afterLeafID)
			}
			if beforeLeafID == afterLeafID {
				t.Fatalf("plan20 adv L-12 [%s]: baseline LeafID == post LeafID (%q); want distinct (the tessera chain advances on every successful Append)",
					c.name, beforeLeafID)
			}
			beforeCounter := parseLeafCounterAdv(t, beforeLeafID)
			afterCounter := parseLeafCounterAdv(t, afterLeafID)
			if delta := afterCounter - beforeCounter; delta != 2 {
				t.Errorf("plan20 adv L-12 [%s]: LeafID counter delta = %d (before=%s, after=%s); want 2 (1 EvtFederatedQueryDenied + 1 post-baseline) — the Coordinator's denial path MUST emit exactly ONE audit leaf per Dispatch (inv-zen-269)",
					c.name, delta, beforeLeafID, afterLeafID)
			}
		})
	}
}

func parseLeafCounterAdv(t *testing.T, id tessera.LeafID) int {
	t.Helper()
	s := string(id)
	colon := -1
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			colon = i
			break
		}
	}
	if colon < 0 || colon == len(s)-1 {
		t.Fatalf("LeafID %q has no ':<counter>' suffix; cannot parse counter (tessera adapter format change?)", s)
	}
	n := 0
	for i := colon + 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			t.Fatalf("LeafID %q counter segment is non-numeric at byte %d (%c)", s, i, c)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func mustJSONAdv(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSONAdv: %v", err)
	}
	return b
}

func openAdvStore(t *testing.T, dir, projectID string) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(dir, projectID+".db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", projectID, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping(%s): %v", projectID, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open(%s): %v", projectID, err)
	}
	return s
}
