// go:build integration
package coordinated

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func disableKeychain(t *testing.T) {
	t.Helper()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
}

type allowOracle struct{}

func (allowOracle) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type denyOracle struct{}

func (denyOracle) Decision(coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeSurface
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

func (p *mockWorktreePool) Release(_ context.Context, _ *worktreepool.Worktree) error {
	return nil
}

func (p *mockWorktreePool) PruneOrphans(_ context.Context) (worktreepool.PruneReport, error) {
	return worktreepool.PruneReport{}, nil
}

func (p *mockWorktreePool) Close(_ context.Context) error { return nil }

func (p *mockWorktreePool) leases() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaseCount
}

type permissivePolicy struct{}

func (permissivePolicy) PrivacyLocked() bool { return false }

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

func newPermissiveWorkspace(t *testing.T, workspaceID string, members ...string) *caronte_store.Workspace {
	t.Helper()
	ms := make([]caronte_store.WorkspaceMember, 0, len(members))
	for _, id := range members {
		ms = append(ms, caronte_store.WorkspaceMember{
			ProjectID: id,
			Store:     openTempCaronteDB(t),
		})
	}
	ws, err := caronte_store.NewWorkspace(workspaceID, ms, permissivePolicy{})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func newBreakage(t *testing.T, ws *caronte_store.Workspace, workspaceID, owningRepo string) coordinated.ContractBreakage {
	t.Helper()
	return coordinated.ContractBreakage{
		Change: caronte_store.BreakingChange{
			ChangeID:     "k6-change-001",
			WorkspaceID:  workspaceID,
			EndpointID:   owningRepo + ":endpoint:1",
			EndpointRepo: owningRepo,
			Kind:         "param_renamed_required",
			Detail:       []byte(`{"why":"k6 test fixture"}`),
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
		Workspace: ws,
	}
}
