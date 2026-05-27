// go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import "context"

type WorkspaceRow struct {
	WorkspaceID   string
	OwningProject string
	PolicyLocked  bool
	CreatedAt     int64
	SchemaVersion int
}

func (w *WorkspaceFederationDB) RegisterWorkspace(_ context.Context, _ WorkspaceRow) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) GetWorkspace(_ context.Context, _ string) (WorkspaceRow, error) {
	return WorkspaceRow{}, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListWorkspaces(_ context.Context) ([]WorkspaceRow, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) RemoveWorkspace(_ context.Context, _ string) (int64, error) {
	return 0, ErrCGODisabled
}

func (w *WorkspaceFederationDB) SetWorkspacePolicy(_ context.Context, _, _ string) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) GetWorkspacePolicy(_ context.Context, _ string) (string, error) {
	return "", ErrCGODisabled
}

func (w *WorkspaceFederationDB) EnableGraphQLNodeFallback(_ context.Context, _ string) (bool, error) {
	return false, ErrCGODisabled
}

func (w *WorkspaceFederationDB) SetEnableGraphQLNodeFallback(_ context.Context, _ string, _ bool) error {
	return ErrCGODisabled
}
