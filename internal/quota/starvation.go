// SPDX-License-Identifier: MIT
package quota

import (
	"sort"
	"sync"
	"time"
)

const DefaultStarveDepthThreshold = 50

const DefaultStarveWindow = 1 * time.Hour

type StarvationStats struct {
	Enqueues       int64
	Dispatches     int64
	LastEnqueueAt  time.Time
	LastDispatchAt time.Time
}

type projectStarvationState struct {
	enqueues       int64
	dispatches     int64
	lastEnqueueAt  time.Time
	lastDispatchAt time.Time
}

type StarvationDetector struct {
	mu             sync.Mutex
	state          map[string]*projectStarvationState
	depthThreshold int
	window         time.Duration
	now            func() time.Time
}

func NewStarvationDetector(depthThreshold int, window time.Duration) *StarvationDetector {
	return NewStarvationDetectorWithClock(depthThreshold, window, time.Now)
}

func NewStarvationDetectorWithClock(depthThreshold int, window time.Duration, now func() time.Time) *StarvationDetector {
	if depthThreshold < 1 {
		depthThreshold = DefaultStarveDepthThreshold
	}
	if window <= 0 {
		window = DefaultStarveWindow
	}
	if now == nil {
		now = time.Now
	}
	return &StarvationDetector{
		state:          make(map[string]*projectStarvationState),
		depthThreshold: depthThreshold,
		window:         window,
		now:            now,
	}
}

func (d *StarvationDetector) RecordEnqueue(alias string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.getOrInitLocked(alias)
	st.enqueues++
	st.lastEnqueueAt = d.now()
}

func (d *StarvationDetector) RecordDispatch(alias string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.getOrInitLocked(alias)
	st.dispatches++
	st.lastDispatchAt = d.now()
}

func (d *StarvationDetector) Check(alias string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	st, ok := d.state[alias]
	if !ok {
		return false
	}
	depth := st.enqueues - st.dispatches
	if int(depth) < d.depthThreshold {
		return false
	}
	cutoff := d.now().Add(-d.window)
	if st.lastDispatchAt.IsZero() {

		return st.lastEnqueueAt.Before(cutoff) || st.lastEnqueueAt.Equal(cutoff)
	}
	return st.lastDispatchAt.Before(cutoff)
}

func (d *StarvationDetector) Stats(alias string) StarvationStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	st, ok := d.state[alias]
	if !ok {
		return StarvationStats{}
	}
	return StarvationStats{
		Enqueues:       st.enqueues,
		Dispatches:     st.dispatches,
		LastEnqueueAt:  st.lastEnqueueAt,
		LastDispatchAt: st.lastDispatchAt,
	}
}

func (d *StarvationDetector) ListStarving() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	cutoff := d.now().Add(-d.window)
	for alias, st := range d.state {
		depth := st.enqueues - st.dispatches
		if int(depth) < d.depthThreshold {
			continue
		}
		if st.lastDispatchAt.IsZero() {
			if st.lastEnqueueAt.After(cutoff) {

				continue
			}
			out = append(out, alias)
			continue
		}
		if st.lastDispatchAt.Before(cutoff) {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

func (d *StarvationDetector) getOrInitLocked(alias string) *projectStarvationState {
	if st, ok := d.state[alias]; ok {
		return st
	}
	st := &projectStarvationState{}
	d.state[alias] = st
	return st
}
