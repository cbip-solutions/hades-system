package filewatcher

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerCoalescesRapidEvents(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	var count atomic.Int32
	for i := 0; i < 10; i++ {
		d.Submit(Event{Path: "/foo", Op: "modify"}, func(Event) {
			count.Add(1)
		})
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(120 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("emitted %d times; want 1 (debounced)", got)
	}
}

func TestDebouncerSeparatesPaths(t *testing.T) {
	d := NewDebouncer(30 * time.Millisecond)
	var count atomic.Int32
	d.Submit(Event{Path: "/a"}, func(Event) { count.Add(1) })
	d.Submit(Event{Path: "/b"}, func(Event) { count.Add(1) })
	time.Sleep(80 * time.Millisecond)
	if got := count.Load(); got != 2 {
		t.Errorf("emitted %d times; want 2 (one per path)", got)
	}
}

func TestDebouncerDefaultsTo1500ms(t *testing.T) {
	d := NewDebouncer(0)
	if d.Window() != 1500*time.Millisecond {
		t.Errorf("Window = %v, want 1500ms", d.Window())
	}
}
