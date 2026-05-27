package coordinated

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

type fakeAudit struct {
	adapter   *tessera.Adapter
	count     atomic.Int64
	lastEvent federation.Event
	failWith  error
}

func newFakeAudit(t *testing.T) *fakeAudit {
	t.Helper()
	a := newDummyTesseraPtr(t)
	return &fakeAudit{adapter: a}
}

func (f *fakeAudit) Adapter() *tessera.Adapter { return f.adapter }

func (f *fakeAudit) Count() int { return int(f.count.Load()) }

func (f *fakeAudit) LastEvent() federation.Event { return f.lastEvent }

func (f *fakeAudit) installEmitAuditFn(t *testing.T) {
	t.Helper()
	prev := emitAuditFn
	emitAuditFn = func(_ context.Context, _ *tessera.Adapter, e federation.Event) (tessera.LeafID, error) {
		f.count.Add(1)
		f.lastEvent = e
		if f.failWith != nil {
			return "", f.failWith
		}
		return tessera.LeafID(fmt.Sprintf("leaf-%d", f.count.Load())), nil
	}
	t.Cleanup(func() { emitAuditFn = prev })
}

type stubOracle DispatchMode

func (s stubOracle) Decision(_ ContractBreakage) DispatchMode {
	return DispatchMode(s)
}

type stubPool struct {
	leaseErr   error
	releaseErr error
	leaseCount atomic.Int64
}

func (p *stubPool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	if p.leaseErr != nil {
		return nil, p.leaseErr
	}
	p.leaseCount.Add(1)
	return &worktreepool.Worktree{}, nil
}

func (p *stubPool) Release(_ context.Context, _ *worktreepool.Worktree) error {
	return p.releaseErr
}

func (p *stubPool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *stubPool) Close(_ context.Context) error { return nil }

func newDummyTesseraPtr(t *testing.T) *tessera.Adapter {
	t.Helper()
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	tmp := t.TempDir() + "/audit-root"
	a, err := tessera.NewProjectAdapter(context.Background(), "test-project", tmp, tessera.Config{
		BatchMaxAge:         50_000_000,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	})
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func stubWorkspace(t *testing.T, locked bool, projectIDs ...string) *store.Workspace {
	t.Helper()
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	members := make([]store.WorkspaceMember, 0, len(projectIDs))
	for _, id := range projectIDs {
		s, err := store.Open(context.Background(), openTestDB(t, id))
		if err != nil {
			t.Fatalf("store.Open(%s): %v", id, err)
		}
		members = append(members, store.WorkspaceMember{ProjectID: id, Store: s})
	}
	var policy store.WorkspacePolicy
	if locked {
		policy = lockedTestPolicy{}
	} else {
		policy = openTestPolicy{}
	}
	ws, err := store.NewWorkspace("ws-test", members, policy)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func openTestDB(t *testing.T, label string) *sql.DB {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), label+".db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL"
	db, err := sql.Open(store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", label, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("ping(%s): %v", label, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type lockedTestPolicy struct{}

func (lockedTestPolicy) PrivacyLocked() bool { return true }

type openTestPolicy struct{}

func (openTestPolicy) PrivacyLocked() bool { return false }

func TestOrchestratorCoordinatorSurfaceModeNoPool(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	oracle := stubOracle(ModeSurface)
	ws := stubWorkspace(t, false, "owning")

	coord := &OrchestratorCoordinator{
		Autonomy: oracle,
		Pool:     nil,
		Audit:    audit.Adapter(),
	}

	b := ContractBreakage{
		Change:    store.BreakingChange{ChangeID: "ch-1", WorkspaceID: "ws-a", EndpointRepo: "owning", Kind: "removed_field"},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", CallID: "c-1", NodeID: "n-1", File: "a.go", Line: 10},
		},
	}
	got, err := coord.Dispatch(context.Background(), b)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got.Mode != ModeSurface {
		t.Errorf("Mode: want %q, got %q", ModeSurface, got.Mode)
	}
	if got.SurfaceMessage == "" {
		t.Errorf("SurfaceMessage: want non-empty (recommendation), got empty")
	}
	if len(got.DispatchedRepos) != 0 {
		t.Errorf("DispatchedRepos: want empty (no pool), got %v", got.DispatchedRepos)
	}
	if got.AuditID == "" {
		t.Errorf("AuditID: want non-empty (audit emitted), got empty")
	}

	if got, want := audit.Count(), 1; got != want {
		t.Errorf("audit appends: want %d, got %d", want, got)
	}

	if got := audit.LastEvent().Type; got != federation.EvtCoordinatedDispatch {
		t.Errorf("audit event type: want %q, got %q", federation.EvtCoordinatedDispatch, got)
	}
}

func TestOrchestratorCoordinator_ErrCoordinatorNoOracle(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: nil,
		Audit:    audit.Adapter(),
	}
	_, err := coord.Dispatch(context.Background(), ContractBreakage{})
	if !errors.Is(err, ErrCoordinatorNoOracle) {
		t.Errorf("Dispatch(nil Autonomy): want ErrCoordinatorNoOracle, got %v", err)
	}
	if got := audit.Count(); got != 0 {
		t.Errorf("audit appends on wiring-bug: want 0 (no side effect), got %d", got)
	}
}

func TestOrchestratorCoordinator_ErrCoordinatorNoAudit(t *testing.T) {
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    nil,
	}
	_, err := coord.Dispatch(context.Background(), ContractBreakage{})
	if !errors.Is(err, ErrCoordinatorNoAudit) {
		t.Errorf("Dispatch(nil Audit): want ErrCoordinatorNoAudit, got %v", err)
	}
}

func TestOrchestratorCoordinator_ErrCoordinatorNoWorkspace(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	_, err := coord.Dispatch(context.Background(), ContractBreakage{Workspace: nil})
	if !errors.Is(err, ErrCoordinatorNoWorkspace) {
		t.Errorf("Dispatch(nil Workspace): want ErrCoordinatorNoWorkspace, got %v", err)
	}
	if got := audit.Count(); got != 0 {
		t.Errorf("audit appends on wiring-bug: want 0, got %d", got)
	}
}

// TestOrchestratorCoordinator_AuditEmitFailureReturnsErr pins step 5's
// chokepoint contract: if emitAuditFn fails, the whole Dispatch
// returns the error rather than dispatch-silently (invariant
// chokepoint guarantee — every dispatch MUST emit a leaf, or fail
// loudly).
func TestOrchestratorCoordinator_AuditEmitFailureReturnsErr(t *testing.T) {
	audit := newFakeAudit(t)
	audit.failWith = errors.New("audit-chain unavailable")
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	ws := stubWorkspace(t, false, "owning")
	b := ContractBreakage{
		Change:    store.BreakingChange{ChangeID: "ch-fail", WorkspaceID: "ws-a", EndpointRepo: "owning", Kind: "removed_field"},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", CallID: "c-1", File: "a.go", Line: 1},
		},
	}
	_, err := coord.Dispatch(context.Background(), b)
	if err == nil {
		t.Fatalf("Dispatch: want error wrapping audit failure, got nil")
	}
	if want := "audit-chain unavailable"; !contains(err.Error(), want) {
		t.Errorf("Dispatch error: want contains %q, got %q", want, err.Error())
	}
}

// TestOrchestratorCoordinator_DenyPathAuditFailure pins the deny-path
// symmetry of the invariant chokepoint contract: when AuthorizeProjects
// denies AND the emit-audit call fails, the returned error MUST surface
// BOTH (a) the original capa-firewall denial reason AND (b) the
// audit-emit failure so the operator can see that the denial was not
// recorded. The success path already does this (the prior test); this
// test pins the matching guarantee on the denial path.
//
// Bite-check before the impl fix: the call site used `_, _ =
// emitAuditFn(...)`, silently dropping the audit-emit error and
// returning only the denial — meaning a deny + audit-chain-down combo
// looked identical to a deny + audit-chain-OK combo from the caller's
// view, even though the audit chain never saw the denial. The fix wraps
// the emit error with errors.Join so both errors are unwrap-discoverable
// (errors.Is across both).
func TestOrchestratorCoordinator_DenyPathAuditFailure(t *testing.T) {
	audit := newFakeAudit(t)
	audit.failWith = errors.New("audit-chain unavailable during denial")
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}

	ws := stubWorkspace(t, true, "owning", "client-a")
	b := ContractBreakage{
		Change:    store.BreakingChange{ChangeID: "ch-deny-and-audit-fail", WorkspaceID: "ws-a", EndpointRepo: "owning", Kind: "removed_field"},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", CallID: "c-1", File: "a.go", Line: 1},
			{Repo: "client-a", CallID: "c-2", File: "b.go", Line: 2},
		},
	}
	_, err := coord.Dispatch(context.Background(), b)
	if err == nil {
		t.Fatalf("Dispatch: want error wrapping BOTH deny AND audit-emit failure, got nil")
	}
	// (a) the underlying capa-firewall denial sentinel MUST be
	// errors.Is-discoverable (so callers that special-case denials can
	// still distinguish).
	if !errors.Is(err, store.ErrCrossProjectDenied) {
		t.Errorf("Dispatch error: want errors.Is(err, ErrCrossProjectDenied) true, got %v", err)
	}
	// (b) the audit-emit failure MUST also be errors.Is-discoverable
	// (operator-visibility contract: a failed denial-audit must NOT be
	// silently swallowed — that breaks the invariant chokepoint
	// symmetry between success and denial paths).
	if !errors.Is(err, audit.failWith) {
		t.Errorf("Dispatch error: want errors.Is(err, %q) true (deny-path audit-emit-failure surfaced), got %v",
			audit.failWith, err)
	}

	if got, want := audit.Count(), 1; got != want {
		t.Errorf("audit attempts: want %d (chokepoint reached even though it failed), got %d", want, got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRecentDispatchesEmpty(t *testing.T) {
	coord := &OrchestratorCoordinator{}
	got, err := coord.RecentDispatches(context.Background(), 0)
	if err != nil {
		t.Fatalf("RecentDispatches(empty): unexpected error %v", err)
	}
	if got == nil {
		t.Errorf("RecentDispatches(empty): want non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("RecentDispatches(empty): want len 0, got %d", len(got))
	}
}

func TestRecentDispatchesOneEntry(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Pool:     nil,
		Audit:    audit.Adapter(),
	}
	ws := stubWorkspace(t, false, "owning")
	b := ContractBreakage{
		Change: store.BreakingChange{
			ChangeID: "ch-ring-1", EndpointRepo: "owning", WorkspaceID: "ws-test", Kind: "removed_field",
		},
		Workspace: ws,
		AffectedConsumers: []ConsumerRef{
			{Repo: "owning", File: "a.go", Line: 1},
		},
	}
	before := time.Now()
	result, err := coord.Dispatch(context.Background(), b)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	got, err := coord.RecentDispatches(context.Background(), 0)
	if err != nil {
		t.Fatalf("RecentDispatches: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("RecentDispatches: want 1 entry, got %d", len(got))
	}
	entry := got[0]
	if entry.ChangeID != "ch-ring-1" {
		t.Errorf("entry.ChangeID: want ch-ring-1, got %q", entry.ChangeID)
	}
	if entry.Mode != ModeSurface {
		t.Errorf("entry.Mode: want %q, got %q", ModeSurface, entry.Mode)
	}
	if entry.AuditID != result.AuditID {
		t.Errorf("entry.AuditID: want %q (matches DispatchResult.AuditID), got %q",
			result.AuditID, entry.AuditID)
	}
	if entry.DecidedAt.Before(before) || entry.DecidedAt.After(time.Now()) {
		t.Errorf("entry.DecidedAt: want within [%v, now], got %v", before, entry.DecidedAt)
	}
}

func TestRecentDispatchesCapRotation(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	coord.SetRecentCap(3)
	ws := stubWorkspace(t, false, "owning")

	for i := 1; i <= 5; i++ {
		b := ContractBreakage{
			Change: store.BreakingChange{
				ChangeID:     fmt.Sprintf("ch-%d", i),
				EndpointRepo: "owning",
				WorkspaceID:  "ws-test",
				Kind:         "removed_field",
			},
			Workspace: ws,
			AffectedConsumers: []ConsumerRef{
				{Repo: "owning", File: "a.go", Line: 1},
			},
		}
		if _, err := coord.Dispatch(context.Background(), b); err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}
	got, err := coord.RecentDispatches(context.Background(), 0)
	if err != nil {
		t.Fatalf("RecentDispatches: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("RecentDispatches(cap=3, n=5): want 3 entries, got %d", len(got))
	}

	wantOrder := []string{"ch-5", "ch-4", "ch-3"}
	for i, want := range wantOrder {
		if got[i].ChangeID != want {
			t.Errorf("ring[%d].ChangeID: want %q (most-recent-first), got %q",
				i, want, got[i].ChangeID)
		}
	}
}

func TestRecentDispatchesLimitClamps(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	coord.SetRecentCap(5)
	ws := stubWorkspace(t, false, "owning")
	for i := 1; i <= 3; i++ {
		b := ContractBreakage{
			Change: store.BreakingChange{
				ChangeID:     fmt.Sprintf("ch-%d", i),
				EndpointRepo: "owning",
				WorkspaceID:  "ws-test",
				Kind:         "removed_field",
			},
			Workspace: ws,
			AffectedConsumers: []ConsumerRef{
				{Repo: "owning", File: "a.go", Line: 1},
			},
		}
		if _, err := coord.Dispatch(context.Background(), b); err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}

	got, _ := coord.RecentDispatches(context.Background(), 10)
	if len(got) != 3 {
		t.Errorf("limit=10, current=3: want 3, got %d", len(got))
	}

	got, _ = coord.RecentDispatches(context.Background(), 2)
	if len(got) != 2 {
		t.Errorf("limit=2, current=3: want 2, got %d", len(got))
	}
	if got[0].ChangeID != "ch-3" || got[1].ChangeID != "ch-2" {
		t.Errorf("limit=2: want [ch-3, ch-2], got [%s, %s]",
			got[0].ChangeID, got[1].ChangeID)
	}

	got, _ = coord.RecentDispatches(context.Background(), 0)
	if len(got) != 3 {
		t.Errorf("limit=0: want all (3), got %d", len(got))
	}
}

func TestSetRecentCapNoOpOnNonPositive(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	coord.SetRecentCap(5)

	ws := stubWorkspace(t, false, "owning")
	b := ContractBreakage{
		Change: store.BreakingChange{
			ChangeID: "ch-1", EndpointRepo: "owning", WorkspaceID: "ws-test", Kind: "removed_field",
		},
		Workspace:         ws,
		AffectedConsumers: []ConsumerRef{{Repo: "owning", File: "a.go", Line: 1}},
	}
	if _, err := coord.Dispatch(context.Background(), b); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	coord.SetRecentCap(0)
	got, _ := coord.RecentDispatches(context.Background(), 0)
	if len(got) != 1 {
		t.Errorf("after SetRecentCap(0): want 1 entry preserved, got %d", len(got))
	}

	coord.SetRecentCap(-5)
	got, _ = coord.RecentDispatches(context.Background(), 0)
	if len(got) != 1 {
		t.Errorf("after SetRecentCap(-5): want 1 entry preserved, got %d", len(got))
	}
}

func TestSetRecentCapTruncatesOldest(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	coord.SetRecentCap(5)
	ws := stubWorkspace(t, false, "owning")
	for i := 1; i <= 5; i++ {
		b := ContractBreakage{
			Change: store.BreakingChange{
				ChangeID:     fmt.Sprintf("ch-%d", i),
				EndpointRepo: "owning",
				WorkspaceID:  "ws-test",
				Kind:         "removed_field",
			},
			Workspace:         ws,
			AffectedConsumers: []ConsumerRef{{Repo: "owning", File: "a.go", Line: 1}},
		}
		if _, err := coord.Dispatch(context.Background(), b); err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}

	coord.SetRecentCap(2)
	got, _ := coord.RecentDispatches(context.Background(), 0)
	if len(got) != 2 {
		t.Fatalf("after SetRecentCap(2): want 2 entries, got %d", len(got))
	}
	if got[0].ChangeID != "ch-5" || got[1].ChangeID != "ch-4" {
		t.Errorf("after SetRecentCap(2): want [ch-5, ch-4] most-recent-first, got [%s, %s]",
			got[0].ChangeID, got[1].ChangeID)
	}
}

func TestRecentDispatchesRaceCleanReadsDuringWrites(t *testing.T) {
	audit := newFakeAudit(t)
	audit.installEmitAuditFn(t)
	coord := &OrchestratorCoordinator{
		Autonomy: stubOracle(ModeSurface),
		Audit:    audit.Adapter(),
	}
	coord.SetRecentCap(20)
	ws := stubWorkspace(t, false, "owning")

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan struct{}, 4)
	readers := 4
	for r := 0; r < readers; r++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					done <- struct{}{}
					return
				default:
					_, _ = coord.RecentDispatches(ctx, 0)
				}
			}
		}()
	}

	for i := 0; i < 50; i++ {
		if ctx.Err() != nil {
			break
		}
		b := ContractBreakage{
			Change: store.BreakingChange{
				ChangeID:     fmt.Sprintf("ch-%d", i),
				EndpointRepo: "owning",
				WorkspaceID:  "ws-test",
				Kind:         "removed_field",
			},
			Workspace:         ws,
			AffectedConsumers: []ConsumerRef{{Repo: "owning", File: "a.go", Line: 1}},
		}
		_, _ = coord.Dispatch(ctx, b)
	}
	cancel()
	for r := 0; r < readers; r++ {
		<-done
	}

}
