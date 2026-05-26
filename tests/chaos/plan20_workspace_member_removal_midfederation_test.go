// tests/chaos/plan20_workspace_member_removal_midfederation_test.go
//
// Build tag: chaos && cgo (cgo satisfied automatically by CGO_ENABLED=1).
// Runs under `make test-chaos`.
//
// Scenario (spec §13.3 second bullet): a caronte_workspaces row is deleted
// during an in-flight FederatedQuery against the in-memory Plan-19-M
// store.Workspace. The seam:
//
//   - Plan-19-M store.Workspace.FederatedQuery is a process-local fan-out
//     across the in-memory roster; it does NOT read from the persistent
//     caronte_workspaces table mid-query (the roster is fixed at
//     NewWorkspace construction time).
//   - Phase A's federation.WorkspaceFederationDB.RemoveWorkspace deletes
//     the persistent row + CASCADE drops members + contract_links +
//     breaking_changes + breaking_change_consumers.
//
// Therefore the property under chaos: the persistent-side delete MUST NOT
// (a) panic any concurrent FederatedQuery goroutine, (b) corrupt the
// federation DB invariants (FK CASCADE chain remains consistent), or
// (c) cause the in-memory FederatedQuery to leak data from a deleted
// workspace (the snapshot-at-entry behaviour is what we're pinning —
// the in-memory roster keeps serving until the Workspace handle is
// Close()d, which is the operator's signal that the federation is gone).
//
// Bite-check candidates:
//   - Remove the persistent-side CASCADE on caronte_workspace_members →
//     RemoveWorkspace fails with FK violation (members rows survive). The
//     chaos test detects this as the persistent-side error.
//   - Make Workspace.FederatedQuery read from a mutable roster (instead of
//     the snapshot at NewWorkspace) → mid-query removal would surface as
//     a partial fan-out. The chaos test detects this as scope mismatch.
//
// Why TDD via revert-impl bite-check: both invariants pass-from-start;
// the test pins them against future regressions. The Phase A CASCADE
// chain is already wired (Task L-3 verified breaking_changes); this test
// pins the workspaces↔members chain + the in-memory snapshot semantics
// jointly under concurrent assault.

//go:build chaos && cgo

package chaos

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestPlan20ChaosWorkspaceMemberRemovalMidFederation(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "workspace.db")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	wsdb, err := federation.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	defer wsdb.Close()

	const (
		wsID  = "ws-chaos-member-removal"
		owner = "service-a"
		peer  = "service-b"
	)

	if err := seedFedMembers(ctx, wsdb, wsID, owner, peer); err != nil {
		t.Fatalf("seedFedMembers: %v", err)
	}

	// Construct the in-memory Plan-19-M Workspace OVER per-project stores.
	// The roster is snapshot at construct time; the persistent-side
	// RemoveWorkspace MUST NOT alter the in-memory scope.
	storeA := openProjectStore(t, dbDir, owner)
	storeB := openProjectStore(t, dbDir, peer)
	ws, err := store.NewWorkspace(wsID, []store.WorkspaceMember{
		{ProjectID: owner, Store: storeA},
		{ProjectID: peer, Store: storeB},
	}, nil)
	if err != nil {
		t.Fatalf("store.NewWorkspace: %v", err)
	}
	defer ws.Close()

	const (
		sweepRounds = 8
		M           = 12
		queryRounds = 25
	)
	var (
		wg         sync.WaitGroup
		partialFan atomic.Int64
		queryErrs  atomic.Int64
		sweepErrs  atomic.Int64
		querySeen  atomic.Int64
	)

	wg.Add(M + 1)

	go func() {
		defer wg.Done()
		for i := 0; i < sweepRounds; i++ {
			if _, err := wsdb.RemoveWorkspace(ctx, wsID); err != nil {
				sweepErrs.Add(1)
				t.Errorf("sweep round %d RemoveWorkspace: %v", i, err)
				return
			}
			time.Sleep(5 * time.Millisecond)
			if err := seedFedMembers(ctx, wsdb, wsID, owner, peer); err != nil {
				sweepErrs.Add(1)
				t.Errorf("sweep round %d reseed: %v", i, err)
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
	}()

	for i := 0; i < M; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < queryRounds; j++ {

				res, err := ws.FederatedQuery(ctx, store.FederatedQuery{})
				if err != nil {

					queryErrs.Add(1)
					t.Errorf("query goroutine %d round %d: %v", id, j, err)
					return
				}
				if len(res) != 2 {
					partialFan.Add(1)
					t.Errorf("query goroutine %d round %d: partial fan-out %d != 2",
						id, j, len(res))
				}
				querySeen.Add(1)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	if got := partialFan.Load(); got != 0 {
		t.Errorf("plan20 chaos L-4: %d partial fan-outs observed across %d query rounds; expected 0 (in-memory snapshot semantic violated)",
			got, querySeen.Load())
	}
	if got := queryErrs.Load(); got != 0 {
		t.Errorf("plan20 chaos L-4: %d query errors observed; expected 0", got)
	}
	if got := sweepErrs.Load(); got != 0 {
		t.Errorf("plan20 chaos L-4: %d sweep errors observed; expected 0", got)
	}

	if _, err := wsdb.RemoveWorkspace(ctx, wsID); err != nil {
		t.Errorf("final RemoveWorkspace: %v", err)
	}
	dangling, err := countDanglingMembers(ctx, wsdb.DB())
	if err != nil {
		t.Fatalf("countDanglingMembers: %v", err)
	}
	if dangling != 0 {
		t.Errorf("plan20 chaos L-4: %d dangling members rows after final delete; expected 0 (FK CASCADE invariant)", dangling)
	}
}

func seedFedMembers(ctx context.Context, wsdb *federation.WorkspaceFederationDB, wsID, owner, peer string) error {
	if err := wsdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: owner,
		PolicyLocked:  false,
		CreatedAt:     time.Now().Unix(),
		SchemaVersion: 1,
	}); err != nil {
		return err
	}
	for _, p := range []string{owner, peer} {
		if err := wsdb.AddMember(ctx, federation.MemberRow{
			WorkspaceID:  wsID,
			ProjectID:    p,
			RegisteredAt: time.Now().Unix(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// countDanglingMembers reports caronte_workspace_members rows with no
// matching caronte_workspaces parent. Under FK + CASCADE this MUST be 0
// at every observable instant.
func countDanglingMembers(ctx context.Context, db *sql.DB) (int, error) {
	const q = `SELECT COUNT(*) FROM caronte_workspace_members m
	           WHERE NOT EXISTS (SELECT 1 FROM caronte_workspaces w
	                             WHERE w.workspace_id = m.workspace_id)`
	var n int
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func openProjectStore(t *testing.T, dbDir, projectID string) *store.Store {
	t.Helper()
	dsn := filepath.Join(dbDir, projectID+".db") + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
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

var _ = errors.New
