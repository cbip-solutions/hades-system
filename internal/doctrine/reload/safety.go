// SPDX-License-Identifier: MIT
package reload

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type failureRecord struct {
	mu            sync.Mutex
	failures      []time.Time
	suppressUntil time.Time
}

func (r *failureRecord) recordFailureAt(t time.Time, window time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := t.Add(-window)
	keep := r.failures[:0]
	for _, ft := range r.failures {
		if ft.After(cutoff) {
			keep = append(keep, ft)
		}
	}
	keep = append(keep, t)
	r.failures = keep
	return len(r.failures)
}

func (r *failureRecord) markSuppressUntil(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suppressUntil = t
}

func (r *failureRecord) isSuppressed(t time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return t.Before(r.suppressUntil)
}

func (r *failureRecord) snapshotCount(t time.Time, window time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := t.Add(-window)
	c := 0
	for _, ft := range r.failures {
		if ft.After(cutoff) {
			c++
		}
	}
	return c
}

func (r *failureRecord) snapshotSuppressUntil() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.suppressUntil
}

func (r *failureRecord) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = nil
	r.suppressUntil = time.Time{}
}

func (w *Watcher) loadOrCreateFailureRecord(path string) *failureRecord {
	if v, ok := w.failureCounter.Load(path); ok {
		return v.(*failureRecord)
	}
	r := &failureRecord{}
	if actual, loaded := w.failureCounter.LoadOrStore(path, r); loaded {
		return actual.(*failureRecord)
	}
	return r
}

func (w *Watcher) recordFailure(path string) {
	r := w.loadOrCreateFailureRecord(path)
	now := w.clock.Now()
	count := r.recordFailureAt(now, w.stormWindow)
	if count >= w.stormThreshold {
		r.markSuppressUntil(now.Add(w.stormCooldown))
	}
}

func (w *Watcher) clearFailures(path string) {
	if v, ok := w.failureCounter.Load(path); ok {
		v.(*failureRecord).reset()
	}
}

func (w *Watcher) isStormSuppressed(path string) bool {
	v, ok := w.failureCounter.Load(path)
	if !ok {
		return false
	}
	return v.(*failureRecord).isSuppressed(w.clock.Now())
}

func (w *Watcher) failureCountSnapshot(path string) int {
	v, ok := w.failureCounter.Load(path)
	if !ok {
		return 0
	}
	return v.(*failureRecord).snapshotCount(w.clock.Now(), w.stormWindow)
}

func (w *Watcher) suppressedUntil(path string) time.Time {
	v, ok := w.failureCounter.Load(path)
	if !ok {
		return time.Time{}
	}
	return v.(*failureRecord).snapshotSuppressUntil()
}
