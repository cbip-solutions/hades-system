//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import "context"

type Workspace struct{}

type WorkspaceMember struct {
	ProjectID string
	Store     *Store
}

func NewWorkspace(_ string, _ []WorkspaceMember, _ WorkspacePolicy) (*Workspace, error) {
	return nil, ErrCGODisabled
}

func (w *Workspace) Projects() []string { return nil }

func (w *Workspace) FederatedQuery(_ context.Context, _ FederatedQuery) ([]FederatedResult, error) {
	return nil, ErrCGODisabled
}

func (w *Workspace) CrossRepoLink(_ context.Context, _ ContractLink) error { return ErrCGODisabled }

func (w *Workspace) AuthorizeProjects(_ []string) error { return ErrCGODisabled }

func (w *Workspace) EnableGraphQLNodeFallback() bool { return false }

func (w *Workspace) Close() error { return nil }

type linkStorePort interface {
	Append(ctx context.Context, link ContractLink) error
}

type graphqlNodeFallbackPort interface {
	EnableGraphQLNodeFallback(ctx context.Context, workspaceID string) (bool, error)
}

type WorkspaceOption func(*Workspace)

func WithLinkStore(_ linkStorePort) WorkspaceOption {
	return func(_ *Workspace) {}
}

func WithGraphQLNodeFallbackPort(_ graphqlNodeFallbackPort) WorkspaceOption {
	return func(_ *Workspace) {}
}

func NewWorkspaceWithOptions(_ string, _ []WorkspaceMember, _ WorkspacePolicy, _ ...WorkspaceOption) (*Workspace, error) {
	return nil, ErrCGODisabled
}
