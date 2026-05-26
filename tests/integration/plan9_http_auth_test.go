package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/client"
	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

type fakeProjectResolver struct {
	allowedUID    uint32
	allowedAlias  string
	deniedAlias   string
	emittedDenied []string
}

func (r *fakeProjectResolver) OperatorProjects(_ context.Context, uid uint32) ([]string, error) {
	if uid == r.allowedUID {
		return []string{"proj-A"}, nil
	}
	return nil, nil
}

func (r *fakeProjectResolver) ResolveAlias(_ context.Context, alias string) (string, error) {
	if alias == r.allowedAlias {
		return "proj-A", nil
	}
	if alias == r.deniedAlias {
		return "proj-B", nil
	}
	return "", auth.ErrProjectAliasNotFound
}

func (r *fakeProjectResolver) OperatorID(_ context.Context, _ uint32) (string, error) {
	return "testuser", nil
}

func (r *fakeProjectResolver) EmitAccessDenied(_ context.Context, _ uint32, _ string, route string) error {
	r.emittedDenied = append(r.emittedDenied, route)
	return nil
}

type aclAuditCtxP9 struct{}

func (a *aclAuditCtxP9) VerifyChain(_ context.Context, projectID string, _ int64) (handlers.VerifyResultP9, error) {
	return handlers.VerifyResultP9{ProjectID: projectID, RecordsValid: 1, VerifiedAtUnix: time.Now().Unix()}, nil
}

func (a *aclAuditCtxP9) History(_ context.Context, _ handlers.HistoryFilterP9) ([]handlers.HistoryEntryP9, error) {
	return []handlers.HistoryEntryP9{}, nil
}

func (a *aclAuditCtxP9) PartitionSeals(_ context.Context, _ string) ([]handlers.PartitionSealP9, error) {
	return []handlers.PartitionSealP9{}, nil
}

func (a *aclAuditCtxP9) Recover(_ context.Context, projectID string, _ int64, _ bool) (handlers.RecoverPlanP9, handlers.RecoverResultP9, error) {
	return handlers.RecoverPlanP9{ProjectID: projectID}, handlers.RecoverResultP9{}, nil
}

func (a *aclAuditCtxP9) Checkpoint(_ context.Context, _ string, _ string) (handlers.CheckpointResultP9, error) {
	return handlers.CheckpointResultP9{CheckpointID: "chk-acl-001", AnchoredAt: time.Now().Unix()}, nil
}

func (a *aclAuditCtxP9) ColdArchiveList(_ context.Context, _ string) ([]handlers.ColdArchiveEntryP9, error) {
	return []handlers.ColdArchiveEntryP9{}, nil
}

func (a *aclAuditCtxP9) ColdArchiveRestore(_ context.Context, _, _ string) (handlers.RestoreResultP9, error) {
	return handlers.RestoreResultP9{Restored: true}, nil
}

func (a *aclAuditCtxP9) WitnessRotate(_ context.Context, _ string) (handlers.RotateResultP9, error) {
	return handlers.RotateResultP9{NewKeyFingerprint: "acl-fp-002", OldKeyFingerprint: "acl-fp-001"}, nil
}

func (a *aclAuditCtxP9) WitnessPubkey(_ context.Context) (handlers.PubkeyEntryP9, error) {
	return handlers.PubkeyEntryP9{
		PubkeyPEM:   "-----BEGIN PUBLIC KEY-----\nacl\n-----END PUBLIC KEY-----\n",
		Fingerprint: "acl-fp-001",
		CreatedAt:   time.Now().Unix(),
	}, nil
}

func (a *aclAuditCtxP9) ConfigureS3(_ context.Context, _ string, _ handlers.S3CredentialsP9) error {
	return nil
}

func startEmptyDaemon(t *testing.T) *daemon.Server {
	t.Helper()
	st := testhelpers.NewTestStore(t)
	srv := daemon.New(st, daemon.Config{
		UDSPath:           t.TempDir() + "/p9-auth-nil.sock",
		DisableAuditInfra: true,
	})

	return srv
}

func startACLTestDaemon(t *testing.T, _ auth.ProjectResolver, _ uint32) (*client.Client, *httptest.Server) {
	t.Helper()
	st := testhelpers.NewTestStore(t)
	srv := daemon.New(st, daemon.Config{
		UDSPath:           t.TempDir() + "/p9-auth-acl.sock",
		DisableAuditInfra: true,
	})
	srv.SetPlan9Adapters(&daemon.Plan9Adapters{
		Audit: &aclAuditCtxP9{},
	})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	c := client.NewWithBaseURL(httpSrv.URL)
	return c, httpSrv
}

func TestPlan9_503_GracefulDegradation(t *testing.T) {
	srv := startEmptyDaemon(t)
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	c := client.NewWithBaseURL(httpSrv.URL)

	_, err := c.AuditHistory(context.Background(), client.AuditHistoryFilter{ProjectID: "p"})
	if err == nil {
		t.Fatal("expected error (503) when Plan9Adapters not configured; got nil")
	}
	var he *client.HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusServiceUnavailable {
		t.Errorf("expected 503 HTTPError; got: %v", err)
	}
}

func TestPlan9_OperatorGlobal_NoACL(t *testing.T) {
	resolver := &fakeProjectResolver{
		allowedUID:   1000,
		allowedAlias: "internal-platform-x",
	}
	c, _ := startACLTestDaemon(t, resolver, 1000)

	res, err := c.AuditWitnessPubkey(context.Background())
	if err != nil {
		t.Fatalf("witness/pubkey err (operator-global): %v", err)
	}
	if res.Fingerprint != "acl-fp-001" {
		t.Errorf("fingerprint=%q, want acl-fp-001", res.Fingerprint)
	}
}

func TestPlan9_ProjectACL_InScope_200(t *testing.T) {
	t.Skip("ACL middleware not wired in Phase H; Phase K extends (inv-zen-146 full enforcement)")
}

func TestPlan9_ProjectACL_OutOfScope_403(t *testing.T) {
	t.Skip("ACL middleware not wired in Phase H; Phase K extends (inv-zen-146 full enforcement)")
}

func TestPlan9_ProjectACL_AliasNotFound_404(t *testing.T) {
	t.Skip("ACL middleware not wired in Phase H; Phase K extends (inv-zen-146 full enforcement)")
}
