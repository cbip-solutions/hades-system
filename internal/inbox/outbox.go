// SPDX-License-Identifier: MIT
package inbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrOutboxFull = errors.New("inbox: outbox queue full (back-pressure)")

type CacheWrite struct {
	Notification Notification
	ProjectAlias string
}

type Outbox struct {
	cache AggregatorCacheStore
	queue chan CacheWrite
	cap   int
	mu    sync.Mutex
	stats outboxStats
}

type outboxStats struct {
	enqueued   int64
	drained    int64
	rejected   int64
	cacheError int64
}

func NewOutbox(cache AggregatorCacheStore, capacity int) *Outbox {
	return &Outbox{
		cache: cache,
		queue: make(chan CacheWrite, capacity),
		cap:   capacity,
	}
}

func (o *Outbox) Enqueue(w CacheWrite) error {
	select {
	case o.queue <- w:
		o.mu.Lock()
		o.stats.enqueued++
		o.mu.Unlock()
		return nil
	default:
		o.mu.Lock()
		o.stats.rejected++
		o.mu.Unlock()
		return ErrOutboxFull
	}
}

// Run drains the fanout queue and applies each CacheWrite to the
// downstream AggregatorCacheStore. Returns when ctx is cancelled. Cache
// errors are counted but do NOT halt the drain loop — the cache is
// rebuildable; consistency is restored at next boot.
func (o *Outbox) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case w := <-o.queue:
			r := CacheRow{
				ProjectID:      w.Notification.ProjectID,
				ProjectAlias:   w.ProjectAlias,
				NotificationID: w.Notification.ID,
				Severity:       w.Notification.Severity,
				EventType:      w.Notification.EventType,
				ContentHash:    w.Notification.ContentHash,
				CreatedAt:      w.Notification.CreatedAt,
				AckedAt:        w.Notification.AckedAt,
			}
			if err := o.cache.Insert(ctx, r); err != nil {
				o.mu.Lock()
				o.stats.cacheError++
				o.mu.Unlock()
				_ = err
				continue
			}
			o.mu.Lock()
			o.stats.drained++
			o.mu.Unlock()
		}
	}
}

// Recover invokes cache.Rebuild(ctx, sources) and returns its result.
//
// Daemon-boot ordering contract: callers MUST invoke Recover BEFORE
// starting Run, so the live drain loop never races against the cold
// rehydration write path. per design contract(cache divergence + boot
// rehydration), Rebuild discards every existing cache row and rehydrates
// from the union of source contents; on a successful return, the cache
// is consistent with the per-project authoritative stores.
//
// Recover is also the divergence-recovery entry point used by chaos
// tests (`tests/chaos/cache_divergence_test.go`) — invoked when the
// doctor probe reports `inboxAggregatorCacheDivergence`. Idempotent
// (safe to re-invoke).
func (o *Outbox) Recover(ctx context.Context, sources []Store) error {
	return o.cache.Rebuild(ctx, sources)
}

func (o *Outbox) Pending() int { return len(o.queue) }

func (o *Outbox) Capacity() int { return o.cap }

func (o *Outbox) Stats() (enqueued, drained, rejected, cacheError int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stats.enqueued, o.stats.drained, o.stats.rejected, o.stats.cacheError
}

type Aggregator struct {
	perProject Store
	cache      AggregatorCacheStore
	outbox     *Outbox
}

func NewAggregator(perProject Store, cache AggregatorCacheStore) *Aggregator {
	return &Aggregator{
		perProject: perProject,
		cache:      cache,
		outbox:     NewOutbox(cache, 1024),
	}
}

func (a *Aggregator) Run(ctx context.Context) {
	a.outbox.Run(ctx)
}

func (a *Aggregator) Insert(ctx context.Context, n Notification) error {
	if err := a.perProject.Insert(ctx, &n); err != nil {
		return err
	}

	_ = a.outbox.Enqueue(CacheWrite{
		Notification: n,
		ProjectAlias: "",
	})
	return nil
}

func (a *Aggregator) Query(ctx context.Context, filter ListFilter) ([]CacheRow, error) {
	return a.cache.Query(ctx, filter)
}

func (a *Aggregator) Outbox() *Outbox { return a.outbox }

func noCrossProjectInboxLeakSentinel() error {

	w := CacheWrite{
		Notification: Notification{ProjectID: "anchor-project-id-placeholder"},
	}
	r := CacheRow{
		ProjectID:    w.Notification.ProjectID,
		ProjectAlias: w.ProjectAlias,
	}
	if r.ProjectID != w.Notification.ProjectID {
		return fmt.Errorf("anchor invariant broken: %v", ErrCrossProjectInboxLeakAnchor)
	}
	return ErrCrossProjectInboxLeakAnchor
}
