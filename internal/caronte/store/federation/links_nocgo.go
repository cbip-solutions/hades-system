// go:build !cgo

// SPDX-License-Identifier: MIT

package federation

import "context"

type LinkRow struct {
	CallID       string
	CallRepo     string
	EndpointID   string
	EndpointRepo string
	Confidence   string
	WorkspaceID  string
	ResolvedAt   int64
	LinkMethod   string
}

type LinkStore interface {
	Append(ctx context.Context, row LinkRow) error
}

type linkStoreImpl struct{}

func (w *WorkspaceFederationDB) LinkStore() LinkStore { return &linkStoreImpl{} }

func (l *linkStoreImpl) Append(_ context.Context, _ LinkRow) error { return ErrCGODisabled }

func (w *WorkspaceFederationDB) GetLink(_ context.Context, _, _, _ string) (LinkRow, error) {
	return LinkRow{}, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListByEndpoint(_ context.Context, _, _, _ string) ([]LinkRow, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListByCall(_ context.Context, _, _, _ string) ([]LinkRow, error) {
	return nil, ErrCGODisabled
}

func (w *WorkspaceFederationDB) DeleteLinksByWorkspace(_ context.Context, _ string) (int64, error) {
	return 0, ErrCGODisabled
}

func (w *WorkspaceFederationDB) ListContractLinks(_ context.Context, _ string, _ int) ([]LinkRow, error) {
	return nil, ErrCGODisabled
}
