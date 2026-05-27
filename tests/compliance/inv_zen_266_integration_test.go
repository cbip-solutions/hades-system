// go:build cgo

package compliance

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type fed245AlwaysAutonomyOracle struct{}

func (fed245AlwaysAutonomyOracle) Decision(_ coordinated.ContractBreakage) coordinated.DispatchMode {
	return coordinated.ModeAutonomy
}

type fed245LockedPolicy struct{}

func (fed245LockedPolicy) PrivacyLocked() bool { return true }

func TestInvZen266OracleAllowUnderLockedWorkspaceDenies(t *testing.T) {
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")

	owning := fed245OpenStore(t, "owning")
	clientA := fed245OpenStore(t, "client-a")
	ws, err := store.NewWorkspace("ws-locked", []store.WorkspaceMember{
		{ProjectID: "owning", Store: owning},
		{ProjectID: "client-a", Store: clientA},
	}, fed245LockedPolicy{})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	defer ws.Close()

	auditDir := filepath.Join(t.TempDir(), "audit-root")
	audit, err := tessera.NewProjectAdapter(context.Background(), "test-project", auditDir, tessera.Config{
		BatchMaxAge:         50_000_000,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	})
	if err != nil {
		t.Fatalf("tessera.NewProjectAdapter: %v", err)
	}
	defer audit.Close()

	coord := &coordinated.OrchestratorCoordinator{
		Autonomy: fed245AlwaysAutonomyOracle{},

		Pool:  nil,
		Audit: audit,
	}

	b := coordinated.ContractBreakage{
		Change: store.BreakingChange{
			ChangeID:     "ch-245-adv",
			EndpointRepo: "owning",
			Kind:         "removed_field",
			WorkspaceID:  "ws-locked",
		},
		AffectedConsumers: []coordinated.ConsumerRef{
			{Repo: "owning", File: "owning.go", Line: 1},
			{Repo: "client-a", File: "client.go", Line: 1},
		},
		Workspace: ws,
	}

	_, err = coord.Dispatch(context.Background(), b)
	if err == nil {
		t.Fatalf("inv-zen-266 §13.4 adversarial: want error wrapping ErrCrossProjectDenied, got nil")
	}
	if !errors.Is(err, store.ErrCrossProjectDenied) {
		t.Errorf("inv-zen-266 §13.4 adversarial: want wraps ErrCrossProjectDenied (cross-project under PrivacyLocked; both projects on roster), got %v", err)
	}
}

func fed245OpenStore(t *testing.T, projectID string) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), projectID+".db")
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
