// + urgent bypass.
//
// Drives inv-zen-125: urgent severity ALWAYS bypasses quiet hours
// (when `UrgentBypass=true` and no `UrgentPauseUntil` is active);
// non-urgent severities are deferred while inside the quiet window.
//
// The implementation surface (internal/inbox/quiet_hours.go) is two
// pure predicates:
//
//   - InQuietHours(QuietHours, now) bool — wallclock-only quiet-window
//     evaluator. Honors wrap-midnight (Start > End) and
//     WeekendExtended.
//   - ShouldEmit(Notification, QuietConfig, now) bool — composes the
//     per-severity policy: urgent returns true even inside quiet hours
//     (unless UrgentBypass=false OR UrgentPauseUntil is active);
//     non-urgent returns false inside quiet hours.
//
// Drift notes (vs plan-template heredoc):
//
//   - The plan template referenced fictional surfaces:
//     `inbox.NewDeliveryGate`, `inbox.DeliveryGateDeps{Store, Emitter,
//     Clock, QuietHours}`, `gate.Submit(ctx, evt)`, `gate.Run(ctx)`,
//     `eventlog.TypeInboxDelivered`, `inbox.SeverityInfoBatched`,
//     `projectctx.ProjectID(testhelpers.MakeProjectID(0))`,
//     `testhelpers.NewMigratedStore(t)`. None exist. Reality:
//
//   - There is NO `DeliveryGate` daemon goroutine; quiet-hours
//     gating is a pure function executed at notification-emit time
//     (the plug site is the daemon's emitter, which calls
//     `ShouldEmit` and either delivers immediately or stores in the
//     inbox table for the operator to read at zen-day-time).
//   - Severity has 4 tiers — the 4th is `SeverityInfoDigest`, NOT
//     `SeverityInfoBatched`. Reality wins.
//   - There is no defer-and-release-at-end-of-quiet pipeline yet;
//     non-urgent during quiet hours is simply NOT emitted at the
//     channel layer (Plan 11 handles channel adapters; until then,
//     the contract is "ShouldEmit returns false" which the daemon
//     interprets as "store row, do not push notification"). The
//     plan template's "release all deferred at 09:00" is an
//     external-channel concern (Plan 11), not an inbox-level one.
//     We pin the load-bearing claim that ShouldEmit is correct
//     across the boundary; channel-side release semantics are out
//     of scope for K-16.
//
//   - The plan template asserted "exactly 5 InboxDelivered events
//     at 09:00 ± 1s (3 from 21:00 batch + 2 mid-night non-urgent)".
//     That assertion presupposes a deferred-release pipeline that
//     does not exist. Reality wins: we instead assert ShouldEmit's
//     pure semantics across virtual time boundaries (boundary
//     21:00 → quiet starts; boundary 09:00 → quiet ends; mid-night
//     03:00 → still quiet; 09:30 → no longer quiet). The semantic
//     is identical to the plan template's intent at the predicate
//     level.
//
//   - WeekendExtended scenario tested with Saturday 12:00
//     timestamps; the WeekendExtended flag forces ALL of Saturday +
//     Sunday into quiet, regardless of Start/End.
//
//go:build timeaccel
// +build timeaccel

package timeaccel_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

var quietHoursStandard = inbox.QuietHours{
	Start:        21 * time.Hour,
	End:          9 * time.Hour,
	UrgentBypass: true,
}

func mkNotification(id string, sev inbox.Severity) inbox.Notification {
	return inbox.Notification{
		ProjectID:   "1111111111111111111111111111111111111111111111111111111111111111",
		Severity:    sev,
		EventType:   id,
		ContentHash: "0000000000000000000000000000000000000000000000000000000000000000",
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   time.Now().UTC(),
	}
}

func TestTimeaccel_QuietHours_PreQuietAllSeveritiesEmit(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}
	preQuiet := time.Date(2026, 5, 1, 20, 30, 0, 0, time.UTC)

	if inbox.InQuietHours(quietHoursStandard, preQuiet) {
		t.Fatalf("20:30 Friday: InQuietHours returned true; expected false (pre-quiet)")
	}

	for _, sev := range []inbox.Severity{
		inbox.SeverityUrgent,
		inbox.SeverityActionNeeded,
		inbox.SeverityInfoImmediate,
		inbox.SeverityInfoDigest,
	} {
		n := mkNotification(fmt.Sprintf("pre-%s", sev), sev)
		if !inbox.ShouldEmit(n, cfg, preQuiet) {
			t.Errorf("20:30 pre-quiet, severity %q: ShouldEmit returned false; want true", sev)
		}
	}
}

func TestTimeaccel_QuietHours_AtQuietStartUrgentBypassesOthersDefer(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}
	atStart := time.Date(2026, 5, 1, 21, 0, 0, 0, time.UTC)

	if !inbox.InQuietHours(quietHoursStandard, atStart) {
		t.Fatalf("21:00: InQuietHours returned false; expected true (Start boundary inclusive)")
	}

	urgent := mkNotification("at-start-urgent", inbox.SeverityUrgent)
	if !inbox.ShouldEmit(urgent, cfg, atStart) {
		t.Errorf("21:00 quiet, urgent: ShouldEmit returned false; inv-zen-125 violated")
	}

	for _, sev := range []inbox.Severity{
		inbox.SeverityActionNeeded,
		inbox.SeverityInfoImmediate,
		inbox.SeverityInfoDigest,
	} {
		n := mkNotification(fmt.Sprintf("at-start-%s", sev), sev)
		if inbox.ShouldEmit(n, cfg, atStart) {
			t.Errorf("21:00 quiet, severity %q: ShouldEmit returned true; should defer", sev)
		}
	}
}

func TestTimeaccel_QuietHours_MidNightUrgentBypassesOthersDefer(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}
	midNight := time.Date(2026, 5, 2, 3, 0, 0, 0, time.UTC)

	if !inbox.InQuietHours(quietHoursStandard, midNight) {
		t.Fatalf("Saturday 03:00: InQuietHours returned false; expected true (mid-quiet)")
	}

	urgent := mkNotification("mid-urgent", inbox.SeverityUrgent)
	if !inbox.ShouldEmit(urgent, cfg, midNight) {
		t.Errorf("03:00 quiet, urgent: ShouldEmit returned false; inv-zen-125 violated")
	}

	other := mkNotification("mid-info", inbox.SeverityInfoImmediate)
	if inbox.ShouldEmit(other, cfg, midNight) {
		t.Errorf("03:00 quiet, info-immediate: ShouldEmit returned true; should defer")
	}
}

func TestTimeaccel_QuietHours_AtQuietEndExclusiveBoundary(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}
	atEnd := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)

	if inbox.InQuietHours(quietHoursStandard, atEnd) {
		t.Fatalf("09:00 sharp: InQuietHours returned true; expected false (End is exclusive)")
	}

	for _, sev := range []inbox.Severity{
		inbox.SeverityUrgent,
		inbox.SeverityActionNeeded,
		inbox.SeverityInfoImmediate,
		inbox.SeverityInfoDigest,
	} {
		n := mkNotification(fmt.Sprintf("at-end-%s", sev), sev)
		if !inbox.ShouldEmit(n, cfg, atEnd) {
			t.Errorf("09:00 post-quiet, severity %q: ShouldEmit returned false; want true",
				sev)
		}
	}
}

func TestTimeaccel_QuietHours_PostQuietAllSeveritiesEmit(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}
	postQuiet := time.Date(2026, 5, 2, 9, 30, 0, 0, time.UTC)

	if inbox.InQuietHours(quietHoursStandard, postQuiet) {
		t.Fatalf("Saturday 09:30 (no WeekendExtended): InQuietHours returned true; expected false")
	}

	for _, sev := range []inbox.Severity{
		inbox.SeverityUrgent,
		inbox.SeverityActionNeeded,
		inbox.SeverityInfoImmediate,
		inbox.SeverityInfoDigest,
	} {
		n := mkNotification(fmt.Sprintf("post-%s", sev), sev)
		if !inbox.ShouldEmit(n, cfg, postQuiet) {
			t.Errorf("09:30 post-quiet, severity %q: ShouldEmit returned false; want true",
				sev)
		}
	}
}

func TestTimeaccel_QuietHours_FullDayBoundaryWalk(t *testing.T) {
	cfg := inbox.QuietConfig{Default: quietHoursStandard}

	start := time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)
	const step = 30 * time.Minute
	const total = 36

	for i := 0; i < total; i++ {
		now := start.Add(time.Duration(i) * step)
		inQuiet := inbox.InQuietHours(quietHoursStandard, now)

		urgent := mkNotification(fmt.Sprintf("walk-%02d-urgent", i), inbox.SeverityUrgent)
		if !inbox.ShouldEmit(urgent, cfg, now) {
			t.Errorf("step %d (now=%v inQuiet=%v): urgent ShouldEmit returned false; inv-zen-125",
				i, now, inQuiet)
		}

		info := mkNotification(fmt.Sprintf("walk-%02d-info", i), inbox.SeverityInfoImmediate)
		emitted := inbox.ShouldEmit(info, cfg, now)
		if inQuiet && emitted {
			t.Errorf("step %d (now=%v): info-immediate emitted inside quiet; expected defer",
				i, now)
		}
		if !inQuiet && !emitted {
			t.Errorf("step %d (now=%v): info-immediate deferred outside quiet; expected emit",
				i, now)
		}
	}
}

func TestTimeaccel_QuietHours_WeekendExtendedDefersInfoOnSaturday(t *testing.T) {
	weekend := inbox.QuietHours{
		Start:           21 * time.Hour,
		End:             9 * time.Hour,
		WeekendExtended: true,
		UrgentBypass:    true,
	}
	cfg := inbox.QuietConfig{Default: weekend}

	saturdayNoon := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	if !inbox.InQuietHours(weekend, saturdayNoon) {
		t.Fatalf("Saturday 12:00 (WeekendExtended): InQuietHours returned false; expected true")
	}

	urgent := mkNotification("weekend-urgent", inbox.SeverityUrgent)
	if !inbox.ShouldEmit(urgent, cfg, saturdayNoon) {
		t.Errorf("WeekendExtended Saturday: urgent ShouldEmit false; inv-zen-125 violated")
	}

	info := mkNotification("weekend-info", inbox.SeverityInfoImmediate)
	if inbox.ShouldEmit(info, cfg, saturdayNoon) {
		t.Errorf("WeekendExtended Saturday: info-immediate ShouldEmit true; should defer")
	}

	monday := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	if inbox.InQuietHours(weekend, monday) {
		t.Fatalf("Monday 09:00 (WeekendExtended): InQuietHours returned true; expected false (weekday + exclusive end)")
	}
	if !inbox.ShouldEmit(info, cfg, monday) {
		t.Errorf("Monday 09:00: info-immediate ShouldEmit returned false; want true (weekend extension lifted)")
	}
}

func TestTimeaccel_QuietHours_UrgentPauseUntilSilencesUrgent(t *testing.T) {
	pauseEnd := time.Date(2026, 5, 1, 21, 30, 0, 0, time.UTC)
	cfg := inbox.QuietConfig{
		Default:          quietHoursStandard,
		UrgentPauseUntil: &pauseEnd,
	}

	insidePause := time.Date(2026, 5, 1, 21, 15, 0, 0, time.UTC)
	urgent := mkNotification("paused-urgent", inbox.SeverityUrgent)
	if inbox.ShouldEmit(urgent, cfg, insidePause) {
		t.Errorf("21:15 inside UrgentPauseUntil: urgent ShouldEmit returned true; pause violated")
	}

	afterPause := time.Date(2026, 5, 1, 21, 31, 0, 0, time.UTC)
	if !inbox.ShouldEmit(urgent, cfg, afterPause) {
		t.Errorf("21:31 after UrgentPauseUntil: urgent ShouldEmit returned false; bypass should resume")
	}
}

func TestTimeaccel_QuietHours_UrgentBypassDisabledMakesUrgentObserveQuiet(t *testing.T) {
	noByPass := quietHoursStandard
	noByPass.UrgentBypass = false
	cfg := inbox.QuietConfig{Default: noByPass}

	insideQuiet := time.Date(2026, 5, 1, 23, 0, 0, 0, time.UTC)
	urgent := mkNotification("nobypass-urgent", inbox.SeverityUrgent)
	if inbox.ShouldEmit(urgent, cfg, insideQuiet) {
		t.Errorf("UrgentBypass=false at 23:00: urgent ShouldEmit returned true; should defer")
	}

	outsideQuiet := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	if !inbox.ShouldEmit(urgent, cfg, outsideQuiet) {
		t.Errorf("UrgentBypass=false at 10:00: urgent ShouldEmit returned false; should emit (post-quiet)")
	}
}
