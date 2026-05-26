// SPDX-License-Identifier: MIT
// internal/daemon/mcpgateway/rbac.go
//
// RBAC — three-layer authorization chain (doctrine filter → per-tool ACL
// → concurrency gate). Every CallRequest passes through Check() before
// the Dispatcher routes; allow returns a release func the caller defers.
//
// Layer 1 — Doctrine filter:
//
//	capa-firewall denies a configurable disabled-tool set; max-scope +
//	default allow all registered tools. The disabled set is wired by
//	main.go from doctrine TOML (Plan 8 [doctrine.gateway.disabled_tools]).
//
// Layer 2 — Per-tool ACL:
//
//	default-deny on tools not in the registry; default-allow on
//	registered tools. Future plans extend with explicit operator-pinned
//	allow/deny lists; Phase A is registry-driven.
//
// Layer 3 — Concurrency gate:
//
//	per-doctrine ceiling (Q8=C; max-scope=20, default=10, capa-firewall=5;
//	queue depth 50). sync.Cond signals queued waiters on release.
//
// The release function returned by Check MUST be called exactly once. The
// Dispatcher (A-5) wraps each Handler call with `defer release()` so the
// slot is freed even on Handler panic.
package mcpgateway

import (
	"context"
	"fmt"
	"sync"
)

type RBACConfig struct {
	DoctrineDisabled map[Doctrine][]string
}

type RBAC struct {
	registry *ToolRegistry
	cfg      RBACConfig

	gateMu     sync.Mutex
	gateCond   *sync.Cond
	current    map[Doctrine]int
	queued     map[Doctrine]int
	totalIn    int
	totalQueue int
}

func NewRBAC(reg *ToolRegistry, cfg RBACConfig) *RBAC {
	r := &RBAC{
		registry: reg,
		cfg:      cfg,
		current:  make(map[Doctrine]int),
		queued:   make(map[Doctrine]int),
	}
	r.gateCond = sync.NewCond(&r.gateMu)
	return r
}

// Check runs the three-layer authorization chain. On allow, returns a
// release func the caller MUST defer to free the concurrency slot.
//
//	Layer 1: doctrine filter (capa-firewall disabled set)
//	Layer 2: registry membership (default-deny on unknown)
//	Layer 3: concurrency gate (per-doctrine ceiling + queue)
//
// Returned errors:
//   - ErrRBACDenied wrapping "doctrine" — Layer 1 deny
//   - ErrRBACDenied wrapping "acl"      — Layer 2 deny
//   - ErrConcurrencyLimit               — Layer 3 deny (queue full)
//   - ctx.Err()                         — context cancelled while queued
func (r *RBAC) Check(ctx context.Context, req CallRequest) (release func(), err error) {
	doctrine := req.Doctrine.Resolved()

	if r.cfg.DoctrineDisabled != nil {
		if disabled := r.cfg.DoctrineDisabled[doctrine]; disabled != nil {
			name := req.Tool.String()
			for _, d := range disabled {
				if d == name {
					return nil, fmt.Errorf("%w: doctrine=%s denies %s",
						ErrRBACDenied, doctrine, name)
				}
			}
		}
	}

	if !r.registry.Has(req.Tool) {
		return nil, fmt.Errorf("%w: acl unknown tool %s",
			ErrRBACDenied, req.Tool.String())
	}

	if err := r.acquire(ctx, doctrine); err != nil {
		return nil, err
	}
	return func() { r.release(doctrine) }, nil
}

func (r *RBAC) acquire(ctx context.Context, d Doctrine) error {
	r.gateMu.Lock()
	defer r.gateMu.Unlock()
	max := d.MaxConcurrent()
	for r.current[d] >= max {

		if r.queued[d] >= queueDepth {
			return fmt.Errorf("%w: doctrine=%s in-flight=%d queued=%d",
				ErrConcurrencyLimit, d, r.current[d], r.queued[d])
		}

		r.queued[d]++
		r.totalQueue++
		ctxDone := ctx.Done()
		watchDone := make(chan struct{})

		if ctxDone != nil {
			go func() {
				select {
				case <-ctxDone:
					r.gateMu.Lock()
					r.gateCond.Broadcast()
					r.gateMu.Unlock()
				case <-watchDone:
				}
			}()
		}
		r.gateCond.Wait()
		close(watchDone)
		r.queued[d]--
		r.totalQueue--

		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

	}
	r.current[d]++
	r.totalIn++
	return nil
}

func (r *RBAC) release(d Doctrine) {
	r.gateMu.Lock()
	if r.current[d] > 0 {
		r.current[d]--
		r.totalIn--
	}
	r.gateMu.Unlock()
	r.gateCond.Broadcast()
}

func (r *RBAC) Stat() (current, queued int) {
	r.gateMu.Lock()
	defer r.gateMu.Unlock()
	return r.totalIn, r.totalQueue
}

func (r *RBAC) StatPerDoctrine() (current, queued map[Doctrine]int) {
	r.gateMu.Lock()
	defer r.gateMu.Unlock()
	current = make(map[Doctrine]int, len(r.current))
	queued = make(map[Doctrine]int, len(r.queued))
	for k, v := range r.current {
		current[k] = v
	}
	for k, v := range r.queued {
		queued[k] = v
	}
	return current, queued
}
