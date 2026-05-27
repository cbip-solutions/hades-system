// go:build !cgo
//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package store

import "context"

func (s *Store) InsertAPIEndpoint(_ context.Context, _ APIEndpoint) error {
	return ErrCGODisabled
}

func (s *Store) GetAPIEndpoint(_ context.Context, _ string) (APIEndpoint, error) {
	return APIEndpoint{}, ErrCGODisabled
}

func (s *Store) ListAPIEndpointsByFile(_ context.Context, _ string) ([]APIEndpoint, error) {
	return nil, ErrCGODisabled
}

func (s *Store) ListAPIEndpointsByRepo(_ context.Context, _ string) ([]APIEndpoint, error) {
	return nil, ErrCGODisabled
}

func (s *Store) DeleteAPIEndpointsByFile(_ context.Context, _ string) (int, error) {
	return 0, ErrCGODisabled
}
