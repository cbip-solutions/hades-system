//go:build !cgo
// +build !cgo

// SPDX-License-Identifier: MIT

package semantic

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type ResolverOpts struct {
	MaxTailSites        int
	PreferStaleSnapshot bool
}

const DefaultMaxTailSites = 32

type Resolver struct {
	store *store.Store
}

func NewResolver(s *store.Store, _ CaronteDispatcher, _ ResolverOpts) *Resolver {
	return &Resolver{store: s}
}

func (r *Resolver) ResolveProject(_ context.Context, _ string, _ string) (ResolutionStats, error) {
	return ResolutionStats{}, ErrCGODisabled
}

func (r *Resolver) GetImplementations(_ context.Context, _ string) ([]Implementation, error) {
	return nil, ErrCGODisabled
}

func (r *Resolver) TraceCallPath(_ context.Context, _ string, _ int) ([]CallPathHop, error) {
	return nil, ErrCGODisabled
}
