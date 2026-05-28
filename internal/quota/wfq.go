// SPDX-License-Identifier: MIT
package quota

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type Weight float64

const minWeight Weight = 0.01

type WorkItem struct {
	ID string

	ProjectAlias string

	EnqueuedAt time.Time

	Cost float64
}

type virtualItem struct {
	work WorkItem

	vft float64
}

type projectQueue struct {
	weight Weight

	items []virtualItem

	lastVFT float64
}

type WfqQueue struct {
	mu sync.Mutex

	queues map[string]*projectQueue

	virtualClock float64

	defaultWeight Weight
}

func NewWfqQueue(weights map[string]Weight) *WfqQueue {
	q := &WfqQueue{
		queues:        make(map[string]*projectQueue),
		defaultWeight: 1.0,
	}
	for alias, w := range weights {
		if w <= 0 {
			w = minWeight
		}
		q.queues[alias] = &projectQueue{weight: w}
	}
	return q
}

func (q *WfqQueue) Enqueue(projectAlias string, work WorkItem) error {
	if work.ProjectAlias != projectAlias {
		return fmt.Errorf("quota: WorkItem.ProjectAlias %q != Enqueue alias %q",
			work.ProjectAlias, projectAlias)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	pq, ok := q.queues[projectAlias]
	if !ok {

		pq = &projectQueue{weight: q.defaultWeight}
		q.queues[projectAlias] = pq
	}
	cost := work.Cost
	if cost <= 0 {
		cost = 1
	}
	start := q.virtualClock
	if pq.lastVFT > start {
		start = pq.lastVFT
	}
	vft := start + cost/float64(pq.weight)
	pq.lastVFT = vft
	pq.items = append(pq.items, virtualItem{work: work, vft: vft})
	return nil
}

func (q *WfqQueue) TryDispatch() (WorkItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var (
		bestAlias string
		bestVFT   float64
		found     bool
	)
	for alias, pq := range q.queues {
		if len(pq.items) == 0 {
			continue
		}
		if !found || pq.items[0].vft < bestVFT {
			bestAlias = alias
			bestVFT = pq.items[0].vft
			found = true
		}
	}
	if !found {
		return WorkItem{}, false
	}
	pq := q.queues[bestAlias]
	item := pq.items[0]
	pq.items = pq.items[1:]
	if bestVFT > q.virtualClock {
		q.virtualClock = bestVFT
	}
	return item.work, true
}

func (q *WfqQueue) Depth(projectAlias string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	pq, ok := q.queues[projectAlias]
	if !ok {
		return 0
	}
	return len(pq.items)
}

func (q *WfqQueue) SetWeight(projectAlias string, w Weight) {
	if w <= 0 {
		w = minWeight
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	pq, ok := q.queues[projectAlias]
	if !ok {
		q.queues[projectAlias] = &projectQueue{weight: w}
		return
	}
	pq.weight = w
}

func (q *WfqQueue) Weight(projectAlias string) (Weight, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	pq, ok := q.queues[projectAlias]
	if !ok {
		return 0, false
	}
	return pq.weight, true
}

var ErrWfqWeightedFairAnchor = errors.New("quota: wfq weighted fair anchor (invariant)")
