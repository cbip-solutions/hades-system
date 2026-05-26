// SPDX-License-Identifier: MIT
package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type BatcherConfig struct {
	MaxBatch      int
	FlushInterval time.Duration
}

type Batcher struct {
	store *store.Store
	cfg   BatcherConfig
	ch    chan store.EventRow
	mu    sync.Mutex
}

func NewBatcher(s *store.Store, cfg BatcherConfig) *Batcher {
	if cfg.MaxBatch == 0 {
		cfg.MaxBatch = 1000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 100 * time.Millisecond
	}
	return &Batcher{
		store: s,
		cfg:   cfg,
		ch:    make(chan store.EventRow, cfg.MaxBatch*2),
	}
}

func (b *Batcher) Submit(ev store.EventRow) {
	b.ch <- ev
}

func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]store.EventRow, 0, b.cfg.MaxBatch)
	for {
		select {
		case <-ctx.Done():
			b.drain(&batch)
			b.flush(batch)
			return
		case ev := <-b.ch:
			batch = append(batch, ev)
			if len(batch) >= b.cfg.MaxBatch {
				b.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				b.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (b *Batcher) drain(batch *[]store.EventRow) {
	for {
		select {
		case ev := <-b.ch:
			*batch = append(*batch, ev)
		default:
			return
		}
	}
}

func (b *Batcher) flush(batch []store.EventRow) {
	if len(batch) == 0 {
		return
	}
	if _, err := b.store.InsertEventsBatch(batch); err != nil {
		slog.Error("batcher: flush failed", "err", err, "size", len(batch))
	}
}

func (b *Batcher) QueueDepth() int { return len(b.ch) }
