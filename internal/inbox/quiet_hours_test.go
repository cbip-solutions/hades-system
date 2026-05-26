package inbox

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func mkQuietNotification(sv Severity, at time.Time) Notification {
	return Notification{
		ProjectID:   "a" + strings.Repeat("0", 63),
		Severity:    sv,
		EventType:   "x.y",
		ContentHash: ComputeContentHash(map[string]any{"k": at.String()}),
		Payload:     json.RawMessage(`{}`),
		CreatedAt:   at,
	}
}

func defaultQuiet() QuietHours {
	return QuietHours{
		Start:           21 * time.Hour,
		End:             9 * time.Hour,
		WeekendExtended: true,
		UrgentBypass:    true,
	}
}

func TestShouldEmitUrgentAlwaysBypassesQuiet(t *testing.T) {
	cfg := QuietConfig{Default: defaultQuiet()}

	for day := 0; day < 7; day++ {
		for h := 0; h < 24; h++ {
			now := time.Date(2026, 5, 1+day, h, 0, 0, 0, time.UTC)
			n := mkQuietNotification(SeverityUrgent, now)
			if !ShouldEmit(n, cfg, now) {
				t.Errorf("urgent at day=%d hour=%d should always emit (inv-zen-125)", day, h)
			}
		}
	}
}

func TestShouldEmitOutsideQuietHoursAllSeverities(t *testing.T) {
	cfg := QuietConfig{Default: defaultQuiet()}

	now := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)
	for _, sv := range AllSeverities() {
		n := mkQuietNotification(sv, now)
		if !ShouldEmit(n, cfg, now) {
			t.Errorf("severity %q outside quiet hours should emit", sv)
		}
	}
}

func TestShouldEmitInsideQuietHoursDefersNonUrgent(t *testing.T) {
	cfg := QuietConfig{Default: defaultQuiet()}

	now := time.Date(2026, 5, 1, 22, 30, 0, 0, time.UTC)
	for _, sv := range AllSeverities() {
		n := mkQuietNotification(sv, now)
		if sv == SeverityUrgent {
			if !ShouldEmit(n, cfg, now) {
				t.Errorf("urgent must bypass quiet hours")
			}
		} else if ShouldEmit(n, cfg, now) {
			t.Errorf("non-urgent severity %q should defer in quiet hours", sv)
		}
	}
}

func TestShouldEmitWrappingMidnight(t *testing.T) {
	cfg := QuietConfig{Default: defaultQuiet()}

	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	if ShouldEmit(mkQuietNotification(SeverityActionNeeded, now), cfg, now) {
		t.Error("3am must be inside quiet hours (wraps midnight)")
	}

	now = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	if !ShouldEmit(mkQuietNotification(SeverityActionNeeded, now), cfg, now) {
		t.Error("09:00 (end exclusive) should emit non-urgent")
	}

	now = time.Date(2026, 5, 1, 21, 0, 0, 0, time.UTC)
	if ShouldEmit(mkQuietNotification(SeverityActionNeeded, now), cfg, now) {
		t.Error("21:00 sharp should defer non-urgent")
	}
}

func TestShouldEmitWeekendExtended(t *testing.T) {
	cfg := QuietConfig{Default: defaultQuiet()}

	for h := 0; h < 24; h++ {
		now := time.Date(2026, 5, 2, h, 0, 0, 0, time.UTC)
		if ShouldEmit(mkQuietNotification(SeverityInfoDigest, now), cfg, now) {
			t.Errorf("Saturday %02d:00 should defer non-urgent (WeekendExtended)", h)
		}
		if !ShouldEmit(mkQuietNotification(SeverityUrgent, now), cfg, now) {
			t.Errorf("Saturday %02d:00 urgent must bypass", h)
		}
	}
}

func TestShouldEmitWeekendNotExtended(t *testing.T) {
	q := defaultQuiet()
	q.WeekendExtended = false
	cfg := QuietConfig{Default: q}

	now := time.Date(2026, 5, 2, 14, 0, 0, 0, time.UTC)
	if !ShouldEmit(mkQuietNotification(SeverityActionNeeded, now), cfg, now) {
		t.Error("Saturday 14:00 with WeekendExtended=false should emit (outside hours)")
	}
}

func TestShouldEmitPerProjectOverride(t *testing.T) {
	cfg := QuietConfig{
		Default: defaultQuiet(),
		PerProject: map[string]QuietHours{
			"a" + strings.Repeat("0", 63): {
				Start:        0,
				End:          0,
				UrgentBypass: true,
			},
		},
	}

	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	if !ShouldEmit(mkQuietNotification(SeverityActionNeeded, now), cfg, now) {
		t.Error("per-project override (no quiet) should emit during default-quiet window")
	}
}

func TestShouldEmitUrgentPauseDisablesBypass(t *testing.T) {
	q := defaultQuiet()
	cfg := QuietConfig{Default: q}

	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)

	if !ShouldEmit(mkQuietNotification(SeverityUrgent, now), cfg, now) {
		t.Fatal("baseline: urgent should emit")
	}

	until := now.Add(30 * time.Minute)
	cfg.UrgentPauseUntil = &until

	if ShouldEmit(mkQuietNotification(SeverityUrgent, now), cfg, now) {
		t.Error("urgent-pause active: urgent must defer")
	}

	after := until.Add(time.Second)
	if !ShouldEmit(mkQuietNotification(SeverityUrgent, after), cfg, after) {
		t.Error("after urgent-pause expiry: urgent must emit again")
	}
}

func TestShouldEmitUrgentBypassDisabledByConfig(t *testing.T) {
	q := defaultQuiet()
	q.UrgentBypass = false
	cfg := QuietConfig{Default: q}

	now := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)

	if ShouldEmit(mkQuietNotification(SeverityUrgent, now), cfg, now) {
		t.Error("urgent-bypass=false: urgent must defer in quiet hours")
	}
}

func TestShouldEmitUrgentBypassDisabledOutsideQuietEmits(t *testing.T) {
	q := defaultQuiet()
	q.UrgentBypass = false
	q.WeekendExtended = false
	cfg := QuietConfig{Default: q}

	now := time.Date(2026, 5, 1, 14, 0, 0, 0, time.UTC)

	if !ShouldEmit(mkQuietNotification(SeverityUrgent, now), cfg, now) {
		t.Error("urgent-bypass=false outside quiet hours: must still emit")
	}
}

func TestInQuietHoursSameDayWindow(t *testing.T) {

	q := QuietHours{
		Start: 13 * time.Hour,
		End:   17 * time.Hour,
	}

	mid := time.Date(2026, 5, 1, 15, 0, 0, 0, time.UTC)
	if !InQuietHours(q, mid) {
		t.Error("15:00 should be inside same-day window 13..17")
	}

	before := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if InQuietHours(q, before) {
		t.Error("12:00 should be outside same-day window 13..17")
	}

	after := time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC)
	if InQuietHours(q, after) {
		t.Error("18:00 should be outside same-day window 13..17")
	}

	startSharp := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	if !InQuietHours(q, startSharp) {
		t.Error("13:00 sharp should be inside same-day window (start inclusive)")
	}

	endSharp := time.Date(2026, 5, 1, 17, 0, 0, 0, time.UTC)
	if InQuietHours(q, endSharp) {
		t.Error("17:00 sharp should be outside same-day window (end exclusive)")
	}
}

func TestLoadQuietConfigErrorOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nonexistent.toml"
	_, err := LoadQuietConfig(path)
	if err == nil {
		t.Fatal("LoadQuietConfig must fail on missing file")
	}
	if !errors.Is(err, ErrInvalidQuietConfig) {
		t.Errorf("expected ErrInvalidQuietConfig wrapping read error, got: %v", err)
	}
}

func TestLoadQuietConfigErrorOnMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notifications.toml"

	src := `[quiet_hours
start = "21:00"
`
	if err := writeFile(t, path, src); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadQuietConfig(path)
	if err == nil {
		t.Fatal("LoadQuietConfig must reject malformed TOML")
	}
	if !errors.Is(err, ErrInvalidQuietConfig) {
		t.Errorf("expected ErrInvalidQuietConfig wrapping toml parse error, got: %v", err)
	}
}

func TestLoadQuietConfigErrorOnInvalidEndFormat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notifications.toml"

	src := `
[quiet_hours]
start = "21:00"
end   = "not-a-time"
`
	if err := writeFile(t, path, src); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadQuietConfig(path)
	if err == nil {
		t.Fatal("LoadQuietConfig must reject invalid end time format")
	}
	if !errors.Is(err, ErrInvalidQuietConfig) {
		t.Errorf("expected ErrInvalidQuietConfig wrapping end parse error, got: %v", err)
	}
}

func TestQuietHoursUrgentBypassSentinelReturnsErr(t *testing.T) {
	if !errors.Is(quietHoursUrgentBypassSentinel(), ErrQuietHoursUrgentBypassAnchor) {
		t.Fatal("expected ErrQuietHoursUrgentBypassAnchor")
	}
}

func TestLoadQuietConfigParsesTOML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notifications.toml"
	src := `
[quiet_hours]
start = "21:00"
end   = "09:00"
weekend_extended = true
urgent_bypass = true
`
	if err := writeFile(t, path, src); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadQuietConfig(path)
	if err != nil {
		t.Fatalf("LoadQuietConfig: %v", err)
	}
	if cfg.Default.Start != 21*time.Hour {
		t.Errorf("Start = %v, want 21h", cfg.Default.Start)
	}
	if cfg.Default.End != 9*time.Hour {
		t.Errorf("End = %v, want 9h", cfg.Default.End)
	}
	if !cfg.Default.WeekendExtended {
		t.Errorf("WeekendExtended = false, want true")
	}
	if !cfg.Default.UrgentBypass {
		t.Errorf("UrgentBypass = false, want true")
	}
}

func TestLoadQuietConfigErrorOnInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notifications.toml"
	src := `
[quiet_hours]
start = "not-a-time"
end   = "09:00"
`
	if err := writeFile(t, path, src); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadQuietConfig(path)
	if err == nil {
		t.Fatal("LoadQuietConfig must reject invalid time format")
	}
	if !errors.Is(err, ErrInvalidQuietConfig) {
		t.Errorf("expected ErrInvalidQuietConfig, got: %v", err)
	}
}

func writeFile(t *testing.T, path, contents string) error {
	t.Helper()
	return writeFileImpl(path, contents)
}
