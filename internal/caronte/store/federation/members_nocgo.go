// go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import "context"

type MemberRow struct {
	WorkspaceID  string
	ProjectID    string
	RegisteredAt int64
}

func (w *WorkspaceFederationDB) AddMember(_ context.Context, _ MemberRow) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListWorkspaceMembers(_ context.Context, _ string) ([]MemberRow, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) RemoveMember(_ context.Context, _, _ string) (int64, error) {
	return 0, ErrCGODisabled
}
