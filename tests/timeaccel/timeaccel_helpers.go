// SPDX-License-Identifier: MIT
// Package timeaccel provides the Tier 8 (time-accelerated) test helpers
// plus the build-tag-gated cadence test cases (timeaccel_test.go).
//
// Helpers are exported and live without a build tag so other tiers
// (chaos, integration) can compose them. The build-tag-gated cases
// hide behind // +build timeaccel because they exercise long doctrine-
// declared cadence windows (24h amendment cooldown, 30min architectural
// review) and would slow the default unit-test pass needlessly.
//
// Pattern: every cadence-sensitive Plan 5 component takes a clock.Clock
// parameter (Phase A invariant). Tests inject *clock.Fake; production
// uses clock.Real{}. The two canonical assertion shapes provided here
// are AdvanceUntilFire (single-fire predicate) and AssertTickerFires
// (cardinality assertion across N periods).
package timeaccel

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
)

type Harness struct {
	anchor time.Time
	clk    *clock.Fake
}

type HarnessOpts struct {
	Anchor time.Time
}

func NewHarness(opts HarnessOpts) *Harness {
	if opts.Anchor.IsZero() {
		opts.Anchor = time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	}
	return &Harness{anchor: opts.Anchor, clk: clock.NewFake(opts.Anchor)}
}

func (h *Harness) Clock() clock.Clock { return h.clk }

func (h *Harness) Fake() *clock.Fake { return h.clk }

func (h *Harness) Anchor() time.Time { return h.anchor }

func AdvanceUntilFire(h *Harness, ch <-chan time.Time, max time.Duration) time.Time {
	const step = 1 * time.Second
	elapsed := time.Duration(0)
	for elapsed < max {
		h.clk.Advance(step)
		select {
		case t := <-ch:
			return t
		default:

		}
		elapsed += step
	}
	return time.Time{}
}

func AssertTickerFires(t *testing.T, h *Harness, tick clock.Ticker, period time.Duration, count int) {
	t.Helper()
	fired := 0
	for i := 0; i < count; i++ {
		h.clk.Advance(period)
		select {
		case <-tick.C():
			fired++
		default:
			t.Errorf("ticker did not fire on step %d (anchor+%v)", i+1, time.Duration(i+1)*period)
			return
		}
	}
	if fired != count {
		t.Errorf("ticker fired %d times, want %d", fired, count)
	}
}
