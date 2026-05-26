// SPDX-License-Identifier: MIT
package worktreepool

import (
	"context"
	"errors"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const prewarmTickDefault = 200 * time.Millisecond

const prewarmBackoffMin = 200 * time.Millisecond

var prewarmBackoffMax = 30 * time.Second

func (p *concretePool) prewarmLoop() {
	defer close(p.prewarmDone)
	backoff := prewarmBackoffMin
	tick := p.clk.NewTicker(prewarmTickDefault)
	defer tick.Stop()

	for {

		shouldSpawn := p.warmShouldSpawn()
		if shouldSpawn {
			w, err := p.spawnOne(p.prewarmCtx)
			if err != nil {

				p.total.Add(-1)

				if errors.Is(err, ErrPoolDegraded) {
					_, _ = p.emitter.Append(p.prewarmCtx, eventlog.Event{
						Type: eventlog.EvtWorktreePoolDegraded,
						Payload: map[string]any{
							"reason":      degradedReason(err),
							"source":      "prewarm",
							"doctrine":    p.cfg.Doctrine,
							"pool_id":     p.cfg.PoolID,
							"elastic_max": p.cfg.ElasticMax,
							"error":       err.Error(),
						},
					})
				}
				// Sleep the backoff window OR exit on ctx.Done. We MUST
				// honor ctx so Close does not block on a 30s sleep.
				if !p.prewarmSleep(backoff) {
					p.drainWarmOnClose()
					return
				}

				backoff *= 2
				if backoff > prewarmBackoffMax {
					backoff = prewarmBackoffMax
				}
				continue
			}

			backoff = prewarmBackoffMin
			p.appendWarmAndSignal(w)
			continue
		}

		select {
		case <-tick.C():
		case <-p.prewarmCtx.Done():
			p.drainWarmOnClose()
			return
		}
	}
}

// warmShouldSpawn returns true if prewarm should call spawnOne now AND
// reserves an elastic slot atomically (p.total.Add(1)) so concurrent
// observations cannot both decide to spawn past ElasticMax. Caller MUST
// undo the reservation via p.total.Add(-1) on spawn failure.
//
// Returns false when:
//   - pool is closed (Close fired; prewarm should idle until ctx.Done)
//   - len(warm) >= Floor (no deficit to fill)
//   - total >= ElasticMax (elastic ceiling reached; Lease saturation
//     drives recovery instead via Release-spawn replacement, not
//     prewarm — prewarm must respect the M ceiling exactly)
func (p *concretePool) warmShouldSpawn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return false
	}
	if len(p.warm) >= p.cfg.Floor {
		return false
	}
	if int(p.total.Load()) >= p.cfg.ElasticMax {
		return false
	}

	p.total.Add(1)
	return true
}

func (p *concretePool) appendWarmAndSignal(w *Worktree) {
	p.mu.Lock()
	if p.closed.Load() {

		p.total.Add(-1)
		p.mu.Unlock()
		return
	}
	p.warm = append(p.warm, w)
	ch := p.signalSlot
	p.mu.Unlock()

	select {
	case ch <- struct{}{}:
	default:
	}
}

// prewarmSleep blocks for d OR until p.prewarmCtx fires, whichever comes
// first. Returns true if the full duration elapsed; false if ctx.Done
// fired (the caller MUST exit the prewarm loop via drainWarmOnClose +
// return). Uses the Phase A Clock seam so future *clock.Fake-driven
// tests can drive backoff windows deterministically.
func (p *concretePool) prewarmSleep(d time.Duration) bool {
	timer := p.clk.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C():
		return true
	case <-p.prewarmCtx.Done():
		return false
	}
}

func (p *concretePool) drainWarmOnClose() {
	p.mu.Lock()
	warm := p.warm
	p.warm = nil
	p.mu.Unlock()
	for _, w := range warm {
		_ = gitWorktreeRemove(context.Background(), p.exec, p.cfg.RepoRoot, w.path)
		p.total.Add(-1)
	}
}
