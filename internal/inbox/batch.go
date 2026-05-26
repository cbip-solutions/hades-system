// SPDX-License-Identifier: MIT
package inbox

import (
	"sync"
	"time"
)

type BatchWindow struct {
	Severity Severity
	Idle     time.Duration
	MaxCount int
	MaxWait  time.Duration
}

func DefaultBatchWindow(s Severity) BatchWindow {
	switch s {
	case SeverityActionNeeded:
		return BatchWindow{
			Severity: s,
			Idle:     30 * time.Second,
			MaxCount: 10,
			MaxWait:  5 * time.Minute,
		}
	case SeverityInfoDigest:
		return BatchWindow{
			Severity: s,
			Idle:     5 * time.Minute,
			MaxCount: 50,
			MaxWait:  30 * time.Minute,
		}
	default:

		return BatchWindow{
			Severity: s,
			Idle:     0,
			MaxCount: 1,
			MaxWait:  0,
		}
	}
}

type Batcher struct {
	window     BatchWindow
	pending    []Notification
	firstAddAt time.Time
	lastAddAt  time.Time
}

func NewBatcher(s Severity) *Batcher {
	return &Batcher{window: DefaultBatchWindow(s)}
}

func NewBatcherFromWindow(w BatchWindow) *Batcher {
	return &Batcher{window: w}
}

func (b *Batcher) Add(n Notification) {
	if len(b.pending) == 0 {
		b.firstAddAt = n.CreatedAt
	}
	b.lastAddAt = n.CreatedAt
	b.pending = append(b.pending, n)
}

func (b *Batcher) ReadyToEmit(now time.Time) []Notification {
	if len(b.pending) == 0 {
		return nil
	}

	idleReached := b.window.Idle == 0 || now.Sub(b.lastAddAt) >= b.window.Idle
	countReached := len(b.pending) >= b.window.MaxCount
	maxWaitReached := b.window.MaxWait > 0 && now.Sub(b.firstAddAt) >= b.window.MaxWait

	if !idleReached && !countReached && !maxWaitReached {
		return nil
	}

	out := b.pending
	b.pending = nil
	b.firstAddAt = time.Time{}
	b.lastAddAt = time.Time{}
	return out
}

type BatchManager struct {
	mu       sync.Mutex
	batchers map[batchKey]*Batcher
}

type batchKey struct {
	projectID string
	severity  Severity
}

func NewBatchManager() *BatchManager {
	return &BatchManager{batchers: make(map[batchKey]*Batcher)}
}

func (m *BatchManager) Add(n Notification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := batchKey{projectID: n.ProjectID, severity: n.Severity}
	b, ok := m.batchers[k]
	if !ok {
		b = NewBatcher(n.Severity)
		m.batchers[k] = b
	}
	b.Add(n)
}

func (m *BatchManager) Tick(now time.Time) []Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Notification
	for _, b := range m.batchers {
		if batch := b.ReadyToEmit(now); len(batch) > 0 {
			out = append(out, batch...)
		}
	}
	return out
}

func (m *BatchManager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.batchers {
		n += len(b.pending)
	}
	return n
}
