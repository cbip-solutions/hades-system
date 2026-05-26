//go:build cgo
// +build cgo

package compliance

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
)

type lockedPolicy243 struct{}

func (lockedPolicy243) PrivacyLocked() bool { return true }

func TestInvZen264CapaFirewallExtendsToPersistentWrite(t *testing.T) {
	ctx := context.Background()

	fedPath := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	fdb, err := federation.Open(ctx, fedPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fdb.Close() })

	if err := fdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: "ws-243", OwningProject: "proj-a",
		PolicyLocked: true, CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}

	w, err := store.NewWorkspaceWithOptions("ws-243",
		[]store.WorkspaceMember{
			{ProjectID: "proj-a", Store: openStore243(t)},
			{ProjectID: "proj-b", Store: openStore243(t)},
		},
		lockedPolicy243{},
		store.WithLinkStore(linkStorePortAdapter{ls: fdb.LinkStore()}),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	link := store.ContractLink{
		CallID: "c-1", CallRepo: "proj-a",
		EndpointID: "ep-1", EndpointRepo: "proj-b",
		Confidence: "static_path", WorkspaceID: "ws-243",
	}
	err = w.CrossRepoLink(ctx, link)
	if !errors.Is(err, store.ErrCrossProjectDenied) {
		t.Fatalf("inv-zen-264 violated: CrossRepoLink under lock err = %v; want ErrCrossProjectDenied", err)
	}

	// Persistent table MUST remain empty — the LinkStore was never invoked
	// because authorize() blocked the path.
	rows, err := fdb.ListByCall(ctx, "ws-243", "c-1", "proj-a")
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("inv-zen-264 violated: contract_links has %d rows under lock; want 0 (gate failed open)", len(rows))
	}
}

func TestInvZen264PositiveControl(t *testing.T) {
	ctx := context.Background()
	fedPath := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	fdb, err := federation.Open(ctx, fedPath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fdb.Close() })
	if err := fdb.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID: "ws-243-ok", OwningProject: "proj-a",
		PolicyLocked: false, CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	w, err := store.NewWorkspaceWithOptions("ws-243-ok",
		[]store.WorkspaceMember{
			{ProjectID: "proj-a", Store: openStore243(t)},
			{ProjectID: "proj-b", Store: openStore243(t)},
		},
		nil,
		store.WithLinkStore(linkStorePortAdapter{ls: fdb.LinkStore()}),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.CrossRepoLink(ctx, store.ContractLink{
		CallID: "c-2", CallRepo: "proj-a",
		EndpointID: "ep-2", EndpointRepo: "proj-b",
		Confidence: "static_path", WorkspaceID: "ws-243-ok",
	}); err != nil {
		t.Fatalf("CrossRepoLink (unlocked): %v", err)
	}
	rows, err := fdb.ListByCall(ctx, "ws-243-ok", "c-2", "proj-a")
	if err != nil {
		t.Fatalf("ListByCall: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("positive control: contract_links has %d rows; want 1 (gate failed shut?)", len(rows))
	}
}

type linkStorePortAdapter struct {
	ls federation.LinkStore
}

func (a linkStorePortAdapter) Append(ctx context.Context, link store.ContractLink) error {
	return a.ls.Append(ctx, federation.LinkRow{
		CallID:       link.CallID,
		CallRepo:     link.CallRepo,
		EndpointID:   link.EndpointID,
		EndpointRepo: link.EndpointRepo,
		Confidence:   link.Confidence,
		WorkspaceID:  link.WorkspaceID,
		ResolvedAt:   link.ResolvedAt,
		LinkMethod:   link.LinkMethod,
	})
}

func openStore243(t *testing.T) *store.Store {
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
