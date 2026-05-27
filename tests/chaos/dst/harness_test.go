// go:build chaos

// DST harness self-tests.
//
// These pin the load-bearing contracts of the harness package:
//
// - Seed reproducibility (same seed + mix → same action stream).
// - Cross-seed divergence (different seed → different stream).
// - Mix weighting (long-run frequency matches declared weights
// within a deterministic tolerance — bounded by the PRNG).
// - Action ↔ String round-trip (FormatStream/ParseStream).
// - FailureInjector pairing (Inject and Recover idempotent + the
// onActive / onClear callbacks fire exactly once per state flip).
//
// The harness self-tests do NOT need synctest bubbling; they exercise
// the Scheduler + Mix + injector primitives in plain test goroutines.
// runner_test.go drives the synctest-bubble path separately.

package dst

import (
	"strings"
	"testing"
	"time"
)

func TestSchedulerReplayDeterminism(t *testing.T) {
	mix := DefaultMix()
	const seed Seed = 42
	const n = 1000
	a := NewScheduler(seed, mix).Stream(n)
	b := NewScheduler(seed, mix).Stream(n)
	if len(a) != len(b) {
		t.Fatalf("stream lengths diverged: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("action[%d] diverged: %s vs %s (seed=%d)", i, a[i], b[i], seed)
		}
	}
}

func TestSchedulerSeedDivergence(t *testing.T) {
	mix := DefaultMix()
	a := NewScheduler(1, mix).Stream(1000)
	b := NewScheduler(2, mix).Stream(1000)
	same := 0
	for i := range a {
		if a[i] == b[i] {
			same++
		}
	}
	if same > 950 {
		t.Fatalf("seeds 1+2 share %d/1000 actions; PRNG wiring suspect", same)
	}
}

func TestSchedulerMixWeighting(t *testing.T) {
	mix := Mix{Sleep: 4, Yield: 1, Inject: 2, Recover: 1, MaxSleep: 10 * time.Millisecond}
	stream := NewScheduler(7, mix).Stream(4000)
	h := Histogram(stream)
	totalWeight := mix.Sleep + mix.Yield + mix.Inject + mix.Recover
	want := map[Action]int{
		ActionSleep:         len(stream) * mix.Sleep / totalWeight,
		ActionYield:         len(stream) * mix.Yield / totalWeight,
		ActionInjectFailure: len(stream) * mix.Inject / totalWeight,
		ActionRecover:       len(stream) * mix.Recover / totalWeight,
	}
	for action, wantCount := range want {
		gotCount := h[action]
		lo := wantCount / 2
		hi := wantCount + wantCount/2 + 1
		if gotCount < lo || gotCount > hi {
			t.Errorf("action %s: got %d, want in [%d, %d]", action, gotCount, lo, hi)
		}
	}
}

func TestSchedulerSleepDurationsBounded(t *testing.T) {
	mix := DefaultMix()
	s := NewScheduler(123, mix)
	for i := 0; i < 1000; i++ {
		d := s.SleepFor()
		if d < 0 {
			t.Fatalf("SleepFor returned negative duration: %s", d)
		}
		if d >= mix.MaxSleep {
			t.Fatalf("SleepFor returned %s, want < %s", d, mix.MaxSleep)
		}
	}
}

func TestSchedulerZeroMaxSleepReturnsZero(t *testing.T) {
	mix := Mix{Sleep: 1, Yield: 1, Inject: 1, Recover: 1, MaxSleep: 0}
	s := NewScheduler(1, mix)
	for i := 0; i < 100; i++ {
		if d := s.SleepFor(); d != 0 {
			t.Fatalf("MaxSleep=0 SleepFor returned %s, want 0", d)
		}
	}
}

func TestMixValidate(t *testing.T) {
	cases := []struct {
		name    string
		mix     Mix
		wantErr bool
	}{
		{"valid_default", DefaultMix(), false},
		{"zero_total", Mix{}, true},
		{"negative_sleep", Mix{Sleep: -1, Yield: 1}, true},
		{"negative_yield", Mix{Sleep: 1, Yield: -1}, true},
		{"negative_inject", Mix{Sleep: 1, Inject: -1}, true},
		{"negative_recover", Mix{Sleep: 1, Recover: -1}, true},
		{"negative_maxsleep", Mix{Sleep: 1, MaxSleep: -1}, true},
		{"sleep_only", Mix{Sleep: 1, MaxSleep: time.Millisecond}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.mix.Validate()
			if c.wantErr && err == nil {
				t.Errorf("expected error; got nil for %+v", c.mix)
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error %v for %+v", err, c.mix)
			}
		})
	}
}

func TestNewSchedulerPanicsOnInvalidMix(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewScheduler with zero mix did not panic")
		}
	}()
	_ = NewScheduler(1, Mix{})
}

func TestDeriveSeedDeterministic(t *testing.T) {
	a := DeriveSeed(42, "worker_0")
	b := DeriveSeed(42, "worker_0")
	if a != b {
		t.Fatalf("DeriveSeed not deterministic: %d vs %d", a, b)
	}
}

// TestDeriveSeedDistinctForDistinctLabels: two different labels under
// the same parent MUST produce different child seeds (otherwise
// goroutines forked off the same scheduler would share state and
// break the replay model).
func TestDeriveSeedDistinctForDistinctLabels(t *testing.T) {
	a := DeriveSeed(42, "worker_0")
	b := DeriveSeed(42, "worker_1")
	if a == b {
		t.Fatalf("DeriveSeed produced same seed for different labels: %d", a)
	}
}

func TestFormatParseStreamRoundTrip(t *testing.T) {
	mix := DefaultMix()
	stream := NewScheduler(99, mix).Stream(100)
	formatted := FormatStream(stream)
	parsed, err := ParseStream(formatted)
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	if len(parsed) != len(stream) {
		t.Fatalf("length drift: %d vs %d", len(parsed), len(stream))
	}
	for i := range stream {
		if parsed[i] != stream[i] {
			t.Fatalf("position %d: parsed=%s, original=%s", i, parsed[i], stream[i])
		}
	}
}

func TestParseStreamRejectsUnknown(t *testing.T) {
	_, err := ParseStream("sleep,nope,inject")
	if err == nil {
		t.Fatal("ParseStream accepted unknown token; want error")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("err = %v, want 'unknown action' message", err)
	}
}

func TestParseStreamEmptyReturnsNil(t *testing.T) {
	stream, err := ParseStream("")
	if err != nil {
		t.Fatalf("ParseStream(\"\") unexpected err: %v", err)
	}
	if stream != nil {
		t.Errorf("ParseStream(\"\") = %v, want nil", stream)
	}
}

func TestActionStringStable(t *testing.T) {
	cases := []struct {
		a    Action
		want string
	}{
		{ActionUnknown, "unknown"},
		{ActionSleep, "sleep"},
		{ActionYield, "yield"},
		{ActionInjectFailure, "inject"},
		{ActionRecover, "recover"},
	}
	for _, c := range cases {
		if got := c.a.String(); got != c.want {
			t.Errorf("Action(%d).String() = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestAllActionsHasExactlyFour(t *testing.T) {
	if got := len(AllActions()); got != 4 {
		t.Fatalf("AllActions() = %d, want 4", got)
	}
}

func TestFailureInjectorPairing(t *testing.T) {
	active := 0
	clear := 0
	inj := NewFailureInjector("test", func() { active++ }, func() { clear++ })
	if inj.Active() {
		t.Error("freshly-constructed injector reports Active=true")
	}
	inj.Activate()
	if !inj.Active() {
		t.Error("post-Activate injector reports Active=false")
	}
	if active != 1 {
		t.Errorf("onActive fired %d times, want 1", active)
	}
	inj.Recover()
	if inj.Active() {
		t.Error("post-Recover injector reports Active=true")
	}
	if clear != 1 {
		t.Errorf("onClear fired %d times, want 1", clear)
	}
}

func TestFailureInjectorIdempotentActivate(t *testing.T) {
	active := 0
	inj := NewFailureInjector("test", func() { active++ }, nil)
	inj.Activate()
	inj.Activate()
	inj.Activate()
	if active != 1 {
		t.Errorf("onActive fired %d times under triple-Activate, want 1", active)
	}
}

func TestFailureInjectorIdempotentRecover(t *testing.T) {
	clear := 0
	inj := NewFailureInjector("test", nil, func() { clear++ })
	inj.Recover()
	inj.Recover()
	if clear != 0 {
		t.Errorf("onClear fired %d times under recover-without-inject, want 0", clear)
	}
}

func TestFailureInjectorNilCallbacks(t *testing.T) {
	inj := NewFailureInjector("nil", nil, nil)
	inj.Activate()
	inj.Recover()
	if inj.Active() {
		t.Error("nil-callback injector should still flip state correctly")
	}
}

func TestSeedSummaryFormat(t *testing.T) {
	mix := Mix{Sleep: 1, Yield: 2, Inject: 3, Recover: 4, MaxSleep: 100 * time.Millisecond}
	got := SeedSummary(42, mix)
	want := "seed=42 mix={sleep:1 yield:2 inject:3 recover:4 max_sleep=100ms}"
	if got != want {
		t.Errorf("SeedSummary drift:\n got: %s\nwant: %s", got, want)
	}
}

func TestSortedSeedsOrder(t *testing.T) {
	in := []Seed{5, 3, 1, 4, 2}
	out := SortedSeeds(in)
	want := []Seed{1, 2, 3, 4, 5}
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("SortedSeeds[%d] = %d, want %d", i, out[i], want[i])
		}
	}

	wantIn := []Seed{5, 3, 1, 4, 2}
	for i := range wantIn {
		if in[i] != wantIn[i] {
			t.Fatalf("SortedSeeds mutated input at %d", i)
		}
	}
}
