package scheduler_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestComputeJitter_Deterministic(t *testing.T) {
	id := "01HZ7K8M9P2Q3R4S5T6V7W8X9Y"
	period := time.Hour
	first := scheduler.ComputeJitter(id, period)
	for i := 0; i < 10000; i++ {
		got := scheduler.ComputeJitter(id, period)
		if got != first {
			t.Fatalf("non-deterministic at iteration %d: %v vs %v", i, first, got)
		}
	}
}

func TestComputeJitter_OrderIndependent(t *testing.T) {
	id1 := "id-1"
	id2 := "id-2"
	period := time.Hour
	want := scheduler.ComputeJitter(id1, period)
	_ = scheduler.ComputeJitter(id2, period)
	got := scheduler.ComputeJitter(id1, period)
	if got != want {
		t.Errorf("order-dependent: %v vs %v", want, got)
	}
}

func TestComputeJitter_BoundsRecurring(t *testing.T) {
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("routine-%d", i)
		got := scheduler.ComputeJitter(id, time.Hour)
		if got < 0 {
			t.Fatalf("negative jitter for %q: %v", id, got)
		}
		if got > 6*time.Minute {
			t.Fatalf("jitter for %q (period=1h) = %v, want <= 6min (10%% bucket)", id, got)
		}
	}
}

func TestComputeJitter_CapRecurring15Min(t *testing.T) {
	saturated := false
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("routine-%d", i)
		got := scheduler.ComputeJitter(id, 24*time.Hour)
		if got > 15*time.Minute {
			t.Fatalf("recurring jitter for %q (24h) = %v, want <= 15min cap", id, got)
		}
		if got == 15*time.Minute {
			saturated = true
		}
	}
	if !saturated {
		t.Errorf("expected at least one routine to hit 15min cap in 10000 samples")
	}
}

func TestComputeJitter_CapOneShot90s(t *testing.T) {
	saturated := false
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("oneshot-%d", i)
		got := scheduler.ComputeJitter(id, 30*time.Minute)
		if got > 90*time.Second {
			t.Fatalf("one-shot jitter for %q (30min) = %v, want <= 90s cap", id, got)
		}
		if got == 90*time.Second {
			saturated = true
		}
	}
	if !saturated {
		t.Errorf("expected at least one one-shot to hit 90s cap in 10000 samples")
	}
}

func TestComputeJitter_ZeroPeriod(t *testing.T) {
	got := scheduler.ComputeJitter("id", 0)
	if got != 0 {
		t.Errorf("ComputeJitter(_, 0) = %v, want 0", got)
	}
}

func TestComputeJitter_NegativePeriod(t *testing.T) {
	got := scheduler.ComputeJitter("id", -time.Hour)
	if got != 0 {
		t.Errorf("ComputeJitter(_, negative) = %v, want 0", got)
	}
}

func TestComputeJitter_DistinctIds(t *testing.T) {
	period := time.Hour
	matches := 0
	for i := 0; i < 100; i++ {
		a := scheduler.ComputeJitter(fmt.Sprintf("a-%d", i), period)
		b := scheduler.ComputeJitter(fmt.Sprintf("b-%d", i), period)
		if a == b {
			matches++
		}
	}
	if matches >= 100 {
		t.Errorf("all 100 pairs collided -- hash is broken")
	}
}

func TestComputeJitter_ExactlyOneHourPeriod(t *testing.T) {

	above90s := false
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("boundary-%d", i)
		got := scheduler.ComputeJitter(id, time.Hour)
		if got > 90*time.Second {
			above90s = true
		}
		if got > 6*time.Minute {
			t.Fatalf("jitter for %q (period=1h) = %v, want <= 6min bucket", id, got)
		}
	}
	if !above90s {
		t.Errorf("period=1h must take recurring branch; expected some jitters > 90s in 10000 samples")
	}
}

func TestComputeJitter_TinyPeriod(t *testing.T) {
	for _, period := range []time.Duration{1, 2, 5, 9} {
		got := scheduler.ComputeJitter("tiny", period*time.Nanosecond)
		if got != 0 {
			t.Errorf("ComputeJitter(_, %v) = %v, want 0 (bucket floor)", period, got)
		}
	}
}
