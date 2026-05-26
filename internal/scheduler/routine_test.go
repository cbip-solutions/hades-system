package scheduler_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestRoutine_Plan_AppliesJitter(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  7 * 24 * time.Hour,
	}
	r, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err != nil {
		t.Fatalf("NewRoutine: %v", err)
	}
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	got := r.Plan(now)

	base := time.Date(2026, 5, 2, 8, 0, 0, 0, time.UTC)
	if got.Before(base) || got.After(base.Add(15*time.Minute)) {
		t.Errorf("Plan = %v, want [%v..%v]", got, base, base.Add(15*time.Minute))
	}
}

func TestRoutine_Plan_Deterministic(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "deterministic-id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
	}
	r, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err != nil {
		t.Fatalf("NewRoutine: %v", err)
	}
	now := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	a := r.Plan(now)
	b := r.Plan(now)
	if !a.Equal(b) {
		t.Errorf("Plan not deterministic: %v vs %v", a, b)
	}
}

func TestRoutine_Advance_PastNow(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "*/5 * * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
	}
	r, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err != nil {
		t.Fatalf("NewRoutine: %v", err)
	}
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	r.Advance(now)
	if !r.Schedule().NextRunAt.After(now) {
		t.Errorf("after Advance, NextRunAt = %v, want > now", r.Schedule().NextRunAt)
	}
}

func TestRoutine_Advance_MutatesUnderlyingSchedule(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
	}
	r, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err != nil {
		t.Fatalf("NewRoutine: %v", err)
	}
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	r.Advance(now)
	if s.NextRunAt.IsZero() {
		t.Errorf("underlying Schedule.NextRunAt not mutated; got zero")
	}
	if !s.NextRunAt.Equal(r.Schedule().NextRunAt) {
		t.Errorf("Schedule() returned different pointer: %v vs %v",
			s.NextRunAt, r.Schedule().NextRunAt)
	}
}

func TestRoutine_BadCronRejected(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "@hourly"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
	}
	_, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err == nil {
		t.Fatalf("NewRoutine(@hourly) = nil error, want ErrInvalidCron")
	}
	if !errors.Is(err, scheduler.ErrInvalidCron) {
		t.Errorf("NewRoutine(@hourly) error = %v, want errors.Is(ErrInvalidCron)", err)
	}
}

func TestRoutine_NonCronRejected(t *testing.T) {
	s := &scheduler.Schedule{
		ID:           "id",
		Tier:         scheduler.TierRoutine,
		TriggerType:  scheduler.TriggerHTTP,
		ProjectAlias: "p",
		Action:       "a",
		CreatedAt:    time.Now(),
		Status:       scheduler.StatusEnabled,
		MissPolicy:   scheduler.MissPolicySkip,
	}
	_, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err == nil {
		t.Fatalf("NewRoutine(HTTP) = nil error, want ErrInvalidSchedule")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("NewRoutine(HTTP) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}
}

func TestRoutine_NilScheduleRejected(t *testing.T) {
	_, err := scheduler.NewRoutine(nil, doctrine.NameDefault)
	if err == nil {
		t.Fatalf("NewRoutine(nil) = nil error, want ErrInvalidSchedule")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("NewRoutine(nil) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}
}

func TestRoutine_WrongTierRejected(t *testing.T) {
	for _, wrong := range []scheduler.Tier{scheduler.TierTask, scheduler.TierLoop} {
		s := &scheduler.Schedule{
			ID:            "id",
			Tier:          wrong,
			TriggerType:   scheduler.TriggerCron,
			TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
			Status:        scheduler.StatusEnabled,
			ProjectAlias:  "p",
			Action:        "a",
			CreatedAt:     time.Now(),
			MissPolicy:    scheduler.MissPolicySkip,
		}
		_, err := scheduler.NewRoutine(s, doctrine.NameDefault)
		if err == nil {
			t.Errorf("NewRoutine(%v) = nil error, want ErrInvalidSchedule", wrong)
			continue
		}
		if !errors.Is(err, scheduler.ErrInvalidSchedule) {
			t.Errorf("NewRoutine(%v) error = %v, want errors.Is(ErrInvalidSchedule)",
				wrong, err)
		}
	}
}

func TestRoutine_InvalidScheduleRejected(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 * * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
	}
	_, err := scheduler.NewRoutine(s, doctrine.NameDefault)
	if err == nil {
		t.Fatalf("NewRoutine(empty alias) = nil error, want ErrInvalidSchedule")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("NewRoutine(empty alias) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}
}

func TestRoutine_DoctrineGranularityFloor(t *testing.T) {
	s := &scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		Status:        scheduler.StatusEnabled,
		ProjectAlias:  "p",
		Action:        "a",
		CreatedAt:     time.Now(),
		MissPolicy:    scheduler.MissPolicySkip,
	}
	_, err := scheduler.NewRoutine(s, doctrine.NameCapaFirewall)
	if err == nil {
		t.Fatalf("NewRoutine(* * * * *, capa-firewall) = nil error, want ErrInvalidCron")
	}
	if !errors.Is(err, scheduler.ErrInvalidCron) {
		t.Errorf("NewRoutine(*, capa-firewall) error = %v, want errors.Is(ErrInvalidCron)", err)
	}

	if _, err := scheduler.NewRoutine(s, doctrine.NameDefault); err != nil {
		t.Errorf("NewRoutine(*, default) = %v, want nil", err)
	}
}
