package timeaccel_test

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/tests/timeaccel"
)

func TestNewHarness_DefaultAnchor(t *testing.T) {
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	want := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	if !h.Anchor().Equal(want) {
		t.Errorf("default anchor = %v, want %v", h.Anchor(), want)
	}
	if !h.Clock().Now().Equal(want) {
		t.Errorf("clock.Now() = %v, want %v", h.Clock().Now(), want)
	}
}

func TestNewHarness_CustomAnchor(t *testing.T) {
	want := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{Anchor: want})
	if !h.Anchor().Equal(want) {
		t.Errorf("anchor = %v, want %v", h.Anchor(), want)
	}
}

func TestHarness_Fake_AdvancesClock(t *testing.T) {
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	h.Fake().Advance(5 * time.Second)
	if got := h.Clock().Since(h.Anchor()); got != 5*time.Second {
		t.Errorf("clock advanced by %v, want 5s", got)
	}
}

func TestAdvanceUntilFire_FiresWithinBudget(t *testing.T) {
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	tick := h.Clock().NewTicker(3 * time.Second)
	defer tick.Stop()

	got := timeaccel.AdvanceUntilFire(h, tick.C(), 4*time.Second)
	if got.IsZero() {
		t.Fatalf("ticker did not fire within 4s")
	}

	want := h.Anchor().Add(3 * time.Second)
	if !got.Equal(want) {
		t.Errorf("first fire at %v, want %v", got, want)
	}
}

func TestAdvanceUntilFire_DoesNotFireBeyondBudget(t *testing.T) {
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	tick := h.Clock().NewTicker(10 * time.Second)
	defer tick.Stop()

	got := timeaccel.AdvanceUntilFire(h, tick.C(), 5*time.Second)
	if !got.IsZero() {
		t.Fatalf("ticker fired at %v despite budget shorter than period", got)
	}
}

func TestAssertTickerFires_ExactCardinality(t *testing.T) {
	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	tick := h.Clock().NewTicker(1 * time.Second)
	defer tick.Stop()

	timeaccel.AssertTickerFires(t, h, tick, 1*time.Second, 5)
	if t.Failed() {
		return
	}
	if got := h.Clock().Since(h.Anchor()); got != 5*time.Second {
		t.Errorf("anchor diff = %v, want 5s", got)
	}
}

func TestAssertTickerFires_DetectsMissingFire(t *testing.T) {

	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	tick := h.Clock().NewTicker(10 * time.Second)
	defer tick.Stop()

	tt := &testing.T{}
	timeaccel.AssertTickerFires(tt, h, tick, 1*time.Second, 3)
	if !tt.Failed() {
		t.Fatalf("AssertTickerFires should have flagged missing fire (period > step)")
	}
}

func TestHRA_TacticalCadence_IntegratesAtT3min(t *testing.T) {

	h := timeaccel.NewHarness(timeaccel.HarnessOpts{})
	const T = 3 * time.Minute
	tick := h.Clock().NewTicker(T)
	defer tick.Stop()

	timeaccel.AssertTickerFires(t, h, tick, T, 10)
}
