// SPDX-License-Identifier: MIT
// AdaptPool wraps a *worktreepool.Pool so it satisfies the FMV-package
// Pool + Lease interfaces. The adapter exists in production (not under
// a _test.go) because will wire FMV into the live orchestrator
// via this surface — production callers MUST be able to reach FMV from
// a real worktreepool without depending on test-package symbols.
//
// Mapping discipline:
// - worktreepool.ErrPoolExhausted → hra.ErrPoolExhausted (package-
// local sentinel) so callers errors.Is against the FMV-package
// surface uniformly.
// - All other lease errors propagate unchanged so substrate-bug
// diagnostics (ErrInvalidConfig, ErrPoolClosed, classified
// subprocess errors) are preserved across the adapter boundary.
// - Release routes through worktreepool.Pool.Release(ctx, *Worktree)
// because Worktree itself has no Release method (the pool owns the
// reset/clean lifecycle).

package hra

import (
	"context"
	"errors"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/worktreepool"
)

func AdaptPool(p worktreepool.Pool) Pool {
	if p == nil {
		return nil
	}
	return &poolAdapter{p: p}
}

type poolAdapter struct {
	p worktreepool.Pool
}

func (a *poolAdapter) Lease(ctx context.Context) (Lease, error) {
	wt, err := a.p.Lease(ctx)
	if err != nil {
		if errors.Is(err, worktreepool.ErrPoolExhausted) {

			return nil, errors.Join(ErrPoolExhausted, err)
		}
		return nil, err
	}
	return &leaseAdapter{wt: wt, pool: a.p}, nil
}

type leaseAdapter struct {
	wt   *worktreepool.Worktree
	pool worktreepool.Pool
}

func (l *leaseAdapter) Path() string { return l.wt.Path() }

func (l *leaseAdapter) Release(ctx context.Context) error {
	return l.pool.Release(ctx, l.wt)
}
