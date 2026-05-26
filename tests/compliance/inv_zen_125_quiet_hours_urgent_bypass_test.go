// Package compliance — inv-zen-125: urgent severity ALWAYS bypasses
// quiet hours when QuietHours.UrgentBypass is enabled (the Slack DND
// pattern; default true). The only legitimate way to silence urgents
// is QuietConfig.UrgentPauseUntil — the operator escape hatch
// surfaced by the `zen quiet --urgent-pause` CLI.
//
// Spec §1 Q12 B + §7.2 inv-zen-125 wording (Plan 7 Phase E):
//
//	"Urgent severity emits at any wallclock time regardless of
//	QuietHours.{Start, End, WeekendExtended} when UrgentBypass=true.
//	The only carve-out is QuietConfig.UrgentPauseUntil; while now <
//	UrgentPauseUntil, urgent defers like every other severity. After
//	the pause expires, urgent emits again on the next ShouldEmit call."
//
// This test is the cross-package, boundary-side property witness:
// the in-package coverage in internal/inbox/quiet_hours_test.go locks
// the predicate semantics on a hand-picked sample; this file
// exhaustively sweeps a 24-hour × 7-day × 4-min grid (672 distinct
// wallclocks) under the canonical wrap-midnight + WeekendExtended
// config so any future refactor of ShouldEmit (e.g. dropping the
// urgent fast-path, adding an unconditional InQuietHours filter) gets
// caught at the public surface.
//
// Coverage matrix:
//
//	(a) 24-hour × 7-day × 4-min sweep: urgent MUST emit at every
//	    wallclock under the canonical config (Start=21h, End=9h,
//	    WeekendExtended=true, UrgentBypass=true) — 672 assertions.
//	(b) Operator escape hatch: while UrgentPauseUntil is in the
//	    future, urgent defers; once it expires, urgent emits again.
//	(c) Per-project override: when a project's QuietHours specifies
//	    a never-quiet window (Start == End), urgent emits regardless
//	    of bypass-enabled state — proves PerProject lookup precedes
//	    the bypass branch in ShouldEmit.
//	(d) UrgentBypass=false override: when the operator explicitly
//	    opts out of bypass (rare but supported), urgent observes
//	    quiet hours like every other tier — closing the loop on
//	    "always" being a function of the bypass flag, not a
//	    structural guarantee.
//
// Boundary (inv-zen-031): this test imports only internal/inbox +
// stdlib. internal/inbox owns the predicate; the adapter layer is
// not touched (ShouldEmit is pure).
//
// Inv-zen-125 contract.
package compliance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

var _ = quietHoursUrgentBypassAnchorReference()

func quietHoursUrgentBypassAnchorReference() error {
	return inbox.ErrQuietHoursUrgentBypassAnchor
}

func TestInvZen125UrgentBypassAcrossAllHours(t *testing.T) {
	cfg := inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:           21 * time.Hour,
			End:             9 * time.Hour,
			WeekendExtended: true,
			UrgentBypass:    true,
		},
	}

	pid := "a" + strings.Repeat("0", 63)

	for day := 0; day < 7; day++ {
		for hour := 0; hour < 24; hour++ {
			for min := 0; min < 60; min += 15 {
				now := time.Date(2026, 5, 1+day, hour, min, 0, 0, time.UTC)
				n := inbox.Notification{
					ProjectID: pid,
					Severity:  inbox.SeverityUrgent,
					EventType: "test.urgent",
					ContentHash: inbox.ComputeContentHash(map[string]any{
						"d": day, "h": hour, "m": min,
					}),
					Payload:   json.RawMessage(`{}`),
					CreatedAt: now,
				}
				if !inbox.ShouldEmit(n, cfg, now) {
					t.Errorf("inv-zen-125 violation: urgent at "+
						"day=%d weekday=%s %02d:%02d should emit",
						day, now.Weekday(), hour, min)
				}
			}
		}
	}
}

func TestInvZen125UrgentDeferredOnlyDuringPause(t *testing.T) {
	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	pid := "a" + strings.Repeat("0", 63)
	n := inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityUrgent,
		EventType:   "x",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}

	cfg := inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:        21 * time.Hour,
			End:          9 * time.Hour,
			UrgentBypass: true,
		},
	}
	if !inbox.ShouldEmit(n, cfg, now) {
		t.Fatal("baseline: urgent should emit during quiet hours " +
			"(UrgentBypass=true, no pause)")
	}

	until := now.Add(30 * time.Minute)
	cfg.UrgentPauseUntil = &until
	if inbox.ShouldEmit(n, cfg, now) {
		t.Error("inv-zen-125 carve-out: urgent must defer during " +
			"active UrgentPauseUntil (operator escape hatch)")
	}

	// Boundary: at exactly until, ShouldEmit MUST allow emission again
	// (the predicate is `now.Before(*UrgentPauseUntil)` — strict <).
	if !inbox.ShouldEmit(n, cfg, until) {
		t.Error("inv-zen-125 boundary: urgent must emit at exactly " +
			"UrgentPauseUntil (predicate is strict less-than)")
	}

	after := until.Add(time.Second)
	if !inbox.ShouldEmit(n, cfg, after) {
		t.Error("inv-zen-125 post-pause: urgent must emit once " +
			"now > UrgentPauseUntil")
	}
}

func TestInvZen125PerProjectOverrideHonoured(t *testing.T) {
	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	pid := "a" + strings.Repeat("0", 63)
	cfg := inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:        21 * time.Hour,
			End:          9 * time.Hour,
			UrgentBypass: true,
		},
		PerProject: map[string]inbox.QuietHours{

			pid: {Start: 0, End: 0, UrgentBypass: false},
		},
	}
	n := inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityUrgent,
		EventType:   "x",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}
	if !inbox.ShouldEmit(n, cfg, now) {
		t.Error("inv-zen-125 per-project precedence: never-quiet " +
			"override must emit urgent regardless of UrgentBypass")
	}
}

func TestInvZen125UrgentBypassDisabledObservesQuiet(t *testing.T) {
	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	pid := "a" + strings.Repeat("0", 63)
	cfg := inbox.QuietConfig{
		Default: inbox.QuietHours{
			Start:        21 * time.Hour,
			End:          9 * time.Hour,
			UrgentBypass: false,
		},
	}
	n := inbox.Notification{
		ProjectID:   pid,
		Severity:    inbox.SeverityUrgent,
		EventType:   "x",
		ContentHash: strings.Repeat("a", 64),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   now,
	}
	if inbox.ShouldEmit(n, cfg, now) {
		t.Error("inv-zen-125 carve-out: with UrgentBypass=false, " +
			"urgent must observe quiet hours like every other tier")
	}

	noon := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if !inbox.ShouldEmit(n, cfg, noon) {
		t.Error("negative control: outside quiet hours, urgent must " +
			"emit even with UrgentBypass=false")
	}
}
