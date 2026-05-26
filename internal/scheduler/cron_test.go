package scheduler_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestParseCron_VixieAccepted(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"every minute", "* * * * *"},
		{"08:00 weekdays", "0 8 * * 1-5"},
		{"every 5 min", "*/5 * * * *"},
		{"midnight Sunday", "0 0 * * 0"},
		{"month-end (no L) by day", "0 0 28-31 * *"},
		{"step + range", "0,15,30,45 9-17 * * 1-5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scheduler.ParseCron(tc.expr, scheduler.DoctrineDefault)
			if err != nil {
				t.Fatalf("ParseCron(%q) = %v, want nil", tc.expr, err)
			}
			if got == nil {
				t.Fatalf("ParseCron(%q) returned nil CronExpr", tc.expr)
			}
			if got.Raw != tc.expr {
				t.Errorf("Raw = %q, want %q", got.Raw, tc.expr)
			}
			if got.Schedule == nil {
				t.Errorf("Schedule = nil, want non-nil")
			}
		})
	}
}

func TestParseCron_RejectsExtendedSyntax(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"6-field with seconds", "0 0 8 * * 1-5", "5-field"},
		{"L (last day)", "0 8 L * *", "extended"},
		{"W (weekday)", "0 8 15W * *", "extended"},
		{"? (no-care)", "0 8 ? * 1-5", "extended"},
		{"named month JAN", "0 8 * JAN *", "extended"},
		{"named day MON", "0 8 * * MON", "extended"},
		{"@hourly", "@hourly", "descriptor"},
		{"@daily", "@daily", "descriptor"},
		{"@reboot", "@reboot", "descriptor"},
		{"leading space", " * * * * *", "extended"},
		{"trailing space", "* * * * * ", "extended"},
		{"embedded comment", "* * * * * # ok", "extended"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scheduler.ParseCron(tc.expr, scheduler.DoctrineDefault)
			if err == nil {
				t.Fatalf("ParseCron(%q) = nil error, want %s rejection", tc.expr, tc.want)
			}
			if !errors.Is(err, scheduler.ErrInvalidCron) {
				t.Errorf("ParseCron(%q) err = %v, want ErrInvalidCron wrap", tc.expr, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseCron(%q) err = %v, want substring %q", tc.expr, err, tc.want)
			}
		})
	}
}

func TestParseCron_GranularityFloorByDoctrine(t *testing.T) {

	got, err := scheduler.ParseCron("*/1 * * * *", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("ParseCron */1 default = %v", err)
	}
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	next := got.Next(now)
	if next.Sub(now) < time.Minute {
		t.Errorf("default doctrine 1min floor: next-now = %v, want >= 1min", next.Sub(now))
	}

	if _, err := scheduler.ParseCron("*/1 * * * *", scheduler.DoctrineMaxScope); err != nil {
		t.Errorf("max-scope ParseCron(*/1) = %v, want nil", err)
	}

	// Capa-firewall doctrine: 5min floor. ParseCron MUST reject
	// schedules that would tick more frequently than 5min.
	_, err = scheduler.ParseCron("*/1 * * * *", scheduler.DoctrineCapaFirewall)
	if !errors.Is(err, scheduler.ErrInvalidCron) {
		t.Errorf("capa-firewall ParseCron(*/1) err = %v, want ErrInvalidCron (5min floor)", err)
	}
	_, err = scheduler.ParseCron("*/4 * * * *", scheduler.DoctrineCapaFirewall)
	if !errors.Is(err, scheduler.ErrInvalidCron) {
		t.Errorf("capa-firewall ParseCron(*/4) err = %v, want ErrInvalidCron (4min < 5min floor)", err)
	}
	if _, err := scheduler.ParseCron("*/5 * * * *", scheduler.DoctrineCapaFirewall); err != nil {
		t.Errorf("capa-firewall ParseCron(*/5) = %v, want nil", err)
	}
}

func TestParseCron_NextDeterministic(t *testing.T) {
	got1, err := scheduler.ParseCron("0 8 * * 1-5", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse 1: %v", err)
	}
	got2, err := scheduler.ParseCron("0 8 * * 1-5", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse 2: %v", err)
	}

	after := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	n1 := got1.Next(after)
	n2 := got2.Next(after)
	if !n1.Equal(n2) {
		t.Errorf("Next not deterministic: %v vs %v", n1, n2)
	}
	want := time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC)
	if !n1.Equal(want) {
		t.Errorf("Next = %v, want %v", n1, want)
	}
}

// TestParseCron_TimezoneIsUTC asserts the wall-clock contract: a
// non-UTC `after` MUST yield a UTC `Next`. daemon.db stores unix
// seconds and the scheduler ticks in UTC; mixing zones in the
// CronExpr surface would invite operator-visible drift across DST
// boundaries.
func TestParseCron_TimezoneIsUTC(t *testing.T) {
	got, err := scheduler.ParseCron("0 8 * * *", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	loc, lerr := time.LoadLocation("America/Montevideo")
	if lerr != nil {
		t.Fatalf("LoadLocation: %v", lerr)
	}

	after := time.Date(2026, 5, 1, 10, 0, 0, 0, loc)
	n := got.Next(after)
	if n.Location() != time.UTC {
		t.Errorf("Next.Location = %v, want UTC", n.Location())
	}
	want := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	if !n.Equal(want) {
		t.Errorf("Next = %v, want %v (UTC daily 08:00 after Montevideo 10:00 = 13:00 UTC)", n, want)
	}
}

func TestParseCron_EmptyExprRejected(t *testing.T) {
	_, err := scheduler.ParseCron("", scheduler.DoctrineDefault)
	if !errors.Is(err, scheduler.ErrInvalidCron) {
		t.Errorf("ParseCron(empty) err = %v, want ErrInvalidCron", err)
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("ParseCron(empty) err = %v, want mention of empty", err)
	}
}

func TestParseCron_UnknownDoctrineUsesDefault(t *testing.T) {

	if _, err := scheduler.ParseCron("*/1 * * * *", "unknown"); err != nil {
		t.Errorf("ParseCron(*/1, unknown doctrine) = %v, want nil (default 1min floor)", err)
	}
}

func TestParseCron_FieldCountRejection(t *testing.T) {
	cases := []string{
		"*",
		"* *",
		"* * *",
		"* * * *",
		"* * * * * *",
		"* * * * * * *",
	}
	for _, expr := range cases {
		_, err := scheduler.ParseCron(expr, scheduler.DoctrineDefault)
		if err == nil {
			t.Errorf("ParseCron(%q) = nil, want field-count rejection", expr)
			continue
		}
		if !errors.Is(err, scheduler.ErrInvalidCron) {
			t.Errorf("ParseCron(%q) err = %v, want ErrInvalidCron wrap", expr, err)
		}
		if !strings.Contains(err.Error(), "5-field") {
			t.Errorf("ParseCron(%q) err = %v, want substring %q", expr, err, "5-field")
		}
	}
}

func TestParseCron_LowercaseNamedTokens(t *testing.T) {
	cases := []string{
		"0 8 * * mon",
		"0 8 * jan *",
		"0 8 * * Mon",
		"0 8 * Jan *",
	}
	for _, expr := range cases {
		_, err := scheduler.ParseCron(expr, scheduler.DoctrineDefault)
		if !errors.Is(err, scheduler.ErrInvalidCron) {
			t.Errorf("ParseCron(%q) err = %v, want ErrInvalidCron", expr, err)
		}
	}
}

func TestParseCron_MalformedSyntaxRejected(t *testing.T) {
	cases := []string{
		"60 * * * *",
		"* 25 * * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 8",
		"abc def ghi jkl mno",
		"-1 * * * *",
	}
	for _, expr := range cases {
		_, err := scheduler.ParseCron(expr, scheduler.DoctrineDefault)
		if !errors.Is(err, scheduler.ErrInvalidCron) {
			t.Errorf("ParseCron(%q) err = %v, want ErrInvalidCron wrap", expr, err)
		}
	}
}

func TestCronExpr_NextNilSafe(t *testing.T) {
	var e *scheduler.CronExpr
	got := e.Next(time.Now())
	if !got.IsZero() {
		t.Errorf("nil.Next = %v, want zero time", got)
	}

	empty := &scheduler.CronExpr{}
	got = empty.Next(time.Now())
	if !got.IsZero() {
		t.Errorf("empty.Next = %v, want zero time (nil Schedule)", got)
	}
}

func TestCronExpr_String(t *testing.T) {
	got, err := scheduler.ParseCron("0 8 * * 1-5", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s := got.String(); s != "0 8 * * 1-5" {
		t.Errorf("String() = %q, want %q", s, "0 8 * * 1-5")
	}

	var nilExpr *scheduler.CronExpr
	if s := nilExpr.String(); s != "" {
		t.Errorf("nil.String() = %q, want empty", s)
	}
}

func TestParseCron_DSTBoundary(t *testing.T) {

	got, err := scheduler.ParseCron("0 9 * * *", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	loc, _ := time.LoadLocation("America/New_York")

	after := time.Date(2026, 3, 8, 1, 30, 0, 0, loc)
	n := got.Next(after)
	if n.Location() != time.UTC {
		t.Errorf("Next.Location = %v, want UTC", n.Location())
	}

	want := time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC)
	if !n.Equal(want) {
		t.Errorf("DST: Next = %v, want %v", n, want)
	}
}

func TestParseCron_LeapYearFeb29(t *testing.T) {
	got, err := scheduler.ParseCron("0 0 29 2 *", scheduler.DoctrineDefault)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	after := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	n := got.Next(after)
	want := time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)
	if !n.Equal(want) {
		t.Errorf("leap: Next = %v, want %v", n, want)
	}
}
