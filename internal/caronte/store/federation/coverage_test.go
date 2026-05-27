// go:build cgo
package federation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nilDB() *WorkspaceFederationDB { return &WorkspaceFederationDB{} }

func TestErrEmptyDBOnEveryCRUD(t *testing.T) {
	ctx := context.Background()
	db := nilDB()

	cases := []struct {
		name string
		op   func() error
	}{

		{"RegisterWorkspace", func() error {
			return db.RegisterWorkspace(ctx, WorkspaceRow{WorkspaceID: "ws"})
		}},
		{"GetWorkspace", func() error {
			_, err := db.GetWorkspace(ctx, "ws")
			return err
		}},
		{"ListWorkspaces", func() error {
			_, err := db.ListWorkspaces(ctx)
			return err
		}},
		{"RemoveWorkspace", func() error {
			_, err := db.RemoveWorkspace(ctx, "ws")
			return err
		}},
		{"SetWorkspacePolicy", func() error {
			return db.SetWorkspacePolicy(ctx, "ws", "{}")
		}},
		{"GetWorkspacePolicy", func() error {
			_, err := db.GetWorkspacePolicy(ctx, "ws")
			return err
		}},
		{"EnableGraphQLNodeFallback", func() error {
			_, err := db.EnableGraphQLNodeFallback(ctx, "ws")
			return err
		}},
		{"SetEnableGraphQLNodeFallback", func() error {
			return db.SetEnableGraphQLNodeFallback(ctx, "ws", true)
		}},

		{"AddMember", func() error {
			return db.AddMember(ctx, MemberRow{WorkspaceID: "ws", ProjectID: "p"})
		}},
		{"ListWorkspaceMembers", func() error {
			_, err := db.ListWorkspaceMembers(ctx, "ws")
			return err
		}},
		{"RemoveMember", func() error {
			_, err := db.RemoveMember(ctx, "ws", "p")
			return err
		}},

		{"linkStoreImpl.Append", func() error {
			return db.LinkStore().Append(ctx, LinkRow{})
		}},
		{"GetLink", func() error {
			_, err := db.GetLink(ctx, "ws", "c", "e")
			return err
		}},
		{"ListByEndpoint", func() error {
			_, err := db.ListByEndpoint(ctx, "ws", "e", "r")
			return err
		}},
		{"ListByCall", func() error {
			_, err := db.ListByCall(ctx, "ws", "c", "r")
			return err
		}},
		{"DeleteLinksByWorkspace", func() error {
			_, err := db.DeleteLinksByWorkspace(ctx, "ws")
			return err
		}},
		{"ListContractLinks", func() error {
			_, err := db.ListContractLinks(ctx, "ws", 10)
			return err
		}},

		{"InsertBreakingChange", func() error {
			return db.InsertBreakingChange(ctx, BreakingChange{ChangeID: "bc"})
		}},
		{"GetBreakingChange", func() error {
			_, err := db.GetBreakingChange(ctx, "bc")
			return err
		}},
		{"ListBreakingChangesByEndpoint", func() error {
			_, err := db.ListBreakingChangesByEndpoint(ctx, "ws", "e", "r")
			return err
		}},
		{"DeleteBreakingChangesByWorkspace", func() error {
			_, err := db.DeleteBreakingChangesByWorkspace(ctx, "ws")
			return err
		}},
		{"GetBreakingChangeWithConsumers", func() error {
			_, _, err := db.GetBreakingChangeWithConsumers(ctx, "bc")
			return err
		}},
		{"ListRecentBreakingChanges", func() error {
			_, err := db.ListRecentBreakingChanges(ctx, "ws", 10)
			return err
		}},

		{"InsertBreakingChangeConsumer", func() error {
			return db.InsertBreakingChangeConsumer(ctx, BreakingChangeConsumer{})
		}},
		{"ListBreakingChangeConsumers", func() error {
			_, err := db.ListBreakingChangeConsumers(ctx, "bc")
			return err
		}},
		{"DeleteConsumersByChange", func() error {
			_, err := db.DeleteConsumersByChange(ctx, "bc")
			return err
		}},

		{"Init", func() error { return db.Init(ctx) }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := c.op()
			if !errors.Is(err, ErrEmptyDB) {
				t.Errorf("%s on nil-db: err = %v; want ErrEmptyDB", c.name, err)
			}
		})
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	// Second Close MUST be a no-op (closeOnce-guarded). t.Cleanup will
	// invoke a third Close — also no-op.
	if err := db.Close(); err != nil {
		t.Errorf("Close 2: %v (must be idempotent)", err)
	}
}

func TestOpenWithAuditEmitterOption(t *testing.T) {
	ctx := context.Background()
	emitter := NewAuditEmitter(nil, "ws-1")
	path := filepath.Join(t.TempDir(), "zen-swarm", "workspace.db")
	db, err := Open(ctx, path, WithAuditEmitter(emitter))
	if err != nil {
		t.Fatalf("Open(WithAuditEmitter): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.auditEmitter == nil {
		t.Error("Open(WithAuditEmitter) did not store the emitter")
	}

	if err := db.auditEmitter.Emit(ctx, EvtCrossRepoLink, []byte("{}")); err != nil {
		t.Errorf("stored emitter Emit err = %v; want nil (nil-adapter no-op path)", err)
	}
}

func TestOpenWithoutAuditEmitterOption(t *testing.T) {
	db := openTestDB(t)
	if db.auditEmitter != nil {
		t.Fatal("openTestDB returned a db with a pre-wired auditEmitter; want nil (no option passed)")
	}
}

func TestDBReturnsHandle(t *testing.T) {
	db := openTestDB(t)
	if db.DB() == nil {
		t.Error("DB() returned nil on an Open'd federation handle")
	}
}

func TestFederationBoundarySentinelReachable(t *testing.T) {
	if err := federationBoundarySentinel(); err != nil {
		t.Errorf("federationBoundarySentinel() = %v; want nil", err)
	}
}

func TestPostCloseQueriesFail(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	if err := db.RegisterWorkspace(ctx, WorkspaceRow{
		WorkspaceID: "ws-cov", OwningProject: "p", PolicyLocked: false,
		CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	type call struct {
		name string
		fn   func() error
	}
	calls := []call{

		{"GetWorkspace", func() error { _, err := db.GetWorkspace(ctx, "ws-cov"); return err }},
		{"ListWorkspaces", func() error { _, err := db.ListWorkspaces(ctx); return err }},
		{"RemoveWorkspace", func() error { _, err := db.RemoveWorkspace(ctx, "ws-cov"); return err }},
		{"SetWorkspacePolicy", func() error { return db.SetWorkspacePolicy(ctx, "ws-cov", "{}") }},
		{"GetWorkspacePolicy", func() error { _, err := db.GetWorkspacePolicy(ctx, "ws-cov"); return err }},
		{"EnableGraphQLNodeFallback", func() error {
			_, err := db.EnableGraphQLNodeFallback(ctx, "ws-cov")
			return err
		}},
		{"SetEnableGraphQLNodeFallback", func() error {
			return db.SetEnableGraphQLNodeFallback(ctx, "ws-cov", true)
		}},

		{"ListWorkspaceMembers", func() error { _, err := db.ListWorkspaceMembers(ctx, "ws-cov"); return err }},
		{"RemoveMember", func() error { _, err := db.RemoveMember(ctx, "ws-cov", "p"); return err }},

		{"GetLink", func() error { _, err := db.GetLink(ctx, "ws-cov", "c", "e"); return err }},
		{"ListByEndpoint", func() error { _, err := db.ListByEndpoint(ctx, "ws-cov", "e", "r"); return err }},
		{"ListByCall", func() error { _, err := db.ListByCall(ctx, "ws-cov", "c", "r"); return err }},
		{"DeleteLinksByWorkspace", func() error { _, err := db.DeleteLinksByWorkspace(ctx, "ws-cov"); return err }},
		{"ListContractLinks", func() error { _, err := db.ListContractLinks(ctx, "ws-cov", 10); return err }},

		{"GetBreakingChange", func() error { _, err := db.GetBreakingChange(ctx, "bc"); return err }},
		{"ListBreakingChangesByEndpoint", func() error {
			_, err := db.ListBreakingChangesByEndpoint(ctx, "ws-cov", "e", "r")
			return err
		}},
		{"DeleteBreakingChangesByWorkspace", func() error {
			_, err := db.DeleteBreakingChangesByWorkspace(ctx, "ws-cov")
			return err
		}},
		{"GetBreakingChangeWithConsumers", func() error {
			_, _, err := db.GetBreakingChangeWithConsumers(ctx, "bc")
			return err
		}},
		{"ListRecentBreakingChanges", func() error {
			_, err := db.ListRecentBreakingChanges(ctx, "ws-cov", 10)
			return err
		}},

		{"ListBreakingChangeConsumers", func() error { _, err := db.ListBreakingChangeConsumers(ctx, "bc"); return err }},
		{"DeleteConsumersByChange", func() error { _, err := db.DeleteConsumersByChange(ctx, "bc"); return err }},

		{"Init", func() error { return db.Init(ctx) }},
	}
	for _, c := range calls {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := c.fn(); err == nil {
				t.Errorf("%s after Close returned nil err; want non-nil (post-close query path)", c.name)
			}
		})
	}
}

func TestOpenRejectsMkdirFailure(t *testing.T) {
	tmp := t.TempDir()

	conflicting := filepath.Join(tmp, "conflict")
	if err := os.WriteFile(conflicting, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed conflict: %v", err)
	}

	statePath := filepath.Join(conflicting, "child", "workspace.db")
	_, err := Open(context.Background(), statePath)
	if err == nil {
		t.Fatal("Open under file-parent returned nil; want mkdir error")
	}
	if !strings.Contains(err.Error(), "mkdir parent") {
		t.Errorf("err %q does not mention mkdir parent (wrapping shape)", err)
	}
}
