// SPDX-License-Identifier: MIT
package amendment

import (
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

type CooldownRegistry struct {
	mu        sync.Mutex
	deadlines map[string]time.Time
	clk       clock.Clock
}

func NewCooldownRegistry(clk clock.Clock) *CooldownRegistry {
	if clk == nil {
		panic("amendment: nil Clock")
	}
	return &CooldownRegistry{deadlines: map[string]time.Time{}, clk: clk}
}

func CooldownWindowFor(doctrine string) time.Duration {
	switch doctrine {
	case "default":
		return 72 * time.Hour
	case "capa-firewall":
		return 168 * time.Hour
	case "max-scope":
		fallthrough
	default:
		return 24 * time.Hour
	}
}

func (r *CooldownRegistry) Arm(pattern, doctrine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deadlines[pattern] = r.clk.Now().Add(CooldownWindowFor(doctrine))
}

// Suppressed returns true iff pattern has an armed deadline strictly
// after clk.Now(). Expired deadlines are removed from the map (lazy
// GC) so long-running registries do not accumulate stale entries
// proportional to total arm count.
func (r *CooldownRegistry) Suppressed(pattern string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deadlines[pattern]
	if !ok {
		return false
	}
	if r.clk.Now().Before(d) {
		return true
	}
	delete(r.deadlines, pattern)
	return false
}
