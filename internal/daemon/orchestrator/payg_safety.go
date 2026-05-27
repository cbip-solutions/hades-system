// SPDX-License-Identifier: MIT
// internal/daemon/orchestrator/payg_safety.go
//
// modes + threshold notifications. CheckCap runs before every dispatch;
// HandleCapReached runs when CheckCap returns ErrCapWillExceed.
//
// Pivots vs the plan reference (lines 1052-1392):
//
// 1. The CapCounters interface (was CostCounters in the plan) is renamed
// to avoid colliding with F-5's *CostCounters struct in
// cost_counters.go (same package). Method shape mirrors the F-5 struct:
// SessionTotal(sessionID) + ProjectProfileTierTotal(project, profile,
// tier, window time.Duration) — so *CostCounters satisfies
// CapCounters directly, no adapter needed at I-5 wiring time. The
// interface-pin lives in payg_safety_test.go:
// var _ CapCounters = (*CostCounters)(nil)
//
// 2. CheckCap signature pivoted to take sessionID — required for the
// WindowSession lookup, since F-5's SessionTotal is keyed on sessionID
// and an unkeyed call cannot retrieve per-session totals. The plan's
// reference signature omitted this and would be impossible to wire to
// F-5 cleanly.
//
// 3. Test-overridable tickInterval field: borrowed from F-7 / I-2 pattern
// so WindowResetScheduler can be exercised in unit tests without a
// 1-hour wait. Default is defaultWindowResetTickInterval (1h);
// test-only callers override before invoking WindowResetScheduler.
//
// Boundary (inv-hades-031): this file imports stdlib only (context, errors,
// fmt, sync, time). The orchestrator package MUST NOT import internal/store.
// Pin pattern matches F-5 / F-7 / I-2 precedent.
//
// inv-hades-063 anchor: capOverridesPin lives here. Removing PaygSafety,
// CheckCap, HandleCapReached, ErrCapWillExceed, OR the file-scope
// `var _ = capOverridesPin` reference breaks the build. The runtime
// ordering invariant — "pin is resolved FIRST, cap is checked SECOND, cap
// override pin" — is enforced inside tier_resolver.Select; this
// file provides the symbol so the doctrinal contract is mechanically
// load-bearing.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrCapWillExceed signals CheckCap that projected total exceeds at
	// least one cap window. The caller MUST invoke HandleCapReached to
	// apply the configured policy.
	ErrCapWillExceed = errors.New("payg: cap will exceed")

	ErrTierPausedHard = errors.New("tier paused: cost cap reached")

	ErrTierPausedDescriptive = errors.New("tier paused: see options")

	ErrCascadeDown = errors.New("tier exhausted, cascade to next tier in chain")
)

const (
	ModePause            = "pause"
	ModePauseDescriptive = "pause_descriptive"
	ModeCascadeDown      = "cascade_down"
	ModeNotifyOnly       = "notify_only"
)

type CapWindow string

const (
	WindowSession CapWindow = "session"

	WindowDay CapWindow = "day"

	WindowMonth CapWindow = "month"
)

type ProfileEffective struct {
	PerSessionUSD    float64
	PerDayUSD        float64
	PerMonthUSD      float64
	NotifyAtPercents []int
	OnCapReached     string
}

type Notifier interface {
	NotifyINFO(title, body, source string)
	NotifyWARN(title, body, source string)
	NotifyCRITICAL(title, body, source string)
}

type CapCounters interface {
	SessionTotal(sessionID string) float64

	ProjectProfileTierTotal(project, profile, tier string, window time.Duration) float64
}

var defaultWindowResetTickInterval = 1 * time.Hour

type PaygSafety struct {
	counters CapCounters
	notifier Notifier

	mu             sync.Mutex
	thresholdsSent map[string]struct{}

	tickInterval time.Duration
}

type PaygSafetyOptions struct {
	Counters CapCounters
	Notifier Notifier
}

func NewPaygSafety(opts PaygSafetyOptions) *PaygSafety {
	if opts.Counters == nil {
		panic("NewPaygSafety: Counters is required")
	}
	return &PaygSafety{
		counters:       opts.Counters,
		notifier:       opts.Notifier,
		thresholdsSent: map[string]struct{}{},
	}
}

func (p *PaygSafety) CheckCap(
	project, profile, tier, sessionID string,
	projectedAddUSD float64,
	effective ProfileEffective,
) error {
	if p == nil {
		return errors.New("PaygSafety: nil receiver")
	}
	thresholds := effective.NotifyAtPercents
	if len(thresholds) == 0 {
		thresholds = []int{50, 80, 100}
	}

	type winCheck struct {
		window CapWindow
		cap    float64
	}
	checks := []winCheck{
		{WindowSession, effective.PerSessionUSD},
		{WindowDay, effective.PerDayUSD},
		{WindowMonth, effective.PerMonthUSD},
	}

	exceeded := false
	for _, c := range checks {
		if c.cap <= 0 {
			continue
		}
		var current float64
		switch c.window {
		case WindowSession:
			current = p.counters.SessionTotal(sessionID)
		case WindowDay:
			current = p.counters.ProjectProfileTierTotal(project, profile, tier, 24*time.Hour)
		case WindowMonth:
			current = p.counters.ProjectProfileTierTotal(project, profile, tier, 30*24*time.Hour)
		}
		projected := current + projectedAddUSD
		if projected > c.cap {
			exceeded = true
		}

		percentNow := percentage(current, c.cap)
		percentProjected := percentage(projected, c.cap)
		for _, th := range thresholds {
			if percentNow < th && percentProjected >= th {
				p.fireThreshold(project, profile, tier, c.window, th, projected, c.cap)
			}
		}
	}

	if exceeded {
		return ErrCapWillExceed
	}
	return nil
}

func (p *PaygSafety) HandleCapReached(project, profile, tier, mode string) error {
	if p == nil {
		return errors.New("PaygSafety: nil receiver")
	}
	if mode == "" {
		mode = ModePauseDescriptive
	}

	switch mode {
	case ModePause:
		p.notifyCritical(project, profile, tier,
			"PAYG cap reached — tier paused (hard)",
			fmt.Sprintf("Tier %s for project=%s profile=%s is paused. Operator action required: raise cap, switch profile, or unblock.",
				tier, project, profile),
		)
		return ErrTierPausedHard

	case ModePauseDescriptive:
		p.notifyCritical(project, profile, tier,
			"PAYG cap reached — tier paused (descriptive)",
			fmt.Sprintf("Tier %s for project=%s profile=%s is paused.\nOptions:\n  - hades budget --raise --tier %s --per-month-usd <amount>\n  - hades orchestrator pin --scope global --tier <other-tier>\n  - Set on_cap_reached=cascade_down to auto-fall-through.",
				tier, project, profile, tier),
		)
		return ErrTierPausedDescriptive

	case ModeCascadeDown:
		p.notifyWarn(project, profile, tier,
			"PAYG cap reached — cascading to next tier",
			fmt.Sprintf("Tier %s exhausted for project=%s profile=%s. Failing over per fallback chain.",
				tier, project, profile),
		)
		return ErrCascadeDown

	case ModeNotifyOnly:
		p.notifyCritical(project, profile, tier,
			"PAYG cap exceeded — request proceeding (notify_only)",
			fmt.Sprintf("Tier %s exceeded cap for project=%s profile=%s, but on_cap_reached=notify_only. Request will proceed; review configuration.",
				tier, project, profile),
		)
		return nil

	default:

		p.notifyCritical(project, profile, tier,
			"PAYG cap reached — unknown mode, falling back to pause_descriptive",
			fmt.Sprintf("on_cap_reached=%q is not recognised; treating as pause_descriptive.", mode),
		)
		return ErrTierPausedDescriptive
	}
}

func (p *PaygSafety) WindowResetScheduler(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	tick := p.tickInterval
	if tick <= 0 {
		tick = defaultWindowResetTickInterval
	}
	go func() {
		defer close(done)
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				p.mu.Lock()
				p.thresholdsSent = map[string]struct{}{}
				p.mu.Unlock()
			}
		}
	}()
	return done
}

func (p *PaygSafety) fireThreshold(
	project, profile, tier string,
	window CapWindow,
	threshold int,
	projected, capValue float64,
) {
	if p.notifier == nil {
		return
	}
	key := fmt.Sprintf("%s|%s|%s|%s|%d", project, profile, tier, window, threshold)
	p.mu.Lock()
	if _, sent := p.thresholdsSent[key]; sent {
		p.mu.Unlock()
		return
	}
	p.thresholdsSent[key] = struct{}{}
	p.mu.Unlock()

	title := fmt.Sprintf("PAYG cap %d%% — %s/%s/%s [%s]", threshold, project, profile, tier, window)
	body := fmt.Sprintf("Projected $%.4f / cap $%.4f (%d%%)", projected, capValue, threshold)
	src := fmt.Sprintf("orchestrator/payg/%s", window)
	switch {
	case threshold >= 100:
		p.notifier.NotifyCRITICAL(title, body, src)
	case threshold >= 80:
		p.notifier.NotifyWARN(title, body, src)
	default:
		p.notifier.NotifyINFO(title, body, src)
	}
}

func (p *PaygSafety) notifyCritical(project, profile, tier, title, body string) {
	if p.notifier == nil {
		return
	}
	p.notifier.NotifyCRITICAL(title, body,
		fmt.Sprintf("orchestrator/payg/%s/%s/%s", project, profile, tier))
}

func (p *PaygSafety) notifyWarn(project, profile, tier, title, body string) {
	if p.notifier == nil {
		return
	}
	p.notifier.NotifyWARN(title, body,
		fmt.Sprintf("orchestrator/payg/%s/%s/%s", project, profile, tier))
}

func percentage(value, capValue float64) int {
	if capValue <= 0 {
		return 0
	}
	pct := value / capValue * 100
	if pct < 0 {
		return 0
	}
	if pct > 999 {
		return 999
	}
	return int(pct)
}

func capOverridesPin() bool {
	var p *PaygSafety
	_ = p.CheckCap
	_ = p.HandleCapReached
	_ = ErrCapWillExceed
	return true
}

var _ = capOverridesPin
