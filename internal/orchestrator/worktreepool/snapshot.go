// SPDX-License-Identifier: MIT
package worktreepool

type Snapshot struct {
	Floor                int
	ElasticMax           int
	Warm                 int
	Leased               int
	Total                int
	Closed               bool
	BackgroundGoroutines int
}

// SnapshotOf returns counters for pools created by NewPool. It leaves the
// Pool interface unchanged so narrow test fakes and consumers are not forced
// to grow read-only methods they do not need.
func SnapshotOf(p Pool) (Snapshot, bool) {
	cp, ok := p.(*concretePool)
	if !ok || cp == nil {
		return Snapshot{}, false
	}

	cp.mu.Lock()
	defer cp.mu.Unlock()

	closed := cp.closed.Load()
	goroutines := 0
	if !closed {
		goroutines = 2
	}

	return Snapshot{
		Floor:                cp.cfg.Floor,
		ElasticMax:           cp.cfg.ElasticMax,
		Warm:                 len(cp.warm),
		Leased:               len(cp.leased),
		Total:                int(cp.total.Load()),
		Closed:               closed,
		BackgroundGoroutines: goroutines,
	}, true
}
