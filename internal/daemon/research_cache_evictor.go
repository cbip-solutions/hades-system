// SPDX-License-Identifier: MIT
// research_cache_evictor.go — background eviction of expired research_cache rows.
//
// schema/054_research_cache.sql + handlers/research_cache.go documented an
// eviction goroutine: every hour, DELETE FROM research_cache WHERE
// ttl_unix < unixepoch(). Without it, the handler-level TTL check hides
// expired rows from reads but never deletes them — research_cache grows
// unbounded over weeks of operation, slowing the partial index scan.
//
// Post-review C-3 fix: this file wires the goroutine that the schema +
// handler doc-comment promised. Server.Start() calls
// startResearchCacheEvictor; Server.Stop() cancels its context and waits
// on the returned done channel.

package daemon

import (
	"context"
	"errors"
	"log"
	"time"
)

const researchCacheEvictionInterval = 1 * time.Hour

type researchCacheEvictor interface {
	EvictResearchCacheExpired() (int64, error)
}

func startResearchCacheEvictor(ctx context.Context, e researchCacheEvictor, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = researchCacheEvictionInterval
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runEvictionSweep(ctx, e)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				runEvictionSweep(ctx, e)
			}
		}
	}()
	return done
}

func runEvictionSweep(ctx context.Context, e researchCacheEvictor) {
	if err := ctx.Err(); err != nil {
		return
	}
	if e == nil {
		return
	}
	n, err := e.EvictResearchCacheExpired()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("daemon: research_cache eviction: %v", err)
		return
	}
	if n > 0 {
		log.Printf("daemon: research_cache eviction: deleted %d expired rows", n)
	}
}

func (s *Server) EvictResearchCacheExpired() (int64, error) {
	if s.store == nil {
		return 0, nil
	}
	res, err := s.store.DB().Exec(
		`DELETE FROM research_cache WHERE ttl_unix < unixepoch()`,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}
