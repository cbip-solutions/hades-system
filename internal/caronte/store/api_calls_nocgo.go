//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import "context"

func (s *Store) InsertAPICall(_ context.Context, _ APICall) error {
	return ErrCGODisabled
}

func (s *Store) GetAPICall(_ context.Context, _ string) (APICall, error) {
	return APICall{}, ErrCGODisabled
}

func (s *Store) ListAPICallsByCaller(_ context.Context, _ string) ([]APICall, error) {
	return nil, ErrCGODisabled
}

func (s *Store) ListAPICallsByRepo(_ context.Context, _ string) ([]APICall, error) {
	return nil, ErrCGODisabled
}

func (s *Store) DeleteAPICallsByFile(_ context.Context, _ string) (int, error) {
	return 0, ErrCGODisabled
}
