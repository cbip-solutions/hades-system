package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/doctrine/active"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func newTesseraManagerForTest(t *testing.T) *tessera.Manager {
	t.Helper()
	root := t.TempDir()
	mgr, err := tessera.NewManager(context.Background(), root, fastTesseraTestConfig())
	if err != nil {
		t.Fatalf("tessera.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

func newDaemonServerForTest(t *testing.T) *daemon.Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	srv := daemon.New(st, daemon.Config{
		UDSPath:           filepath.Join(t.TempDir(), "test.sock"),
		HTTPAddr:          "",
		DisableAuditInfra: true,
	})
	return srv
}

func envSnapshotWithStateRoot(stateRoot string) map[string]string {
	return map[string]string{
		"ZEN_STATE_DIR": stateRoot,
	}
}

type mainWiringTestPool struct{}

func (mainWiringTestPool) Lease(context.Context) (*worktreepool.Worktree, error) {
	return &worktreepool.Worktree{}, nil
}

func (mainWiringTestPool) Release(context.Context, *worktreepool.Worktree) error { return nil }

func (mainWiringTestPool) PruneOrphans(context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (mainWiringTestPool) Close(context.Context) error { return nil }

func TestWireContractFederation_HappyPathSetsBothSettersAndClosesCleanly(t *testing.T) {
	t.Parallel()

	srv := newDaemonServerForTest(t)
	mgr := newTesseraManagerForTest(t)
	stateRoot := t.TempDir()
	env := envSnapshotWithStateRoot(stateRoot)

	closer, err := wireContractFederation(context.Background(), srv, mgr, env, wireContractFederationOpts{})
	if err != nil {
		t.Fatalf("wireContractFederation: %v", err)
	}
	t.Cleanup(func() {
		if cerr := closer(); cerr != nil {
			t.Errorf("closer: %v", cerr)
		}
	})

	if srv.ContractFederation() == nil {
		t.Fatal("ContractFederation accessor returned nil; SetContractFederation did not fire")
	}
	if srv.ContractCoordinator() == nil {
		t.Fatal("ContractCoordinator accessor returned nil; SetContractCoordinator did not fire")
	}

	rows, lwErr := srv.ContractFederation().ListWorkspaces(context.Background())
	if lwErr != nil {
		t.Fatalf("ListWorkspaces on empty roster: %v", lwErr)
	}
	if len(rows) != 0 {
		t.Errorf("ListWorkspaces empty-roster: got %d rows; want 0", len(rows))
	}

	dispatches, rdErr := srv.ContractCoordinator().RecentDispatches(context.Background(), 10)
	if rdErr != nil {
		t.Errorf("RecentDispatches empty ring: %v", rdErr)
	}
	if len(dispatches) != 0 {
		t.Errorf("RecentDispatches empty ring: got %d entries; want 0", len(dispatches))
	}

	wantPrefix := filepath.Join(stateRoot, "zen-swarm")
	matches, _ := filepath.Glob(filepath.Join(wantPrefix, "workspace.db"))
	if len(matches) == 0 {
		t.Errorf("expected workspace.db under %q; found nothing", wantPrefix)
	}
}

func TestWireContractFederation_PoolOptionReachesCoordinator(t *testing.T) {
	t.Parallel()

	srv := newDaemonServerForTest(t)
	mgr := newTesseraManagerForTest(t)
	pool := mainWiringTestPool{}

	closer, err := wireContractFederation(
		context.Background(),
		srv,
		mgr,
		envSnapshotWithStateRoot(t.TempDir()),
		wireContractFederationOpts{Pool: pool},
	)
	if err != nil {
		t.Fatalf("wireContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = closer() })

	adapter, ok := srv.ContractCoordinator().(*coordinatorDaemonAdapter)
	if !ok {
		t.Fatalf("ContractCoordinator type = %T; want *coordinatorDaemonAdapter", srv.ContractCoordinator())
	}
	coord, ok := adapter.coord.(*coordinated.OrchestratorCoordinator)
	if !ok {
		t.Fatalf("coordinator source type = %T; want *coordinated.OrchestratorCoordinator", adapter.coord)
	}
	if coord.Pool != pool {
		t.Fatalf("coord.Pool = %#v; want injected pool %#v", coord.Pool, pool)
	}
}

func TestMainCompositionRootConstructsPoolBeforeContractFederation(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile(main.go): %v", err)
	}
	text := string(src)
	newPoolAt := strings.Index(text, "worktreepool.NewPool(")
	wireAt := strings.Index(text, "wireContractFederation(")
	if newPoolAt < 0 {
		t.Fatal("main.go does not construct worktreepool.NewPool before contract federation")
	}
	if wireAt < 0 {
		t.Fatal("main.go does not call wireContractFederation")
	}
	if newPoolAt > wireAt {
		t.Fatalf("worktreepool.NewPool appears after wireContractFederation (%d > %d)", newPoolAt, wireAt)
	}
	if !strings.Contains(text, "Pool: contractPool") {
		t.Fatal("wireContractFederationOpts does not pass the daemon contractPool")
	}
}

func TestWireContractFederation_ErrorOnNilServer(t *testing.T) {
	t.Parallel()
	mgr := newTesseraManagerForTest(t)
	env := envSnapshotWithStateRoot(t.TempDir())
	closer, err := wireContractFederation(context.Background(), nil, mgr, env, wireContractFederationOpts{})
	if err == nil {
		_ = closer()
		t.Fatal("expected error on nil srv; got nil")
	}
	if !strings.Contains(err.Error(), "srv") {
		t.Errorf("expected error mentioning srv; got %v", err)
	}
}

func TestWireContractFederation_ErrorOnNilTesseraManager(t *testing.T) {
	t.Parallel()
	srv := newDaemonServerForTest(t)
	env := envSnapshotWithStateRoot(t.TempDir())
	closer, err := wireContractFederation(context.Background(), srv, nil, env, wireContractFederationOpts{})
	if err == nil {
		_ = closer()
		t.Fatal("expected error on nil tesseraMgr; got nil")
	}
	if !strings.Contains(err.Error(), "tessera") {
		t.Errorf("expected error mentioning tessera; got %v", err)
	}
}

func TestWireContractFederation_ErrorOnWorkspaceDBPathUnresolvable(t *testing.T) {
	t.Parallel()
	srv := newDaemonServerForTest(t)
	mgr := newTesseraManagerForTest(t)
	closer, err := wireContractFederation(context.Background(), srv, mgr, map[string]string{}, wireContractFederationOpts{})
	if err == nil {
		_ = closer()
		t.Fatal("expected error on unresolvable env; got nil")
	}
	if !strings.Contains(err.Error(), "WorkspaceDBPath") && !strings.Contains(err.Error(), "anchor") {
		t.Errorf("expected error mentioning WorkspaceDBPath or anchor; got %v", err)
	}
}

func TestWireContractFederation_WorkspaceIDFromOptsOverridesDefault(t *testing.T) {
	t.Parallel()
	srv := newDaemonServerForTest(t)
	mgr := newTesseraManagerForTest(t)
	env := envSnapshotWithStateRoot(t.TempDir())
	closer, err := wireContractFederation(context.Background(), srv, mgr, env, wireContractFederationOpts{
		WorkspaceID: "ws-explicit",
	})
	if err != nil {
		t.Fatalf("wireContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = closer() })

	if srv.ContractFederation() == nil {
		t.Fatal("ContractFederation accessor returned nil")
	}
}

// TestProductionDoctrineResolverPolicyNonNil — the production resolver
// returns a non-nil WorkspacePolicy (PrivacyLocked is callable) after
// the doctrine registry is wired (the invariant init-order contract:
// active.SetRegistry MUST run before any Policy() consumer reads). The
// active.Active() call panics if the registry is unwired — that is the
// daemon-boot misconfiguration sentinel, NOT a resolver bug. Sister-
// test pinning the doc-comment claim per [[feedback_sister_test_pattern]].
//
// NOT parallel: the global active.Accessor is process-singleton state
// that other tests in the same binary (production_boot_smoke_test.go,
// doctrine_eval_wiring_test.go) reset via active.ResetForTest in
// t.Cleanup. Running this test in parallel risks racing the cleanup +
// surfacing a phantom invariant panic. Serial execution + an explicit
// per-test setup-and-cleanup keeps the assertion deterministic under
// -race -count=2.
func TestProductionDoctrineResolverPolicyNonNil(t *testing.T) {
	active.ResetForTest()
	t.Cleanup(active.ResetForTest)
	if err := bootDoctrineRegistry(); err != nil {
		t.Fatalf("bootDoctrineRegistry: %v", err)
	}
	r := newProductionDoctrineResolver()
	p := r.Policy()
	if p == nil {
		t.Fatal("Policy returned nil; want non-nil store.WorkspacePolicy")
	}

	if p.PrivacyLocked() {
		t.Errorf("PrivacyLocked: got true; want false (default-doctrine policy at fresh boot)")
	}
}

func TestProductionDoctrineResolverBoolPolicyRoundtrip(t *testing.T) {
	t.Parallel()
	if (boolPolicy{locked: false}).PrivacyLocked() {
		t.Errorf("boolPolicy{false}.PrivacyLocked() = true; want false")
	}
	if !(boolPolicy{locked: true}).PrivacyLocked() {
		t.Errorf("boolPolicy{true}.PrivacyLocked() = false; want true")
	}
}

func TestBuildEnvSnapshot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{
			name: "empty input",
			in:   nil,
			want: map[string]string{},
		},
		{
			name: "single key",
			in:   []string{"FOO=bar"},
			want: map[string]string{"FOO": "bar"},
		},
		{
			name: "skip empty key + skip no-equals",
			in:   []string{"=ignored", "noequals", "K=v"},
			want: map[string]string{"K": "v"},
		},
		{
			name: "value with =",
			in:   []string{"PATH=/a=/b", "KEY=val=with=eq"},
			want: map[string]string{"PATH": "/a=/b", "KEY": "val=with=eq"},
		},
		{
			name: "empty value preserved",
			in:   []string{"EMPTYVAL="},
			want: map[string]string{"EMPTYVAL": ""},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildEnvSnapshot(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("len(got)=%d; len(want)=%d (got=%+v)", len(got), len(c.want), got)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("got[%q]=%q; want %q", k, got[k], v)
				}
			}
		})
	}
}
