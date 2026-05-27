// tests/chaos/plan20_stale_extract_concurrent_breaking_test.go
//
// Build tag: chaos && cgo (cgo satisfied automatically by CGO_ENABLED=1).
// Runs under `make test-chaos`.
//
// Scenario (spec §13.3 first bullet): an extractor re-runs on the server repo
// (deleting + re-creating breaking_changes rows for the workspace) WHILE
// scanner goroutines iterate breaking_changes + the contract_links JOIN to
// fan out the consumer set. Assert: no breaking_changes row ever references
// a now-deleted workspace_id (the ON DELETE CASCADE on breaking_changes(
// workspace_id) protects; this test pins the invariant under concurrency).
//
// Bite-check: temporarily remove the CASCADE clause on
// federation/schema.go's breaking_changes table → this test must fail
// (orphan rows survive after a workspace deletion); restore.
//
// The "orphan" definition used here is: a breaking_changes row whose
// workspace_id does NOT appear in caronte_workspaces. With FK + CASCADE,
// the count MUST always be zero — even mid-race, the FK rejects the
// dangling row (or the CASCADE clears it before the scan reads it).

// go:build chaos && cgo
package chaos

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func TestPlan20ChaosStaleExtractConcurrentBreaking(t *testing.T) {
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
		wsID     = "ws-chaos-stale"
		owner    = "server"
		consumer = "client"
		N        = 8
	)

	if err := seedWorkspaceStale(ctx, wsdb, wsID, owner, consumer, N, 0); err != nil {
		t.Fatalf("initial seed: %v", err)
	}

	const (
		M           = 12
		sweepRounds = 4
		scanRounds  = 25
	)

	var (
		wg        sync.WaitGroup
		orphanCnt atomic.Int64
		scanOK    atomic.Int64
		sweepErrs atomic.Int64
		joinReads atomic.Int64
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
			time.Sleep(8 * time.Millisecond)
			if err := seedWorkspaceStale(ctx, wsdb, wsID, owner, consumer, N, i+1); err != nil {
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
			for j := 0; j < scanRounds; j++ {
				orphans, err := countOrphanBreakingChanges(ctx, wsdb.DB())
				if err != nil {
					t.Errorf("scan goroutine %d round %d: %v", id, j, err)
					return
				}
				if orphans > 0 {
					orphanCnt.Add(int64(orphans))
				}

				danglingConsumers, err := countDanglingConsumers(ctx, wsdb.DB())
				if err != nil {
					t.Errorf("scan goroutine %d round %d dangling consumers: %v", id, j, err)
					return
				}
				if danglingConsumers > 0 {
					t.Errorf("scan goroutine %d round %d: %d dangling breaking_change_consumers rows observed; expected 0 (FK CASCADE invariant)",
						id, j, danglingConsumers)
				}
				joinReads.Add(1)
				scanOK.Add(1)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	if got := orphanCnt.Load(); got != 0 {
		t.Errorf("plan20 chaos L-3: %d orphan breaking_changes rows observed across %d scan rounds (joins %d); expected 0 (ON DELETE CASCADE invariant)",
			got, scanOK.Load(), joinReads.Load())
	}
	if sweepErrs.Load() != 0 {
		t.Errorf("plan20 chaos L-3: %d sweep errors observed; expected 0", sweepErrs.Load())
	}
}

// seedWorkspaceStale registers wsID with owner+consumer members + inserts
// N breaking_changes rows + 1 consumer per row. round is folded into the
// change_id so successive seed rounds produce distinct primary keys (so
// they do NOT collide post-CASCADE).
func seedWorkspaceStale(ctx context.Context, wsdb *federation.WorkspaceFederationDB, wsID, owner, consumer string, n, round int) error {
	if err := wsdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   wsID,
		OwningProject: owner,
		PolicyLocked:  false,
		CreatedAt:     time.Now().Unix(),
		SchemaVersion: 1,
	}); err != nil {
		return err
	}
	for _, p := range []string{owner, consumer} {
		if err := wsdb.AddMember(ctx, federation.MemberRow{
			WorkspaceID:  wsID,
			ProjectID:    p,
			RegisteredAt: time.Now().Unix(),
		}); err != nil {
			return err
		}
	}
	for i := 0; i < n; i++ {
		changeID := makeID("ch", round, i)
		endpointID := makeID("ep", round, i)
		if err := wsdb.InsertBreakingChange(ctx, federation.BreakingChange{
			ChangeID:     changeID,
			WorkspaceID:  wsID,
			EndpointID:   endpointID,
			EndpointRepo: owner,
			Kind:         "param_added_required",
			Detail:       `{"op":"added"}`,
			DetectedAt:   time.Now().Unix(),
			DetectorID:   "oasdiff",
		}); err != nil {
			return err
		}
		if err := wsdb.InsertBreakingChangeConsumer(ctx, federation.BreakingChangeConsumer{
			ChangeID: changeID,
			CallID:   makeID("call", round, i),
			CallRepo: consumer,
		}); err != nil {
			return err
		}
	}
	return nil
}

// countOrphanBreakingChanges queries the dangling-FK case: a
// breaking_changes row whose workspace_id has no caronte_workspaces row.
// Under SQLite FK enforcement + ON DELETE CASCADE this count MUST be zero
// at every observable instant; non-zero ⇒ a real FK invariant violation.
func countOrphanBreakingChanges(ctx context.Context, db *sql.DB) (int, error) {
	const q = `SELECT COUNT(*) FROM breaking_changes bc
	           WHERE NOT EXISTS (SELECT 1 FROM caronte_workspaces w
	                             WHERE w.workspace_id = bc.workspace_id)`
	var n int
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func countDanglingConsumers(ctx context.Context, db *sql.DB) (int, error) {
	const q = `SELECT COUNT(*) FROM breaking_change_consumers bcc
	           WHERE NOT EXISTS (SELECT 1 FROM breaking_changes bc
	                             WHERE bc.change_id = bcc.change_id)`
	var n int
	if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// makeID synthesizes a deterministic id from a prefix + round + index.
// Distinct rounds produce distinct ids so successive reseeds do not
// re-insert the same primary key.
func makeID(prefix string, round, i int) string {
	return prefix + "-" + itoa(round) + "-" + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
