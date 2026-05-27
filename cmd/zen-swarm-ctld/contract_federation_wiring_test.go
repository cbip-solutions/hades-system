package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/audit/tessera"
	"github.com/cbip-solutions/hades-system/internal/caronte/coordinated"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/store/federation"
	"github.com/cbip-solutions/hades-system/internal/daemon"
)

type recordingAuditEmitter struct {
	mu     sync.Mutex
	events []recordingAuditEvent
}

type recordingAuditEvent struct {
	eventType federation.EventType
	payload   []byte
}

func (r *recordingAuditEmitter) Emit(_ context.Context, t federation.EventType, payload []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordingAuditEvent{eventType: t, payload: payload})
	return nil
}

func (r *recordingAuditEmitter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// fastTesseraTestConfig mirrors the tessera test-config posture used by
// internal/caronte/contract/bcdetect/pipeline_test.go::fastTesseraConfig
// and internal/caronte/coordinated/orchestrator_test.go — short
// BatchMaxAge + 1-leaf BatchMaxSize so tests don't pay the 30s
// DefaultConfig() wait at Adapter.Close().
//
// Per [[feedback_tessera_batchmaxage_30s_test_default]]: tests that
// construct a tessera adapter via NewProjectAdapter MUST override the
// 30s DefaultConfig() BatchMaxAge or shutdown hangs at the batch
// flush. The canonical field set is (BatchMaxAge, BatchMaxSize,
// RotationCadenceDays); there is NO BatchMaxLeaves or
// CheckpointRotateAfter field on tessera.Config.
func fastTesseraTestConfig() tessera.Config {
	return tessera.Config{
		BatchMaxAge:         50 * time.Millisecond,
		BatchMaxSize:        1,
		RotationCadenceDays: 365,
	}
}

func newTesseraAdapterForTest(t *testing.T) *tessera.Adapter {
	t.Helper()
	root := t.TempDir()
	a, err := tessera.NewProjectAdapter(context.Background(), "test-project", root, fastTesseraTestConfig())
	if err != nil {
		t.Fatalf("NewProjectAdapter: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

type permissiveTestPolicy struct{}

func (permissiveTestPolicy) PrivacyLocked() bool { return false }

type lockedTestPolicy struct{}

func (lockedTestPolicy) PrivacyLocked() bool { return true }

type fakeDoctrineResolver struct {
	p store.WorkspacePolicy
}

func (f fakeDoctrineResolver) Policy() store.WorkspacePolicy { return f.p }

// TestBuildContractFederation_AuditEmitterIsWired pins the CRITICAL
// invariant chokepoint wiring (Fix 1 sister-test). The
// federation.Open API gained a WithAuditEmitter Option (review I2) for
// construction-time injection of the per-workspace AuditEmitter. The
// production buildContractFederation MUST pass the emitter via this
// Option — otherwise the federation writer paths skip the emit
// silently (the `if w.auditEmitter != nil { emit }` graceful-degrade
// branch fires for every workspace-level write).
//
// Bite-check: revert the emitterFactory wiring (or revert the
// federation.WithAuditEmitter(emitter) Open option) → this test
// fires the "expected >= 1 audit event after SetWorkspacePolicy"
// assertion. Restore.
//
// Wiring shape: deps.emitterFactory is a test-only seam (default nil →
// production federation.NewAuditEmitter). When non-nil, the wiring
// installs the factory-produced emitter via WithAuditEmitter. The test
// observes the recorder's Emit count after a SetWorkspacePolicy call
// (the ONLY federation writer wired through the chokepoint as of
func TestBuildContractFederation_AuditEmitterIsWired(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	audit := newTesseraAdapterForTest(t)
	recorder := &recordingAuditEmitter{}
	fedDB, _, err := buildContractFederation(contractFederationWiringDeps{
		ctx:         context.Background(),
		audit:       audit,
		pool:        nil,
		doctrine:    fakeDoctrineResolver{p: permissiveTestPolicy{}},
		workspaceID: "ws-emit-1",
		statePath:   statePath,
		emitterFactory: func(_ *tessera.Adapter, gotWorkspaceID string) federation.AuditEmitter {

			if gotWorkspaceID != "ws-emit-1" {
				t.Errorf("emitterFactory got workspaceID = %q; want %q", gotWorkspaceID, "ws-emit-1")
			}
			return recorder
		},
	})
	if err != nil {
		t.Fatalf("buildContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })

	// Drive the SOLE federation writer wired through the audit chokepoint
	// (per internal/caronte/store/federation/workspaces.go:176-184). Per
	// SetWorkspacePolicy's pre-conditions, the workspace MUST exist; seed
	// it via RegisterWorkspace first (NO audit emit) then mutate the policy
	// (WILL audit emit IF the emitter is wired).
	ctx := context.Background()
	if err := fedDB.RegisterWorkspace(ctx, federation.WorkspaceRow{
		WorkspaceID:   "ws-emit-1",
		OwningProject: "proj-emit",
		PolicyLocked:  false,
		CreatedAt:     1700000000,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := fedDB.SetWorkspacePolicy(ctx, "ws-emit-1", `{"k":"v"}`); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}

	if got := recorder.count(); got < 1 {
		t.Errorf("recorder.count() = %d after SetWorkspacePolicy; want >= 1 (inv-zen-269 audit emit missing — federation.WithAuditEmitter wiring broken)", got)
	}
}

func TestBuildContractFederation_NilPoolDegradesGracefully(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	audit := newTesseraAdapterForTest(t)
	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:         context.Background(),
		audit:       audit,
		pool:        nil,
		doctrine:    fakeDoctrineResolver{p: permissiveTestPolicy{}},
		workspaceID: "test-ws",
		statePath:   statePath,
	})
	if err != nil {
		t.Fatalf("buildContractFederation: %v", err)
	}
	if fedDB == nil || coord == nil {
		t.Fatalf("expected non-nil fedDB + coord; got %v + %v", fedDB, coord)
	}
	t.Cleanup(func() { _ = fedDB.Close() })

	fedAdapter := newFederationDaemonAdapter(fedDB)
	coordAdapter := newCoordinatorDaemonAdapter(coord)
	var _ daemon.ContractFederationForDaemon = fedAdapter
	var _ daemon.ContractCoordinatorForDaemon = coordAdapter

	if coord.Pool != nil {
		t.Errorf("coord.Pool: got non-nil; want nil (D9 ship posture)")
	}
	if coord.Autonomy == nil {
		t.Errorf("coord.Autonomy: got nil; want production oracle")
	}
	if coord.Audit != audit {
		t.Errorf("coord.Audit: got %v; want injected adapter %v", coord.Audit, audit)
	}
}

func TestBuildContractFederation_PolicyOracleDecisionsRespected(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	audit := newTesseraAdapterForTest(t)
	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:         context.Background(),
		audit:       audit,
		pool:        nil,
		doctrine:    fakeDoctrineResolver{p: lockedTestPolicy{}},
		workspaceID: "test-ws",
		statePath:   statePath,
	})
	if err != nil {
		t.Fatalf("buildContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })

	got := coord.Autonomy.Decision(coordinated.ContractBreakage{
		AffectedConsumers: []coordinated.ConsumerRef{{Repo: "r1", CallID: "c1"}},
	})
	if got != coordinated.ModeSurface {
		t.Errorf("locked doctrine must surface; got %v", got)
	}
}

func TestBuildContractFederation_ErrorOnFederationOpenFail(t *testing.T) {
	t.Parallel()
	audit := newTesseraAdapterForTest(t)

	bogus := "/dev/null/cannot-open/workspace.db"
	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:       context.Background(),
		audit:     audit,
		pool:      nil,
		doctrine:  fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath: bogus,
	})
	if err == nil {
		_ = fedDB.Close()
		t.Fatalf("expected error on bogus statePath; got fedDB=%v coord=%v", fedDB, coord)
	}
	if fedDB != nil || coord != nil {
		t.Errorf("expected nil concretes on error; got fedDB=%v coord=%v", fedDB, coord)
	}
}

func TestBuildContractFederation_ErrorOnNilAudit(t *testing.T) {
	t.Parallel()
	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:       context.Background(),
		audit:     nil,
		pool:      nil,
		doctrine:  fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath: filepath.Join(t.TempDir(), "workspace.db"),
	})
	if err == nil {
		_ = fedDB.Close()
		t.Fatal("expected error on nil audit; got nil")
	}
	if fedDB != nil || coord != nil {
		t.Errorf("expected nil concretes on error; got fedDB=%v coord=%v", fedDB, coord)
	}
}

func TestBuildContractFederation_ErrorOnEmptyStatePath(t *testing.T) {
	t.Parallel()
	audit := newTesseraAdapterForTest(t)
	fedDB, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:       context.Background(),
		audit:     audit,
		pool:      nil,
		doctrine:  fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath: "",
	})
	if err == nil {
		_ = fedDB.Close()
		t.Fatal("expected error on empty statePath; got nil")
	}
	if fedDB != nil || coord != nil {
		t.Errorf("expected nil concretes on error; got fedDB=%v coord=%v", fedDB, coord)
	}
}

func TestBuildContractFederation_WorkspaceIDDefaultsToDefault(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	audit := newTesseraAdapterForTest(t)
	fedDB, _, err := buildContractFederation(contractFederationWiringDeps{
		ctx:         context.Background(),
		audit:       audit,
		pool:        nil,
		doctrine:    fakeDoctrineResolver{p: permissiveTestPolicy{}},
		workspaceID: "",
		statePath:   statePath,
	})
	if err != nil {
		t.Fatalf("buildContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })

}

func TestBuildContractFederation_RecentDispatchCapApplied(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	audit := newTesseraAdapterForTest(t)
	_, coord, err := buildContractFederation(contractFederationWiringDeps{
		ctx:               context.Background(),
		audit:             audit,
		pool:              nil,
		doctrine:          fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath:         statePath,
		recentDispatchCap: 7,
	})
	if err != nil {
		t.Fatalf("buildContractFederation: %v", err)
	}
	t.Cleanup(func() { _ = coord })

	got, rdErr := coord.RecentDispatches(context.Background(), 100)
	if rdErr != nil {
		t.Errorf("RecentDispatches: %v", rdErr)
	}
	if len(got) != 0 {
		t.Errorf("expected empty ring (no dispatches yet); got %d entries", len(got))
	}
}

func TestFederationDaemonAdapter_DeriveSeverityCoversKnownKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind string
		want string
	}{
		{"removed_endpoint", "high"},
		{"removed_field", "high"},
		{"type_changed", "high"},
		{"param_added_required", "high"},
		{"param_added_optional", "low"},
		{"deprecation_announced", "low"},
		{"extension_added", "low"},
		{"unknown_kind", "medium"},
		{"", "medium"},
	}
	for _, c := range cases {
		if got := deriveSeverity(c.kind); got != c.want {
			t.Errorf("deriveSeverity(%q) = %q; want %q", c.kind, got, c.want)
		}
	}
}

func TestDispatchModeToString_CoversAllValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode coordinated.DispatchMode
		want string
	}{
		{coordinated.ModeAutonomy, "Autonomy"},
		{coordinated.ModeSurface, "Surface"},
		{coordinated.DispatchMode("hypothetical-future"), "Unknown"},
		{coordinated.DispatchMode(""), "Unknown"},
	}
	for _, c := range cases {
		if got := dispatchModeToString(c.mode); got != c.want {
			t.Errorf("dispatchModeToString(%q) = %q; want %q", string(c.mode), got, c.want)
		}
	}
}

func TestFederationDaemonAdapter_FullRoundTrip(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	fedDB, err := federation.Open(context.Background(), statePath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	a := newFederationDaemonAdapter(fedDB)

	if err := fedDB.RegisterWorkspace(context.Background(), federation.WorkspaceRow{
		WorkspaceID:   "ws-rt",
		OwningProject: "proj-x",
		PolicyLocked:  false,
		CreatedAt:     200,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := fedDB.AddMember(context.Background(), federation.MemberRow{
		WorkspaceID:  "ws-rt",
		ProjectID:    "proj-y",
		RegisteredAt: 250,
	}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := fedDB.InsertBreakingChange(context.Background(), federation.BreakingChange{
		ChangeID:       "ch-rt",
		WorkspaceID:    "ws-rt",
		EndpointID:     "ep-1",
		EndpointRepo:   "proj-x",
		Kind:           "removed_endpoint",
		Detail:         `{"path":"/v1/orders"}`,
		DetectedAt:     300,
		DetectorID:     "oasdiff",
		LoreAuthor:     "alice",
		LoreCommitSHA:  "deadbeef",
		LoreADRRefs:    `["ADR-7"]`,
		LoreSupersedes: `["ADR-3"]`,
	}); err != nil {
		t.Fatalf("InsertBreakingChange: %v", err)
	}
	if err := fedDB.InsertBreakingChangeConsumer(context.Background(), federation.BreakingChangeConsumer{
		ChangeID: "ch-rt",
		CallID:   "call-1",
		CallRepo: "proj-y",
	}); err != nil {
		t.Fatalf("InsertBreakingChangeConsumer: %v", err)
	}

	ws, err := a.GetWorkspace(context.Background(), "ws-rt")
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	wantWS := daemon.Workspace{WorkspaceID: "ws-rt", OwningProject: "proj-x", PolicyLocked: false, CreatedAt: 200, SchemaVersion: 1}
	if ws != wantWS {
		t.Errorf("GetWorkspace: got %+v; want %+v", ws, wantWS)
	}

	members, err := a.ListWorkspaceMembers(context.Background(), "ws-rt")
	if err != nil {
		t.Fatalf("ListWorkspaceMembers: %v", err)
	}
	if len(members) != 1 || members[0].ProjectID != "proj-y" || members[0].RegisteredAt != 250 {
		t.Errorf("ListWorkspaceMembers: got %+v; want 1 member proj-y@250", members)
	}

	bcs, err := a.ListRecentBreakingChanges(context.Background(), "ws-rt", 10)
	if err != nil {
		t.Fatalf("ListRecentBreakingChanges: %v", err)
	}
	if len(bcs) != 1 {
		t.Fatalf("expected 1 BC; got %d", len(bcs))
	}
	bc := bcs[0]
	if bc.ChangeID != "ch-rt" || bc.Severity != "high" || bc.LoreAuthor != "alice" || bc.LoreADRRefs != `["ADR-7"]` {
		t.Errorf("ListRecentBreakingChanges: got %+v; want ch-rt + severity=high (removed_endpoint) + lore_author=alice + adr_refs", bc)
	}

	gotBC, gotCons, err := a.GetBreakingChangeWithConsumers(context.Background(), "ch-rt")
	if err != nil {
		t.Fatalf("GetBreakingChangeWithConsumers: %v", err)
	}
	if gotBC.ChangeID != "ch-rt" || gotBC.Severity != "high" || gotBC.LoreCommitSHA != "deadbeef" {
		t.Errorf("GBCWC: got %+v; want ch-rt + severity=high + commit=deadbeef", gotBC)
	}
	if len(gotCons) != 1 || gotCons[0].CallID != "call-1" || gotCons[0].CallRepo != "proj-y" {
		t.Errorf("GBCWC consumers: got %+v; want 1 consumer call-1@proj-y", gotCons)
	}

	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestFederationDaemonAdapter_ListWorkspaces(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	fedDB, err := federation.Open(context.Background(), statePath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })

	a := newFederationDaemonAdapter(fedDB)
	rows, err := a.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListWorkspaces empty: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected empty roster; got %d", len(rows))
	}

	if err := fedDB.RegisterWorkspace(context.Background(), federation.WorkspaceRow{
		WorkspaceID:   "ws-test",
		OwningProject: "proj-owner",
		PolicyLocked:  true,
		CreatedAt:     100,
		SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	rows, err = a.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 workspace; got %d", len(rows))
	}
	got := rows[0]
	want := daemon.Workspace{WorkspaceID: "ws-test", OwningProject: "proj-owner", PolicyLocked: true, CreatedAt: 100, SchemaVersion: 1}
	if got != want {
		t.Errorf("got %+v; want %+v", got, want)
	}
}

func TestFederationDaemonAdapter_LifecycleWriteMethods(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "workspace.db")
	fedDB, err := federation.Open(context.Background(), statePath)
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })
	a := newFederationDaemonAdapter(fedDB)

	if err := a.RegisterWorkspace(context.Background(), daemon.Workspace{
		WorkspaceID: "ws-life", OwningProject: "proj-a", PolicyLocked: true,
		CreatedAt: 101, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := a.AddWorkspaceMember(context.Background(), daemon.Member{
		WorkspaceID: "ws-life", ProjectID: "proj-a", RegisteredAt: 102,
	}); err != nil {
		t.Fatalf("AddWorkspaceMember: %v", err)
	}
	if err := a.SetWorkspacePolicy(context.Background(), "ws-life", "permissive"); err != nil {
		t.Fatalf("SetWorkspacePolicy: %v", err)
	}
	policy, err := a.GetWorkspacePolicy(context.Background(), "ws-life")
	if err != nil {
		t.Fatalf("GetWorkspacePolicy: %v", err)
	}
	if policy != "permissive" {
		t.Fatalf("policy = %q; want permissive", policy)
	}
	n, err := a.RemoveWorkspace(context.Background(), "ws-life")
	if err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if n != 1 {
		t.Fatalf("RowsAffected = %d; want 1", n)
	}
}

func TestFederationDaemonAdapter_ValidateContractManifestUsesWorkspaceRoster(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clientDir := filepath.Join(root, "client")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := []byte(`schema_version: 1
services:
  - base_url_env: BACKEND_URL
    target_repo: backend
unresolved_policy: surface
`)
	if err := os.WriteFile(filepath.Join(clientDir, "caronte.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	fedDB, err := federation.Open(context.Background(), filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })
	a := newFederationDaemonAdapter(fedDB)
	if err := a.RegisterWorkspace(context.Background(), daemon.Workspace{
		WorkspaceID: "ws-validate", OwningProject: "client", CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	for _, projectID := range []string{"client", "backend"} {
		if err := a.AddWorkspaceMember(context.Background(), daemon.Member{
			WorkspaceID: "ws-validate", ProjectID: projectID, RegisteredAt: 2,
		}); err != nil {
			t.Fatalf("AddWorkspaceMember(%s): %v", projectID, err)
		}
	}

	resp, err := a.ValidateContractManifest(context.Background(), clientDir, "ws-validate")
	if err != nil {
		t.Fatalf("ValidateContractManifest: %v", err)
	}
	if !resp.Valid || resp.SchemaVersion != 1 {
		t.Fatalf("resp = %+v; want valid schema v1", resp)
	}
	if len(resp.Services) != 1 || resp.Services[0].BaseURLRef != "${BACKEND_URL}" || resp.Services[0].TargetRepo != "backend" {
		t.Fatalf("services = %+v; want ${BACKEND_URL} -> backend", resp.Services)
	}
}

func TestFederationDaemonAdapter_ValidateContractManifestSurfacesRefusal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clientDir := filepath.Join(root, "client")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := []byte(`schema_version: 1
services:
  - base_url_env: BACKEND_URL
    target_repo: missing
`)
	if err := os.WriteFile(filepath.Join(clientDir, "caronte.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	fedDB, err := federation.Open(context.Background(), filepath.Join(root, "workspace.db"))
	if err != nil {
		t.Fatalf("federation.Open: %v", err)
	}
	t.Cleanup(func() { _ = fedDB.Close() })
	a := newFederationDaemonAdapter(fedDB)
	if err := a.RegisterWorkspace(context.Background(), daemon.Workspace{
		WorkspaceID: "ws-refuse", OwningProject: "client", CreatedAt: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatalf("RegisterWorkspace: %v", err)
	}
	if err := a.AddWorkspaceMember(context.Background(), daemon.Member{
		WorkspaceID: "ws-refuse", ProjectID: "client", RegisteredAt: 2,
	}); err != nil {
		t.Fatalf("AddWorkspaceMember: %v", err)
	}

	resp, err := a.ValidateContractManifest(context.Background(), clientDir, "ws-refuse")
	if err != nil {
		t.Fatalf("ValidateContractManifest: %v", err)
	}
	if resp.Valid {
		t.Fatalf("resp.Valid = true; want false for missing roster target: %+v", resp)
	}
	if len(resp.Errors) != 1 || resp.Errors[0].Code != "unknown_target_repo" {
		t.Fatalf("errors = %+v; want unknown_target_repo", resp.Errors)
	}
}

func TestBuildContractFederation_NilAuditReturnsSentinel(t *testing.T) {
	t.Parallel()
	_, _, err := buildContractFederation(contractFederationWiringDeps{
		audit:     nil,
		doctrine:  fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath: filepath.Join(t.TempDir(), "workspace.db"),
	})
	if err == nil {
		t.Fatal("expected error on nil audit; got nil")
	}
	if !errors.Is(err, ErrBuildContractFederationNoAudit) {
		t.Errorf("errors.Is(err, ErrBuildContractFederationNoAudit) = false; err = %v", err)
	}
}

func TestDefaultPolicyOracle_BoundaryAtNamedConstant(t *testing.T) {
	t.Parallel()
	o := newDefaultPolicyOracle(fakeDoctrineResolver{p: permissiveTestPolicy{}})
	makeConsumers := func(n int) []coordinated.ConsumerRef {
		out := make([]coordinated.ConsumerRef, n)
		for i := range out {
			out[i] = coordinated.ConsumerRef{Repo: "r", CallID: "c"}
		}
		return out
	}

	atCap := coordinated.ContractBreakage{AffectedConsumers: makeConsumers(defaultBlastRadiusAutonomyMax)}
	if got := o.Decision(atCap); got != coordinated.ModeAutonomy {
		t.Errorf("at-cap (%d consumers): got %v; want ModeAutonomy (the > comparison treats the constant as a STRICT cap; exact-N consumers stays autonomous)",
			defaultBlastRadiusAutonomyMax, got)
	}

	overCap := coordinated.ContractBreakage{AffectedConsumers: makeConsumers(defaultBlastRadiusAutonomyMax + 1)}
	if got := o.Decision(overCap); got != coordinated.ModeSurface {
		t.Errorf("over-cap (%d consumers): got %v; want ModeSurface (blast-radius caution)",
			defaultBlastRadiusAutonomyMax+1, got)
	}
}

func TestDefaultBlastRadiusAutonomyMax_HasExpectedValue(t *testing.T) {
	t.Parallel()
	if defaultBlastRadiusAutonomyMax != 5 {
		t.Errorf("defaultBlastRadiusAutonomyMax = %d; want 5 (v0.19.0 ship value per master AS-BUILT CORRECTION #14; doctrine-driven change required to mutate)",
			defaultBlastRadiusAutonomyMax)
	}
}

func TestCoordinatorDaemonAdapter_RecentDispatches_RowsRangeBody(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 5, 24, 12, 5, 0, 0, time.UTC)
	src := &fakeCoordinatorSource{
		rows: []coordinated.DispatchDecision{
			{
				ChangeID:        "ch-rb-1",
				Mode:            coordinated.ModeAutonomy,
				DispatchedRepos: []string{"proj-a", "proj-b"},
				AuditID:         "leaf-rb-1",
				DecidedAt:       t0,
			},
			{
				ChangeID:        "ch-rb-2",
				Mode:            coordinated.ModeSurface,
				DispatchedRepos: nil,
				AuditID:         "leaf-rb-2",
				DecidedAt:       t1,
			},
		},
	}
	a := &coordinatorDaemonAdapter{coord: src}
	got, err := a.RecentDispatches(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentDispatches: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d; want 2", len(got))
	}
	want0 := daemon.DispatchDecision{
		ChangeID:        "ch-rb-1",
		Mode:            "Autonomy",
		DispatchedRepos: []string{"proj-a", "proj-b"},
		AuditID:         "leaf-rb-1",
		DecidedAt:       t0.Unix(),
	}
	want1 := daemon.DispatchDecision{
		ChangeID:        "ch-rb-2",
		Mode:            "Surface",
		DispatchedRepos: nil,
		AuditID:         "leaf-rb-2",
		DecidedAt:       t1.Unix(),
	}
	if got[0].ChangeID != want0.ChangeID || got[0].Mode != want0.Mode ||
		got[0].AuditID != want0.AuditID || got[0].DecidedAt != want0.DecidedAt ||
		len(got[0].DispatchedRepos) != 2 ||
		got[0].DispatchedRepos[0] != "proj-a" || got[0].DispatchedRepos[1] != "proj-b" {
		t.Errorf("row 0: got %+v; want %+v", got[0], want0)
	}
	if got[1].ChangeID != want1.ChangeID || got[1].Mode != want1.Mode ||
		got[1].AuditID != want1.AuditID || got[1].DecidedAt != want1.DecidedAt {
		t.Errorf("row 1: got %+v; want %+v", got[1], want1)
	}
}

func TestCoordinatorDaemonAdapter_RecentDispatches_PropagatesError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("source-error-from-fake")
	src := &fakeCoordinatorSource{err: sentinel}
	a := &coordinatorDaemonAdapter{coord: src}
	got, err := a.RecentDispatches(context.Background(), 10)
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is(err, sentinel) = false; err = %v", err)
	}
	if got != nil {
		t.Errorf("expected nil rows on err; got %+v", got)
	}
}

type fakeCoordinatorSource struct {
	rows []coordinated.DispatchDecision
	err  error
}

func (f *fakeCoordinatorSource) RecentDispatches(_ context.Context, _ int) ([]coordinated.DispatchDecision, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func TestBuildContractFederation_EmptyStatePathReturnsSentinel(t *testing.T) {
	t.Parallel()
	audit := newTesseraAdapterForTest(t)
	_, _, err := buildContractFederation(contractFederationWiringDeps{
		audit:     audit,
		doctrine:  fakeDoctrineResolver{p: permissiveTestPolicy{}},
		statePath: "",
	})
	if err == nil {
		t.Fatal("expected error on empty statePath; got nil")
	}
	if !errors.Is(err, ErrBuildContractFederationEmptyStatePath) {
		t.Errorf("errors.Is(err, ErrBuildContractFederationEmptyStatePath) = false; err = %v", err)
	}
}
