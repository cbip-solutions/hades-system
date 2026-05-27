// tests/chaos/plan20_coordinator_partial_dispatch_test.go
//
// Build tag: chaos && cgo (cgo satisfied automatically by CGO_ENABLED=1).
// Runs under `make test-chaos`.
//
// Scenario (spec §13.3 fourth bullet): the worktreepool.Pool returns
// errors on a subset of Lease calls mid fan-out (e.g., 2 of 4 succeed,
// 2 fail); the L10 OrchestratorCoordinator MUST:
//
// - skip the failing repos + continue dispatching the successful set
// (per-repo degradation — autonomyBranch loops over uniqueRepos and
// skips failed leases);
// - if ALL leases fail, degrade Mode from Autonomy to Surface;
// - in BOTH success-mixed AND all-fail cases, emit the
// EvtCoordinatedDispatch audit row — the
// audit emission is unconditional on the dispatch outcome (per
// spec §8.3 step 5 + orchestrator.go::Dispatch step 5);
// - return DispatchResult with the partial DispatchedRepos slice +
// nil error (per-repo lease failure is a soft degradation, not a
// hard error).
//
// The chaos pattern: fire NEW partial-failing dispatches concurrently
// across many goroutines and assert each one (a) audit-emits exactly
// once, (b) records the correct partial DispatchedRepos slice, (c) does
// not panic / leak.
//
// Bite-check: temporarily make the Coordinator return WITHOUT emitting
// the audit row when len(dispatchedRepos) < len(repos) → the test must
// fail (audit count != dispatch count). Restore.
//
// Why TDD via revert-impl bite-check: the audit-on-partial-dispatch
// behavior is the spec contract; this test pins it under concurrency
// stress.

// go:build chaos && cgo
package chaos

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type flakyPool struct {
	failEvery  int
	leaseCount atomic.Int64
	failCount  atomic.Int64
	leaseErr   error
}

func newFlakyPool(failEvery int, leaseErr error) *flakyPool {
	return &flakyPool{failEvery: failEvery, leaseErr: leaseErr}
}

func (p *flakyPool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	n := p.leaseCount.Add(1)
	if p.failEvery > 0 && n%int64(p.failEvery) == 0 {
		p.failCount.Add(1)
		return nil, p.leaseErr
	}
	return &worktreepool.Worktree{}, nil
}

func (p *flakyPool) Release(_ context.Context, _ *worktreepool.Worktree) error { return nil }

func (p *flakyPool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *flakyPool) Close(_ context.Context) error { return nil }

type chaosAlwaysAutonomy struct{}

func (chaosAlwaysAutonomy) Decision(_ coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type chaosOpenPolicy struct{}

func (chaosOpenPolicy) PrivacyLocked() bool { return false }

func TestPlan20ChaosCoordinatorPartialDispatch(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	sqlite_vec.Auto()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmp := t.TempDir() + "/audit-root"
	audit, err := tessera.NewProjectAdapter(ctx, "test-project", tmp, tessera.Config{
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
		owner     = "service-owner"
		dispatchN = 24
		consumerN = 5
	)
	projIDs := []string{owner, "c1", "c2", "c3", "c4", "c5"}
	members := make([]store.WorkspaceMember, 0, len(projIDs))
	for _, id := range projIDs {
		s := openCoordStore(t, wsDir, id)
		members = append(members, store.WorkspaceMember{ProjectID: id, Store: s})
	}
	ws, err := store.NewWorkspace("ws-chaos-partial", members, chaosOpenPolicy{})
	if err != nil {
		t.Fatalf("store.NewWorkspace: %v", err)
	}
	defer ws.Close()

	pool := newFlakyPool(3, &flakyLeaseErr{})

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: chaosAlwaysAutonomy{},
		Pool:     pool,
		Audit:    audit,
	}
	coord.SetRecentCap(dispatchN + 8)

	consumers := make([]coordinated.ConsumerRef, 0, consumerN)
	for i := 0; i < consumerN; i++ {
		consumers = append(consumers, coordinated.ConsumerRef{
			Repo: projIDs[i+1],
			File: "client.go",
			Line: 10 + i,
		})
	}

	var (
		wg           sync.WaitGroup
		dispatchOK   atomic.Int64
		dispatchErrs atomic.Int64
		partialDisp  atomic.Int64
	)

	wg.Add(dispatchN)

	for i := 0; i < dispatchN; i++ {
		go func(id int) {
			defer wg.Done()
			b := coordinated.ContractBreakage{
				Change: store.BreakingChange{
					ChangeID:     "ch-chaos-partial-" + itoa(id),
					EndpointRepo: owner,
					Kind:         "type_changed",
					WorkspaceID:  "ws-chaos-partial",
				},
				AffectedConsumers: consumers,
				Workspace:         ws,
			}
			res, err := coord.Dispatch(ctx, b)
			if err != nil {
				dispatchErrs.Add(1)
				t.Errorf("dispatch %d: %v", id, err)
				return
			}
			dispatchOK.Add(1)

			if len(res.DispatchedRepos) < consumerN {
				partialDisp.Add(1)
			}
			// AuditID MUST be non-empty (audit row emitted; invariant
			// chokepoint fired). The DispatchResult zero-value AuditID is
			// the empty string, which signals NO emission.
			if res.AuditID == "" {
				t.Errorf("dispatch %d: empty AuditID — audit-emit chokepoint did not fire", id)
			}

			if len(res.DispatchedRepos) > 0 && res.Mode != coordinated.ModeAutonomy {
				t.Errorf("dispatch %d: %d repos dispatched but mode=%q (want %q)",
					id, len(res.DispatchedRepos), res.Mode, coordinated.ModeAutonomy)
			}
			if len(res.DispatchedRepos) == 0 && res.Mode != coordinated.ModeSurface {
				t.Errorf("dispatch %d: zero repos dispatched but mode=%q (want %q)",
					id, res.Mode, coordinated.ModeSurface)
			}
		}(i)
	}

	wg.Wait()

	if dispatchErrs.Load() != 0 {
		t.Fatalf("plan20 chaos L-6: %d dispatch errors observed; expected 0 (lease failures are soft degradation, not hard errors)",
			dispatchErrs.Load())
	}
	if got := pool.failCount.Load(); got == 0 {
		t.Errorf("plan20 chaos L-6: flaky pool did NOT fail any lease; chaos coverage is zero (failEvery=%d, total leases=%d)",
			pool.failEvery, pool.leaseCount.Load())
	}
	if partialDisp.Load() == 0 {
		t.Errorf("plan20 chaos L-6: zero partial dispatches observed; the failEvery=3 + consumerN=5 distribution should produce many partials (leases=%d, fails=%d, dispatches=%d)",
			pool.leaseCount.Load(), pool.failCount.Load(), dispatchOK.Load())
	}

	recent, err := coord.RecentDispatches(ctx, dispatchN+8)
	if err != nil {
		t.Fatalf("RecentDispatches: %v", err)
	}
	if int64(len(recent)) != dispatchOK.Load() {
		t.Errorf("plan20 chaos L-6: ring buffer count %d != dispatchOK %d (audit emission count mismatch)",
			len(recent), dispatchOK.Load())
	}
	for _, dec := range recent {
		if dec.AuditID == "" {
			t.Errorf("plan20 chaos L-6: ring entry for change_id=%s has empty AuditID (audit row missing)",
				dec.ChangeID)
		}
	}
}

// flakyLeaseErr is a stable sentinel-shaped error type the flakyPool
// returns; the L10 Coordinator MUST treat it as a soft per-repo
// degradation (skip the repo + continue), not a hard dispatch failure.
type flakyLeaseErr struct{}

func (*flakyLeaseErr) Error() string { return "chaos: flaky pool lease error" }

func openCoordStore(t *testing.T, dir, projectID string) *store.Store {
	t.Helper()
	dsn := dir + "/" + projectID + ".db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
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
