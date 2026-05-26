package eventlog

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

func TestSubscribeReceivesAllByDefault(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{}, 10)
	defer sub.Close()

	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStarted{SessionID: "s1"}); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: "w-1"}); err != nil {
		t.Fatalf("append2: %v", err)
	}

	got := drainSubscription(t, sub, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].EventType != EvtOrchestratorStarted {
		t.Errorf("got[0] type = %v want OrchestratorStarted", got[0].EventType)
	}
	if got[1].EventType != EvtWorkerDispatched {
		t.Errorf("got[1] type = %v want WorkerDispatched", got[1].EventType)
	}
}

func TestSubscribeFilterByType(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{
		Types: []EventType{EvtWorkerCheckpoint},
	}, 10)
	defer sub.Close()

	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStarted{}); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerCheckpoint{WorkerID: "w-1"}); err != nil {
		t.Fatalf("append2: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s1", "p1", OrchestratorStopped{Outcome: "success"}); err != nil {
		t.Fatalf("append3: %v", err)
	}

	got := drainSubscription(t, sub, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].EventType != EvtWorkerCheckpoint {
		t.Errorf("filter let through wrong type: %v", got[0].EventType)
	}
}

func TestSubscribeFilterByProjectID(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{ProjectID: "proj-A"}, 10)
	defer sub.Close()

	if _, err := log.appendTyped(ctx, "s-A", "proj-A", OrchestratorStarted{}); err != nil {
		t.Fatalf("append1: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s-B", "proj-B", OrchestratorStarted{}); err != nil {
		t.Fatalf("append2: %v", err)
	}
	if _, err := log.appendTyped(ctx, "s-A", "proj-A", OrchestratorStopped{}); err != nil {
		t.Fatalf("append3: %v", err)
	}

	got := drainSubscription(t, sub, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	for _, r := range got {
		if r.ProjectID != "proj-A" {
			t.Errorf("filter leaked project_id %q", r.ProjectID)
		}
	}
}

func TestSubscribeFilterByTypeAndProjectID(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{
		Types:     []EventType{EvtWorkerCheckpoint},
		ProjectID: "proj-A",
	}, 10)
	defer sub.Close()

	if _, err := log.appendTyped(ctx, "s-B", "proj-B", WorkerCheckpoint{WorkerID: "w-x"}); err != nil {
		t.Fatalf("append1: %v", err)
	}

	if _, err := log.appendTyped(ctx, "s-A", "proj-A", OrchestratorStarted{}); err != nil {
		t.Fatalf("append2: %v", err)
	}

	if _, err := log.appendTyped(ctx, "s-A", "proj-A", WorkerCheckpoint{WorkerID: "w-A"}); err != nil {
		t.Fatalf("append3: %v", err)
	}

	got := drainSubscription(t, sub, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].ProjectID != "proj-A" || got[0].EventType != EvtWorkerCheckpoint {
		t.Errorf("wrong record: project=%q type=%v", got[0].ProjectID, got[0].EventType)
	}
}

// TestSubscribeDropOldestBackpressure verifies that when the subscriber's
// buffer is full, the OLDEST record is dropped to make room for the newest
// (spec §1 Q5 C). The publisher MUST NOT block on a slow subscriber.
func TestSubscribeDropOldestBackpressure(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{}, 2)
	defer sub.Close()

	for i := 0; i < 5; i++ {
		if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{
			WorkerID: indexLabel(i),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	got := drainSubscription(t, sub, 2, 200*time.Millisecond)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (drop-oldest)", len(got))
	}

	first, _ := decodeWorkerDispatched(t, got[0])
	if first.WorkerID == "w-0" || first.WorkerID == "w-1" || first.WorkerID == "w-2" {
		t.Errorf("expected oldest dropped; got first=%s", first.WorkerID)
	}
}

// TestSubscribeCloseStopsDelivery verifies that after Close(), publish()
// drops the subscriber from rotation and subsequent Appends do not deliver.
//
// Note (C-1 fix): Close() does NOT close sub.Events() — the data channel
// stays open and is GC'd when the subscriber drops out of the hub's slice
// + the consumer goroutine exits. Termination is signalled via Done().
func TestSubscribeCloseStopsDelivery(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	sub := log.Subscribe(Filter{}, 4)
	sub.Close()

	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: "w-1"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	select {
	case <-sub.Done():
	case <-time.After(50 * time.Millisecond):
		t.Errorf("Done() did not close after Close()")
	}

	select {
	case r, ok := <-sub.Events():
		if ok {
			t.Errorf("unexpected event delivered after Close: %+v", r)
		}
	default:

	}
}

func TestSubscribeCloseIdempotent(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	sub := log.Subscribe(Filter{}, 4)
	sub.Close()
	sub.Close()
	sub.Close()
}

func TestSubscribeBufferSizeMustBePositive(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	for _, size := range []int{0, -1, -100} {
		size := size
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Subscribe(bufferSize=%d) did not panic", size)
				}
			}()
			_ = log.Subscribe(Filter{}, size)
		})
	}
}

func TestSubscribeClosedSubscriberGCd(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	subA := log.Subscribe(Filter{}, 4)
	subB := log.Subscribe(Filter{}, 4)
	defer subB.Close()

	subA.Close()

	if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: "w-1"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	got := drainSubscription(t, subB, 1, 200*time.Millisecond)
	if len(got) != 1 {
		t.Fatalf("subB got %d events, want 1", len(got))
	}
}

func TestSubscribeConcurrent(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	const n = 5
	subs := make([]Subscription, n)
	for i := range subs {
		subs[i] = log.Subscribe(Filter{}, 100)
	}
	defer func() {
		for _, s := range subs {
			s.Close()
		}
	}()

	var wg sync.WaitGroup
	wg.Add(n)
	got := make([]int, n)
	for i := range subs {
		go func(idx int) {
			defer wg.Done()
			for range subs[idx].Events() {
				got[idx]++
				if got[idx] == 50 {
					return
				}
			}
		}(i)
	}

	for i := 0; i < 50; i++ {
		if _, err := log.appendTyped(ctx, "s1", "p1", WorkerDispatched{WorkerID: indexLabel(i)}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("subscribers did not all receive 50 events; got: %v", got)
	}
}

func TestSubscribePublishCloseRace(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	const rounds = 1000
	for i := 0; i < rounds; i++ {
		sub := log.Subscribe(Filter{}, 4)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = log.appendTyped(ctx, "sess", "proj", OrchestratorStarted{
					SessionID: "sess",
				})
			}
		}()
		go func() {
			defer wg.Done()

			time.Sleep(time.Microsecond)
			sub.Close()
		}()
		wg.Wait()
	}

}

func TestSubscribePublishCloseRaceFullBuffer(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)
	ctx := context.Background()

	const rounds = 200
	for i := 0; i < rounds; i++ {
		sub := log.Subscribe(Filter{}, 1)
		var wg sync.WaitGroup

		const publishers = 4
		const eventsPerPub = 8
		wg.Add(publishers + 1)
		for p := 0; p < publishers; p++ {
			go func() {
				defer wg.Done()
				for j := 0; j < eventsPerPub; j++ {
					_, _ = log.appendTyped(ctx, "sess", "proj", WorkerDispatched{
						WorkerID: "w-x",
					})
				}
			}()
		}
		go func() {
			defer wg.Done()

			sub.Close()
		}()
		wg.Wait()
	}
}

func TestSubscribeDoneSignal(t *testing.T) {
	em := &inMemoryEmitter{}
	fc := clock.NewFake(time.Unix(1700000000, 0))
	log := New(em, fc)

	sub := log.Subscribe(Filter{}, 4)

	select {
	case <-sub.Done():
		t.Fatalf("Done() closed before Close() was called")
	default:
	}

	sub.Close()

	select {
	case <-sub.Done():
	case <-time.After(50 * time.Millisecond):
		t.Errorf("Done() did not close after Close()")
	}

	sub.Close()
}

func drainSubscription(t *testing.T, sub Subscription, want int, timeout time.Duration) []Record {
	t.Helper()
	out := make([]Record, 0, want)
	deadline := time.Now().Add(timeout)
	for len(out) < want {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return out
		}
		select {
		case r, ok := <-sub.Events():
			if !ok {
				return out
			}
			out = append(out, r)
		case <-time.After(remaining):
			return out
		}
	}
	return out
}

func indexLabel(i int) string { return "w-" + strconv.Itoa(i) }

func decodeWorkerDispatched(t *testing.T, r Record) (WorkerDispatched, error) {
	t.Helper()
	dec, err := Decode(r.EventType, r.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return dec.(WorkerDispatched), nil
}
