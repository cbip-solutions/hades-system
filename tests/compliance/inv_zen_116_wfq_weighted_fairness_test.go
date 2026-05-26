package compliance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/quota"
)

func TestInvZen116_EqualWeightRoundRobin(t *testing.T) {

	if !errors.Is(quota.ErrWfqWeightedFairAnchor, quota.ErrWfqWeightedFairAnchor) {
		t.Fatal("ErrWfqWeightedFairAnchor sentinel unreachable")
	}

	q := quota.NewWfqQueue(map[string]quota.Weight{"a": 1.0, "b": 1.0})
	for i := 0; i < 100; i++ {
		if err := q.Enqueue("a", quota.WorkItem{ID: "ai", ProjectAlias: "a", Cost: 10}); err != nil {
			t.Fatalf("Enqueue[a,%d]: %v", i, err)
		}
		if err := q.Enqueue("b", quota.WorkItem{ID: "bi", ProjectAlias: "b", Cost: 10}); err != nil {
			t.Fatalf("Enqueue[b,%d]: %v", i, err)
		}
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] = ok=false", i)
		}
		counts[got.ProjectAlias]++
	}
	if counts["a"] != 100 || counts["b"] != 100 {
		t.Errorf("equal-weight counts a=%d b=%d, want 100/100", counts["a"], counts["b"])
	}
}

func TestInvZen116_WeightedRatioConverges(t *testing.T) {
	q := quota.NewWfqQueue(map[string]quota.Weight{
		"max-scope": 1.5,
		"default":   1.0,
	})

	for i := 0; i < 5000; i++ {
		_ = q.Enqueue("max-scope", quota.WorkItem{ID: "ms", ProjectAlias: "max-scope", Cost: 10})
		_ = q.Enqueue("default", quota.WorkItem{ID: "df", ProjectAlias: "default", Cost: 10})
	}
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] = ok=false", i)
		}
		counts[got.ProjectAlias]++
	}
	ms := counts["max-scope"]
	df := counts["default"]

	if ms < 550 || ms > 650 {
		t.Errorf("max-scope dispatches = %d, want 550..650 (±5%% of 600)", ms)
	}
	if df < 350 || df > 450 {
		t.Errorf("default dispatches = %d, want 350..450 (±5%% of 400)", df)
	}

	if ms+df != 1000 {
		t.Errorf("ms+df = %d, want 1000 (lost dispatches)", ms+df)
	}
}

func TestInvZen116_NoStarvationOver1Hour(t *testing.T) {
	q := quota.NewWfqQueue(map[string]quota.Weight{
		"max-scope": 1.5,
		"default":   1.0,
		"capa":      0.7,
	})
	clock := &fakeStarvationClock{
		t: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	d := quota.NewStarvationDetectorWithClock(50, time.Hour, clock.Now)

	for _, alias := range []string{"max-scope", "default", "capa"} {
		for i := 0; i < 100; i++ {
			if err := q.Enqueue(alias, quota.WorkItem{ID: alias, ProjectAlias: alias, Cost: 10}); err != nil {
				t.Fatalf("Enqueue[%s,%d]: %v", alias, i, err)
			}
			d.RecordEnqueue(alias)
		}
	}

	for tick := 0; tick < 180; tick++ {
		clock.advance(10 * time.Second)
		got, ok := q.TryDispatch()
		if !ok {
			break
		}
		d.RecordDispatch(got.ProjectAlias)
	}

	clock.advance(40 * time.Minute)
	starving := d.ListStarving()
	if len(starving) > 0 {
		t.Errorf("ListStarving = %v; want empty (every project dispatched within window)", starving)
	}
}

func TestInvZen116_StarvationDetectsBlockedProject(t *testing.T) {
	clock := &fakeStarvationClock{
		t: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	d := quota.NewStarvationDetectorWithClock(50, time.Hour, clock.Now)
	for i := 0; i < 60; i++ {
		d.RecordEnqueue("blocked")
	}
	clock.advance(2 * time.Hour)
	if !d.Check("blocked") {
		t.Error("Check('blocked') = false; want true (depth >= 50 + 0 dispatches in 1h)")
	}

	starving := d.ListStarving()
	found := false
	for _, alias := range starving {
		if alias == "blocked" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListStarving = %v; want to include 'blocked'", starving)
	}
}

func TestInvZen116_StarvationClearsAfterDispatch(t *testing.T) {
	clock := &fakeStarvationClock{
		t: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	d := quota.NewStarvationDetectorWithClock(50, time.Hour, clock.Now)
	for i := 0; i < 60; i++ {
		d.RecordEnqueue("recovers")
	}
	clock.advance(2 * time.Hour)
	if !d.Check("recovers") {
		t.Fatal("setup failed: expected starving before dispatch")
	}
	d.RecordDispatch("recovers")
	if d.Check("recovers") {
		t.Error("Check after RecordDispatch = true; want false (recent dispatch within window)")
	}
}

func TestInvZen116_ConcurrentEnqueueDispatchRaceFree(t *testing.T) {
	q := quota.NewWfqQueue(map[string]quota.Weight{"p1": 1.0, "p2": 1.5})
	d := quota.NewStarvationDetector(100, time.Hour)
	var wg sync.WaitGroup
	wg.Add(4)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			alias := "p1"
			if idx%2 == 0 {
				alias = "p2"
			}
			for j := 0; j < 1000; j++ {
				_ = q.Enqueue(alias, quota.WorkItem{ID: alias, ProjectAlias: alias, Cost: 10})
				d.RecordEnqueue(alias)
			}
		}(i)
	}
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				if got, ok := q.TryDispatch(); ok {
					d.RecordDispatch(got.ProjectAlias)
				}
			}
		}()
	}
	wg.Wait()
	for _, alias := range []string{"p1", "p2"} {
		stats := d.Stats(alias)
		if stats.Enqueues == 0 {
			t.Errorf("alias %q has zero enqueues after concurrent run", alias)
		}
	}
}

type fakeStarvationClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeStarvationClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeStarvationClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
