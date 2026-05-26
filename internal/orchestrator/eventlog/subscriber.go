// SPDX-License-Identifier: MIT
package eventlog

import (
	"sync"
)

type Filter struct {
	Types     []EventType
	ProjectID string
}

// DefaultBufferSize is the recommended channel buffer size for typical
// callers (Phase H reviewers, Phase K amendment proposer, Phase M drift
// detector). It balances burst tolerance (small bursts ride out without
// drop-oldest activation) against memory footprint (~2 KiB per
// subscription at sizeof(Record) ≈ 32 bytes plus payload pointer).
//
// Range guidance (N-3): pick 16–32 for low-volume filtered subscriptions
// (single EventType narrowing); pick 64 for unfiltered debug consumers;
// pick 256+ only if a consumer is known to do bursty multi-ms work per
// event. Larger buffers do NOT increase delivery guarantees — drop-oldest
// always wins under sustained overrun.
const DefaultBufferSize = 64

// match reports whether r satisfies the filter.
//
// Invariant a zero-value Filter matches every record (no narrowing).
// Pre r is the Record produced by Log.Append (Payload may be a shared
// []byte; Filter MUST NOT mutate it).
//
// Complexity (N-1): O(|Types|) linear scan over the Types slice. Intentional
// — typical |Types| ≤ 10 (Phase H reviewer, Phase K amendment proposer
// each subscribe to ≤ 5 EventTypes), and a 10-element linear scan beats a
// map lookup on cache-friendliness for these sizes. If a future caller
// needs |Types| > 32, replace with a precomputed bitset over EventType.
func (f Filter) match(r Record) bool {
	if f.ProjectID != "" && r.ProjectID != f.ProjectID {
		return false
	}
	if len(f.Types) == 0 {
		return true
	}
	for _, t := range f.Types {
		if t == r.EventType {
			return true
		}
	}
	return false
}

type Subscription interface {
	Events() <-chan Record
	Done() <-chan struct{}
	Close()
}

type subscriber struct {
	filter Filter
	ch     chan Record
	closed chan struct{}
	once   sync.Once
}

func (s *subscriber) Events() <-chan Record { return s.ch }

func (s *subscriber) Done() <-chan struct{} { return s.closed }

func (s *subscriber) Close() {
	s.once.Do(func() {
		close(s.closed)
	})
}

type subscriberHub struct {
	mu   sync.RWMutex
	subs []*subscriber
}

func newSubscriberHub() *subscriberHub { return &subscriberHub{} }

func (h *subscriberHub) add(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs = append(h.subs, s)
}

// publish fans r out to every subscriber whose Filter matches.
//
// Algorithm
//  1. Snapshot the subs slice under RLock; release the lock before any
//     channel work to keep contention bounded.
//  2. For each subscriber: check filter, then attempt delivery via a
//     single select that watches s.closed alongside the s.ch <- r send
//     case. This atomically handles the publish/Close race:
//     - If s.ch has buffer space and s.closed is open: deliver.
//     - If s.closed has been closed by Close(): skip and mark for prune.
//     - If buffer is full and s.closed is open: fall through to
//     drop-oldest path.
//  3. Drop-oldest path: drain ONE oldest record non-blocking, then retry
//     the send (still under the s.closed-watching select). If the channel
//     re-fills in the race window, drop the new record — spec §1 Q5 C:
//     publisher MUST NOT block.
//  4. Closed subscribers are pruned in a final write-lock pass.
//
// C-1 fix: pre-fix, the closed-check was a separate select before the
// send select, leaving an atomicity gap where Close() could close s.ch
// between the two. The fix folds the closed check INTO every send select
// — if Close() fires mid-publish, the <-s.closed case wins and we never
// touch s.ch. Combined with Close() no longer closing s.ch (signal-only),
// this eliminates "send on closed channel" panics under stress.
//
// Note this method does NOT mutate r.Payload — subscribers receive the
// same shared []byte reference that Log.Append produced (Task A-3 N-2
// contract: Record.Payload is read-only).
func (h *subscriberHub) publish(r Record) {
	h.mu.RLock()
	subs := make([]*subscriber, len(h.subs))
	copy(subs, h.subs)
	h.mu.RUnlock()

	live := make([]*subscriber, 0, len(subs))
	pruned := false
	for _, s := range subs {

		select {
		case <-s.closed:
			pruned = true
			continue
		default:
		}
		live = append(live, s)
		if !s.filter.match(r) {
			continue
		}

		select {
		case s.ch <- r:

		case <-s.closed:

			pruned = true
		default:

			select {
			case <-s.ch:
			case <-s.closed:
				pruned = true
				continue
			default:

			}
			select {
			case s.ch <- r:
			case <-s.closed:
				pruned = true
			default:
				// Still full (rare race with a publisher-side burst into
				// a freshly-vacated slot). Drop the new record rather
				// than block — spec Q5 C: publisher MUST NOT block.
			}
		}
	}
	if pruned {

		h.mu.Lock()
		h.subs = live
		h.mu.Unlock()
	}
}

func (l *Log) Subscribe(filter Filter, bufferSize int) Subscription {
	if bufferSize < 1 {
		panic("eventlog.Subscribe: bufferSize must be >= 1")
	}
	s := &subscriber{
		filter: filter,
		ch:     make(chan Record, bufferSize),
		closed: make(chan struct{}),
	}
	l.subs.add(s)
	return s
}
