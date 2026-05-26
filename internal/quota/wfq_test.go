package quota

import (
	"sync"
	"testing"
	"time"
)

func TestNewWfqQueueEmpty(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{})
	if got := q.Depth("nonexistent"); got != 0 {
		t.Errorf("Depth(nonexistent) = %d, want 0", got)
	}
	if _, ok := q.TryDispatch(); ok {
		t.Error("TryDispatch on empty queue returned ok=true")
	}
}

func TestWfqEnqueueIncrementsDepth(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"internal-platform-x": 1.0})
	work := WorkItem{ID: "w1", ProjectAlias: "internal-platform-x", EnqueuedAt: time.Now(), Cost: 100}
	if err := q.Enqueue("internal-platform-x", work); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got := q.Depth("internal-platform-x"); got != 1 {
		t.Errorf("Depth = %d, want 1", got)
	}
}

func TestWfqDispatchEmptiesQueue(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"internal-platform-x": 1.0})
	work := WorkItem{ID: "w1", ProjectAlias: "internal-platform-x", EnqueuedAt: time.Now(), Cost: 100}
	_ = q.Enqueue("internal-platform-x", work)
	got, ok := q.TryDispatch()
	if !ok {
		t.Fatal("TryDispatch returned ok=false")
	}
	if got.ID != "w1" {
		t.Errorf("dispatched ID = %q, want %q", got.ID, "w1")
	}
	if d := q.Depth("internal-platform-x"); d != 0 {
		t.Errorf("Depth after dispatch = %d, want 0", d)
	}
}

func TestWfqSingleProjectFIFO(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"internal-platform-x": 1.0})
	for i := 0; i < 5; i++ {
		_ = q.Enqueue("internal-platform-x", WorkItem{ID: itemID(i), ProjectAlias: "internal-platform-x", EnqueuedAt: time.Now(), Cost: 100})
	}
	for i := 0; i < 5; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] returned ok=false", i)
		}
		want := itemID(i)
		if got.ID != want {
			t.Errorf("dispatch[%d] ID = %q, want %q (FIFO)", i, got.ID, want)
		}
	}
}

func TestWfqEqualWeightsRoundRobin(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"a": 1.0, "b": 1.0})

	for i := 0; i < 4; i++ {
		_ = q.Enqueue("a", WorkItem{ID: "a-" + itemID(i), ProjectAlias: "a", Cost: 10})
		_ = q.Enqueue("b", WorkItem{ID: "b-" + itemID(i), ProjectAlias: "b", Cost: 10})
	}
	dispatched := []string{}
	for i := 0; i < 8; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] returned ok=false", i)
		}
		dispatched = append(dispatched, got.ProjectAlias)
	}

	counts := map[string]int{}
	for _, p := range dispatched {
		counts[p]++
	}
	if counts["a"] != 4 || counts["b"] != 4 {
		t.Errorf("equal weight dispatch counts a=%d b=%d, want 4/4", counts["a"], counts["b"])
	}
}

func TestWfqWeightedFairnessRatio(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"max-scope-proj": 1.5, "default-proj": 1.0})
	for i := 0; i < 100; i++ {
		_ = q.Enqueue("max-scope-proj", WorkItem{ID: "ms-" + itemID(i), ProjectAlias: "max-scope-proj", Cost: 10})
		_ = q.Enqueue("default-proj", WorkItem{ID: "df-" + itemID(i), ProjectAlias: "default-proj", Cost: 10})
	}
	counts := map[string]int{}
	for i := 0; i < 50; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] returned ok=false", i)
		}
		counts[got.ProjectAlias]++
	}
	ms := counts["max-scope-proj"]
	df := counts["default-proj"]

	if ms < 27 || ms > 33 {
		t.Errorf("max-scope dispatches = %d, want 27..33 (1.5/2.5 of 50 ±10%%)", ms)
	}
	if df < 17 || df > 23 {
		t.Errorf("default dispatches = %d, want 17..23 (1.0/2.5 of 50 ±10%%)", df)
	}
	if ms+df != 50 {
		t.Errorf("total dispatches = %d, want 50", ms+df)
	}
}

func TestWfqZeroWeightRebound(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"zero-weight": 0, "normal": 1.0})
	for i := 0; i < 4; i++ {
		_ = q.Enqueue("zero-weight", WorkItem{ID: "z-" + itemID(i), ProjectAlias: "zero-weight", Cost: 10})
		_ = q.Enqueue("normal", WorkItem{ID: "n-" + itemID(i), ProjectAlias: "normal", Cost: 10})
	}

	counts := map[string]int{}
	for i := 0; i < 4; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] returned ok=false", i)
		}
		counts[got.ProjectAlias]++
	}
	if counts["normal"] < 4 {
		t.Errorf("normal dispatches = %d, want 4 (zero weight starves)", counts["normal"])
	}
}

func TestWfqEnqueueUnknownProjectInitialisesWithDefault(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"a": 1.0})
	work := WorkItem{ID: "x", ProjectAlias: "newcomer", Cost: 10}
	if err := q.Enqueue("newcomer", work); err != nil {
		t.Fatalf("Enqueue unknown project: %v", err)
	}
	got, ok := q.TryDispatch()
	if !ok || got.ID != "x" {
		t.Errorf("TryDispatch for unknown project: got=(%v,%v)", got.ID, ok)
	}
}

func TestWfqEnqueueWrongAliasMismatch(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"a": 1.0, "b": 1.0})
	work := WorkItem{ID: "w", ProjectAlias: "a", Cost: 10}
	err := q.Enqueue("b", work)
	if err == nil {
		t.Error("Enqueue with mismatched alias should error")
	}
}

func TestWfqZeroCostStillAdvancesVFT(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"a": 1.0})
	for i := 0; i < 3; i++ {
		_ = q.Enqueue("a", WorkItem{ID: itemID(i), ProjectAlias: "a", Cost: 0})
	}
	for i := 0; i < 3; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("dispatch[%d]: ok=false", i)
		}
		if got.ID != itemID(i) {
			t.Errorf("dispatch[%d] = %q, want %q", i, got.ID, itemID(i))
		}
	}
}

func itemID(i int) string {
	switch i {
	case 0:
		return "id-0"
	case 1:
		return "id-1"
	case 2:
		return "id-2"
	case 3:
		return "id-3"
	case 4:
		return "id-4"
	default:
		return "id-N"
	}
}

func TestWfqWeightedFairSentinelReturnsErr(t *testing.T) {
	err := wfqWeightedFairSentinel()
	if err != ErrWfqWeightedFairAnchor {
		t.Errorf("wfqWeightedFairSentinel = %v, want ErrWfqWeightedFairAnchor", err)
	}
}

func TestWfqWeightAccessor(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"internal-platform-x": 1.5})
	got, ok := q.Weight("internal-platform-x")
	if !ok {
		t.Fatal("Weight(known) returned ok=false")
	}
	if got != 1.5 {
		t.Errorf("Weight = %v, want 1.5", got)
	}
}

func TestWfqWeightAccessorUnknownProject(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{})
	got, ok := q.Weight("nonexistent")
	if ok {
		t.Error("Weight(unknown) returned ok=true")
	}
	if got != 0 {
		t.Errorf("Weight(unknown) = %v, want 0", got)
	}
}

func TestWfqWeightAccessorReboundFloor(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"zero-w": 0, "neg-w": -1.0})
	if got, ok := q.Weight("zero-w"); !ok || got != minWeight {
		t.Errorf("Weight(zero-w) = (%v, %v), want (%v, true)", got, ok, minWeight)
	}
	if got, ok := q.Weight("neg-w"); !ok || got != minWeight {
		t.Errorf("Weight(neg-w) = (%v, %v), want (%v, true)", got, ok, minWeight)
	}
}

func TestWfqSetWeightUpdatesExisting(t *testing.T) {
	q := NewWfqQueue(map[string]Weight{"internal-platform-x": 1.0})
	q.SetWeight("internal-platform-x", 2.5)
	got, ok := q.Weight("internal-platform-x")
	if !ok || got != 2.5 {
		t.Errorf("Weight after SetWeight = (%v, %v), want (2.5, true)", got, ok)
	}
}

func TestWfqSetWeightCreatesUnknownProject(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{})
	q.SetWeight("newcomer", 1.5)
	got, ok := q.Weight("newcomer")
	if !ok || got != 1.5 {
		t.Errorf("Weight after SetWeight(newcomer) = (%v, %v), want (1.5, true)", got, ok)
	}
}

func TestWfqSetWeightZeroOrNegativeReboundsToFloor(t *testing.T) {
	cases := []struct {
		name string
		w    Weight
	}{
		{"zero", 0},
		{"negative", -10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			q := NewWfqQueue(map[string]Weight{"existing": 1.0})
			q.SetWeight("existing", c.w)
			if got, ok := q.Weight("existing"); !ok || got != minWeight {
				t.Errorf("Weight(existing) after SetWeight(%v) = (%v, %v), want (%v, true)", c.w, got, ok, minWeight)
			}
			q.SetWeight("auto-register", c.w)
			if got, ok := q.Weight("auto-register"); !ok || got != minWeight {
				t.Errorf("Weight(auto-register) after SetWeight(%v) = (%v, %v), want (%v, true)", c.w, got, ok, minWeight)
			}
		})
	}
}

func TestWfqSetWeightDoesNotReVFTInFlightItems(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"a": 1.0, "b": 1.0})

	for i := 0; i < 4; i++ {
		_ = q.Enqueue("a", WorkItem{ID: "a-" + itemID(i), ProjectAlias: "a", Cost: 10})
		_ = q.Enqueue("b", WorkItem{ID: "b-" + itemID(i), ProjectAlias: "b", Cost: 10})
	}

	q.SetWeight("a", 100.0)

	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("TryDispatch[%d] returned ok=false", i)
		}
		counts[got.ProjectAlias]++
	}
	if counts["a"] != 4 || counts["b"] != 4 {
		t.Errorf("post-SetWeight in-flight dispatch counts a=%d b=%d, want 4/4 (no re-VFT)", counts["a"], counts["b"])
	}

	for i := 0; i < 5; i++ {
		_ = q.Enqueue("a", WorkItem{ID: "a2-" + itemID(i), ProjectAlias: "a", Cost: 10})
		_ = q.Enqueue("b", WorkItem{ID: "b2-" + itemID(i), ProjectAlias: "b", Cost: 10})
	}
	postCounts := map[string]int{}
	for i := 0; i < 5; i++ {
		got, ok := q.TryDispatch()
		if !ok {
			t.Fatalf("post-SetWeight dispatch[%d]: ok=false", i)
		}
		postCounts[got.ProjectAlias]++
	}

	if postCounts["a"] < 4 {
		t.Errorf("post-SetWeight new-item dispatch a=%d, want >=4 (weight 100 dominates)", postCounts["a"])
	}
}

func TestWfqConcurrentEnqueueAndDispatch(t *testing.T) {

	q := NewWfqQueue(map[string]Weight{"a": 1.0, "b": 1.0, "c": 1.0})
	const enqPerProject = 200
	projects := []string{"a", "b", "c"}

	var enqWg sync.WaitGroup
	for _, p := range projects {
		enqWg.Add(1)
		go func(alias string) {
			defer enqWg.Done()
			for i := 0; i < enqPerProject; i++ {
				_ = q.Enqueue(alias, WorkItem{
					ID:           alias + "-" + itemID(i%5) + "-" + itemID(i/5%5),
					ProjectAlias: alias,
					Cost:         10,
				})
			}
		}(p)
	}

	dispatched := make(chan WorkItem, len(projects)*enqPerProject)
	var dispWg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 3; i++ {
		dispWg.Add(1)
		go func() {
			defer dispWg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if w, ok := q.TryDispatch(); ok {
					dispatched <- w
				}
			}
		}()
	}

	enqWg.Wait()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		w, ok := q.TryDispatch()
		if !ok {

			if q.Depth("a")+q.Depth("b")+q.Depth("c") == 0 {
				break
			}
			continue
		}
		dispatched <- w
	}
	close(stop)
	dispWg.Wait()
	close(dispatched)

	count := 0
	for range dispatched {
		count++
	}
	totalEnqueued := len(projects) * enqPerProject
	if count != totalEnqueued {
		t.Errorf("dispatched count = %d, want %d (no item lost or duplicated)", count, totalEnqueued)
	}
	for _, p := range projects {
		if d := q.Depth(p); d != 0 {
			t.Errorf("Depth(%q) post-drain = %d, want 0", p, d)
		}
	}
}
