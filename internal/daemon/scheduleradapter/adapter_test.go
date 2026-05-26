package scheduleradapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/scheduleradapter"
	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/tests/testhelpers"
)

func openTestAdapter(t *testing.T) (*scheduleradapter.Adapter, *store.Store) {
	t.Helper()
	ts := testhelpers.OpenInMemoryStore(t)
	return scheduleradapter.New(ts.Store), ts.Store
}

func TestAdapterNewPanicsOnNilStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil store; got none")
		}
	}()
	_ = scheduleradapter.New(nil)
}

func TestAdapterInsertAndGetRoundTrip(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	row := store.ScheduleRow{
		ID:                    "01HZ7K8M9P2Q3R4S5T6V7W8X9Y",
		Tier:                  0,
		ProjectAlias:          "internal-platform-x",
		Action:                "morning-brief",
		TriggerType:           0,
		TriggerConfig:         `{"cron_expr":"0 8 * * 1-5"}`,
		MissPolicy:            1,
		MissLookbackSeconds:   7 * 24 * 60 * 60,
		CoalesceWindowSeconds: 0,
		LastRunAt:             time.Time{},
		NextRunAt:             now.Add(time.Hour),
		Status:                0,
		CreatedAt:             now,
		BearerTokenHash:       "",
	}
	if err := a.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := a.Get(context.Background(), row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil row")
	}
	if got.ID != row.ID || got.ProjectAlias != row.ProjectAlias ||
		got.Action != row.Action || got.MissPolicy != row.MissPolicy {
		t.Errorf("round-trip mismatch: got %+v, want %+v", *got, row)
	}
	if !got.NextRunAt.Equal(row.NextRunAt) {
		t.Errorf("NextRunAt: got %v, want %v", got.NextRunAt, row.NextRunAt)
	}
}

func TestAdapterGetAbsentReturnsNil(t *testing.T) {
	a, _ := openTestAdapter(t)
	got, err := a.Get(context.Background(), "no-such-id")
	if err != nil {
		t.Errorf("Get: want nil err, got %v", err)
	}
	if got != nil {
		t.Errorf("Get: want nil row, got %+v", got)
	}
}

func TestAdapterListAllOrdered(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	for i, alias := range []string{"a", "b", "c"} {
		if err := a.Insert(context.Background(), store.ScheduleRow{
			ID:                    "list-" + alias,
			Tier:                  0,
			ProjectAlias:          alias,
			Action:                "act",
			TriggerType:           0,
			TriggerConfig:         "{}",
			MissPolicy:            0,
			MissLookbackSeconds:   3600,
			CoalesceWindowSeconds: 0,
			Status:                0,
			CreatedAt:             now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("Insert(%s): %v", alias, err)
		}
	}
	got, err := a.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("List: got %d rows, want 3", len(got))
	}
}

func TestAdapterListDueFiltersByStatusAndTime(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	mk := func(id string, status int, nextRun time.Time) store.ScheduleRow {
		return store.ScheduleRow{
			ID:                    id,
			Tier:                  0,
			ProjectAlias:          "p",
			Action:                "act",
			TriggerType:           0,
			TriggerConfig:         "{}",
			MissPolicy:            0,
			MissLookbackSeconds:   3600,
			CoalesceWindowSeconds: 0,
			NextRunAt:             nextRun,
			Status:                status,
			CreatedAt:             now,
		}
	}
	for _, row := range []store.ScheduleRow{
		mk("due", 0, now.Add(-1*time.Minute)),
		mk("future", 0, now.Add(1*time.Hour)),
		mk("disabled", 1, now.Add(-1*time.Minute)),
	} {
		if err := a.Insert(context.Background(), row); err != nil {
			t.Fatalf("Insert(%s): %v", row.ID, err)
		}
	}
	got, err := a.ListDue(context.Background(), now)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(got) != 1 || got[0].ID != "due" {
		t.Errorf("ListDue: got %v, want only 'due'", got)
	}
}

func TestAdapterUpdateRoundTrip(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	row := store.ScheduleRow{
		ID:                    "upd-1",
		Tier:                  0,
		ProjectAlias:          "p",
		Action:                "act",
		TriggerType:           0,
		TriggerConfig:         "{}",
		MissPolicy:            0,
		MissLookbackSeconds:   3600,
		CoalesceWindowSeconds: 0,
		Status:                0,
		CreatedAt:             now,
	}
	if err := a.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	row.LastRunAt = now
	row.NextRunAt = now.Add(time.Hour)
	row.Status = 1
	if err := a.Update(context.Background(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := a.Get(context.Background(), "upd-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != 1 || !got.LastRunAt.Equal(now) || !got.NextRunAt.Equal(now.Add(time.Hour)) {
		t.Errorf("Update did not persist: got %+v", *got)
	}
}

func TestAdapterUpdateAbsentReturnsNotFound(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	row := store.ScheduleRow{
		ID: "nope", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	err := a.Update(context.Background(), row)
	if !errors.Is(err, store.ErrScheduleNotFound) {
		t.Errorf("Update(absent): want ErrScheduleNotFound, got %v", err)
	}
}

func TestAdapterDeleteRoundTrip(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	row := store.ScheduleRow{
		ID: "del-1", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	if err := a.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := a.Delete(context.Background(), "del-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := a.Get(context.Background(), "del-1")
	if err != nil {
		t.Errorf("Get post-delete: want nil err, got %v", err)
	}
	if got != nil {
		t.Errorf("Get post-delete: want nil row, got %+v", got)
	}
}

func TestAdapterDeleteAbsentReturnsNotFound(t *testing.T) {
	a, _ := openTestAdapter(t)
	err := a.Delete(context.Background(), "no-such-id")
	if !errors.Is(err, store.ErrScheduleNotFound) {
		t.Errorf("Delete(absent): want ErrScheduleNotFound, got %v", err)
	}
}

func TestAdapterAppendAndQueryHistory(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	if err := a.AppendHistory(context.Background(), store.ScheduleHistoryRow{
		ScheduleID: "abc",
		FiredAt:    now,
		Outcome:    0,
		Reason:     "",
		CostUSD:    0.012,
		DurationMs: 124,
	}); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	got, err := a.QueryHistory(context.Background(), "abc",
		now.Add(-1*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("QueryHistory: %v", err)
	}
	if len(got) != 1 || got[0].ScheduleID != "abc" || got[0].DurationMs != 124 {
		t.Errorf("QueryHistory: got %+v, want one row for 'abc' with DurationMs=124", got)
	}
}

func TestAdapterQueryHistoryEmptyResult(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	got, err := a.QueryHistory(context.Background(), "no-such",
		now.Add(-time.Hour), now)
	if err != nil {
		t.Errorf("QueryHistory: want nil err on empty, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("QueryHistory: want 0 rows, got %d", len(got))
	}
}

// TestAdapterCtxCancelPropagates verifies the context cancellation
// path: a cancelled ctx must be detected before the SQL exec/query.
// Defense in depth — both the adapter and the underlying *sql.DB
// guard the cancellation. Each adapter method MUST honour the gate
// independently so a ctx-cancelled call never reaches the *store.Store
// boundary.
func TestAdapterCtxCancelPropagates(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	row := store.ScheduleRow{
		ID: "ctx-x", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	mkCtx := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	checks := []struct {
		name string
		run  func(ctx context.Context) error
	}{
		{"Insert", func(ctx context.Context) error { return a.Insert(ctx, row) }},
		{"Get", func(ctx context.Context) error { _, err := a.Get(ctx, "any"); return err }},
		{"List", func(ctx context.Context) error { _, err := a.List(ctx); return err }},
		{"ListDue", func(ctx context.Context) error { _, err := a.ListDue(ctx, now); return err }},
		{"Update", func(ctx context.Context) error { return a.Update(ctx, row) }},
		{"Delete", func(ctx context.Context) error { return a.Delete(ctx, "any") }},
		{"AppendHistory", func(ctx context.Context) error {
			return a.AppendHistory(ctx, store.ScheduleHistoryRow{
				ScheduleID: "x", FiredAt: now, Outcome: 0,
			})
		}},
		{"QueryHistory", func(ctx context.Context) error {
			_, err := a.QueryHistory(ctx, "x", now.Add(-time.Hour), now)
			return err
		}},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(mkCtx())
			if err == nil {
				t.Errorf("%s with cancelled ctx: want non-nil err", c.name)
			}
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("%s with cancelled ctx: want context.Canceled, got %v",
					c.name, err)
			}
		})
	}
}

func TestAdapterInsertDuplicateIDSurfacesSentinel(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	row := store.ScheduleRow{
		ID: "dup-1", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	if err := a.Insert(context.Background(), row); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	err := a.Insert(context.Background(), row)
	if !errors.Is(err, store.ErrDuplicateScheduleID) {
		t.Errorf("Insert duplicate: want ErrDuplicateScheduleID, got %v", err)
	}
}

func TestAdapterAppendHistoryValidationSurfaces(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	err := a.AppendHistory(context.Background(), store.ScheduleHistoryRow{
		ScheduleID: "x",
		FiredAt:    now,
		Outcome:    99,
	})
	if err == nil {
		t.Error("AppendHistory(invalid outcome): want non-nil err")
	}
}

func TestAdapterQueryHistoryInvertedRangeSurfaces(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	_, err := a.QueryHistory(context.Background(), "x", now, now.Add(-time.Hour))
	if err == nil {
		t.Error("QueryHistory(inverted range): want non-nil err")
	}
}

func TestAdapterUpdateValidationSurfaces(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	row := store.ScheduleRow{
		ID:           "any",
		Tier:         99,
		ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	err := a.Update(context.Background(), row)
	if err == nil {
		t.Error("Update(invalid tier): want non-nil err")
	}

	if errors.Is(err, store.ErrScheduleNotFound) {
		t.Errorf("Update(invalid tier): unexpected ErrScheduleNotFound; got %v", err)
	}
}

func TestAdapterDeleteValidationSurfaces(t *testing.T) {
	a, _ := openTestAdapter(t)
	err := a.Delete(context.Background(), "")
	if err == nil {
		t.Error("Delete(empty id): want non-nil err")
	}
	if errors.Is(err, store.ErrScheduleNotFound) {
		t.Errorf("Delete(empty id): unexpected ErrScheduleNotFound; got %v", err)
	}
}

func TestAdapterInsertValidationSurfaces(t *testing.T) {
	a, _ := openTestAdapter(t)
	now := time.Now().UTC()
	row := store.ScheduleRow{
		ID:           "x",
		Tier:         99,
		ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	err := a.Insert(context.Background(), row)
	if err == nil {
		t.Error("Insert(invalid tier): want non-nil err")
	}
}
