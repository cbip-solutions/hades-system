//go:build cgo
// +build cgo

// tests/compliance/inv_zen_241_workspace_capa_firewall_test.go
//
// inv-zen-241 (workspace federation capa-firewall gate, Plan 19 Phase M, the
// FIRST Plan-20-range invariant — allocated now because Plan 19 ships the gate
//
//	store.Workspace.FederatedQuery + CrossRepoLink MUST refuse any access to a
//	projectID not on the workspace roster (ErrUnauthorizedProject) and, under a
//	privacy-locked (capa-firewall) WorkspacePolicy, MUST confine federation to
//	the single owning project (ErrCrossProjectDenied). No cross-project leakage
//	(extends inv-zen-163/231; mirrors the inv-zen-100 autonomy hard guard).
package compliance

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type lockedPolicy struct{}

func (lockedPolicy) PrivacyLocked() bool { return true }

func TestInvZen241_UnauthorizedProjectRefused(t *testing.T) {
	w := newWS241(t, false)
	_, err := w.FederatedQuery(context.Background(), store.FederatedQuery{Scope: []string{"member-0", "intruder"}})
	if !errors.Is(err, store.ErrUnauthorizedProject) {
		t.Fatalf("FederatedQuery(non-member) err = %v; want ErrUnauthorizedProject", err)
	}
}

func TestInvZen241_CapaFirewallConfinesLocal(t *testing.T) {
	w := newWS241(t, true)
	_, err := w.FederatedQuery(context.Background(), store.FederatedQuery{Scope: []string{"member-0", "member-1"}})
	if !errors.Is(err, store.ErrCrossProjectDenied) {
		t.Fatalf("privacy-locked cross-project err = %v; want ErrCrossProjectDenied", err)
	}
	if _, err := w.FederatedQuery(context.Background(), store.FederatedQuery{Scope: []string{"member-0"}}); err != nil {
		t.Fatalf("privacy-locked local-only must be permitted: %v", err)
	}
}

func newWS241(t *testing.T, locked bool) *store.Workspace {
	t.Helper()
	members := []store.WorkspaceMember{
		{ProjectID: "member-0", Store: openStore241(t)},
		{ProjectID: "member-1", Store: openStore241(t)},
	}
	var pol store.WorkspacePolicy
	if locked {
		pol = lockedPolicy{}
	}
	w, err := store.NewWorkspace("ws-241", members, pol)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func openStore241(t *testing.T) *store.Store {
	t.Helper()
	sqlite_vec.Auto()
	db, err := sql.Open(store.DefaultDriver, ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	s, err := store.Open(context.Background(), db)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return s
}
