//go:build cgo
// +build cgo

package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newWorkspaceMember(t *testing.T, projectID string) WorkspaceMember {
	t.Helper()
	s, err := Open(context.Background(), openRawTestDB(t))
	if err != nil {
		t.Fatalf("store.Open(%s): %v", projectID, err)
	}
	return WorkspaceMember{ProjectID: projectID, Store: s}
}

func TestNewWorkspaceRejectsEmptyRoster(t *testing.T) {
	_, err := NewWorkspace("ws-1", nil, nil)
	if !errors.Is(err, ErrEmptyWorkspace) {
		t.Errorf("NewWorkspace(empty) err = %v; want ErrEmptyWorkspace", err)
	}
}

func TestProjectsPreservesOrder(t *testing.T) {
	members := []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
		newWorkspaceMember(t, "shared"),
	}
	w, err := NewWorkspace("ws-1", members, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	got := w.Projects()
	want := []string{"backend", "ui", "shared"}
	if len(got) != len(want) {
		t.Fatalf("Projects() len = %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Projects()[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestNewWorkspaceRejectsDuplicateProject(t *testing.T) {
	members := []WorkspaceMember{
		newWorkspaceMember(t, "dup"),
		newWorkspaceMember(t, "dup"),
	}
	_, err := NewWorkspace("ws-1", members, nil)
	if !errors.Is(err, ErrDuplicateProject) {
		t.Errorf("NewWorkspace(dup) err = %v; want ErrDuplicateProject", err)
	}
}

func TestNewWorkspaceRejectsNilStore(t *testing.T) {
	_, err := NewWorkspace("ws-1", []WorkspaceMember{{ProjectID: "p", Store: nil}}, nil)
	if !errors.Is(err, ErrEmptyDB) {
		t.Errorf("NewWorkspace(nil store) err = %v; want ErrEmptyDB", err)
	}
}

func TestWorkspaceBoundarySentinelReachable(t *testing.T) {
	if err := workspaceBoundarySentinel(); err != nil {
		t.Errorf("workspaceBoundarySentinel() = %v; want nil", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "p")}, nil)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close 2: %v (must be idempotent)", err)
	}
}

func TestFederatedQueryEmptyButCorrect(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
	}, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	got, err := w.FederatedQuery(context.Background(), FederatedQuery{Kind: "contract_link"})
	if err != nil {
		t.Fatalf("FederatedQuery: %v (must succeed with empty result, not error)", err)
	}
	if got == nil {
		t.Error("FederatedQuery returned nil slice; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("FederatedQuery returned %d results; want 0 (no contract extractors in Plan 19)", len(got))
	}
}

func TestFederatedQueryRosterProbe(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
	}, stubPolicy{locked: false})
	got, err := w.FederatedQuery(context.Background(), FederatedQuery{})
	if err != nil {
		t.Fatalf("FederatedQuery(probe): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("roster probe returned %d; want 2", len(got))
	}
	seen := map[string]bool{}
	for _, r := range got {
		seen[r.ProjectID] = true
		if r.Link != nil {
			t.Errorf("roster probe result for %s has non-nil Link; want nil in Plan 19", r.ProjectID)
		}
	}
	if !seen["backend"] || !seen["ui"] {
		t.Errorf("roster probe missing a member: %v", seen)
	}
}

func TestFederatedQueryRejectsUnauthorized(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "backend")}, stubPolicy{locked: false})
	_, err := w.FederatedQuery(context.Background(), FederatedQuery{Kind: "contract_link", Scope: []string{"backend", "evil-other-repo"}})
	if !errors.Is(err, ErrUnauthorizedProject) {
		t.Errorf("FederatedQuery(unauthorized scope) err = %v; want ErrUnauthorizedProject", err)
	}
}

func TestFederatedQueryCapaFirewallLocal(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "owning"),
		newWorkspaceMember(t, "other"),
	}, stubPolicy{locked: true})

	_, err := w.FederatedQuery(context.Background(), FederatedQuery{Kind: "contract_link", Scope: []string{"owning", "other"}})
	if !errors.Is(err, ErrCrossProjectDenied) {
		t.Errorf("privacy-locked cross-project FederatedQuery err = %v; want ErrCrossProjectDenied", err)
	}

	got, err := w.FederatedQuery(context.Background(), FederatedQuery{Kind: "contract_link", Scope: []string{"owning"}})
	if err != nil {
		t.Fatalf("privacy-locked local FederatedQuery: %v (local must be permitted)", err)
	}
	if len(got) != 0 {
		t.Errorf("local query returned %d; want 0", len(got))
	}
}

func TestCrossRepoLinkAuthorized(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
	}, stubPolicy{locked: false})
	link := ContractLink{CallID: "ui:call:1", CallRepo: "ui", EndpointID: "backend:http:GET /users/{param}", EndpointRepo: "backend", Confidence: "spec_artifact", WorkspaceID: "ws-1"}
	if err := w.CrossRepoLink(context.Background(), link); err != nil {
		t.Fatalf("CrossRepoLink(authorized): %v", err)
	}
}

func TestCrossRepoLinkRejectsNonMember(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "backend")}, stubPolicy{locked: false})
	link := ContractLink{CallID: "x:call:1", CallRepo: "x-not-a-member", EndpointID: "backend:e", EndpointRepo: "backend", Confidence: "fuzzy_path", WorkspaceID: "ws-1"}
	if !errors.Is(w.CrossRepoLink(context.Background(), link), ErrUnauthorizedProject) {
		t.Error("CrossRepoLink(non-member call repo) must return ErrUnauthorizedProject")
	}
}

func TestCrossRepoLinkCapaFirewallDenied(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
	}, stubPolicy{locked: true})
	link := ContractLink{CallID: "ui:call:1", CallRepo: "ui", EndpointID: "backend:e", EndpointRepo: "backend", Confidence: "spec_artifact", WorkspaceID: "ws-1"}
	if !errors.Is(w.CrossRepoLink(context.Background(), link), ErrCrossProjectDenied) {
		t.Error("privacy-locked cross-repo CrossRepoLink must return ErrCrossProjectDenied")
	}
}

func TestFederatedQueryNilPolicyDefaultsPermissive(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "ui"),
	}, nil)
	if err != nil {
		t.Fatalf("NewWorkspace(nil policy): %v", err)
	}
	got, err := w.FederatedQuery(context.Background(), FederatedQuery{Scope: []string{"backend", "ui"}})
	if err != nil {
		t.Fatalf("nil-policy cross-project FederatedQuery: %v (permissive default must allow cross-project)", err)
	}
	if len(got) != 2 {
		t.Errorf("nil-policy roster probe returned %d; want 2 (permissive fan-out reaches all)", len(got))
	}
}

func TestFederatedQueryContextCancelled(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "backend")}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.FederatedQuery(ctx, FederatedQuery{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FederatedQuery(cancelled ctx) err = %v; want context.Canceled", err)
	}
}

func TestCrossRepoLinkContextCancelled(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "backend")}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	link := ContractLink{CallID: "c", CallRepo: "backend", EndpointID: "e", EndpointRepo: "backend", Confidence: "exact_proto_import", WorkspaceID: "ws-1"}
	if !errors.Is(w.CrossRepoLink(ctx, link), context.Canceled) {
		t.Error("CrossRepoLink(cancelled ctx) must return context.Canceled")
	}
}

func TestCrossRepoLinkAfterCloseRejected(t *testing.T) {
	w, _ := NewWorkspace("ws-1", []WorkspaceMember{newWorkspaceMember(t, "backend")}, nil)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	link := ContractLink{CallID: "c", CallRepo: "backend", EndpointID: "e", EndpointRepo: "backend", Confidence: "exact_proto_import", WorkspaceID: "ws-1"}
	if !errors.Is(w.CrossRepoLink(context.Background(), link), ErrEmptyWorkspace) {
		t.Error("CrossRepoLink after Close must return ErrEmptyWorkspace (sealed ledger)")
	}
}

type fakeLinkStorePort struct {
	calls    int
	lastLink ContractLink
	failWith error
}

func (f *fakeLinkStorePort) Append(_ context.Context, link ContractLink) error {
	f.calls++
	f.lastLink = link
	return f.failWith
}

func TestWorkspaceCrossRepoLink_PersistsViaLinkStore(t *testing.T) {
	ctx := context.Background()
	fake := &fakeLinkStorePort{}
	w, err := NewWorkspaceWithOptions("ws-x",
		[]WorkspaceMember{
			newWorkspaceMember(t, "p1"),
			newWorkspaceMember(t, "p2"),
		},
		nil,
		WithLinkStore(fake),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	link := ContractLink{
		CallID:       "c-1",
		CallRepo:     "p1",
		EndpointID:   "ep-1",
		EndpointRepo: "p2",
		Confidence:   "static_path",
		WorkspaceID:  "ws-x",
	}
	if err := w.CrossRepoLink(ctx, link); err != nil {
		t.Fatalf("CrossRepoLink: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("linkStore.Append calls = %d; want 1", fake.calls)
	}
	if fake.lastLink.CallID != link.CallID ||
		fake.lastLink.EndpointID != link.EndpointID ||
		fake.lastLink.WorkspaceID != link.WorkspaceID {
		t.Errorf("linkStore.lastLink core fields = %+v; want %+v", fake.lastLink, link)
	}
	if fake.lastLink.ResolvedAt == 0 {
		t.Error("linkStore.lastLink.ResolvedAt = 0; want a non-zero unix timestamp populated by CrossRepoLink")
	}
	if fake.lastLink.LinkMethod == "" {
		t.Error("linkStore.lastLink.LinkMethod = empty; want a populated default (\"caronte_yaml\")")
	}
}

func TestWorkspaceCrossRepoLink_NoLinkStore_KeepsPlan19Behavior(t *testing.T) {
	ctx := context.Background()
	w, err := NewWorkspace("ws-y",
		[]WorkspaceMember{
			newWorkspaceMember(t, "p1"),
			newWorkspaceMember(t, "p2"),
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	link := ContractLink{
		CallID: "c-1", CallRepo: "p1",
		EndpointID: "ep-1", EndpointRepo: "p2",
		Confidence: "static_path", WorkspaceID: "ws-y",
	}
	if err := w.CrossRepoLink(ctx, link); err != nil {
		t.Fatalf("CrossRepoLink: %v", err)
	}
	w.mu.RLock()
	n := len(w.pending)
	w.mu.RUnlock()
	if n != 1 {
		t.Errorf("pending ledger len = %d; want 1 (Plan 19 behaviour)", n)
	}
}

// TestWorkspaceCrossRepoLink_LinkStoreErrorPropagates asserts a downstream
// persistence error fans out as a clean wrapped error (callers in Phase F
// switch on it). Review I1 extension: the in-memory `pending` ledger
// MUST NOT be polluted when the persistent write fails — ledger and
// store are kept in lock-step (persist-then-append).
func TestWorkspaceCrossRepoLink_LinkStoreErrorPropagates(t *testing.T) {
	ctx := context.Background()
	fake := &fakeLinkStorePort{failWith: errors.New("disk full")}
	w, err := NewWorkspaceWithOptions("ws-z",
		[]WorkspaceMember{
			newWorkspaceMember(t, "p1"),
			newWorkspaceMember(t, "p2"),
		},
		nil,
		WithLinkStore(fake),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.mu.RLock()
	initialLen := len(w.pending)
	w.mu.RUnlock()
	err = w.CrossRepoLink(ctx, ContractLink{
		CallID: "c-1", CallRepo: "p1",
		EndpointID: "ep-1", EndpointRepo: "p2",
		Confidence: "static_path", WorkspaceID: "ws-z",
	})
	if err == nil {
		t.Fatal("CrossRepoLink returned nil err on linkStore failure")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err %q does not wrap linkStore error", err)
	}
	// Review I1: the pending ledger MUST NOT have grown — the store-
	// append failed, so the ledger and the store stay in lock-step (no
	// divergence: a future retry won't double-write).
	w.mu.RLock()
	finalLen := len(w.pending)
	w.mu.RUnlock()
	if finalLen != initialLen {
		t.Errorf("pending ledger len = %d after failed linkStore.Append; want %d (no divergence)", finalLen, initialLen)
	}
}

// TestWorkspaceCrossRepoLink_LinkStorePreservesCallerSetFields asserts a
// caller-set non-zero ResolvedAt + non-empty LinkMethod SURVIVE the
// default-population branch — the Phase F linker constructs the full
// 8-field literal directly and the seam must not clobber those values.
// Review I1 extension: the in-memory pending ledger entry and the
// fake-linkStore captured entry MUST be byte-identical (single source
// of truth — populate-defaults BEFORE the ledger append).
func TestWorkspaceCrossRepoLink_LinkStorePreservesCallerSetFields(t *testing.T) {
	ctx := context.Background()
	fake := &fakeLinkStorePort{}
	w, err := NewWorkspaceWithOptions("ws-q",
		[]WorkspaceMember{
			newWorkspaceMember(t, "p1"),
			newWorkspaceMember(t, "p2"),
		},
		nil,
		WithLinkStore(fake),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	link := ContractLink{
		CallID: "c-1", CallRepo: "p1",
		EndpointID: "ep-1", EndpointRepo: "p2",
		Confidence: "static_path", WorkspaceID: "ws-q",
		ResolvedAt: 17_000_000_001, LinkMethod: "artifact",
	}
	if err := w.CrossRepoLink(ctx, link); err != nil {
		t.Fatalf("CrossRepoLink: %v", err)
	}
	if fake.lastLink.ResolvedAt != 17_000_000_001 {
		t.Errorf("ResolvedAt = %d; want 17000000001 (caller-set must survive)", fake.lastLink.ResolvedAt)
	}
	if fake.lastLink.LinkMethod != "artifact" {
		t.Errorf("LinkMethod = %q; want %q (caller-set must survive)", fake.lastLink.LinkMethod, "artifact")
	}
	// Review I1: the ledger entry and the store entry MUST be byte-
	// identical (defaults populated BEFORE the persist-then-append
	// sequence). A divergence here would mean the in-memory snapshot
	// and the persistent table see different values for the SAME link
	// — a single-source-of-truth break.
	w.mu.RLock()
	pending := append([]ContractLink(nil), w.pending...)
	w.mu.RUnlock()
	if len(pending) != 1 {
		t.Fatalf("pending ledger len = %d after one successful CrossRepoLink; want 1", len(pending))
	}
	if pending[0] != fake.lastLink {
		t.Errorf("ledger/store divergence:\n ledger: %+v\n store:  %+v", pending[0], fake.lastLink)
	}
}

// TestWorkspaceCrossRepoLink_LinkStorePopulatesDefaultsBeforeLedger pins
// review I1 from the opposite angle — the additive defaults populated
// by the Workspace (ResolvedAt = unix-now, LinkMethod = "caronte_yaml")
// MUST land in BOTH the ledger entry AND the captured store entry, so
// the ledger never holds a "ResolvedAt=0 / LinkMethod=\"\"" zero-default
// snapshot while the store sees the populated values.
func TestWorkspaceCrossRepoLink_LinkStorePopulatesDefaultsBeforeLedger(t *testing.T) {
	ctx := context.Background()
	fake := &fakeLinkStorePort{}
	w, err := NewWorkspaceWithOptions("ws-r",
		[]WorkspaceMember{
			newWorkspaceMember(t, "p1"),
			newWorkspaceMember(t, "p2"),
		},
		nil,
		WithLinkStore(fake),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	link := ContractLink{
		CallID: "c-1", CallRepo: "p1",
		EndpointID: "ep-1", EndpointRepo: "p2",
		Confidence: "static_path", WorkspaceID: "ws-r",
	}
	if err := w.CrossRepoLink(ctx, link); err != nil {
		t.Fatalf("CrossRepoLink: %v", err)
	}
	w.mu.RLock()
	pending := append([]ContractLink(nil), w.pending...)
	w.mu.RUnlock()
	if len(pending) != 1 {
		t.Fatalf("pending ledger len = %d; want 1", len(pending))
	}
	if pending[0].ResolvedAt == 0 {
		t.Error("ledger entry ResolvedAt = 0; want a populated default (defaults must apply BEFORE ledger append)")
	}
	if pending[0].LinkMethod == "" {
		t.Error("ledger entry LinkMethod = \"\"; want \"caronte_yaml\" default (defaults must apply BEFORE ledger append)")
	}
	if pending[0] != fake.lastLink {
		t.Errorf("ledger/store divergence:\n ledger: %+v\n store:  %+v", pending[0], fake.lastLink)
	}
}

func TestAuthorizeProjectsAcceptsRosterMembers(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "client-a"),
		newWorkspaceMember(t, "client-b"),
	}, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if err := w.AuthorizeProjects([]string{"backend", "client-a"}); err != nil {
		t.Errorf("AuthorizeProjects([backend, client-a]) = %v; want nil", err)
	}
	if err := w.AuthorizeProjects([]string{"backend", "client-a", "client-b"}); err != nil {
		t.Errorf("AuthorizeProjects(all 3) = %v; want nil", err)
	}
}

func TestAuthorizeProjectsRejectsNonMember(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "client-a"),
	}, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	err = w.AuthorizeProjects([]string{"backend", "client-z"})
	if !errors.Is(err, ErrUnauthorizedProject) {
		t.Errorf("AuthorizeProjects([backend, client-z]) = %v; want ErrUnauthorizedProject", err)
	}
}

func TestAuthorizeProjectsCapaFirewallDenied(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
		newWorkspaceMember(t, "client-a"),
	}, stubPolicy{locked: true})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}

	if err := w.AuthorizeProjects([]string{"backend"}); err != nil {
		t.Errorf("AuthorizeProjects(owning) = %v; want nil under capa-firewall", err)
	}

	err = w.AuthorizeProjects([]string{"backend", "client-a"})
	if !errors.Is(err, ErrCrossProjectDenied) {
		t.Errorf("AuthorizeProjects(cross) = %v; want ErrCrossProjectDenied under capa-firewall", err)
	}
}

func TestAuthorizeProjectsEmptyListPermitted(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
	}, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if err := w.AuthorizeProjects(nil); err != nil {
		t.Errorf("AuthorizeProjects(nil) = %v; want nil", err)
	}
	if err := w.AuthorizeProjects([]string{}); err != nil {
		t.Errorf("AuthorizeProjects(empty) = %v; want nil", err)
	}
}

type fakeGraphQLNodeFallbackPort struct {
	enabled bool
	err     error
}

func (f *fakeGraphQLNodeFallbackPort) EnableGraphQLNodeFallback(_ context.Context, _ string) (bool, error) {
	return f.enabled, f.err
}

func TestEnableGraphQLNodeFallbackDefaultFalse(t *testing.T) {
	w, err := NewWorkspace("ws-1", []WorkspaceMember{
		newWorkspaceMember(t, "backend"),
	}, stubPolicy{locked: false})
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	if got := w.EnableGraphQLNodeFallback(); got {
		t.Errorf("EnableGraphQLNodeFallback() default = %v; want false (port unset ⇒ gate closed)", got)
	}
}

func TestEnableGraphQLNodeFallbackPortDelegatesTrue(t *testing.T) {
	port := &fakeGraphQLNodeFallbackPort{enabled: true}
	w, err := NewWorkspaceWithOptions("ws-1",
		[]WorkspaceMember{newWorkspaceMember(t, "backend")},
		stubPolicy{locked: false},
		WithGraphQLNodeFallbackPort(port),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	if got := w.EnableGraphQLNodeFallback(); !got {
		t.Errorf("EnableGraphQLNodeFallback() = %v; want true (port returned true)", got)
	}
}

func TestEnableGraphQLNodeFallbackPortDelegatesFalse(t *testing.T) {
	port := &fakeGraphQLNodeFallbackPort{enabled: false}
	w, err := NewWorkspaceWithOptions("ws-1",
		[]WorkspaceMember{newWorkspaceMember(t, "backend")},
		stubPolicy{locked: false},
		WithGraphQLNodeFallbackPort(port),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	if got := w.EnableGraphQLNodeFallback(); got {
		t.Errorf("EnableGraphQLNodeFallback() = %v; want false (port returned false)", got)
	}
}

// TestEnableGraphQLNodeFallbackPortErrorFailsClosed pins the fail-closed
// contract: a port-lookup error MUST degrade to false (the inv-zen-272
// spawn gate is fail-closed under any persistence failure; a transient
// federation-DB error must NEVER open the spawn site). The accessor's
// contract is bool-only: the error is swallowed (the port impl is the
// appropriate place to log it for observability).
func TestEnableGraphQLNodeFallbackPortErrorFailsClosed(t *testing.T) {
	port := &fakeGraphQLNodeFallbackPort{enabled: true, err: errors.New("federation db unreachable")}
	w, err := NewWorkspaceWithOptions("ws-1",
		[]WorkspaceMember{newWorkspaceMember(t, "backend")},
		stubPolicy{locked: false},
		WithGraphQLNodeFallbackPort(port),
	)
	if err != nil {
		t.Fatalf("NewWorkspaceWithOptions: %v", err)
	}
	// Even though the port would return enabled=true, the error MUST
	// fail-closed to false. A transient persistence failure must NEVER
	// open the inv-zen-272 spawn gate.
	if got := w.EnableGraphQLNodeFallback(); got {
		t.Errorf("EnableGraphQLNodeFallback() with port error = %v; want false (fail-closed contract)", got)
	}
}
