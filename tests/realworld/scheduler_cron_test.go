// go:build realworld
//go:build realworld
// +build realworld

package realworld

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestRealworld_CronTicksWeekdayMornings(t *testing.T) {
	expr, err := scheduler.ParseCron("0 8 * * 1-5", doctrine.NameDefault)
	if err != nil {
		t.Fatalf("ParseCron(0 8 * * 1-5): %v", err)
	}
	cursor := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	want := []time.Time{
		time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 6, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 7, 8, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 8, 8, 0, 0, 0, time.UTC),
	}
	for i, w := range want {
		next := expr.Next(cursor)
		if !next.Equal(w) {
			t.Errorf("tick %d = %v, want %v (cursor=%v)", i, next, w, cursor)
		}
		cursor = next
	}
}

func TestRealworld_CronTicksHourly(t *testing.T) {
	expr, err := scheduler.ParseCron("0 * * * *", doctrine.NameDefault)
	if err != nil {
		t.Fatalf("ParseCron(0 * * * *): %v", err)
	}
	cursor := time.Date(2026, 5, 1, 0, 30, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		next := expr.Next(cursor)
		want := cursor.Add(time.Hour - 30*time.Minute)
		if i > 0 {
			want = cursor.Add(time.Hour)
		}
		if !next.Equal(want) {
			t.Errorf("tick %d = %v, want %v (cursor=%v)", i, next, want, cursor)
		}
		cursor = next
	}
}

func TestRealworld_CronJitterRoundTrip(t *testing.T) {
	expr, err := scheduler.ParseCron("0 8 * * *", doctrine.NameDefault)
	if err != nil {
		t.Fatalf("ParseCron: %v", err)
	}
	cursor := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	next := expr.Next(cursor)
	period := next.Sub(cursor)

	jitter := scheduler.ComputeJitter("01HZ7K8M9P2Q3R4S5T6V7W8X9Y", period)
	if jitter < 0 {
		t.Errorf("inv-zen-120 violated: jitter %v < 0", jitter)
	}
	if jitter > 15*time.Minute {
		t.Errorf("inv-zen-120 violated: jitter %v > 15min recurring cap", jitter)
	}

	again := scheduler.ComputeJitter("01HZ7K8M9P2Q3R4S5T6V7W8X9Y", period)
	if again != jitter {
		t.Errorf("inv-zen-120 violated: realworld determinism failed; %v vs %v",
			jitter, again)
	}

	finalRun := next.Add(jitter)
	if !finalRun.After(cursor) {
		t.Errorf("realworld: finalRun %v not after cursor %v", finalRun, cursor)
	}
	if finalRun.After(next.Add(15 * time.Minute)) {
		t.Errorf("realworld: finalRun %v exceeds next+15min %v", finalRun, next.Add(15*time.Minute))
	}
}

func TestRealworld_CronEvery30Min_MaxScopeFloor(t *testing.T) {
	expr, err := scheduler.ParseCron("*/30 * * * *", doctrine.NameMaxScope)
	if err != nil {
		t.Fatalf("ParseCron(*/30 max-scope): %v", err)
	}
	cursor := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	first := expr.Next(cursor)
	second := expr.Next(first)
	if delta := second.Sub(first); delta != 30*time.Minute {
		t.Errorf("realworld: */30 tick delta %v, want 30min", delta)
	}
}

func TestRealworld_CronCapaFirewallRejectsSubFiveMin(t *testing.T) {
	_, err := scheduler.ParseCron("*/2 * * * *", doctrine.NameCapaFirewall)
	if err == nil {
		t.Errorf("inv-zen-121 / capa-firewall floor violated: ParseCron(*/2, capa-firewall) accepted; want ErrInvalidCron")
	}
}
