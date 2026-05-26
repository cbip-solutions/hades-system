//go:build integration

package crossplan

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func disableKeychain(t *testing.T) {
	t.Helper()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test file")
		}
		dir = parent
	}
}

func fastTesseraConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

func newTesseraAdapter(t *testing.T, ctx context.Context, projectID, tmp string) *tessera.Adapter {
	t.Helper()
	a, err := tessera.NewProjectAdapter(ctx, projectID, filepath.Join(tmp, "tessera"), fastTesseraConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

type recordingBlastRadiusProvider struct {
	mu      sync.Mutex
	records []orchestrator.Verdict
}

func (r *recordingBlastRadiusProvider) BlastRadius(_ context.Context, _ string, changedSymbols, _ []string) (orchestrator.Verdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := orchestrator.Verdict{
		Level:       "high",
		Score:       0.95,
		TopAffected: changedSymbols,
	}
	r.records = append(r.records, v)
	return v, nil
}

func (r *recordingBlastRadiusProvider) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

var _ orchestrator.BlastRadiusProvider = (*recordingBlastRadiusProvider)(nil)

type allowOracle struct{}

func (allowOracle) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type denyOracle struct{}

func (denyOracle) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeSurface
}

type permissivePolicy struct{}

func (permissivePolicy) PrivacyLocked() bool { return false }

type lockedPolicy struct{}

func (lockedPolicy) PrivacyLocked() bool { return true }

func openTempCaronteDB(t *testing.T) *caronte_store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", dbPath)
	db, err := sql.Open(caronte_store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	st, err := caronte_store.Open(context.Background(), db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("caronte_store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return st
}

func newWorkspace(t *testing.T, workspaceID string, policy caronte_store.WorkspacePolicy, members ...string) *caronte_store.Workspace {
	t.Helper()
	ms := make([]caronte_store.WorkspaceMember, 0, len(members))
	for _, id := range members {
		ms = append(ms, caronte_store.WorkspaceMember{
			ProjectID: id,
			Store:     openTempCaronteDB(t),
		})
	}
	ws, err := caronte_store.NewWorkspace(workspaceID, ms, policy)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func newBreakageOwning(workspaceID, owningRepo string) coordinated.ContractBreakage {
	return coordinated.ContractBreakage{
		Change: caronte_store.BreakingChange{
			ChangeID:     "k-crossplan-change-001",
			WorkspaceID:  workspaceID,
			EndpointID:   owningRepo + ":endpoint:1",
			EndpointRepo: owningRepo,
			Kind:         "param_renamed_required",
			Detail:       []byte(`{"why":"crossplan test fixture"}`),
			DetectedAt:   time.Now().Unix(),
			DetectorID:   "oasdiff",
		},
		AffectedConsumers: []coordinated.ConsumerRef{
			{
				Repo:   owningRepo,
				CallID: owningRepo + ":call:1",
				NodeID: owningRepo + ":node:1",
				File:   "main.go",
				Line:   10,
			},
		},
	}
}

type mockWorktreePool struct {
	mu         sync.Mutex
	leaseCount int
}

var _ worktreepool.Pool = (*mockWorktreePool)(nil)

func (p *mockWorktreePool) Lease(_ context.Context) (*worktreepool.Worktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.leaseCount++
	return &worktreepool.Worktree{}, nil
}

func (p *mockWorktreePool) Release(_ context.Context, _ *worktreepool.Worktree) error { return nil }

func (p *mockWorktreePool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *mockWorktreePool) Close(_ context.Context) error { return nil }
