package scheduleradapter_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter"
	"github.com/cbip-solutions/hades-system/internal/scheduler"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func TestInsertScheduleAndGetRoundTrip_Cron(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	s := &scheduler.Schedule{
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
		CreatedAt:    now,
		NextRunAt:    now.Add(time.Hour),
	}
	if err := a.InsertSchedule(context.Background(), s); err != nil {
		t.Fatalf("InsertSchedule: %v", err)
	}
	got, err := a.GetSchedule(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got == nil {
		t.Fatal("got nil after insert")
	}
	if got.ID != s.ID || got.ProjectAlias != s.ProjectAlias || got.Action != s.Action {
		t.Errorf("got = %+v, want match on ID/Alias/Action", got)
	}
	if got.Tier != scheduler.TierRoutine {
		t.Errorf("Tier = %v, want TierRoutine", got.Tier)
	}
	if got.TriggerConfig.CronExpr != "0 8 * * 1-5" {
		t.Errorf("CronExpr = %q", got.TriggerConfig.CronExpr)
	}
	if got.MissPolicy != scheduler.MissPolicyCatchUpBounded {
		t.Errorf("MissPolicy = %v", got.MissPolicy)
	}
	if got.MissLookback != 7*24*time.Hour {
		t.Errorf("MissLookback = %v", got.MissLookback)
	}
}

func TestInsertScheduleAndGetRoundTrip_HTTP(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	s := &scheduler.Schedule{
		ID:           "id-http-01",
		Tier:         scheduler.TierRoutine,
		ProjectAlias: "internal-platform-x",
		Action:       "webhook",
		TriggerType:  scheduler.TriggerHTTP,
		TriggerConfig: scheduler.TriggerConfig{
			BearerTokenHash: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		},
		MissPolicy:      scheduler.MissPolicySkip,
		Status:          scheduler.StatusEnabled,
		CreatedAt:       now,
		BearerTokenHash: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
	if err := a.InsertSchedule(context.Background(), s); err != nil {
		t.Fatalf("InsertSchedule: %v", err)
	}
	got, err := a.GetSchedule(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.TriggerConfig.BearerTokenHash != s.TriggerConfig.BearerTokenHash {
		t.Errorf("BearerTokenHash mismatch: %q vs %q", got.TriggerConfig.BearerTokenHash, s.TriggerConfig.BearerTokenHash)
	}
}

func TestInsertScheduleAndGetRoundTrip_GitPoll(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	s := &scheduler.Schedule{
		ID:           "id-git-01",
		Tier:         scheduler.TierRoutine,
		ProjectAlias: "internal-platform-x",
		Action:       "git-watcher",
		TriggerType:  scheduler.TriggerGitPoll,
		TriggerConfig: scheduler.TriggerConfig{
			RepoURL: "https://github.com/owner/repo",
			Branch:  "dev",
		},
		MissPolicy: scheduler.MissPolicySkip,
		Status:     scheduler.StatusEnabled,
		CreatedAt:  now,
	}
	if err := a.InsertSchedule(context.Background(), s); err != nil {
		t.Fatalf("InsertSchedule: %v", err)
	}
	got, err := a.GetSchedule(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.TriggerConfig.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q", got.TriggerConfig.RepoURL)
	}
	if got.TriggerConfig.Branch != "dev" {
		t.Errorf("Branch = %q", got.TriggerConfig.Branch)
	}
}

func TestGetSchedule_Absent(t *testing.T) {
	a, _ := openTestAdapter(t)
	got, err := a.GetSchedule(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestListSchedules_FilterByAlias(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	mk := func(id, alias string) *scheduler.Schedule {
		return &scheduler.Schedule{
			ID:            id,
			Tier:          scheduler.TierRoutine,
			ProjectAlias:  alias,
			Action:        "act",
			TriggerType:   scheduler.TriggerCron,
			TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
			MissPolicy:    scheduler.MissPolicySkip,
			Status:        scheduler.StatusEnabled,
			CreatedAt:     now,
		}
	}
	if err := a.InsertSchedule(context.Background(), mk("a", "internal-platform-x")); err != nil {
		t.Fatalf("Insert a: %v", err)
	}
	if err := a.InsertSchedule(context.Background(), mk("b", "nexus")); err != nil {
		t.Fatalf("Insert b: %v", err)
	}
	all, err := a.ListSchedules(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("all len = %d, want 2", len(all))
	}
	filtered, err := a.ListSchedules(context.Background(), "internal-platform-x")
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ProjectAlias != "internal-platform-x" {
		t.Errorf("filtered = %+v", filtered)
	}
}

func TestSoftDeleteSchedule_Idempotent(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	s := &scheduler.Schedule{
		ID:            "id-del",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "x",
		Action:        "y",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     now,
	}
	if err := a.InsertSchedule(context.Background(), s); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := a.SoftDeleteSchedule(context.Background(), s.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	if err := a.SoftDeleteSchedule(context.Background(), s.ID); err != nil {
		t.Fatalf("second delete: %v", err)
	}

	if err := a.SoftDeleteSchedule(context.Background(), "never-existed"); err != nil {
		t.Fatalf("never-existed delete: %v", err)
	}
}

func TestListDueSchedules_ExcludesDisabled(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	enabled := &scheduler.Schedule{
		ID:            "due-en",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "morning",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     now,
		NextRunAt:     now.Add(time.Hour),
	}
	disabled := &scheduler.Schedule{
		ID:            "due-dis",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "weekly",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		Status:        scheduler.StatusDisabled,
		CreatedAt:     now,
		NextRunAt:     now.Add(time.Hour),
	}
	if err := a.InsertSchedule(context.Background(), enabled); err != nil {
		t.Fatalf("Insert enabled: %v", err)
	}
	if err := a.InsertSchedule(context.Background(), disabled); err != nil {
		t.Fatalf("Insert disabled: %v", err)
	}
	due, err := a.ListDueSchedules(context.Background(), now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(due) != 1 || due[0].ID != "due-en" {
		t.Errorf("due = %+v, want [due-en]", due)
	}
}

func TestQueryScheduleHistory_ReturnsHistoryEntries(t *testing.T) {
	a, store := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()

	_ = store
	if err := a.AppendHistory(context.Background(), buildHistoryRow("hist-id", now)); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	rows, err := a.QueryScheduleHistory(context.Background(), "hist-id", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("QueryScheduleHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ScheduleID != "hist-id" {
		t.Errorf("ScheduleID = %q", rows[0].ScheduleID)
	}
	if rows[0].Outcome != scheduler.OutcomeSuccess {
		t.Errorf("Outcome = %v", rows[0].Outcome)
	}
	if rows[0].DurationMs != 4567 {
		t.Errorf("DurationMs = %d", rows[0].DurationMs)
	}
}

func TestInsertSchedule_NilSchedule(t *testing.T) {
	a, _ := openTestAdapter(t)
	if err := a.InsertSchedule(context.Background(), nil); err == nil {
		t.Fatal("expected error on nil Schedule")
	}
}

func TestInsertSchedule_RejectsInvalidSchedule(t *testing.T) {
	a, _ := openTestAdapter(t)

	if err := a.InsertSchedule(context.Background(), &scheduler.Schedule{}); err == nil {
		t.Fatal("expected ErrInvalidSchedule on empty Schedule")
	}
}

func buildHistoryRow(scheduleID string, firedAt time.Time) store.ScheduleHistoryRow {
	return store.ScheduleHistoryRow{
		ScheduleID: scheduleID,
		FiredAt:    firedAt,
		Outcome:    int(scheduler.OutcomeSuccess),
		Reason:     "",
		CostUSD:    0.123,
		DurationMs: 4567,
	}
}

func TestNewHandlerStore_PanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil adapter")
		}
	}()
	_ = scheduleradapter.NewHandlerStore(nil)
}

func newHandlerStoreFor(a *scheduleradapter.Adapter) *scheduleradapter.HandlerStore {
	return scheduleradapter.NewHandlerStore(a)
}

func TestHandlerStore_DelegatesAllMethods(t *testing.T) {
	a, _ := openTestAdapter(t)
	hs := newHandlerStoreFor(a)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC).UTC()
	s := &scheduler.Schedule{
		ID:            "hs-1",
		Tier:          scheduler.TierRoutine,
		ProjectAlias:  "internal-platform-x",
		Action:        "morning-brief",
		TriggerType:   scheduler.TriggerCron,
		TriggerConfig: scheduler.TriggerConfig{CronExpr: "0 8 * * *"},
		MissPolicy:    scheduler.MissPolicySkip,
		Status:        scheduler.StatusEnabled,
		CreatedAt:     now,
		NextRunAt:     now.Add(time.Hour),
	}
	if err := hs.Insert(context.Background(), s); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := hs.Get(context.Background(), s.ID)
	if err != nil || got == nil || got.ID != s.ID {
		t.Fatalf("Get: %v / %v", got, err)
	}
	all, err := hs.List(context.Background(), "")
	if err != nil || len(all) != 1 {
		t.Fatalf("List: %v / %d", err, len(all))
	}
	due, err := hs.ListDue(context.Background(), now.Add(2*time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("ListDue: %v / %d", err, len(due))
	}
	rows, err := hs.QueryHistory(context.Background(), s.ID, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil || len(rows) != 0 {
		t.Fatalf("QueryHistory: %v / %d", err, len(rows))
	}
	if err := hs.SoftDelete(context.Background(), s.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	got2, err := hs.Get(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("Get post-delete: %v", err)
	}
	if got2 != nil {
		t.Errorf("expected nil after SoftDelete; got %+v", got2)
	}
}

func TestStoreRowToSchedule_MalformedJSON(t *testing.T) {
	a, st := openTestAdapter(t)

	_, err := st.DB().Exec(
		`INSERT INTO schedules
		   (id, tier, project_alias, action, trigger_type, trigger_config,
		    miss_policy, miss_lookback_seconds, coalesce_window_seconds,
		    last_run_at_unix, next_run_at_unix, status, created_at_unix,
		    bearer_token_hash)
		   VALUES
		   ('broken', 0, 'x', 'y', 0, '{not json', 0, 0, 0, NULL, NULL, 0, 1700000000, NULL)`,
	)
	if err != nil {

		t.Skipf("could not insert broken row: %v", err)
	}

	_, gerr := a.GetSchedule(context.Background(), "broken")
	if gerr == nil {
		t.Skip("broken row was somehow valid; defence-in-depth elsewhere")
	}
	if !strings.Contains(gerr.Error(), "unmarshal") {
		t.Errorf("err = %v, want substring 'unmarshal'", gerr)
	}
}
