//go:build chaos

// SPDX-License-Identifier: MIT

package dst

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"testing/synctest"
	"time"
)

// SystemUnderTest is the per-step contract a DST scenario implements.
// The harness drives one Action at a time; the SUT decides how the
// action translates into a system event:
//
//   - OnSleep:   advance / observe state during the synthetic sleep.
//   - OnYield:   yield to other goroutines (the harness runs this
//     inside the synctest bubble; goroutine scheduling is
//     already deterministic).
//   - OnInject:  activate the system-side failure (call gofail hook,
//     close stub connection, etc.).
//   - OnRecover: clear the injected failure.
//
// Implementations MUST be non-blocking on a "sane" duration budget;
// the harness wraps each step in a context with a per-step deadline so
// a runaway SUT step doesn't tie up the bubble forever.
//
// Returning a non-nil error aborts the run; the harness reports the
// step number, action, and seed so the failure log is reproducible.
type SystemUnderTest interface {
	OnSleep(ctx context.Context, d time.Duration) error
	OnYield(ctx context.Context) error
	OnInject(ctx context.Context) error
	OnRecover(ctx context.Context) error
}

type noopSUT struct{}

func (noopSUT) OnSleep(_ context.Context, _ time.Duration) error { return nil }
func (noopSUT) OnYield(_ context.Context) error                  { return nil }
func (noopSUT) OnInject(_ context.Context) error                 { return nil }
func (noopSUT) OnRecover(_ context.Context) error                { return nil }

func NewNoopSUT() SystemUnderTest { return noopSUT{} }

type RunResult struct {
	Seed     Seed
	Steps    int
	Actions  []Action
	Sleeps   []time.Duration
	Injects  int
	Recovers int
	Yields   int
}

func (r RunResult) String() string {
	return fmt.Sprintf(
		"seed=%d steps=%d injects=%d recovers=%d yields=%d sleeps=%d",
		r.Seed, r.Steps, r.Injects, r.Recovers, r.Yields, len(r.Sleeps),
	)
}

type RunConfig struct {
	Seed    Seed
	Mix     Mix
	Steps   int
	StepDDL time.Duration
	SkipBub bool
}

func (c RunConfig) Validate() error {
	if c.Steps <= 0 {
		return fmt.Errorf("dst.RunConfig: Steps must be ≥ 1; got %d", c.Steps)
	}
	if err := c.Mix.Validate(); err != nil {
		return fmt.Errorf("dst.RunConfig: %w", err)
	}
	return nil
}

func Run(t *testing.T, cfg RunConfig, sut SystemUnderTest) (RunResult, error) {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		return RunResult{}, err
	}
	if sut == nil {
		return RunResult{}, fmt.Errorf("dst.Run: SystemUnderTest must be non-nil")
	}
	stepDDL := cfg.StepDDL
	if stepDDL == 0 {
		stepDDL = cfg.Mix.MaxSleep + 50*time.Millisecond
	}

	result := RunResult{
		Seed:    cfg.Seed,
		Actions: make([]Action, 0, cfg.Steps),
		Sleeps:  make([]time.Duration, 0),
	}

	body := func() error {
		sched := NewScheduler(cfg.Seed, cfg.Mix)
		for i := 0; i < cfg.Steps; i++ {
			action := sched.Next()
			result.Actions = append(result.Actions, action)
			result.Steps++

			ctx, cancel := context.WithTimeout(context.Background(), stepDDL)
			var err error
			switch action {
			case ActionSleep:
				d := sched.SleepFor()
				result.Sleeps = append(result.Sleeps, d)
				err = sut.OnSleep(ctx, d)
			case ActionYield:
				result.Yields++
				runtime.Gosched()
				err = sut.OnYield(ctx)
			case ActionInjectFailure:
				result.Injects++
				err = sut.OnInject(ctx)
			case ActionRecover:
				result.Recovers++
				err = sut.OnRecover(ctx)
			default:
				err = fmt.Errorf("unknown action %v at step %d", action, i)
			}
			cancel()
			if err != nil {
				return fmt.Errorf("step %d action=%s seed=%d: %w", i, action, cfg.Seed, err)
			}
		}
		return nil
	}

	var runErr error
	if cfg.SkipBub {

		runErr = body()
	} else {
		synctest.Test(t, func(_ *testing.T) {
			runErr = body()
		})
	}
	return result, runErr
}

func Replay(t *testing.T, cfg RunConfig, sut SystemUnderTest, wantActions []Action) error {
	t.Helper()
	got, err := Run(t, cfg, sut)
	if err != nil {
		return fmt.Errorf("Replay: run error: %w", err)
	}
	if len(got.Actions) != len(wantActions) {
		return fmt.Errorf(
			"Replay: action count = %d, want %d (seed=%d)",
			len(got.Actions), len(wantActions), cfg.Seed,
		)
	}
	for i := range got.Actions {
		if got.Actions[i] != wantActions[i] {
			return fmt.Errorf(
				"Replay: action[%d] = %s, want %s (seed=%d, first divergence)",
				i, got.Actions[i], wantActions[i], cfg.Seed,
			)
		}
	}
	return nil
}
