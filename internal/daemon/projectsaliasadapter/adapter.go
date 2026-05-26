// SPDX-License-Identifier: MIT
// Package projectsaliasadapter bridges *store.Store → mcpgateway.ProjectsAliasResolver.
//
// This package exists outside internal/daemon/mcpgateway on purpose:
// inv-zen-031 forbids the mcpgateway package from importing internal/store
// directly. The adapter is the explicit boundary-crosser that owns the
// SQL queries against projects_alias and exposes a single-method
// interface (mcpgateway.ProjectsAliasResolver) to the gateway.
//
// Adapter pattern mirror: this file mirrors internal/daemon/projectctxadapter
// (the Plan 7 reference). Both packages satisfy a single interface owned
// by their consumer (projectctx.ProjectStore / mcpgateway.ProjectsAliasResolver),
// both wrap *store.Store, both translate to/from the store row types.
// Adopting the same idiom keeps the daemon-side adapter surface uniform.
//
// Concurrency the daemon's mcpgateway handler serves HTTP concurrently.
// The cache is guarded by a single mutex; reads + writes both take the
// lock briefly (cache is small — one entry per active project per TTL
// window). For a daemon with ≤100 active projects + 60-second TTL the
// uncontended lock cost is dominated by the DB round-trip on miss, not
// the mutex itself.
//
// LRU semantics: the current implementation is a flat map with TTL-based
// eviction (no size-bound). Operator projects rarely exceed 10-20 at a
// time; a 60-second TTL caps the working set naturally. If the daemon
// ever needs to support >1000 projects, the map can be swapped for a
// container/list LRU without changing the public surface.
//
// inv-zen-277: alias resolver returns canonical id_sha256 OR ErrAliasNotFound.
// inv-zen-031: this package crosses the mcpgateway↔store boundary on
// mcpgateway's behalf (sanctioned bridge).
package projectsaliasadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/mcpgateway"
	"github.com/cbip-solutions/hades-system/internal/store"
)

// hexID matches the canonical id_sha256 shape: exactly 64 lowercase hex
// characters. Anchored to start + end so partial matches do not pass
// (e.g. an alias that happens to contain 64 hex chars in the middle
// would NOT match). The pattern aligns with the SQL column constraint
// `id_sha256 = ?` lookup: callers pass the canonical form, the adapter
// recognises it without a DB round-trip.
var hexID = regexp.MustCompile(`^[0-9a-f]{64}$`)

const defaultTTL = 60 * time.Second

type Adapter struct {
	s     *store.Store
	mu    sync.Mutex
	cache map[string]cachedEntry
	ttl   time.Duration
	now   func() time.Time
}

type cachedEntry struct {
	id string
	at time.Time
}

func New(s *store.Store) *Adapter {
	return NewWithTTL(s, defaultTTL)
}

func NewWithTTL(s *store.Store, ttl time.Duration) *Adapter {
	if s == nil {
		panic("projectsaliasadapter.New: store is nil")
	}
	return &Adapter{
		s:     s,
		cache: make(map[string]cachedEntry),
		ttl:   ttl,
		now:   time.Now,
	}
}

var _ mcpgateway.ProjectsAliasResolver = (*Adapter)(nil)

func (a *Adapter) Resolve(ctx context.Context, idOrAlias string) (string, error) {

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("projectsaliasadapter.Resolve: %w", err)
	}

	if hexID.MatchString(idOrAlias) {
		return idOrAlias, nil
	}

	a.mu.Lock()
	if e, ok := a.cache[idOrAlias]; ok && a.now().Sub(e.at) < a.ttl {
		a.mu.Unlock()
		return e.id, nil
	}
	a.mu.Unlock()

	var id string
	err := a.s.DB().QueryRowContext(ctx,
		`SELECT id_sha256
		   FROM projects_alias
		  WHERE (alias = ? OR id_sha256 = ?)
		    AND archived_at IS NULL`,
		idOrAlias, idOrAlias,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", mcpgateway.ErrAliasNotFound
	}
	if err != nil {
		return "", fmt.Errorf("projectsaliasadapter.Resolve: %w", err)
	}

	a.mu.Lock()
	a.cache[idOrAlias] = cachedEntry{id: id, at: a.now()}
	a.mu.Unlock()
	return id, nil
}

func (a *Adapter) Invalidate(idOrAlias string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.cache, idOrAlias)
}

func (a *Adapter) InvalidateAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cache = make(map[string]cachedEntry)
}
