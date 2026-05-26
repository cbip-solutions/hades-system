package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func openMigratedScheduleStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "schedules.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigration063SchedulesTableExists(t *testing.T) {
	s := openMigratedScheduleStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schedules'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("schedules table missing after Migrate: %v", err)
	}
	if name != "schedules" {
		t.Errorf("expected schedules table, got %q", name)
	}
}

func TestMigration063ScheduleHistoryTableExists(t *testing.T) {
	s := openMigratedScheduleStore(t)
	var name string
	err := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schedule_history'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("schedule_history table missing after Migrate: %v", err)
	}
	if name != "schedule_history" {
		t.Errorf("expected schedule_history table, got %q", name)
	}
}

func TestMigration063SchedulesColumns(t *testing.T) {
	s := openMigratedScheduleStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(schedules)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = ctype
	}
	want := map[string]string{
		"id":                      "TEXT",
		"tier":                    "INTEGER",
		"project_alias":           "TEXT",
		"action":                  "TEXT",
		"trigger_type":            "INTEGER",
		"trigger_config":          "TEXT",
		"miss_policy":             "INTEGER",
		"miss_lookback_seconds":   "INTEGER",
		"coalesce_window_seconds": "INTEGER",
		"last_run_at_unix":        "INTEGER",
		"next_run_at_unix":        "INTEGER",
		"status":                  "INTEGER",
		"created_at_unix":         "INTEGER",
		"bearer_token_hash":       "TEXT",
	}
	for col, ty := range want {
		if got[col] != ty {
			t.Errorf("schedules.%q: got type %q, want %q", col, got[col], ty)
		}
	}
}

func TestMigration063ScheduleHistoryColumns(t *testing.T) {
	s := openMigratedScheduleStore(t)
	rows, err := s.DB().Query(`PRAGMA table_info(schedule_history)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = ctype
	}
	want := map[string]string{
		"id":            "INTEGER",
		"schedule_id":   "TEXT",
		"fired_at_unix": "INTEGER",
		"outcome":       "INTEGER",
		"reason":        "TEXT",
		"cost_usd":      "REAL",
		"duration_ms":   "INTEGER",
	}
	for col, ty := range want {
		if got[col] != ty {
			t.Errorf("schedule_history.%q: got type %q, want %q", col, got[col], ty)
		}
	}
}

func TestMigration063SchedulesIndexes(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, idx := range []struct {
		name string
		tbl  string
	}{
		{"idx_schedules_due", "schedules"},
		{"idx_schedules_project_alias", "schedules"},
		{"idx_schedule_history_lookup", "schedule_history"},
	} {
		var name string
		err := s.DB().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='index' AND tbl_name=? AND name=?`,
			idx.tbl, idx.name,
		).Scan(&name)
		if err != nil {
			t.Errorf("index %q missing on %q: %v", idx.name, idx.tbl, err)
		}
	}
}

func TestMigration063SchedulesTierCheck(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, bad := range []int{-1, 3, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO schedules
			 (id, tier, project_alias, action, trigger_type, trigger_config,
			  miss_policy, miss_lookback_seconds, coalesce_window_seconds,
			  status, created_at_unix)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"sched-tier-bad", bad, "p", "act", 0, "{}", 0, 604800, 0, 0, 1,
		)
		if err == nil {
			t.Errorf("tier=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestMigration063SchedulesTriggerTypeCheck(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, bad := range []int{-1, 3, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO schedules
			 (id, tier, project_alias, action, trigger_type, trigger_config,
			  miss_policy, miss_lookback_seconds, coalesce_window_seconds,
			  status, created_at_unix)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"sched-tt-bad", 0, "p", "act", bad, "{}", 0, 604800, 0, 0, 1,
		)
		if err == nil {
			t.Errorf("trigger_type=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestMigration063SchedulesMissPolicyCheck(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, bad := range []int{-1, 4, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO schedules
			 (id, tier, project_alias, action, trigger_type, trigger_config,
			  miss_policy, miss_lookback_seconds, coalesce_window_seconds,
			  status, created_at_unix)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"sched-mp-bad", 0, "p", "act", 0, "{}", bad, 604800, 0, 0, 1,
		)
		if err == nil {
			t.Errorf("miss_policy=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestMigration063SchedulesStatusCheck(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, bad := range []int{-1, 3, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO schedules
			 (id, tier, project_alias, action, trigger_type, trigger_config,
			  miss_policy, miss_lookback_seconds, coalesce_window_seconds,
			  status, created_at_unix)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"sched-st-bad", 0, "p", "act", 0, "{}", 0, 604800, 0, bad, 1,
		)
		if err == nil {
			t.Errorf("status=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestMigration063ScheduleHistoryOutcomeCheck(t *testing.T) {
	s := openMigratedScheduleStore(t)
	for _, bad := range []int{-1, 4, 99} {
		_, err := s.DB().Exec(
			`INSERT INTO schedule_history
			 (schedule_id, fired_at_unix, outcome, reason, cost_usd, duration_ms)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			"sched-oc-bad", 1, bad, "", 0, 0,
		)
		if err == nil {
			t.Errorf("outcome=%d: expected CHECK violation, got nil", bad)
		}
	}
}

func TestSchemaVersionAtLeast27(t *testing.T) {
	t.Parallel()
	if schemaVersion < 27 {
		t.Errorf("schemaVersion = %d, want >= 27 (Plan 7 Phase D-1 migration 063 must remain applied)", schemaVersion)
	}
}

func TestInsertScheduleSuccess(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
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
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("InsertSchedule: %v", err)
	}
}

func TestInsertScheduleDuplicateIDReturnsErr(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
		ID:                    "dup-id",
		Tier:                  0,
		ProjectAlias:          "dup",
		Action:                "act",
		TriggerType:           0,
		TriggerConfig:         "{}",
		MissPolicy:            0,
		MissLookbackSeconds:   3600,
		CoalesceWindowSeconds: 0,
		Status:                0,
		CreatedAt:             now,
	}
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := InsertSchedule(s.DB(), row)
	if !errors.Is(err, ErrDuplicateScheduleID) {
		t.Errorf("want ErrDuplicateScheduleID, got %v", err)
	}

	if err != nil && !errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
		_ = err
	}
}

func TestInsertScheduleValidationErrors(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	cases := []struct {
		name string
		mut  func(r *ScheduleRow)
	}{
		{"empty id", func(r *ScheduleRow) { r.ID = "" }},
		{"empty project_alias", func(r *ScheduleRow) { r.ProjectAlias = "" }},
		{"empty action", func(r *ScheduleRow) { r.Action = "" }},
		{"tier out of range high", func(r *ScheduleRow) { r.Tier = 3 }},
		{"tier out of range low", func(r *ScheduleRow) { r.Tier = -1 }},
		{"trigger_type out of range high", func(r *ScheduleRow) { r.TriggerType = 3 }},
		{"trigger_type out of range low", func(r *ScheduleRow) { r.TriggerType = -1 }},
		{"miss_policy out of range high", func(r *ScheduleRow) { r.MissPolicy = 4 }},
		{"miss_policy out of range low", func(r *ScheduleRow) { r.MissPolicy = -1 }},
		{"status out of range high", func(r *ScheduleRow) { r.Status = 3 }},
		{"status out of range low", func(r *ScheduleRow) { r.Status = -1 }},
		{"miss_lookback_seconds negative", func(r *ScheduleRow) { r.MissLookbackSeconds = -1 }},
		{"coalesce_window_seconds negative", func(r *ScheduleRow) { r.CoalesceWindowSeconds = -1 }},
		{"empty trigger_config", func(r *ScheduleRow) { r.TriggerConfig = "" }},
		{"malformed trigger_config", func(r *ScheduleRow) { r.TriggerConfig = "not json" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := ScheduleRow{
				ID:                    "v-" + tc.name,
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
			tc.mut(&row)
			if err := InsertSchedule(s.DB(), row); err == nil {
				t.Errorf("expected validation error for %s, got nil", tc.name)
			}
		})
	}
}

func TestGetScheduleRoundTrip(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	want := ScheduleRow{
		ID:                    "rt-1",
		Tier:                  1,
		ProjectAlias:          "internal-platform-x",
		Action:                "morning-brief",
		TriggerType:           1,
		TriggerConfig:         `{"endpoint":"/v1/schedules/x/fire"}`,
		MissPolicy:            2,
		MissLookbackSeconds:   86400,
		CoalesceWindowSeconds: 300,
		LastRunAt:             now.Add(-time.Hour),
		NextRunAt:             now.Add(time.Hour),
		Status:                0,
		CreatedAt:             now,
		BearerTokenHash:       "abcd1234",
	}
	if err := InsertSchedule(s.DB(), want); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := GetSchedule(s.DB(), "rt-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil row")
	}
	if got.ID != want.ID || got.Tier != want.Tier || got.ProjectAlias != want.ProjectAlias ||
		got.Action != want.Action || got.TriggerType != want.TriggerType ||
		got.TriggerConfig != want.TriggerConfig || got.MissPolicy != want.MissPolicy ||
		got.MissLookbackSeconds != want.MissLookbackSeconds ||
		got.CoalesceWindowSeconds != want.CoalesceWindowSeconds ||
		got.Status != want.Status || got.BearerTokenHash != want.BearerTokenHash {
		t.Errorf("round-trip mismatch: got %+v, want %+v", *got, want)
	}
	if !got.LastRunAt.Equal(want.LastRunAt) {
		t.Errorf("LastRunAt: got %v, want %v", got.LastRunAt, want.LastRunAt)
	}
	if !got.NextRunAt.Equal(want.NextRunAt) {
		t.Errorf("NextRunAt: got %v, want %v", got.NextRunAt, want.NextRunAt)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

func TestGetScheduleAbsentReturnsNil(t *testing.T) {
	s := openMigratedScheduleStore(t)
	got, err := GetSchedule(s.DB(), "no-such-id")
	if err != nil {
		t.Errorf("Get: want nil err, got %v", err)
	}
	if got != nil {
		t.Errorf("Get: want nil row, got %+v", got)
	}
}

func TestListSchedulesOrdered(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i, alias := range []string{"a", "b", "c"} {
		row := ScheduleRow{
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
		}
		if err := InsertSchedule(s.DB(), row); err != nil {
			t.Fatalf("Insert(%s): %v", alias, err)
		}
	}
	got, err := ListSchedules(s.DB())
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListSchedules: got %d rows, want 3", len(got))
	}

	if got[0].ProjectAlias != "c" || got[2].ProjectAlias != "a" {
		t.Errorf("ListSchedules order: got %v %v %v, want c b a",
			got[0].ProjectAlias, got[1].ProjectAlias, got[2].ProjectAlias)
	}
}

func TestListSchedulesDueFiltersByStatusAndTime(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	mk := func(id string, status int, nextRun time.Time) ScheduleRow {
		return ScheduleRow{
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
	rows := []ScheduleRow{
		mk("due", 0, now.Add(-1*time.Minute)),
		mk("future", 0, now.Add(1*time.Hour)),
		mk("disabled-but-due", 1, now.Add(-1*time.Minute)),
		mk("failed-but-due", 2, now.Add(-1*time.Minute)),
	}
	for _, row := range rows {
		if err := InsertSchedule(s.DB(), row); err != nil {
			t.Fatalf("Insert(%s): %v", row.ID, err)
		}
	}

	nullable := mk("null-next", 0, time.Time{})
	if err := InsertSchedule(s.DB(), nullable); err != nil {
		t.Fatalf("Insert(null-next): %v", err)
	}
	got, err := ListSchedulesDue(s.DB(), now)
	if err != nil {
		t.Fatalf("ListSchedulesDue: %v", err)
	}
	if len(got) != 1 || got[0].ID != "due" {
		t.Errorf("ListSchedulesDue: got %v, want only 'due'", got)
	}
}

func TestUpdateScheduleSuccess(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
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
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	row.LastRunAt = now
	row.NextRunAt = now.Add(time.Hour)
	row.Status = 1
	if err := UpdateSchedule(s.DB(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := GetSchedule(s.DB(), "upd-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.LastRunAt.Equal(now) || !got.NextRunAt.Equal(now.Add(time.Hour)) || got.Status != 1 {
		t.Errorf("Update did not persist: got %+v", *got)
	}
}

func TestUpdateScheduleAbsentReturnsNotFound(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
		ID: "nope", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	err := UpdateSchedule(s.DB(), row)
	if !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("UpdateSchedule(absent): want ErrScheduleNotFound, got %v", err)
	}
}

func TestDeleteScheduleSuccess(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
		ID: "del-1", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0, CreatedAt: now,
	}
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := DeleteSchedule(s.DB(), "del-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := GetSchedule(s.DB(), "del-1")
	if err != nil {
		t.Errorf("Get post-delete: want nil err, got %v", err)
	}
	if got != nil {
		t.Errorf("Get post-delete: want nil row, got %+v", got)
	}
}

func TestDeleteScheduleAbsentReturnsNotFound(t *testing.T) {
	s := openMigratedScheduleStore(t)
	err := DeleteSchedule(s.DB(), "no-such-id")
	if !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("Delete(absent): want ErrScheduleNotFound, got %v", err)
	}
}

func TestAppendScheduleHistorySuccess(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	h := ScheduleHistoryRow{
		ScheduleID: "sched-x",
		FiredAt:    now,
		Outcome:    0,
		Reason:     "",
		CostUSD:    0.012,
		DurationMs: 124,
	}
	if err := AppendScheduleHistory(s.DB(), h); err != nil {
		t.Fatalf("AppendScheduleHistory: %v", err)
	}
}

func TestAppendScheduleHistoryValidationErrors(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	cases := []struct {
		name string
		mut  func(h *ScheduleHistoryRow)
	}{
		{"empty schedule_id", func(h *ScheduleHistoryRow) { h.ScheduleID = "" }},
		{"outcome out of range high", func(h *ScheduleHistoryRow) { h.Outcome = 4 }},
		{"outcome out of range low", func(h *ScheduleHistoryRow) { h.Outcome = -1 }},
		{"negative cost", func(h *ScheduleHistoryRow) { h.CostUSD = -0.001 }},
		{"negative duration", func(h *ScheduleHistoryRow) { h.DurationMs = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := ScheduleHistoryRow{
				ScheduleID: "x",
				FiredAt:    now,
				Outcome:    0,
				Reason:     "",
				CostUSD:    0,
				DurationMs: 0,
			}
			tc.mut(&h)
			if err := AppendScheduleHistory(s.DB(), h); err == nil {
				t.Errorf("expected validation error for %s, got nil", tc.name)
			}
		})
	}
}

func TestQueryScheduleHistoryFiltersByTime(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, h := range []ScheduleHistoryRow{
		{ScheduleID: "abc", FiredAt: now.Add(-2 * time.Hour), Outcome: 0, CostUSD: 0.01, DurationMs: 100},
		{ScheduleID: "abc", FiredAt: now, Outcome: 1, Reason: "fail", CostUSD: 0, DurationMs: 50},
		{ScheduleID: "abc", FiredAt: now.Add(2 * time.Hour), Outcome: 2, CostUSD: 0, DurationMs: 0},
		{ScheduleID: "other", FiredAt: now, Outcome: 0, CostUSD: 0.02, DurationMs: 75},
	} {
		if err := AppendScheduleHistory(s.DB(), h); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := QueryScheduleHistory(s.DB(), "abc", now.Add(-1*time.Hour), now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].Outcome != 1 || got[0].Reason != "fail" {
		t.Errorf("Query: got %+v, want 1 row with Outcome=1 reason=fail", got)
	}
}

func TestQueryScheduleHistoryEmptyResultIsNotErr(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	got, err := QueryScheduleHistory(s.DB(), "no-such", now.Add(-time.Hour), now)
	if err != nil {
		t.Errorf("Query: want nil err on empty, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Query: want 0 rows, got %d", len(got))
	}
}

func TestQueryScheduleHistoryRejectsInvertedRange(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	_, err := QueryScheduleHistory(s.DB(), "abc", now, now.Add(-time.Hour))
	if err == nil {
		t.Errorf("Query: want err on from > to, got nil")
	}
}

var _ = sqlite3.CONSTRAINT_PRIMARYKEY

func TestUpdateScheduleValidationErrors(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	good := ScheduleRow{
		ID: "upd-base", Tier: 0, ProjectAlias: "p", Action: "a",
		TriggerType: 0, TriggerConfig: "{}", MissPolicy: 0,
		MissLookbackSeconds: 0, CoalesceWindowSeconds: 0, Status: 0,
		CreatedAt: now,
	}
	if err := InsertSchedule(s.DB(), good); err != nil {
		t.Fatalf("Insert seed: %v", err)
	}
	cases := []struct {
		name string
		mut  func(r *ScheduleRow)
	}{
		{"empty id", func(r *ScheduleRow) { r.ID = "" }},
		{"empty project_alias", func(r *ScheduleRow) { r.ProjectAlias = "" }},
		{"empty action", func(r *ScheduleRow) { r.Action = "" }},
		{"tier out of range high", func(r *ScheduleRow) { r.Tier = 3 }},
		{"tier out of range low", func(r *ScheduleRow) { r.Tier = -1 }},
		{"trigger_type out of range high", func(r *ScheduleRow) { r.TriggerType = 3 }},
		{"trigger_type out of range low", func(r *ScheduleRow) { r.TriggerType = -1 }},
		{"miss_policy out of range high", func(r *ScheduleRow) { r.MissPolicy = 4 }},
		{"miss_policy out of range low", func(r *ScheduleRow) { r.MissPolicy = -1 }},
		{"status out of range high", func(r *ScheduleRow) { r.Status = 3 }},
		{"status out of range low", func(r *ScheduleRow) { r.Status = -1 }},
		{"miss_lookback_seconds negative", func(r *ScheduleRow) { r.MissLookbackSeconds = -1 }},
		{"coalesce_window_seconds negative", func(r *ScheduleRow) { r.CoalesceWindowSeconds = -1 }},
		{"empty trigger_config", func(r *ScheduleRow) { r.TriggerConfig = "" }},
		{"malformed trigger_config", func(r *ScheduleRow) { r.TriggerConfig = "not json" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := good
			tc.mut(&row)
			if err := UpdateSchedule(s.DB(), row); err == nil {
				t.Errorf("expected validation error for %s, got nil", tc.name)
			}
		})
	}
}

func TestGetScheduleEmptyID(t *testing.T) {
	s := openMigratedScheduleStore(t)
	_, err := GetSchedule(s.DB(), "")
	if err == nil {
		t.Error("GetSchedule(\"\"): want non-nil err")
	}
}

func TestDeleteScheduleEmptyID(t *testing.T) {
	s := openMigratedScheduleStore(t)
	err := DeleteSchedule(s.DB(), "")
	if err == nil {
		t.Error("DeleteSchedule(\"\"): want non-nil err")
	}
}

func TestQueryScheduleHistoryEmptyID(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	_, err := QueryScheduleHistory(s.DB(), "", now, now.Add(time.Hour))
	if err == nil {
		t.Error("QueryScheduleHistory(\"\"): want non-nil err")
	}
}

func TestAppendScheduleHistoryZeroFiredAt(t *testing.T) {
	s := openMigratedScheduleStore(t)
	err := AppendScheduleHistory(s.DB(), ScheduleHistoryRow{
		ScheduleID: "x",
		FiredAt:    time.Time{},
		Outcome:    0,
	})
	if err == nil {
		t.Error("AppendScheduleHistory(zero FiredAt): want non-nil err")
	}
}

func TestInsertScheduleZeroCreatedAt(t *testing.T) {
	s := openMigratedScheduleStore(t)
	row := ScheduleRow{
		ID: "x", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0,
		CreatedAt: time.Time{},
	}
	if err := InsertSchedule(s.DB(), row); err == nil {
		t.Error("InsertSchedule(zero CreatedAt): want non-nil err")
	}
}

func TestIsScheduleIDPKViolationStringFallback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"column-qualified", errors.New("UNIQUE constraint failed: schedules.id"), true},
		{"generic primary-key tail", errors.New("constraint failed: PRIMARY KEY"), true},
		{"unrelated", errors.New("disk full"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isScheduleIDPKViolation(tc.err)
			if got != tc.want {
				t.Errorf("isScheduleIDPKViolation(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestListSchedulesEmpty(t *testing.T) {
	s := openMigratedScheduleStore(t)
	got, err := ListSchedules(s.DB())
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListSchedules empty store: got %d rows, want 0", len(got))
	}
}

func TestListSchedulesDueEmpty(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC()
	got, err := ListSchedulesDue(s.DB(), now)
	if err != nil {
		t.Fatalf("ListSchedulesDue: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListSchedulesDue empty store: got %d rows, want 0", len(got))
	}
}

func TestListSchedulesDueAtBoundary(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	row := ScheduleRow{
		ID: "boundary", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0,
		NextRunAt: now,
		CreatedAt: now,
	}
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := ListSchedulesDue(s.DB(), now)
	if err != nil {
		t.Fatalf("ListSchedulesDue: %v", err)
	}
	if len(got) != 1 || got[0].ID != "boundary" {
		t.Errorf("ListSchedulesDue at boundary: got %v, want [boundary]", got)
	}
}

func TestUpdateSchedulePreservesCreatedAt(t *testing.T) {
	s := openMigratedScheduleStore(t)
	created := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	row := ScheduleRow{
		ID: "preserve", Tier: 0, ProjectAlias: "p", Action: "a", TriggerType: 0,
		TriggerConfig: "{}", MissPolicy: 0, MissLookbackSeconds: 0,
		CoalesceWindowSeconds: 0, Status: 0,
		CreatedAt: created,
	}
	if err := InsertSchedule(s.DB(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	row.CreatedAt = created.Add(24 * time.Hour)
	row.Status = 1
	if err := UpdateSchedule(s.DB(), row); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := GetSchedule(s.DB(), "preserve")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt mutated by Update: got %v, want %v",
			got.CreatedAt, created)
	}
	if got.Status != 1 {
		t.Errorf("Status not persisted: got %d, want 1", got.Status)
	}
}

func TestQueryScheduleHistoryRoundsToOrder(t *testing.T) {
	s := openMigratedScheduleStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i, h := range []ScheduleHistoryRow{
		{ScheduleID: "ord", FiredAt: now.Add(-2 * time.Minute), Outcome: 0, CostUSD: 0.01, DurationMs: 100},
		{ScheduleID: "ord", FiredAt: now.Add(-1 * time.Minute), Outcome: 1, CostUSD: 0, DurationMs: 50},
		{ScheduleID: "ord", FiredAt: now, Outcome: 2, CostUSD: 0, DurationMs: 0},
	} {
		_ = i
		if err := AppendScheduleHistory(s.DB(), h); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := QueryScheduleHistory(s.DB(), "ord", now.Add(-3*time.Minute), now)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Query: got %d rows, want 3", len(got))
	}
	if got[0].Outcome != 0 || got[1].Outcome != 1 || got[2].Outcome != 2 {
		t.Errorf("Query order: got %v %v %v, want 0 1 2",
			got[0].Outcome, got[1].Outcome, got[2].Outcome)
	}
}
