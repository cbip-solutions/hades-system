package scheduler_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestNewTask_AppliesJitter(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		ProjectAlias: "p",
		Action:       "a",
		FireAt:       now.Add(30 * time.Minute),
		MissPolicy:   scheduler.MissPolicySkip,
		MissLookback: time.Hour,
	}, now)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if s.Tier != scheduler.TierTask {
		t.Errorf("Tier = %v, want TierTask", s.Tier)
	}
	if s.Status != scheduler.StatusEnabled {
		t.Errorf("Status = %v, want StatusEnabled", s.Status)
	}
	want := now.Add(30 * time.Minute)
	if s.NextRunAt.Before(want) || s.NextRunAt.After(want.Add(90*time.Second)) {
		t.Errorf("NextRunAt = %v, want [%v..%v] (one-shot 90s cap)",
			s.NextRunAt, want, want.Add(90*time.Second))
	}
}

func TestNewTask_RecurringPeriodCap(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	want := now.Add(2 * time.Hour)
	s, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           "two-hour-task",
		ProjectAlias: "p",
		Action:       "a",
		FireAt:       want,
		MissPolicy:   scheduler.MissPolicySkip,
	}, now)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if s.NextRunAt.Before(want) || s.NextRunAt.After(want.Add(15*time.Minute)) {
		t.Errorf("NextRunAt = %v, want [%v..%v] (recurring 15min cap)",
			s.NextRunAt, want, want.Add(15*time.Minute))
	}
}

func TestNewTask_Deterministic(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	mk := func() *scheduler.Schedule {
		s, err := scheduler.NewTask(scheduler.TaskParams{
			ID:           "deterministic-id",
			ProjectAlias: "p",
			Action:       "a",
			FireAt:       now.Add(45 * time.Minute),
			MissPolicy:   scheduler.MissPolicySkip,
		}, now)
		if err != nil {
			t.Fatalf("NewTask: %v", err)
		}
		return s
	}
	a, b := mk(), mk()
	if !a.NextRunAt.Equal(b.NextRunAt) {
		t.Errorf("NewTask not deterministic: %v vs %v", a.NextRunAt, b.NextRunAt)
	}
}

// TestNewTask_RejectsPastFireAt guards against operator typo /
// timezone-confusion: a FireAt at-or-before now must be refused. The
// dispatcher MUST NOT see a Task with NextRunAt <= now from
// construction (that would fire immediately, which is not the
// "ephemeral one-shot at scheduled time" contract).
func TestNewTask_RejectsPastFireAt(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		fireAt time.Time
	}{
		{"past", now.Add(-1 * time.Hour)},
		{"now-exact", now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scheduler.NewTask(scheduler.TaskParams{
				ID:           "id",
				ProjectAlias: "p",
				Action:       "a",
				FireAt:       tc.fireAt,
				MissPolicy:   scheduler.MissPolicySkip,
			}, now)
			if err == nil {
				t.Fatalf("NewTask(%v) = nil error, want ErrInvalidSchedule", tc.name)
			}
			if !errors.Is(err, scheduler.ErrInvalidSchedule) {
				t.Errorf("NewTask(%v) error = %v, want errors.Is(ErrInvalidSchedule)",
					tc.name, err)
			}
		})
	}
}

func TestNewTask_RejectsEmptyFields(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	base := scheduler.TaskParams{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		FireAt:       now.Add(30 * time.Minute),
		MissPolicy:   scheduler.MissPolicySkip,
	}
	cases := []struct {
		name  string
		mut   func(p *scheduler.TaskParams)
		match string
	}{
		{"empty ID", func(p *scheduler.TaskParams) { p.ID = "" }, "ID"},
		{"empty ProjectAlias", func(p *scheduler.TaskParams) { p.ProjectAlias = "" }, "ProjectAlias"},
		{"empty Action", func(p *scheduler.TaskParams) { p.Action = "" }, "Action"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mut(&p)
			_, err := scheduler.NewTask(p, now)
			if err == nil {
				t.Fatalf("NewTask(%v) = nil error, want ErrInvalidSchedule", tc.name)
			}
			if !errors.Is(err, scheduler.ErrInvalidSchedule) {
				t.Errorf("NewTask(%v) error = %v, want errors.Is(ErrInvalidSchedule)",
					tc.name, err)
			}
			if !strings.Contains(err.Error(), tc.match) {
				t.Errorf("NewTask(%v) error = %q, want substring %q",
					tc.name, err.Error(), tc.match)
			}
		})
	}
}

// TestNewTask_PopulatesContract verifies every required field on the
// returned Schedule is set, so a Validate() call on the returned row
// passes without further mutation. (Adapters MUST NOT need to backfill
// fields after construction.)
func TestNewTask_PopulatesContract(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	fireAt := now.Add(30 * time.Minute)
	s, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           "id",
		ProjectAlias: "internal-platform-x",
		Action:       "morning-brief",
		FireAt:       fireAt,
		MissPolicy:   scheduler.MissPolicyNotifyOnly,
		MissLookback: 6 * time.Hour,
	}, now)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if s.ID != "id" {
		t.Errorf("ID = %q, want %q", s.ID, "id")
	}
	if s.ProjectAlias != "internal-platform-x" {
		t.Errorf("ProjectAlias = %q, want %q", s.ProjectAlias, "internal-platform-x")
	}
	if s.Action != "morning-brief" {
		t.Errorf("Action = %q, want %q", s.Action, "morning-brief")
	}
	if s.TriggerType != scheduler.TriggerCron {
		t.Errorf("TriggerType = %v, want TriggerCron (synthetic sentinel)", s.TriggerType)
	}
	if s.TriggerConfig.CronExpr == "" {
		t.Errorf("TriggerConfig.CronExpr empty; want sentinel for Validate()")
	}
	if s.MissPolicy != scheduler.MissPolicyNotifyOnly {
		t.Errorf("MissPolicy = %v, want MissPolicyNotifyOnly", s.MissPolicy)
	}
	if s.MissLookback != 6*time.Hour {
		t.Errorf("MissLookback = %v, want 6h", s.MissLookback)
	}
	if !s.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", s.CreatedAt, now)
	}

	if err := s.Validate(); err != nil {
		t.Errorf("returned Schedule fails Validate: %v", err)
	}
}

func TestNewTask_DueAtSemantics(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		FireAt:       now.Add(30 * time.Minute),
		MissPolicy:   scheduler.MissPolicySkip,
	}, now)
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if s.DueAt(now) {
		t.Errorf("DueAt(now) = true at construction; want false (NextRunAt in future)")
	}
	if !s.DueAt(s.NextRunAt) {
		t.Errorf("DueAt(NextRunAt) = false; want true (== boundary fires)")
	}
	if !s.DueAt(s.NextRunAt.Add(time.Hour)) {
		t.Errorf("DueAt(future) = false; want true (NextRunAt in past)")
	}
}

func TestNewTask_RejectsInvalidMissPolicy(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	_, err := scheduler.NewTask(scheduler.TaskParams{
		ID:           "id",
		ProjectAlias: "p",
		Action:       "a",
		FireAt:       now.Add(30 * time.Minute),
		MissPolicy:   scheduler.MissPolicy(99),
	}, now)
	if err == nil {
		t.Fatalf("NewTask(bogus MissPolicy) = nil error, want ErrInvalidSchedule")
	}
	if !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("NewTask(bogus MissPolicy) error = %v, want errors.Is(ErrInvalidSchedule)", err)
	}
}

func TestMarkTaskFired_AutoDisables(t *testing.T) {
	s := &scheduler.Schedule{
		Tier:   scheduler.TierTask,
		Status: scheduler.StatusEnabled,
	}
	scheduler.MarkTaskFired(s)
	if s.Status != scheduler.StatusDisabled {
		t.Errorf("MarkTaskFired Status = %v, want StatusDisabled", s.Status)
	}
}

func TestMarkTaskFired_FromFailed(t *testing.T) {
	s := &scheduler.Schedule{
		Tier:   scheduler.TierTask,
		Status: scheduler.StatusFailed,
	}
	scheduler.MarkTaskFired(s)
	if s.Status != scheduler.StatusDisabled {
		t.Errorf("MarkTaskFired (from Failed) Status = %v, want StatusDisabled", s.Status)
	}
}

func TestMarkTaskFired_NoOpOnRoutine(t *testing.T) {
	for _, tier := range []scheduler.Tier{scheduler.TierRoutine, scheduler.TierLoop} {
		s := &scheduler.Schedule{Tier: tier, Status: scheduler.StatusEnabled}
		scheduler.MarkTaskFired(s)
		if s.Status != scheduler.StatusEnabled {
			t.Errorf("MarkTaskFired(%v) mutated Status to %v; want unchanged StatusEnabled",
				tier, s.Status)
		}
	}
}

func TestMarkTaskFired_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MarkTaskFired(nil) panicked: %v", r)
		}
	}()
	scheduler.MarkTaskFired(nil)
}
