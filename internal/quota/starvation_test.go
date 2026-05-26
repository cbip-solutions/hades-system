package quota

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestStarvationDetectorEmptyNotStarving(t *testing.T) {
	d := NewStarvationDetector(50, time.Hour)
	if d.Check("nobody") {
		t.Error("Check on never-seen alias = true; want false (no work, no starvation)")
	}
}

func TestStarvationDetectorBelowDepthNotStarving(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(10, time.Hour, clock.Now)
	for i := 0; i < 5; i++ {
		d.RecordEnqueue("a")
	}
	clock.Advance(2 * time.Hour)
	if d.Check("a") {
		t.Error("Check below depth threshold = true; want false")
	}
}

func TestStarvationDetectorAtDepthThresholdAndStaleDispatch(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(10, time.Hour, clock.Now)
	for i := 0; i < 12; i++ {
		d.RecordEnqueue("a")
	}

	clock.Advance(2 * time.Hour)
	if !d.Check("a") {
		t.Error("Check (depth >= threshold + 0 dispatches in window) = false; want true")
	}
}

func TestStarvationDetectorRecentDispatchClears(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(10, time.Hour, clock.Now)
	for i := 0; i < 20; i++ {
		d.RecordEnqueue("a")
	}

	clock.Advance(30 * time.Minute)
	d.RecordDispatch("a")
	clock.Advance(20 * time.Minute)
	if d.Check("a") {
		t.Error("Check after recent dispatch = true; want false")
	}
}

func TestStarvationDetectorDispatchOlderThanWindowStarves(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(10, time.Hour, clock.Now)
	for i := 0; i < 20; i++ {
		d.RecordEnqueue("a")
	}
	d.RecordDispatch("a")

	clock.Advance(2 * time.Hour)
	if !d.Check("a") {
		t.Error("Check with stale dispatch (>window) = false; want true")
	}
}

func TestStarvationDetectorMultiProjectIndependent(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(5, 1*time.Hour, clock.Now)
	for i := 0; i < 10; i++ {
		d.RecordEnqueue("a")
		d.RecordEnqueue("b")
	}
	clock.Advance(30 * time.Minute)
	d.RecordDispatch("a")
	clock.Advance(35 * time.Minute)
	if d.Check("a") {
		t.Error("Check 'a' (recent dispatch) = true; want false")
	}
	if !d.Check("b") {
		t.Error("Check 'b' (no dispatch ever, depth >= threshold) = false; want true")
	}
}

func TestStarvationDetectorRaceFreeUnderConcurrent(t *testing.T) {
	d := NewStarvationDetector(100, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				d.RecordEnqueue("p")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				d.RecordDispatch("p")
			}
		}()
	}
	wg.Wait()

	stats := d.Stats("p")
	if stats.Enqueues != 16*1000 {
		t.Errorf("Enqueues = %d, want %d", stats.Enqueues, 16*1000)
	}
	if stats.Dispatches != 16*1000 {
		t.Errorf("Dispatches = %d, want %d", stats.Dispatches, 16*1000)
	}
}

func TestStarvationDetectorStatsZeroForUnknown(t *testing.T) {
	d := NewStarvationDetector(50, time.Hour)
	got := d.Stats("nobody")
	if got.Enqueues != 0 || got.Dispatches != 0 {
		t.Errorf("Stats(unknown) = %+v, want zero", got)
	}
}

func TestStarvationDetectorListStarvingAcrossProjects(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(5, time.Hour, clock.Now)
	for _, alias := range []string{"a", "b", "c"} {
		for i := 0; i < 10; i++ {
			d.RecordEnqueue(alias)
		}
	}

	clock.Advance(15 * time.Minute)
	for i := 0; i < 5; i++ {
		d.RecordDispatch("b")
	}
	clock.Advance(2 * time.Hour)
	starving := d.ListStarving()
	got := map[string]bool{}
	for _, a := range starving {
		got[a] = true
	}
	want := map[string]bool{"a": true, "b": true, "c": true}
	for k := range want {
		if !got[k] {
			t.Errorf("ListStarving missing %q; got %v", k, starving)
		}
	}
}

func TestStarvationDetectorDefaults(t *testing.T) {
	if DefaultStarveDepthThreshold != 50 {
		t.Errorf("DefaultStarveDepthThreshold = %d, want 50 (spec §4.3)", DefaultStarveDepthThreshold)
	}
	if DefaultStarveWindow != time.Hour {
		t.Errorf("DefaultStarveWindow = %v, want 1h (spec §4.3)", DefaultStarveWindow)
	}
}

func TestStarvationDetectorConstructorDefensiveDefaults(t *testing.T) {

	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(0, time.Hour, clock.Now)
	for i := 0; i < DefaultStarveDepthThreshold; i++ {
		d.RecordEnqueue("a")
	}
	clock.Advance(2 * time.Hour)
	if !d.Check("a") {
		t.Errorf("Check after default-depth fallback = false; want true (depth=%d, threshold should be DefaultStarveDepthThreshold=%d)",
			DefaultStarveDepthThreshold, DefaultStarveDepthThreshold)
	}

	clock2 := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d2 := NewStarvationDetectorWithClock(5, 0, clock2.Now)
	for i := 0; i < 10; i++ {
		d2.RecordEnqueue("b")
	}
	clock2.Advance(30 * time.Minute)
	if d2.Check("b") {
		t.Error("Check 30min in with default-window fallback = true; want false (1h window not yet expired)")
	}
	clock2.Advance(45 * time.Minute)
	if !d2.Check("b") {
		t.Error("Check 75min in with default-window fallback = false; want true")
	}

	d3 := NewStarvationDetectorWithClock(5, time.Hour, nil)
	d3.RecordEnqueue("c")
	stats := d3.Stats("c")
	if stats.LastEnqueueAt.IsZero() {
		t.Error("LastEnqueueAt is zero with nil now-fallback; want non-zero (time.Now default)")
	}
}

func TestStarvationDetectorListStarvingBelowDepthSkipped(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(10, time.Hour, clock.Now)

	for i := 0; i < 5; i++ {
		d.RecordEnqueue("low")
	}
	for i := 0; i < 15; i++ {
		d.RecordEnqueue("high")
	}
	clock.Advance(2 * time.Hour)
	starving := d.ListStarving()
	for _, a := range starving {
		if a == "low" {
			t.Errorf("ListStarving included %q; want exclusion (depth < threshold)", a)
		}
	}
	found := false
	for _, a := range starving {
		if a == "high" {
			found = true
		}
	}
	if !found {
		t.Errorf("ListStarving missing %q; got %v", "high", starving)
	}
}

func TestStarvationDetectorListStarvingTooYoungSkipped(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	d := NewStarvationDetectorWithClock(5, time.Hour, clock.Now)
	for i := 0; i < 10; i++ {
		d.RecordEnqueue("fresh")
	}

	clock.Advance(10 * time.Minute)
	starving := d.ListStarving()
	for _, a := range starving {
		if a == "fresh" {
			t.Errorf("ListStarving included %q (too young, < window); want exclusion", a)
		}
	}
}
