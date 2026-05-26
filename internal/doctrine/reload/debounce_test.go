package reload_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine/reload"
)

func TestDebounce_FiresAfterWindow(t *testing.T) {
	var fired atomic.Int32
	d := reload.NewDebouncer(50*time.Millisecond, func(_ context.Context, path string) {
		if path == "/p" {
			fired.Add(1)
		}
	})
	defer d.Close()
	d.Trigger(context.Background(), "/p")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fired.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := fired.Load(); got != 1 {
		t.Errorf("fired count = %d; want 1", got)
	}
}

func TestDebounce_CoalescesRapidEvents(t *testing.T) {
	var fired atomic.Int32
	d := reload.NewDebouncer(100*time.Millisecond, func(_ context.Context, _ string) {
		fired.Add(1)
	})
	defer d.Close()
	for i := 0; i < 5; i++ {
		d.Trigger(context.Background(), "/p")
		time.Sleep(20 * time.Millisecond)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fired.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(150 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Errorf("fired count = %d; want 1 (debounce coalesced)", got)
	}
}

func TestDebounce_PerPathIndependent(t *testing.T) {
	var firedA, firedB atomic.Int32
	d := reload.NewDebouncer(50*time.Millisecond, func(_ context.Context, path string) {
		switch path {
		case "/a":
			firedA.Add(1)
		case "/b":
			firedB.Add(1)
		}
	})
	defer d.Close()
	d.Trigger(context.Background(), "/a")
	d.Trigger(context.Background(), "/b")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if firedA.Load() == 1 && firedB.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := firedA.Load(); got != 1 {
		t.Errorf("firedA = %d; want 1", got)
	}
	if got := firedB.Load(); got != 1 {
		t.Errorf("firedB = %d; want 1", got)
	}
}

func TestDebounce_CloseStopsPendingTimers(t *testing.T) {
	var fired atomic.Int32
	d := reload.NewDebouncer(200*time.Millisecond, func(_ context.Context, _ string) {
		fired.Add(1)
	})
	d.Trigger(context.Background(), "/p")
	time.Sleep(50 * time.Millisecond)
	d.Close()
	time.Sleep(300 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Errorf("fired count after Close = %d; want 0", got)
	}
}

func TestDebounce_TriggerAfterCloseIsNoop(t *testing.T) {
	var fired atomic.Int32
	d := reload.NewDebouncer(50*time.Millisecond, func(_ context.Context, _ string) {
		fired.Add(1)
	})
	d.Close()
	d.Trigger(context.Background(), "/p")
	time.Sleep(150 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Errorf("fired count after Trigger-post-Close = %d; want 0", got)
	}
}
