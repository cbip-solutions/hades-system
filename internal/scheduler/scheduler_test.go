package scheduler_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
)

func TestScheduleZeroValueDefaults(t *testing.T) {
	var s scheduler.Schedule
	if s.Tier != scheduler.TierRoutine {
		t.Errorf("zero Tier = %v, want TierRoutine (0)", s.Tier)
	}
	if s.Status != scheduler.StatusEnabled {
		t.Errorf("zero Status = %v, want StatusEnabled (0)", s.Status)
	}
	if s.TriggerType != scheduler.TriggerCron {
		t.Errorf("zero TriggerType = %v, want TriggerCron (0)", s.TriggerType)
	}
	if s.MissPolicy != scheduler.MissPolicySkip {
		t.Errorf("zero MissPolicy = %v, want MissPolicySkip (0 = default doctrine)", s.MissPolicy)
	}
}

func TestScheduleValidate_OK(t *testing.T) {
	s := scheduler.Schedule{
		ID:           "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:         scheduler.TierRoutine,
		ProjectAlias: "internal-platform-x",
		Action:       "morning-brief",
		TriggerType:  scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{
			CronExpr: "0 8 * * 1-5",
		},
		MissPolicy:   scheduler.MissPolicyCatchUpBounded,
		MissLookback: 7 * 24 * time.Hour,
		Status:       scheduler.StatusEnabled,
		CreatedAt:    time.Now(),
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate(valid Schedule) = %v, want nil", err)
	}
}

func TestScheduleValidate_BadID(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(empty ID) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_BadAlias(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(empty ProjectAlias) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_BadAction(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(empty Action) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_NegativeLookback(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicyCatchUpBounded,
		MissLookback:  -1 * time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(negative MissLookback) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_NegativeCoalesceWindow(t *testing.T) {
	s := scheduler.Schedule{
		ID:             "id",
		Tier:           scheduler.TierRoutine,
		ProjectAlias:   "p",
		Action:         "a",
		TriggerType:    scheduler.TriggerCron,
		TriggerConfig:  scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:     scheduler.MissPolicyCoalesce,
		MissLookback:   time.Hour,
		CoalesceWindow: -1 * time.Hour,
		Status:         scheduler.StatusEnabled,
		CreatedAt:      time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(negative CoalesceWindow) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_TriggerCronRequiresExpr(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(TriggerCron without CronExpr) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_TriggerHTTPRequiresTokenHash(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(TriggerHTTP without bearer hash) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_TriggerGitPollRequiresRepo(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(TriggerGitPoll without RepoURL) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_UnknownTriggerType(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerType(99),
		TriggerConfig: scheduler.TriggerConfig{},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(unknown TriggerType) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_UnknownMissPolicy(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicy(99),
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(unknown MissPolicy) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_UnknownStatus(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.Status(99),
		CreatedAt:     time.Now(),
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(unknown Status) = %v, want ErrInvalidSchedule", err)
	}
}

func TestScheduleValidate_ZeroCreatedAt(t *testing.T) {
	s := scheduler.Schedule{
		ID:            "id",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "p",
		Action:        "a",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "* * * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		MissLookback:  time.Hour,
		Status:        scheduler.StatusEnabled,
	}
	if err := s.Validate(); !errors.Is(err, scheduler.ErrInvalidSchedule) {
		t.Errorf("Validate(zero CreatedAt) = %v, want ErrInvalidSchedule", err)
	}
}

func TestSchedule_DueAt(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := scheduler.Schedule{
		Status:    scheduler.StatusEnabled,
		NextRunAt: now.Add(-1 * time.Minute),
	}
	if !s.DueAt(now) {
		t.Errorf("DueAt(NextRunAt-1min, now) = false, want true")
	}
	s2 := scheduler.Schedule{
		Status:    scheduler.StatusDisabled,
		NextRunAt: now.Add(-1 * time.Minute),
	}
	if s2.DueAt(now) {
		t.Errorf("DueAt(disabled) = true, want false")
	}
	s3 := scheduler.Schedule{
		Status:    scheduler.StatusEnabled,
		NextRunAt: now.Add(1 * time.Hour),
	}
	if s3.DueAt(now) {
		t.Errorf("DueAt(future) = true, want false")
	}

	s4 := scheduler.Schedule{
		Status: scheduler.StatusEnabled,
	}
	if s4.DueAt(now) {
		t.Errorf("DueAt(zero NextRunAt) = true, want false")
	}
}

func TestSchedule_DueAtBoundary(t *testing.T) {

	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	s := scheduler.Schedule{
		Status:    scheduler.StatusEnabled,
		NextRunAt: now,
	}
	if !s.DueAt(now) {
		t.Errorf("DueAt(NextRunAt==now) = false, want true (≤ boundary)")
	}
}

func TestHistoryEntryValidate(t *testing.T) {
	h := scheduler.HistoryEntry{
		ScheduleID: "s",
		FiredAt:    time.Now(),
		Outcome:    scheduler.OutcomeSuccess,
		DurationMs: 100,
	}
	if err := h.Validate(); err != nil {
		t.Errorf("Validate(valid HistoryEntry) = %v, want nil", err)
	}
	h2 := scheduler.HistoryEntry{ScheduleID: ""}
	if err := h2.Validate(); !errors.Is(err, scheduler.ErrInvalidHistoryEntry) {
		t.Errorf("Validate(empty ScheduleID) = %v, want ErrInvalidHistoryEntry", err)
	}
}

func TestHistoryEntryValidate_ZeroFiredAt(t *testing.T) {
	h := scheduler.HistoryEntry{
		ScheduleID: "s",
		Outcome:    scheduler.OutcomeSuccess,
		DurationMs: 100,
	}
	if err := h.Validate(); !errors.Is(err, scheduler.ErrInvalidHistoryEntry) {
		t.Errorf("Validate(zero FiredAt) = %v, want ErrInvalidHistoryEntry", err)
	}
}

func TestHistoryEntryValidate_NegativeDuration(t *testing.T) {
	h := scheduler.HistoryEntry{
		ScheduleID: "s",
		FiredAt:    time.Now(),
		Outcome:    scheduler.OutcomeSuccess,
		DurationMs: -1,
	}
	if err := h.Validate(); !errors.Is(err, scheduler.ErrInvalidHistoryEntry) {
		t.Errorf("Validate(negative DurationMs) = %v, want ErrInvalidHistoryEntry", err)
	}
}

func TestHistoryEntryValidate_UnknownOutcome(t *testing.T) {
	h := scheduler.HistoryEntry{
		ScheduleID: "s",
		FiredAt:    time.Now(),
		Outcome:    scheduler.Outcome(99),
		DurationMs: 100,
	}
	if err := h.Validate(); !errors.Is(err, scheduler.ErrInvalidHistoryEntry) {
		t.Errorf("Validate(unknown Outcome) = %v, want ErrInvalidHistoryEntry", err)
	}
}

func TestTier_String(t *testing.T) {
	cases := []struct {
		in   scheduler.Tier
		want string
	}{
		{scheduler.TierRoutine, "routine"},
		{scheduler.TierTask, "task"},
		{scheduler.TierLoop, "loop"},
		{scheduler.Tier(99), "tier(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Tier(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestTriggerType_String(t *testing.T) {
	cases := []struct {
		in   scheduler.TriggerType
		want string
	}{
		{scheduler.TriggerCron, "cron"},
		{scheduler.TriggerHTTP, "http"},
		{scheduler.TriggerGitPoll, "git-poll"},
		{scheduler.TriggerType(99), "trigger(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("TriggerType(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestMissPolicy_String(t *testing.T) {
	cases := []struct {
		in   scheduler.MissPolicy
		want string
	}{
		{scheduler.MissPolicySkip, "skip"},
		{scheduler.MissPolicyCatchUpBounded, "catch-up-bounded"},
		{scheduler.MissPolicyCoalesce, "coalesce"},
		{scheduler.MissPolicyNotifyOnly, "notify-only"},
		{scheduler.MissPolicy(99), "miss-policy(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("MissPolicy(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestStatus_String(t *testing.T) {
	cases := []struct {
		in   scheduler.Status
		want string
	}{
		{scheduler.StatusEnabled, "enabled"},
		{scheduler.StatusDisabled, "disabled"},
		{scheduler.StatusFailed, "failed"},
		{scheduler.Status(99), "status(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Status(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestOutcome_String(t *testing.T) {
	cases := []struct {
		in   scheduler.Outcome
		want string
	}{
		{scheduler.OutcomeSuccess, "success"},
		{scheduler.OutcomeFailed, "failed"},
		{scheduler.OutcomeSkipped, "skipped"},
		{scheduler.OutcomeRateLimited, "rate-limited"},
		{scheduler.Outcome(99), "outcome(99)"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestEventKind_String(t *testing.T) {
	cases := []struct {
		in   scheduler.EventKind
		want string
	}{
		{scheduler.EventRoutineFired, "routine.fired"},
		{scheduler.EventRoutineFailed, "routine.failed"},
		{scheduler.EventRoutineSkipped, "routine.skipped"},
		{scheduler.EventMissedFire, "scheduler.missed_fire"},
		{scheduler.EventQuotaCapReached, "scheduler.quota_cap_reached"},
		{scheduler.EventRateLimited, "scheduler.rate_limited"},
		{scheduler.EventLoopBound, "scheduler.loop_bound"},
		{scheduler.EventLoopReleased, "scheduler.loop_released"},
		{scheduler.EventKind(99), "scheduler.unknown"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("EventKind(%d).String() = %q, want %q", int(c.in), got, c.want)
		}
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {
	pairs := []struct {
		a, b error
		name string
	}{
		{scheduler.ErrInvalidSchedule, scheduler.ErrInvalidHistoryEntry, "ErrInvalidSchedule vs ErrInvalidHistoryEntry"},
		{scheduler.ErrInvalidSchedule, scheduler.ErrNotFound, "ErrInvalidSchedule vs ErrNotFound"},
		{scheduler.ErrInvalidCron, scheduler.ErrRateLimited, "ErrInvalidCron vs ErrRateLimited"},
		{scheduler.ErrQuotaCap, scheduler.ErrSessionGone, "ErrQuotaCap vs ErrSessionGone"},
	}
	for _, p := range pairs {
		if errors.Is(p.a, p.b) {
			t.Errorf("%s: errors.Is says they alias; must be distinct", p.name)
		}
	}
}

func TestSentinelErrorsCarryPrefix(t *testing.T) {
	for name, err := range map[string]error{
		"ErrInvalidSchedule":     scheduler.ErrInvalidSchedule,
		"ErrInvalidHistoryEntry": scheduler.ErrInvalidHistoryEntry,
		"ErrNotFound":            scheduler.ErrNotFound,
		"ErrInvalidCron":         scheduler.ErrInvalidCron,
		"ErrRateLimited":         scheduler.ErrRateLimited,
		"ErrQuotaCap":            scheduler.ErrQuotaCap,
		"ErrSessionGone":         scheduler.ErrSessionGone,
	} {
		if err == nil {
			t.Errorf("%s = nil, want non-nil sentinel", name)
			continue
		}
		msg := err.Error()
		if len(msg) < len("scheduler: ") || msg[:len("scheduler: ")] != "scheduler: " {
			t.Errorf("%s.Error() = %q, want prefix \"scheduler: \"", name, msg)
		}
	}
}
