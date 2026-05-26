//go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import "context"

type UnresolvedRow struct {
	WorkspaceID string
	CallID      string
	CallRepo    string
	BaseURLRef  string
	Reason      string
	RecordedAt  int64
}

type UnresolvedStore interface {
	Insert(ctx context.Context, row UnresolvedRow) error
}

type unresolvedStoreImpl struct{}

func (w *WorkspaceFederationDB) UnresolvedStore() UnresolvedStore {
	return &unresolvedStoreImpl{}
}

func (u *unresolvedStoreImpl) Insert(_ context.Context, _ UnresolvedRow) error {
	return ErrCGODisabled
}

func (w *WorkspaceFederationDB) GetUnresolved(_ context.Context, _, _, _ string) (UnresolvedRow, error) {
	return UnresolvedRow{}, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListUnresolvedByWorkspace(_ context.Context, _ string, _ int) ([]UnresolvedRow, error) {
	return nil, ErrCGODisabled
}
