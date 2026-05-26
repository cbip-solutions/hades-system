//go:build chaos

package dst

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunHappyPath(t *testing.T) {
	cfg := RunConfig{
		Seed:  42,
		Mix:   DefaultMix(),
		Steps: 100,
	}
	result, err := Run(t, cfg, NewNoopSUT())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Steps != 100 {
		t.Errorf("Steps = %d, want 100", result.Steps)
	}
	if len(result.Actions) != 100 {
		t.Errorf("len(Actions) = %d, want 100", len(result.Actions))
	}
	sum := result.Injects + result.Recovers + result.Yields + len(result.Sleeps)
	if sum != 100 {
		t.Errorf("action-counter sum = %d, want 100", sum)
	}
}

func TestRunSkipBub(t *testing.T) {
	cfg := RunConfig{
		Seed:    1,
		Mix:     DefaultMix(),
		Steps:   50,
		SkipBub: true,
	}
	result, err := Run(t, cfg, NewNoopSUT())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Steps != 50 {
		t.Errorf("Steps = %d, want 50", result.Steps)
	}
}

func TestRunReplay(t *testing.T) {
	cfg := RunConfig{
		Seed:    123,
		Mix:     DefaultMix(),
		Steps:   200,
		SkipBub: true,
	}
	first, err := Run(t, cfg, NewNoopSUT())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := Replay(t, cfg, NewNoopSUT(), first.Actions); err != nil {
		t.Fatalf("Replay diverged: %v", err)
	}
}

func TestReplayDetectsDivergence(t *testing.T) {
	cfg := RunConfig{
		Seed:    7,
		Mix:     DefaultMix(),
		Steps:   20,
		SkipBub: true,
	}
	first, err := Run(t, cfg, NewNoopSUT())
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	corrupt := append([]Action{}, first.Actions...)

	for i := range corrupt {
		if corrupt[i] != ActionInjectFailure {
			corrupt[i] = ActionInjectFailure
			break
		}
	}
	err = Replay(t, cfg, NewNoopSUT(), corrupt)
	if err == nil {
		t.Fatal("Replay accepted corrupt stream; want error")
	}
}

func TestReplayDetectsLengthMismatch(t *testing.T) {
	cfg := RunConfig{
		Seed:    7,
		Mix:     DefaultMix(),
		Steps:   20,
		SkipBub: true,
	}
	err := Replay(t, cfg, NewNoopSUT(), []Action{ActionSleep})
	if err == nil {
		t.Fatal("Replay accepted truncated stream; want error")
	}
	if !strings.Contains(err.Error(), "action count") {
		t.Errorf("err = %v, want 'action count' message", err)
	}
}

type errorSUT struct{ injectErr error }

func (s errorSUT) OnSleep(_ context.Context, _ time.Duration) error { return nil }
func (s errorSUT) OnYield(_ context.Context) error                  { return nil }
func (s errorSUT) OnInject(_ context.Context) error                 { return s.injectErr }
func (s errorSUT) OnRecover(_ context.Context) error                { return nil }

func TestRunPropagatesSUTError(t *testing.T) {
	sentinel := errors.New("synthetic inject failure")
	cfg := RunConfig{
		Seed:    42,
		Mix:     Mix{Inject: 1, MaxSleep: 0},
		Steps:   5,
		SkipBub: true,
	}
	_, err := Run(t, cfg, errorSUT{injectErr: sentinel})
	if err == nil {
		t.Fatal("Run completed despite SUT error; want non-nil error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err does not wrap sentinel: %v", err)
	}
	if !strings.Contains(err.Error(), "seed=42") {
		t.Errorf("err lacks seed context: %v", err)
	}
	if !strings.Contains(err.Error(), "action=inject") {
		t.Errorf("err lacks action context: %v", err)
	}
}

func TestRunRejectsBadConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  RunConfig
	}{
		{"zero_steps", RunConfig{Seed: 1, Mix: DefaultMix(), Steps: 0}},
		{"negative_steps", RunConfig{Seed: 1, Mix: DefaultMix(), Steps: -1}},
		{"invalid_mix", RunConfig{Seed: 1, Mix: Mix{}, Steps: 10}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Run(t, c.cfg, NewNoopSUT())
			if err == nil {
				t.Errorf("Run with %+v: got nil, want error", c.cfg)
			}
		})
	}
}

func TestRunRejectsNilSUT(t *testing.T) {
	cfg := RunConfig{Seed: 1, Mix: DefaultMix(), Steps: 1}
	_, err := Run(t, cfg, nil)
	if err == nil {
		t.Fatal("Run with nil SUT: got nil, want error")
	}
}

type recordingSUT struct {
	sleeps   int
	yields   int
	injects  int
	recovers int
	totalDur time.Duration
}

func (s *recordingSUT) OnSleep(_ context.Context, d time.Duration) error {
	s.sleeps++
	s.totalDur += d
	return nil
}
func (s *recordingSUT) OnYield(_ context.Context) error   { s.yields++; return nil }
func (s *recordingSUT) OnInject(_ context.Context) error  { s.injects++; return nil }
func (s *recordingSUT) OnRecover(_ context.Context) error { s.recovers++; return nil }

func TestRunCallbackFiring(t *testing.T) {
	sut := &recordingSUT{}
	cfg := RunConfig{
		Seed:    314,
		Mix:     DefaultMix(),
		Steps:   400,
		SkipBub: true,
	}
	result, err := Run(t, cfg, sut)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sut.sleeps != len(result.Sleeps) {
		t.Errorf("sleeps callback fired %d, want %d", sut.sleeps, len(result.Sleeps))
	}
	if sut.yields != result.Yields {
		t.Errorf("yields callback fired %d, want %d", sut.yields, result.Yields)
	}
	if sut.injects != result.Injects {
		t.Errorf("injects callback fired %d, want %d", sut.injects, result.Injects)
	}
	if sut.recovers != result.Recovers {
		t.Errorf("recovers callback fired %d, want %d", sut.recovers, result.Recovers)
	}
}

func TestRunSynctestBubbleAdvancesFakeClock(t *testing.T) {
	type observation struct {
		start time.Time
		end   time.Time
		dur   time.Duration
	}
	var obs observation
	obs.start = time.Now()

	cfg := RunConfig{
		Seed:  9,
		Mix:   Mix{Sleep: 1, MaxSleep: 10 * time.Millisecond},
		Steps: 20,
	}
	sleepSUT := sleepDriverSUT{}
	result, err := Run(t, cfg, sleepSUT)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	obs.end = time.Now()
	obs.dur = obs.end.Sub(obs.start)
	if result.Injects != 0 || result.Recovers != 0 || result.Yields != 0 {
		t.Errorf("sleep-only mix produced non-sleep actions: %+v", result)
	}

	if obs.dur > 50*time.Millisecond {
		t.Errorf("synctest bubble did not advance fake clock; wall-clock=%s", obs.dur)
	}
}

type sleepDriverSUT struct{}

func (sleepDriverSUT) OnSleep(_ context.Context, d time.Duration) error {
	time.Sleep(d)
	return nil
}
func (sleepDriverSUT) OnYield(_ context.Context) error   { return nil }
func (sleepDriverSUT) OnInject(_ context.Context) error  { return nil }
func (sleepDriverSUT) OnRecover(_ context.Context) error { return nil }

func TestRunResultStringFormat(t *testing.T) {
	r := RunResult{
		Seed: 7, Steps: 100,
		Sleeps:   make([]time.Duration, 25),
		Injects:  20,
		Recovers: 25,
		Yields:   30,
	}
	got := r.String()
	want := "seed=7 steps=100 injects=20 recovers=25 yields=30 sleeps=25"
	if got != want {
		t.Errorf("RunResult.String() drift:\n got: %s\nwant: %s", got, want)
	}
}
