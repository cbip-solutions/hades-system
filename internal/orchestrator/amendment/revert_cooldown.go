// SPDX-License-Identifier: MIT
package amendment

import (
	"sync"
	"time"
)

type RevertCooldown struct {
	mu    sync.RWMutex
	state map[string]revertCooldownEntry
}

type revertCooldownEntry struct {
	when     time.Time
	cooldown time.Duration
}

func NewRevertCooldown() *RevertCooldown {
	return &RevertCooldown{state: map[string]revertCooldownEntry{}}
}

func (c *RevertCooldown) LastRevertedAt(rulePath string) (when time.Time, cooldown time.Duration, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, exists := c.state[rulePath]
	if !exists {
		return time.Time{}, 0, false
	}
	return e.when, e.cooldown, true
}

// MarkReverted records a successful revert at `when` with the cooldown
// duration in effect. Subsequent LastRevertedAt calls within
// (when, when+cooldown) MUST return ok=true so callers
// (TelemetrySubscriber) can suppress re-dispatch.
func (c *RevertCooldown) MarkReverted(rulePath string, when time.Time, cooldown time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state[rulePath] = revertCooldownEntry{when: when, cooldown: cooldown}
}

type SnapshotEntry struct {
	RulePath string
	When     time.Time
	Cooldown time.Duration
}

func (c *RevertCooldown) Snapshot() []SnapshotEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]SnapshotEntry, 0, len(c.state))
	for k, v := range c.state {
		out = append(out, SnapshotEntry{RulePath: k, When: v.when, Cooldown: v.cooldown})
	}
	return out
}

var _ CooldownTracker = (*RevertCooldown)(nil)
