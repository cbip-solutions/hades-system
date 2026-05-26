//go:build chaos

package chaos

import (
	"fmt"
	"hash/crc32"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/quota"
)

func TestChaos_ConcurrentDispatchStarvation_BalancedNoStarve(t *testing.T) {
	rng := rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(t.Name())))))
	_ = rng

	const N = 10
	weights := map[string]quota.Weight{}
	aliases := make([]string, N)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("proj-%02d", i)
		weights[aliases[i]] = 1.0
	}

	q := quota.NewWfqQueue(weights)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}
	det := quota.NewStarvationDetectorWithClock(
		quota.DefaultStarveDepthThreshold,
		quota.DefaultStarveWindow,
		clk.Now,
	)

	for i := 0; i < 1000; i++ {
		w := quota.WorkItem{
			ID:           fmt.Sprintf("noisy-%d", i),
			ProjectAlias: aliases[0],
			EnqueuedAt:   clk.Now(),
			Cost:         1.0,
		}
		if err := q.Enqueue(aliases[0], w); err != nil {
			t.Fatalf("Enqueue noisy %d: %v", i, err)
		}
		det.RecordEnqueue(aliases[0])
	}
	for j := 1; j < N; j++ {
		for k := 0; k < 100; k++ {
			w := quota.WorkItem{
				ID:           fmt.Sprintf("quiet-%s-%d", aliases[j], k),
				ProjectAlias: aliases[j],
				EnqueuedAt:   clk.Now(),
				Cost:         1.0,
			}
			if err := q.Enqueue(aliases[j], w); err != nil {
				t.Fatalf("Enqueue quiet %s/%d: %v", aliases[j], k, err)
			}
			det.RecordEnqueue(aliases[j])
		}
	}

	dispatched := map[string]int{}
	for {
		w, ok := q.TryDispatch()
		if !ok {
			break
		}
		dispatched[w.ProjectAlias]++
		det.RecordDispatch(w.ProjectAlias)
		clk.advance(time.Millisecond)
	}

	for _, a := range aliases {
		if c := dispatched[a]; c < 50 {
			t.Fatalf("project %s under-dispatched: %d (want >=50)", a, c)
		}
	}

	if starving := det.ListStarving(); len(starving) != 0 {
		t.Fatalf("expected 0 starving projects on balanced load, got %d (%v)",
			len(starving), starving)
	}
}

func TestChaos_ConcurrentDispatchStarvation_MisconfiguredWeightsDetect(t *testing.T) {
	const N = 10
	weights := map[string]quota.Weight{}
	aliases := make([]string, N)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("p-%02d", i)
		if i == 0 {
			weights[aliases[i]] = 0.01
		} else {
			weights[aliases[i]] = 1.0
		}
	}

	q := quota.NewWfqQueue(weights)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: now}
	det := quota.NewStarvationDetectorWithClock(
		quota.DefaultStarveDepthThreshold,
		quota.DefaultStarveWindow,
		clk.Now,
	)

	for i := 0; i < 100; i++ {
		if err := q.Enqueue(aliases[0], quota.WorkItem{
			ID:           fmt.Sprintf("starved-%d", i),
			ProjectAlias: aliases[0],
			EnqueuedAt:   clk.Now(),
			Cost:         1.0,
		}); err != nil {
			t.Fatalf("Enqueue starved %d: %v", i, err)
		}
		det.RecordEnqueue(aliases[0])
	}
	for j := 1; j < N; j++ {
		for k := 0; k < 200; k++ {
			if err := q.Enqueue(aliases[j], quota.WorkItem{
				ID:           fmt.Sprintf("other-%s-%d", aliases[j], k),
				ProjectAlias: aliases[j],
				EnqueuedAt:   clk.Now(),
				Cost:         1.0,
			}); err != nil {
				t.Fatalf("Enqueue other %s/%d: %v", aliases[j], k, err)
			}
			det.RecordEnqueue(aliases[j])
		}
	}

	for i := 0; i < 500; i++ {
		w, ok := q.TryDispatch()
		if !ok {
			break
		}
		det.RecordDispatch(w.ProjectAlias)
	}

	clk.advance(quota.DefaultStarveWindow + time.Minute)

	starving := det.ListStarving()
	if len(starving) == 0 {
		t.Fatalf("expected >=1 starving project, got 0; det.Stats(p-00)=%+v",
			det.Stats(aliases[0]))
	}
	found := false
	for _, a := range starving {
		if a == aliases[0] {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected p-00 in starving list, got %v", starving)
	}

	q.SetWeight(aliases[0], 3.0)

	for i := 0; i < 50; i++ {
		_ = q.Enqueue(aliases[0], quota.WorkItem{
			ID:           fmt.Sprintf("post-boost-%d", i),
			ProjectAlias: aliases[0],
			EnqueuedAt:   clk.Now(),
			Cost:         1.0,
		})
		det.RecordEnqueue(aliases[0])
	}

	preCount := starvedDispatchCount(aliases[0], det)
	for i := 0; i < 1000 && starvedDispatchCount(aliases[0], det)-preCount == 0; i++ {
		w, ok := q.TryDispatch()
		if !ok {
			break
		}
		det.RecordDispatch(w.ProjectAlias)
	}
	postCount := starvedDispatchCount(aliases[0], det)
	if postCount <= preCount {
		t.Fatalf("after SetWeight + fresh enqueue, expected starved project dispatch count to grow; pre=%d post=%d",
			preCount, postCount)
	}
}

func starvedDispatchCount(alias string, det *quota.StarvationDetector) int64 {
	return det.Stats(alias).Dispatches
}

func TestChaos_ConcurrentDispatchStarvation_DeterministicOrdering(t *testing.T) {
	run := func() []string {
		const N = 5
		weights := map[string]quota.Weight{}
		aliases := make([]string, N)
		for i := range aliases {
			aliases[i] = fmt.Sprintf("p%d", i)
			weights[aliases[i]] = 1.0
		}
		q := quota.NewWfqQueue(weights)
		t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

		for i := 0; i < 50; i++ {
			for _, a := range aliases {
				if err := q.Enqueue(a, quota.WorkItem{
					ID:           fmt.Sprintf("%s-%d", a, i),
					ProjectAlias: a,
					EnqueuedAt:   t0,
					Cost:         1.0,
				}); err != nil {
					t.Fatalf("Enqueue: %v", err)
				}
			}
		}
		var seq []string
		for {
			w, ok := q.TryDispatch()
			if !ok {
				break
			}
			seq = append(seq, w.ID)
		}
		return seq
	}

	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("non-deterministic dispatch length: a=%d b=%d", len(a), len(b))
	}

	aSorted := append([]string(nil), a...)
	bSorted := append([]string(nil), b...)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			t.Fatalf("dispatched ID set differs at index %d: a=%s b=%s",
				i, aSorted[i], bSorted[i])
		}
	}
}

func TestChaos_ConcurrentDispatchStarvation_ConcurrentEnqueueDrain(t *testing.T) {
	const N = 10
	weights := map[string]quota.Weight{}
	aliases := make([]string, N)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("p%d", i)
		weights[aliases[i]] = 1.0
	}
	q := quota.NewWfqQueue(weights)
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	const total = 5000
	per := total / N

	var enqWg sync.WaitGroup
	for i := 0; i < N; i++ {
		enqWg.Add(1)
		go func(alias string) {
			defer enqWg.Done()
			for j := 0; j < per; j++ {
				_ = q.Enqueue(alias, quota.WorkItem{
					ID:           fmt.Sprintf("%s-%d", alias, j),
					ProjectAlias: alias,
					EnqueuedAt:   t0,
					Cost:         1.0,
				})
			}
		}(aliases[i])
	}
	enqWg.Wait()

	dispatched := 0
	var mu sync.Mutex
	var drnWg sync.WaitGroup
	for i := 0; i < N; i++ {
		drnWg.Add(1)
		go func() {
			defer drnWg.Done()
			for {
				_, ok := q.TryDispatch()
				if !ok {
					return
				}
				mu.Lock()
				dispatched++
				mu.Unlock()
			}
		}()
	}
	drnWg.Wait()

	if dispatched != total {
		t.Fatalf("expected %d dispatched (no drops), got %d", total, dispatched)
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
