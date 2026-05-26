//go:build integration

package capafw

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	caronte_store "github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

func disableKeychain(t *testing.T) {
	t.Helper()
	t.Setenv("ZEN_KEYCHAIN_DISABLE", "1")
	t.Setenv("ZEN_BYPASS_DISABLE_KEYCHAIN", "1")
}

type lockedPolicy struct{}

func (lockedPolicy) PrivacyLocked() bool { return true }

func openTempCaronteDB(t *testing.T) *caronte_store.Store {
	t.Helper()
	sqlite_vec.Auto()
	dbPath := filepath.Join(t.TempDir(), "caronte.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1&_synchronous=NORMAL", dbPath)
	db, err := sql.Open(caronte_store.DefaultDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open(%s): %v", dbPath, err)
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

func countContractLinksByWorkspace(t *testing.T, ctx context.Context, fed *federation.WorkspaceFederationDB, workspaceID string) int {
	t.Helper()
	rows, err := fed.ListContractLinks(ctx, workspaceID, 0)
	if err != nil {
		t.Fatalf("ListContractLinks: %v", err)
	}
	return len(rows)
}
