//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package store

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Workspace struct {
	workspaceID   string
	members       []wsMember
	byID          map[string]*Store
	policy        WorkspacePolicy
	mu            sync.RWMutex
	pending       []ContractLink
	linkStore     linkStorePort
	graphqlNFPort graphqlNodeFallbackPort
	closed        bool
}

type wsMember struct {
	projectID string
	store     *Store
}

type WorkspaceMember struct {
	ProjectID string
	Store     *Store
}

type permissivePolicy struct{}

func (permissivePolicy) PrivacyLocked() bool { return false }

type linkStorePort interface {
	Append(ctx context.Context, link ContractLink) error
}

type WorkspaceOption func(*Workspace)

func WithLinkStore(ls linkStorePort) WorkspaceOption {
	return func(w *Workspace) { w.linkStore = ls }
}

// graphqlNodeFallbackPort is the narrow indirection through which the
// invariant production wiring
// reads the persistent caronte_workspaces.enable_graphql_node_fallback
// flag without creating an import cycle (the federation package imports
// internal/caronte/store for value types, so a direct import of
// federation.WorkspaceFederationDB here would cycle). The daemon
// composition root wires a tiny adapter over
// federation.WorkspaceFederationDB.GetWorkspace behind this interface.
//
// EnableGraphQLNodeFallback returns the persisted flag for the workspaceID
// the Workspace was constructed with. Returning (false, nil) on lookup
// failure is the graceful-degrade contract: an unreachable federation DB
// MUST NOT open the gate; the release default is gate-closed (the
// SevInsufficient result is surfaced to the operator instead of triggering
// a Node spawn). Implementations MAY return a wrapped error for
// observability; the Workspace accessor swallows the error and degrades
// to false (the gate is fail-closed, see EnableGraphQLNodeFallback below).
type graphqlNodeFallbackPort interface {
	EnableGraphQLNodeFallback(ctx context.Context, workspaceID string) (bool, error)
}

func WithGraphQLNodeFallbackPort(p graphqlNodeFallbackPort) WorkspaceOption {
	return func(w *Workspace) { w.graphqlNFPort = p }
}

func workspaceBoundarySentinel() error { return nil }

func NewWorkspace(workspaceID string, members []WorkspaceMember, policy WorkspacePolicy) (*Workspace, error) {
	if err := workspaceBoundarySentinel(); err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, ErrEmptyWorkspace
	}
	if policy == nil {
		policy = permissivePolicy{}
	}
	w := &Workspace{
		workspaceID: workspaceID,
		members:     make([]wsMember, 0, len(members)),
		byID:        make(map[string]*Store, len(members)),
		policy:      policy,
	}
	for _, m := range members {
		if m.Store == nil {
			return nil, ErrEmptyDB
		}
		if _, dup := w.byID[m.ProjectID]; dup {
			return nil, ErrDuplicateProject
		}
		w.members = append(w.members, wsMember{projectID: m.ProjectID, store: m.Store})
		w.byID[m.ProjectID] = m.Store
	}
	return w, nil
}

func NewWorkspaceWithOptions(workspaceID string, members []WorkspaceMember, policy WorkspacePolicy, opts ...WorkspaceOption) (*Workspace, error) {
	w, err := NewWorkspace(workspaceID, members, policy)
	if err != nil {
		return nil, err
	}
	for _, opt := range opts {
		opt(w)
	}
	return w, nil
}

func (w *Workspace) Projects() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, len(w.members))
	for i, m := range w.members {
		out[i] = m.projectID
	}
	return out
}

func (w *Workspace) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *Workspace) owningProject() string { return w.members[0].projectID }

func (w *Workspace) AuthorizeProjects(projects []string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.authorize(projects)
}

// EnableGraphQLNodeFallback is the invariant production-wiring accessor.
// Returns the persisted caronte_workspaces.enable_graphql_node_fallback
// flag for this workspace via the
// graphqlNodeFallbackPort seam wired at construction with
// WithGraphQLNodeFallbackPort.
//
// Gate-closed defaults:
//
// - Port unset (nil) → returns false. The default Workspace constructed
// via NewWorkspace has no port; the daemon composition root
// is the only production caller that wires WithGraphQLNodeFallbackPort.
// gate-closed.
// - Port lookup error (e.g., unreachable federation DB) → returns false.
// The error is intentionally NOT surfaced — the invariant spawn
// gate MUST be fail-closed (a transient persistence failure must
// NEVER open the spawn-site by accident). The port implementation
// SHOULD log the error for observability; this accessor's contract
// is bool-only so consumers (bcdetect.Pipeline.Fan) can treat the
// result as a simple gate condition.
//
// Acquires the same read lock as Projects() / AuthorizeProjects() (mu.RLock
// inside). Safe to call concurrently. Uses context.Background() at the
// port boundary — the lookup is a fast in-process DB read (single
// indexed SELECT) and the upstream Pipeline.Fan caller's ctx is not
// available here without changing the accessor's signature (the
// signature is bool-only by contract, matching the spec-review MAJOR
// fix-shape "workspace.EnableGraphQLNodeFallback()").
func (w *Workspace) EnableGraphQLNodeFallback() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.graphqlNFPort == nil {
		return false
	}
	enabled, err := w.graphqlNFPort.EnableGraphQLNodeFallback(context.Background(), w.workspaceID)
	if err != nil {

		return false
	}
	return enabled
}

func (w *Workspace) authorize(ids []string) error {
	locked := w.policy.PrivacyLocked()
	own := w.owningProject()
	for _, id := range ids {
		if _, ok := w.byID[id]; !ok {
			return ErrUnauthorizedProject
		}
		if locked && id != own {
			return ErrCrossProjectDenied
		}
	}
	return nil
}

func (w *Workspace) FederatedQuery(ctx context.Context, q FederatedQuery) ([]FederatedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	scope := q.Scope
	if len(scope) == 0 {
		scope = make([]string, len(w.members))
		for i, m := range w.members {
			scope[i] = m.projectID
		}
	}
	if err := w.authorize(scope); err != nil {
		return nil, err
	}
	results := make([]FederatedResult, 0, len(scope))
	for _, id := range scope {

		if q.Kind == "" {
			results = append(results, FederatedResult{ProjectID: id, Kind: "roster", Link: nil})
		}

	}
	return results, nil
}

// CrossRepoLink validates a cross-repo contract edge and records it. Both
// CallRepo + EndpointRepo MUST be roster members; under a privacy-locked
// doctrine a cross-repo link (CallRepo != EndpointRepo) is denied. release
// records the validated link in an in-memory ledger (w.pending); release
// extends with a persistent contract_links table via w.linkStore (a
// federation.LinkStore wired at the daemon composition root). The
// persistent write runs AFTER w.authorize so capa-firewall consistency is
// preserved. A link to a non-member repo is refused
// (ErrUnauthorizedProject) — never a false cross-workspace link.
//
// are populated INSIDE this method — the signature stays unchanged so
// the C-2 columns the table requires. Defaults respect non-zero
// caller-set values so a custom-resolved link from the linker
// survives the seam untouched.
//
// Review I1 atomicity contract: ledger and store are kept in lock-step.
// The sequence is:
//
// 1. authorize() gate (capa-firewall)
// 2. populate FIX-4 defaults (ResolvedAt, LinkMethod) — applied
// UNCONDITIONALLY so the ledger snapshot mirrors the persisted row
// even when w.linkStore is nil
// 3. persist via w.linkStore.Append (when non-nil) — FAILURE PROPAGATES
// and the ledger is NOT modified
// 4. append to in-memory ledger ONLY on persist success
//
// Rationale previously the ledger was appended FIRST. Two divergence
// modes resulted — (a) a failed Append left the ledger with an entry
// that the persistent table never received (retry would double-write);
// (b) defaults populated only inside the linkStore branch meant the
// ledger snapshot held ResolvedAt=0/LinkMethod="" while the store saw
// the populated values. Both broken; both now structurally impossible.
func (w *Workspace) CrossRepoLink(ctx context.Context, link ContractLink) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrEmptyWorkspace
	}
	if err := w.authorize([]string{link.CallRepo, link.EndpointRepo}); err != nil {
		return err
	}

	if link.ResolvedAt == 0 {
		link.ResolvedAt = time.Now().Unix()
	}
	if link.LinkMethod == "" {
		link.LinkMethod = "caronte_yaml"
	}
	// Step 3: persist FIRST — a failure here MUST NOT pollute the ledger.
	if w.linkStore != nil {
		if err := w.linkStore.Append(ctx, link); err != nil {
			return fmt.Errorf("caronte/store: linkStore.Append: %w", err)
		}
	}

	w.pending = append(w.pending, link)
	return nil
}
