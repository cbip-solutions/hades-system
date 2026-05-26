package store

import (
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func openMigratedPauseStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "pause.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBudgetPausesTableExistsAfterMigration(t *testing.T) {
	s := openMigratedPauseStore(t)
	row := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='budget_pauses'`,
	)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("budget_pauses table missing: %v", err)
	}
}

func TestBudgetAnomaliesTableExistsAfterMigration(t *testing.T) {
	s := openMigratedPauseStore(t)
	row := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='budget_anomalies'`,
	)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("budget_anomalies table missing: %v", err)
	}
}

func TestBudgetAnomalySamplesTableExistsAfterMigration(t *testing.T) {
	s := openMigratedPauseStore(t)
	row := s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='budget_anomaly_samples'`,
	)
	var name string
	if err := row.Scan(&name); err != nil {
		t.Fatalf("budget_anomaly_samples table missing: %v", err)
	}
}

func TestUpsertBudgetPauseSuccess(t *testing.T) {
	s := openMigratedPauseStore(t)
	now := time.Now().UnixMilli()
	if err := UpsertBudgetPause(s.DB(), "worker_id", "w-42", "z_score>4", now, now+3600000); err != nil {
		t.Fatalf("UpsertBudgetPause: %v", err)
	}
	active, autoResume, err := GetBudgetPause(s.DB(), "worker_id", "w-42")
	if err != nil {
		t.Fatalf("GetBudgetPause: %v", err)
	}
	if !active {
		t.Errorf("active = false, want true")
	}
	if autoResume != now+3600000 {
		t.Errorf("autoResume = %d, want %d", autoResume, now+3600000)
	}
}

func TestUpsertBudgetPauseValidation(t *testing.T) {
	s := openMigratedPauseStore(t)
	cases := []struct {
		name string
		s    string
		v    string
	}{
		{"empty scope", "", "x"},
		{"empty value", "stage", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := UpsertBudgetPause(s.DB(), tc.s, tc.v, "r", 0, 0); err == nil {
				t.Error("err = nil, want error")
			}
		})
	}
}

func TestUpsertBudgetPauseUpdatesExistingRow(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := UpsertBudgetPause(s.DB(), "stage", "design", "first-reason", 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := UpsertBudgetPause(s.DB(), "stage", "design", "second-reason", 300, 400); err != nil {
		t.Fatal(err)
	}
	rows, _ := ListActiveBudgetPauses(s.DB())
	if len(rows) != 1 {
		t.Errorf("len(rows) = %d, want 1 (upsert)", len(rows))
	}
	if rows[0].Reason != "second-reason" {
		t.Errorf("reason = %q, want second-reason", rows[0].Reason)
	}
	if rows[0].StartedAt.UnixMilli() != 300 {
		t.Errorf("started_at = %d, want 300", rows[0].StartedAt.UnixMilli())
	}
}

func TestGetBudgetPauseAbsent(t *testing.T) {
	s := openMigratedPauseStore(t)
	active, _, err := GetBudgetPause(s.DB(), "stage", "nonexistent")
	if err != nil {
		t.Fatalf("GetBudgetPause: %v", err)
	}
	if active {
		t.Error("active = true, want false")
	}
}

func TestDeleteBudgetPauseClears(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "project", "internal-platform-x", "manual", 10, 0)
	if err := DeleteBudgetPause(s.DB(), "project", "internal-platform-x"); err != nil {
		t.Fatalf("DeleteBudgetPause: %v", err)
	}
	active, _, _ := GetBudgetPause(s.DB(), "project", "internal-platform-x")
	if active {
		t.Error("active = true, want false (after delete)")
	}
}

func TestDeleteBudgetPauseAbsentNoOp(t *testing.T) {
	s := openMigratedPauseStore(t)

	if err := DeleteBudgetPause(s.DB(), "stage", "nonexistent"); err != nil {
		t.Errorf("DeleteBudgetPause: %v", err)
	}
}

func TestDeleteBudgetPauseIfExpiredCASExpiredRow(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "expired", 100, 200)
	// auto_resume_at=200; beforeMs=300 → expired, MUST delete.
	if err := DeleteBudgetPauseIfExpired(s.DB(), "stage", "design", 300); err != nil {
		t.Fatalf("DeleteBudgetPauseIfExpired: %v", err)
	}
	active, _, _ := GetBudgetPause(s.DB(), "stage", "design")
	if active {
		t.Error("active = true after CAS expire, want false")
	}
}

// TestDeleteBudgetPauseIfExpiredCASPreservesExtended is the storage-layer
// twin of the engine-layer C-1 regression: when auto_resume_at moves past
// beforeMs (concurrent extension), the row MUST survive.
func TestDeleteBudgetPauseIfExpiredCASPreservesExtended(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "extended", 100, 1000)
	// auto_resume_at=1000; beforeMs=500 → not yet expired, MUST NOT delete.
	if err := DeleteBudgetPauseIfExpired(s.DB(), "stage", "design", 500); err != nil {
		t.Fatalf("DeleteBudgetPauseIfExpired: %v", err)
	}
	active, autoMs, _ := GetBudgetPause(s.DB(), "stage", "design")
	if !active {
		t.Error("active = false, want true (extension survived)")
	}
	if autoMs != 1000 {
		t.Errorf("autoMs = %d, want 1000 (extension intact)", autoMs)
	}
}

func TestDeleteBudgetPauseIfExpiredIndefiniteNeverDeleted(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "project", "internal-platform-x", "indefinite", 100, 0)
	// beforeMs is far future; indefinite rows MUST NOT delete.
	if err := DeleteBudgetPauseIfExpired(s.DB(), "project", "internal-platform-x", 1<<62); err != nil {
		t.Fatalf("DeleteBudgetPauseIfExpired: %v", err)
	}
	active, _, _ := GetBudgetPause(s.DB(), "project", "internal-platform-x")
	if !active {
		t.Error("active = false, want true (indefinite preserved)")
	}
}

func TestDeleteBudgetPauseIfExpiredAbsentNoOp(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := DeleteBudgetPauseIfExpired(s.DB(), "stage", "nonexistent", 1000); err != nil {
		t.Errorf("DeleteBudgetPauseIfExpired: %v", err)
	}
}

func TestDeleteBudgetPauseIfExpiredSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := DeleteBudgetPauseIfExpired(s.DB(), "stage", "design", 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestListActiveBudgetPausesOrdered(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "r1", 100, 0)
	_ = UpsertBudgetPause(s.DB(), "worker_id", "w-1", "r2", 200, 0)
	_ = UpsertBudgetPause(s.DB(), "project", "internal-platform-x", "r3", 300, 0)
	rows, _ := ListActiveBudgetPauses(s.DB())
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].StartedAt.UnixMilli() != 100 || rows[1].StartedAt.UnixMilli() != 200 || rows[2].StartedAt.UnixMilli() != 300 {
		t.Errorf("rows not ordered by started_at ASC: got %+v", rows)
	}
}

func TestListActiveBudgetPausesAutoResumeFieldPopulated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "r1", 100, 200)
	rows, _ := ListActiveBudgetPauses(s.DB())
	if rows[0].AutoResumeAt.UnixMilli() != 200 {
		t.Errorf("AutoResumeAt = %v, want unix-ms 200", rows[0].AutoResumeAt)
	}
}

func TestListActiveBudgetPausesIndefiniteAutoResumeIsZero(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "project", "internal-platform-x", "r", 100, 0)
	rows, _ := ListActiveBudgetPauses(s.DB())
	if !rows[0].AutoResumeAt.IsZero() {
		t.Errorf("AutoResumeAt = %v, want zero (indefinite)", rows[0].AutoResumeAt)
	}
}

func TestInsertBudgetAnomalySuccess(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := InsertBudgetAnomaly(s.DB(), "stage", "design", 4.5, 1.0, 0.1, 60, time.Now().UnixMilli()); err != nil {
		t.Fatalf("InsertBudgetAnomaly: %v", err)
	}
	rows, err := ListBudgetAnomalies(s.DB(), 100)
	if err != nil {
		t.Fatalf("ListBudgetAnomalies: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("len = %d, want 1", len(rows))
	}
	if rows[0].ZScore != 4.5 || rows[0].Mean != 1.0 || rows[0].Std != 0.1 || rows[0].WindowSize != 60 {
		t.Errorf("row = %+v, want z=4.5 mean=1 std=0.1 wsize=60", rows[0])
	}
}

func TestInsertBudgetAnomalyValidation(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := InsertBudgetAnomaly(s.DB(), "", "design", 4.5, 1.0, 0.1, 60, 0); err == nil {
		t.Error("err = nil, want error on empty scope")
	}
	if err := InsertBudgetAnomaly(s.DB(), "stage", "", 4.5, 1.0, 0.1, 60, 0); err == nil {
		t.Error("err = nil, want error on empty scope_value")
	}
}

func TestInsertBudgetAnomalyRejectsNaNInf(t *testing.T) {
	s := openMigratedPauseStore(t)
	cases := []struct {
		name           string
		z, mean, std   float64
		wantErrAnomNaN bool
	}{
		{"z=NaN", math.NaN(), 1.0, 0.1, true},
		{"z=+Inf", math.Inf(1), 1.0, 0.1, true},
		{"z=-Inf", math.Inf(-1), 1.0, 0.1, true},
		{"mean=NaN", 4.5, math.NaN(), 0.1, true},
		{"mean=+Inf", 4.5, math.Inf(1), 0.1, true},
		{"std=NaN", 4.5, 1.0, math.NaN(), true},
		{"std=+Inf", 4.5, 1.0, math.Inf(1), true},
		{"all-finite", 4.5, 1.0, 0.1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := InsertBudgetAnomaly(s.DB(), "stage", "design", tc.z, tc.mean, tc.std, 60, 0)
			if tc.wantErrAnomNaN {
				if !errors.Is(err, ErrAnomalyNaN) {
					t.Errorf("err = %v, want ErrAnomalyNaN", err)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil (all-finite input)", err)
			}
		})
	}
}

func TestListBudgetAnomaliesNewestFirst(t *testing.T) {
	s := openMigratedPauseStore(t)
	for i, ms := range []int64{100, 300, 200} {
		_ = InsertBudgetAnomaly(s.DB(), "stage", "design", float64(i), 1.0, 0.1, 60, ms)
	}
	rows, _ := ListBudgetAnomalies(s.DB(), 100)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].DetectedAt.UnixMilli() != 300 || rows[1].DetectedAt.UnixMilli() != 200 || rows[2].DetectedAt.UnixMilli() != 100 {
		t.Errorf("rows not ordered DESC: %+v", rows)
	}
}

func TestListBudgetAnomaliesDefaultLimit(t *testing.T) {
	s := openMigratedPauseStore(t)

	rows, err := ListBudgetAnomalies(s.DB(), 0)
	if err != nil {
		t.Fatalf("ListBudgetAnomalies: %v", err)
	}
	if rows != nil {

	}
}

func TestAppendAnomalySampleAndQueryWindow(t *testing.T) {
	s := openMigratedPauseStore(t)
	now := time.Now().UnixMilli()
	for i, sample := range []float64{1.0, 1.05, 0.99, 1.02, 0.98} {
		if err := AppendAnomalySample(s.DB(), "stage", "design", sample, now+int64(i)); err != nil {
			t.Fatalf("AppendAnomalySample: %v", err)
		}
	}
	window, err := QueryAnomalyWindow(s.DB(), "stage", "design", 100)
	if err != nil {
		t.Fatalf("QueryAnomalyWindow: %v", err)
	}
	if len(window) != 5 {
		t.Errorf("len = %d, want 5", len(window))
	}

	if window[0] != 1.0 || window[4] != 0.98 {
		t.Errorf("ordering wrong: window = %v", window)
	}
}

func TestAppendAnomalySampleValidation(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := AppendAnomalySample(s.DB(), "", "design", 1.0, 0); err == nil {
		t.Error("err = nil, want error on empty scope")
	}
	if err := AppendAnomalySample(s.DB(), "stage", "", 1.0, 0); err == nil {
		t.Error("err = nil, want error on empty scope_value")
	}
}

func TestQueryAnomalyWindowLimitTrimsToMostRecent(t *testing.T) {
	s := openMigratedPauseStore(t)
	now := time.Now().UnixMilli()
	for i := 0; i < 10; i++ {
		_ = AppendAnomalySample(s.DB(), "stage", "design", float64(i), now+int64(i))
	}
	window, _ := QueryAnomalyWindow(s.DB(), "stage", "design", 3)
	if len(window) != 3 {
		t.Fatalf("len = %d, want 3", len(window))
	}

	if window[0] != 7 || window[1] != 8 || window[2] != 9 {
		t.Errorf("window = %v, want [7 8 9]", window)
	}
}

func TestQueryAnomalyWindowNoLimitReturnsAll(t *testing.T) {
	s := openMigratedPauseStore(t)
	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		_ = AppendAnomalySample(s.DB(), "stage", "design", float64(i), now+int64(i))
	}
	window, _ := QueryAnomalyWindow(s.DB(), "stage", "design", 0)
	if len(window) != 5 {
		t.Errorf("len = %d, want 5", len(window))
	}
}

func TestPruneAnomalySamplesOlderThan(t *testing.T) {
	s := openMigratedPauseStore(t)
	base := int64(1000)
	for i, ms := range []int64{base, base + 100, base + 200, base + 300} {
		_ = AppendAnomalySample(s.DB(), "stage", "design", float64(i), ms)
	}
	deleted, err := PruneAnomalySamplesOlderThan(s.DB(), base+150)
	if err != nil {
		t.Fatalf("PruneAnomalySamplesOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	window, _ := QueryAnomalyWindow(s.DB(), "stage", "design", 100)
	if len(window) != 2 {
		t.Errorf("remaining = %d, want 2", len(window))
	}
}

func TestUpsertBudgetPauseSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := UpsertBudgetPause(s.DB(), "stage", "design", "r", 0, 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestGetBudgetPauseSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	_, _, err := GetBudgetPause(s.DB(), "stage", "design")
	if err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestDeleteBudgetPauseSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := DeleteBudgetPause(s.DB(), "stage", "design"); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestListActiveBudgetPausesSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	_, err := ListActiveBudgetPauses(s.DB())
	if err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestListActiveBudgetPausesScanErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "r", 100, 200)
	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}
	if _, err := ListActiveBudgetPauses(s.DB()); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestListActiveBudgetPausesRowsErrPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "r", 100, 200)
	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}
	if _, err := ListActiveBudgetPauses(s.DB()); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}

func TestInsertBudgetAnomalySQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := InsertBudgetAnomaly(s.DB(), "stage", "design", 4.5, 1.0, 0.1, 60, 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestListBudgetAnomaliesSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if _, err := ListBudgetAnomalies(s.DB(), 100); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestListBudgetAnomaliesScanErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = InsertBudgetAnomaly(s.DB(), "stage", "design", 4.5, 1.0, 0.1, 60, 100)
	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}
	if _, err := ListBudgetAnomalies(s.DB(), 100); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestListBudgetAnomaliesRowsErrPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = InsertBudgetAnomaly(s.DB(), "stage", "design", 4.5, 1.0, 0.1, 60, 100)
	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}
	if _, err := ListBudgetAnomalies(s.DB(), 100); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}

func TestAppendAnomalySampleSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := AppendAnomalySample(s.DB(), "stage", "design", 1.0, 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestQueryAnomalyWindowSQLErrorPropagatedWithLimit(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if _, err := QueryAnomalyWindow(s.DB(), "stage", "design", 100); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestQueryAnomalyWindowSQLErrorPropagatedNoLimit(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if _, err := QueryAnomalyWindow(s.DB(), "stage", "design", 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestQueryAnomalyWindowScanErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = AppendAnomalySample(s.DB(), "stage", "design", 1.0, 100)
	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}
	if _, err := QueryAnomalyWindow(s.DB(), "stage", "design", 100); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestQueryAnomalyWindowRowsErrPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = AppendAnomalySample(s.DB(), "stage", "design", 1.0, 100)
	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}
	if _, err := QueryAnomalyWindow(s.DB(), "stage", "design", 100); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}

func TestPruneAnomalySamplesOlderThanSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if _, err := PruneAnomalySamplesOlderThan(s.DB(), 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}

func TestGetBudgetPauseScanErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = UpsertBudgetPause(s.DB(), "stage", "design", "r", 100, 200)
	_ = s.Close()

	_, _, err := GetBudgetPause(s.DB(), "stage", "design")
	if err == nil {
		t.Error("err = nil, want generic scan error")
	}
}

func TestPruneAnomalySamplesRowsAffectedErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	orig := rowsAffectedFn
	t.Cleanup(func() { rowsAffectedFn = orig })
	rowsAffectedFn = func(sql.Result) (int64, error) {
		return 0, errors.New("injected rows-affected error")
	}
	if _, err := PruneAnomalySamplesOlderThan(s.DB(), 0); err == nil {
		t.Error("err = nil, want injected rows-affected error")
	}
}

// TestAppendAnomalySampleByCostIDIdempotent is the C-2 storage-layer
// regression: same (scope, scope_value, cost_id) MUST collapse to one
// row via INSERT OR IGNORE.
func TestAppendAnomalySampleByCostIDIdempotent(t *testing.T) {
	s := openMigratedPauseStore(t)
	for i := 0; i < 5; i++ {
		if err := AppendAnomalySampleByCostID(
			s.DB(), "stage", "design", 42, 1.50, int64(100+i),
		); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	row := s.DB().QueryRow(
		`SELECT COUNT(*) FROM budget_anomaly_samples WHERE scope = 'stage' AND scope_value = 'design' AND cost_id = 42`,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (INSERT OR IGNORE keyed on (scope, scope_value, cost_id))", count)
	}
}

func TestAppendAnomalySampleByCostIDDistinctCostIDsAppendIndependently(t *testing.T) {
	s := openMigratedPauseStore(t)
	for cid := int64(1); cid <= 5; cid++ {
		if err := AppendAnomalySampleByCostID(
			s.DB(), "stage", "design", cid, 1.0, 100+cid,
		); err != nil {
			t.Fatalf("cid=%d: %v", cid, err)
		}
	}
	row := s.DB().QueryRow(
		`SELECT COUNT(*) FROM budget_anomaly_samples WHERE scope = 'stage' AND scope_value = 'design'`,
	)
	var count int
	_ = row.Scan(&count)
	if count != 5 {
		t.Errorf("count = %d, want 5 (distinct cost_ids each appended)", count)
	}
}

func TestAppendAnomalySampleByCostIDValidation(t *testing.T) {
	s := openMigratedPauseStore(t)
	if err := AppendAnomalySampleByCostID(s.DB(), "", "design", 1, 1.0, 0); err == nil {
		t.Error("err = nil, want error on empty scope")
	}
	if err := AppendAnomalySampleByCostID(s.DB(), "stage", "", 1, 1.0, 0); err == nil {
		t.Error("err = nil, want error on empty scope_value")
	}
}

func TestAppendAnomalySampleByCostIDSQLErrorPropagated(t *testing.T) {
	s := openMigratedPauseStore(t)
	_ = s.Close()
	if err := AppendAnomalySampleByCostID(s.DB(), "stage", "design", 1, 1.0, 0); err == nil {
		t.Error("err = nil, want SQL error")
	}
}
