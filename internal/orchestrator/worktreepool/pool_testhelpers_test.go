package worktreepool

import (
	"errors"
	"fmt"
	"path/filepath"
)

// This file contains test-only entry points compiled ONLY into the
// worktreepool test binary (Go's `go test` includes *_test.go files in
// the package's test build). Production binaries do not see these
// symbols, so doctrine "no test-only methods on production types" holds
// while still letting external _test packages (worktreepool_test) reach
// internal pool state deterministically.
//
// Forward-looking: when more test-only entry points accumulate (B-4..B-9)
// they consolidate here. inv-zen-105 (added by Plan 5 self-review) will
// add a static check that no production build artefact references these
// symbols.

func WarmCountForTest(p Pool) int {
	cp, ok := p.(*concretePool)
	if !ok {
		return -1
	}
	cp.mu.Lock()
	defer cp.mu.Unlock()
	return len(cp.warm)
}

// WarmPathsForTest returns a snapshot of the on-disk paths held by the
// warm slice at call time. Used by Layer-B GC tests that need to
// materialise warm dirs on disk WITHOUT racing the prewarm goroutine's
// nextID allocation: hardcoding "p-seed-1" only works if prewarm has not
// yet incremented nextID, which is non-deterministic. Reading the actual
// paths after Seed/Lease lets the test stay correct regardless of which
// goroutine called nextID.Add first.
//
// Returns nil if p is not *concretePool. Caller MUST NOT mutate the
// returned slice; entries are pointer-stable copies of cp.warm[i].path.
func WarmPathsForTest(p Pool) []string {
	cp, ok := p.(*concretePool)
	if !ok {
		return nil
	}
	cp.mu.Lock()
	defer cp.mu.Unlock()
	out := make([]string, len(cp.warm))
	for i, w := range cp.warm {
		out[i] = w.path
	}
	return out
}

func SeedWarmForTest(p Pool, n int) error {
	cp, ok := p.(*concretePool)
	if !ok {
		return errors.New("SeedWarmForTest: pool is not *concretePool")
	}
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closed.Load() {
		return errors.New("SeedWarmForTest: pool already closed")
	}
	for i := 0; i < n; i++ {
		id := cp.nextID.Add(1)
		w := &Worktree{
			id:   id,
			path: filepath.Join(cp.cfg.WorktreeDir, fmt.Sprintf("%s-seed-%d", cp.cfg.PoolID, id)),

			branch:    fmt.Sprintf("zen-pool-%s-%d", cp.cfg.PoolID, id),
			createdAt: cp.clk.Now(),
		}
		cp.warm = append(cp.warm, w)
		cp.total.Add(1)
	}
	return nil
}
