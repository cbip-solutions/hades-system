package scheduler_test

import (
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestComputeMissed_NeverRun(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  7 * 24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     now.Add(-1 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(LastRunAt zero) MissedCount = %d, want 0", got.MissedCount)
	}
	if got.ScheduleID != s.ID {
		t.Errorf("ScheduleID = %q, want %q", got.ScheduleID, s.ID)
	}
}

func TestComputeMissed_OneHourGap(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-3 * time.Hour),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 2 {
		t.Errorf("ComputeMissed(3h gap, hourly) MissedCount = %d, want 2", got.MissedCount)
	}
	wantFrom := time.Date(2026, 5, 1, 6, 0, 0, 0, time.UTC)
	if !got.From.Equal(wantFrom) {
		t.Errorf("From = %v, want %v (first missed tick)", got.From, wantFrom)
	}
	if !got.To.Equal(now) {
		t.Errorf("To = %v, want %v (now)", got.To, now)
	}
	if got.LookbackUsed != 3*time.Hour {
		t.Errorf("LookbackUsed = %v, want 3h (gap < lookback ⇒ gap)", got.LookbackUsed)
	}
}

func TestComputeMissed_LookbackClamps(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  5 * time.Minute,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-1 * time.Hour),
		CreatedAt:     now.Add(-2 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 4 {
		t.Errorf("ComputeMissed(60min gap, lookback 5min) MissedCount = %d, want 4", got.MissedCount)
	}
	if got.LookbackUsed != 5*time.Minute {
		t.Errorf("LookbackUsed = %v, want 5min (clamped)", got.LookbackUsed)
	}
}

func TestComputeMissed_HTTPTriggerNeverMisses(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{BearerTokenHash: "x"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  7 * 24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-100 * time.Hour),
		CreatedAt:     now.Add(-200 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("HTTP trigger MissedCount = %d, want 0", got.MissedCount)
	}
	if got.ScheduleID != s.ID {
		t.Errorf("ScheduleID = %q, want %q", got.ScheduleID, s.ID)
	}
}

func TestComputeMissed_ClockSkew(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(time.Hour),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(now < LastRunAt) MissedCount = %d, want 0", got.MissedCount)
	}
}

func TestComputeMissed_NowEqualsLastRun(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now,
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(now == LastRunAt) MissedCount = %d, want 0", got.MissedCount)
	}
}

func TestComputeMissed_HundredFires(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  200 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-100 * time.Hour),
		CreatedAt:     now.Add(-200 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 99 {
		t.Errorf("ComputeMissed(100h gap, hourly) MissedCount = %d, want 99", got.MissedCount)
	}
}

func TestComputeMissed_OneFire(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-1 * time.Hour),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(1h gap, hourly) MissedCount = %d, want 0 (only tick is current)", got.MissedCount)
	}
}

func TestComputeMissed_BadCronExpr(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "this is not a cron"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-3 * time.Hour),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(bad cron) MissedCount = %d, want 0 (graceful)", got.MissedCount)
	}
}

func TestComputeMissed_GitPoll(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		TriggerType:  scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:             "https://github.com/owner/repo",
			Branch:              "main",
			PollIntervalSeconds: 600,
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: 24 * time.Hour,
		Status:       scheduler.StatusEnabled,
		LastRunAt:    now.Add(-1 * time.Hour),
		CreatedAt:    now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 5 {
		t.Errorf("ComputeMissed(git-poll 60min gap, 10min interval) MissedCount = %d, want 5", got.MissedCount)
	}
}

func TestComputeMissed_GitPoll_FloorBelowOneMinute(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		TriggerType:  scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:             "https://github.com/owner/repo",
			Branch:              "main",
			PollIntervalSeconds: 30,
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: 24 * time.Hour,
		Status:       scheduler.StatusEnabled,
		LastRunAt:    now.Add(-10 * time.Minute),
		CreatedAt:    now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 9 {
		t.Errorf("ComputeMissed(git-poll sub-min interval) MissedCount = %d, want 9 (floored)", got.MissedCount)
	}
}

func TestComputeMissed_GitPoll_LookbackClamps(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		TriggerType:  scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:             "https://github.com/owner/repo",
			Branch:              "main",
			PollIntervalSeconds: 60,
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: 5 * time.Minute,
		Status:       scheduler.StatusEnabled,
		LastRunAt:    now.Add(-1 * time.Hour),
		CreatedAt:    now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 4 {
		t.Errorf("ComputeMissed(git-poll 60min, lookback 5min) MissedCount = %d, want 4", got.MissedCount)
	}
	if got.LookbackUsed != 5*time.Minute {
		t.Errorf("LookbackUsed = %v, want 5min (clamped)", got.LookbackUsed)
	}
}

func TestComputeMissed_NoLookback(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  0,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     now.Add(-3 * time.Hour),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)

	if got.MissedCount != 2 {
		t.Errorf("ComputeMissed(no lookback) MissedCount = %d, want 2", got.MissedCount)
	}
	if got.LookbackUsed != 3*time.Hour {
		t.Errorf("LookbackUsed = %v, want 3h (gap, no clamp)", got.LookbackUsed)
	}
}

func TestComputeMissed_NoTicksInWindow(t *testing.T) {
	now := time.Date(2026, 5, 1, 7, 45, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:            "id",
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  24 * time.Hour,
		Status:        scheduler.StatusEnabled,
		LastRunAt:     time.Date(2026, 5, 1, 7, 30, 0, 0, time.UTC),
		CreatedAt:     now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(no ticks in window) MissedCount = %d, want 0 (clamped from -1)", got.MissedCount)
	}
	if !got.From.IsZero() {
		t.Errorf("From = %v, want zero (no ticks ⇒ no first-missed-tick)", got.From)
	}
}

func TestComputeMissed_GitPoll_GapBelowInterval(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		TriggerType:  scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL:             "https://github.com/owner/repo",
			Branch:              "main",
			PollIntervalSeconds: 60,
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: 24 * time.Hour,
		Status:       scheduler.StatusEnabled,
		LastRunAt:    now.Add(-30 * time.Second),
		CreatedAt:    now.Add(-24 * time.Hour),
	}
	got := scheduler.ComputeMissed(s, now)
	if got.MissedCount != 0 {
		t.Errorf("ComputeMissed(git-poll gap<interval) MissedCount = %d, want 0 (clamped from -1)", got.MissedCount)
	}
}

func TestComputeMissed_PolicyAgnostic(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	policies := []scheduler.MissPolicy{
		scheduler.MissPolicySkip,
		scheduler.MissPolicyCatchUpBounded,
		scheduler.MissPolicyCoalesce,
		scheduler.MissPolicyNotifyOnly,
	}
	for _, p := range policies {
		s := &scheduler.Schedule{
			ID:            "id",
			ProjectAlias:  "p",
			Action:        "a",
			TriggerType:   scheduler.TriggerCron,
			TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
			MissPolicy:    p,
			MissLookback:  24 * time.Hour,
			Status:        scheduler.StatusEnabled,
			LastRunAt:     now.Add(-3 * time.Hour),
			CreatedAt:     now.Add(-24 * time.Hour),
		}
		got := scheduler.ComputeMissed(s, now)
		if got.MissedCount != 2 {
			t.Errorf("ComputeMissed(policy=%v) MissedCount = %d, want 2 (policy-agnostic)", p, got.MissedCount)
		}
	}
}

func TestCoalesce_PolicyCoalesce_Aggregates(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		MissPolicy: scheduler.MissPolicyCoalesce,
		LastRunAt:  now.Add(-3 * time.Hour),
	}
	missed := scheduler.MissedFire{
		ScheduleID:   "id",
		MissedCount:  2,
		LookbackUsed: 24 * time.Hour,
		From:         s.LastRunAt.Add(time.Hour),
		To:           now,
	}
	got, ok := scheduler.Coalesce(s, missed)
	if !ok {
		t.Fatalf("Coalesce(MissPolicyCoalesce, missed=2) ok = false, want true")
	}
	if !got.From.Equal(missed.From) {
		t.Errorf("From = %v, want %v", got.From, missed.From)
	}
	if !got.To.Equal(missed.To) {
		t.Errorf("To = %v, want %v", got.To, missed.To)
	}
}

func TestCoalesce_PolicyCatchUpBounded_NoCoalesce(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := &scheduler.Schedule{
		MissPolicy: scheduler.MissPolicyCatchUpBounded,
		LastRunAt:  now.Add(-3 * time.Hour),
	}
	missed := scheduler.MissedFire{MissedCount: 2}
	got, ok := scheduler.Coalesce(s, missed)
	if ok {
		t.Errorf("Coalesce(CatchUpBounded) ok = true, want false (N individual fires expected)")
	}
	if !got.From.IsZero() || !got.To.IsZero() {
		t.Errorf("Coalesce(CatchUpBounded) returned non-zero window %+v, want zero", got)
	}
}

func TestCoalesce_PolicySkip_NoCoalesce(t *testing.T) {
	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicySkip}
	missed := scheduler.MissedFire{MissedCount: 5}
	_, ok := scheduler.Coalesce(s, missed)
	if ok {
		t.Errorf("Coalesce(Skip) = true, want false")
	}
}

func TestCoalesce_PolicyNotifyOnly_NoCoalesce(t *testing.T) {
	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicyNotifyOnly}
	missed := scheduler.MissedFire{MissedCount: 5}
	_, ok := scheduler.Coalesce(s, missed)
	if ok {
		t.Errorf("Coalesce(NotifyOnly) = true, want false")
	}
}

func TestCoalesce_NoMissedFires(t *testing.T) {
	s := &scheduler.Schedule{MissPolicy: scheduler.MissPolicyCoalesce}
	missed := scheduler.MissedFire{MissedCount: 0}
	got, ok := scheduler.Coalesce(s, missed)
	if ok {
		t.Errorf("Coalesce(missed=0) ok = true, want false")
	}
	if !got.From.IsZero() || !got.To.IsZero() {
		t.Errorf("Coalesce(missed=0) returned non-zero window %+v, want zero", got)
	}
}

func TestCoalesce_NilSchedule(t *testing.T) {
	missed := scheduler.MissedFire{MissedCount: 5}
	got, ok := scheduler.Coalesce(nil, missed)
	if ok {
		t.Errorf("Coalesce(nil) ok = true, want false")
	}
	if !got.From.IsZero() || !got.To.IsZero() {
		t.Errorf("Coalesce(nil) returned non-zero window %+v, want zero", got)
	}
}
