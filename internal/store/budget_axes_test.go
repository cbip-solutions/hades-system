package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openMigratedBudgetStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "budget.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertCostAxisTagSuccess(t *testing.T) {
	s := openMigratedBudgetStore(t)
	costID := int64(101)
	if err := InsertCostAxisTag(s.DB(), costID, "project", "internal-platform-x"); err != nil {
		t.Fatalf("InsertCostAxisTag: %v", err)
	}
	if err := InsertCostAxisTag(s.DB(), costID, "doctrine", "max-scope"); err != nil {
		t.Fatalf("InsertCostAxisTag doctrine: %v", err)
	}
	if err := InsertCostAxisTag(s.DB(), costID, "stage", "design"); err != nil {
		t.Fatalf("InsertCostAxisTag stage: %v", err)
	}
	if err := InsertCostAxisTag(s.DB(), costID, "task", "T-12"); err != nil {
		t.Fatalf("InsertCostAxisTag task: %v", err)
	}
	tags, err := QueryCostAxisTags(s.DB(), costID)
	if err != nil {
		t.Fatalf("QueryCostAxisTags: %v", err)
	}
	if len(tags) != 4 {
		t.Fatalf("len(tags) = %d, want 4", len(tags))
	}
	want := map[string]string{
		"project":  "internal-platform-x",
		"doctrine": "max-scope",
		"stage":    "design",
		"task":     "T-12",
	}
	for _, tag := range tags {
		if want[tag.AxisName] != tag.AxisValue {
			t.Errorf("axis %q = %q, want %q", tag.AxisName, tag.AxisValue, want[tag.AxisName])
		}
		if tag.CostID != costID {
			t.Errorf("cost_id = %d, want %d", tag.CostID, costID)
		}
		if tag.WrittenAt.IsZero() {
			t.Errorf("written_at is zero")
		}
	}
}

func TestInsertCostAxisTagIdempotentOnDuplicate(t *testing.T) {
	s := openMigratedBudgetStore(t)
	costID := int64(202)
	if err := InsertCostAxisTag(s.DB(), costID, "project", "internal-platform-x"); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	if err := InsertCostAxisTag(s.DB(), costID, "project", "different-value"); err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	tags, err := QueryCostAxisTags(s.DB(), costID)
	if err != nil {
		t.Fatalf("QueryCostAxisTags: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("len(tags) = %d, want 1 (duplicate ignored)", len(tags))
	}
	if tags[0].AxisValue != "internal-platform-x" {
		t.Errorf("axis value = %q, want internal-platform-x (first write wins)", tags[0].AxisValue)
	}
}

func TestInsertCostAxisTagValidation(t *testing.T) {
	s := openMigratedBudgetStore(t)
	cases := []struct {
		name      string
		costID    int64
		axisName  string
		axisValue string
		wantErr   bool
	}{
		{"zero cost_id", 0, "project", "x", true},
		{"negative cost_id", -1, "project", "x", true},
		{"empty axis_name", 1, "", "x", true},
		{"empty axis_value", 1, "project", "", true},
		{"valid", 1, "project", "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := InsertCostAxisTag(s.DB(), tc.costID, tc.axisName, tc.axisValue)
			if tc.wantErr && err == nil {
				t.Error("err = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

func TestQueryCostAxisTagsEmpty(t *testing.T) {
	s := openMigratedBudgetStore(t)
	tags, err := QueryCostAxisTags(s.DB(), 9999)
	if err != nil {
		t.Fatalf("QueryCostAxisTags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("len(tags) = %d, want 0", len(tags))
	}
}

func TestQueryCostIDsByAxis(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = InsertCostAxisTag(s.DB(), 301, "stage", "design")
	_ = InsertCostAxisTag(s.DB(), 302, "stage", "design")
	_ = InsertCostAxisTag(s.DB(), 303, "stage", "build")
	costs, err := QueryCostIDsByAxis(s.DB(), "stage", "design")
	if err != nil {
		t.Fatalf("QueryCostIDsByAxis: %v", err)
	}
	if len(costs) != 2 {
		t.Fatalf("len(costs) = %d, want 2", len(costs))
	}
	if costs[0] != 301 || costs[1] != 302 {
		t.Errorf("costs = %v, want [301 302]", costs)
	}
}

func TestQueryCostIDsByAxisEmpty(t *testing.T) {
	s := openMigratedBudgetStore(t)
	costs, err := QueryCostIDsByAxis(s.DB(), "stage", "nonexistent")
	if err != nil {
		t.Fatalf("QueryCostIDsByAxis: %v", err)
	}
	if len(costs) != 0 {
		t.Errorf("len(costs) = %d, want 0", len(costs))
	}
}

func TestEmitAxisTagLossWritesEvent(t *testing.T) {
	s := openMigratedBudgetStore(t)
	costID := int64(404)
	if err := EmitAxisTagLoss(s.DB(), costID, "doctrine"); err != nil {
		t.Fatalf("EmitAxisTagLoss: %v", err)
	}
	losses, err := QueryAxisTagLosses(s.DB(), costID)
	if err != nil {
		t.Fatalf("QueryAxisTagLosses: %v", err)
	}
	if len(losses) != 1 {
		t.Fatalf("len(losses) = %d, want 1", len(losses))
	}
	if losses[0].MissingAxis != "doctrine" {
		t.Errorf("missing_axis = %q, want doctrine", losses[0].MissingAxis)
	}
	if losses[0].CostID != costID {
		t.Errorf("cost_id = %d, want %d", losses[0].CostID, costID)
	}
	if losses[0].DetectedAt.IsZero() {
		t.Error("detected_at is zero")
	}
	if losses[0].ID <= 0 {
		t.Errorf("id = %d, want > 0", losses[0].ID)
	}
}

func TestEmitAxisTagLossValidation(t *testing.T) {
	s := openMigratedBudgetStore(t)
	cases := []struct {
		name        string
		costID      int64
		missingAxis string
		wantErr     bool
	}{
		{"zero cost_id", 0, "project", true},
		{"negative cost_id", -1, "project", true},
		{"empty missing_axis", 1, "", true},
		{"valid", 1, "project", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EmitAxisTagLoss(s.DB(), tc.costID, tc.missingAxis)
			if tc.wantErr && err == nil {
				t.Error("err = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

func TestQueryAxisTagLossesEmpty(t *testing.T) {
	s := openMigratedBudgetStore(t)
	losses, err := QueryAxisTagLosses(s.DB(), 9999)
	if err != nil {
		t.Fatalf("QueryAxisTagLosses: %v", err)
	}
	if len(losses) != 0 {
		t.Errorf("len(losses) = %d, want 0", len(losses))
	}
}

func TestEmitAxisTagLossOrderedByDetectedAt(t *testing.T) {
	s := openMigratedBudgetStore(t)
	costID := int64(505)
	for _, axis := range []string{"project", "doctrine", "stage"} {
		if err := EmitAxisTagLoss(s.DB(), costID, axis); err != nil {
			t.Fatal(err)
		}
	}
	losses, err := QueryAxisTagLosses(s.DB(), costID)
	if err != nil {
		t.Fatal(err)
	}
	if len(losses) != 3 {
		t.Fatalf("len(losses) = %d, want 3", len(losses))
	}

	for i := 1; i < len(losses); i++ {
		if losses[i].ID < losses[i-1].ID {
			t.Errorf("losses[%d].ID=%d < losses[%d].ID=%d (not ordered)",
				i, losses[i].ID, i-1, losses[i-1].ID)
		}
	}
}

func closedDBStore(t *testing.T) *Store {
	t.Helper()
	s := openMigratedBudgetStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return s
}

func TestInsertCostAxisTagSQLErrorPropagated(t *testing.T) {
	s := closedDBStore(t)
	if err := InsertCostAxisTag(s.DB(), 1, "stage", "design"); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestQueryCostAxisTagsSQLErrorPropagated(t *testing.T) {
	s := closedDBStore(t)
	if _, err := QueryCostAxisTags(s.DB(), 1); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestQueryCostIDsByAxisSQLErrorPropagated(t *testing.T) {
	s := closedDBStore(t)
	if _, err := QueryCostIDsByAxis(s.DB(), "stage", "design"); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestEmitAxisTagLossSQLErrorPropagated(t *testing.T) {
	s := closedDBStore(t)
	if err := EmitAxisTagLoss(s.DB(), 1, "stage"); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestQueryAxisTagLossesSQLErrorPropagated(t *testing.T) {
	s := closedDBStore(t)
	if _, err := QueryAxisTagLosses(s.DB(), 1); err == nil {
		t.Error("err = nil, want SQL error after DB close")
	}
}

func TestQueryCostAxisTagsScanErrorPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = InsertCostAxisTag(s.DB(), 1, "project", "internal-platform-x")

	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}

	if _, err := QueryCostAxisTags(s.DB(), 1); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestQueryCostAxisTagsRowsErrPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = InsertCostAxisTag(s.DB(), 1, "project", "internal-platform-x")

	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}

	if _, err := QueryCostAxisTags(s.DB(), 1); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}

func TestQueryCostIDsByAxisScanErrorPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = InsertCostAxisTag(s.DB(), 1, "project", "internal-platform-x")

	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}

	if _, err := QueryCostIDsByAxis(s.DB(), "project", "internal-platform-x"); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestQueryCostIDsByAxisRowsErrPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = InsertCostAxisTag(s.DB(), 1, "project", "internal-platform-x")

	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}

	if _, err := QueryCostIDsByAxis(s.DB(), "project", "internal-platform-x"); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}

func TestQueryAxisTagLossesScanErrorPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = EmitAxisTagLoss(s.DB(), 1, "stage")

	orig := scanFn
	t.Cleanup(func() { scanFn = orig })
	scanFn = func(scannableRow, ...any) error {
		return errors.New("injected scan error")
	}

	if _, err := QueryAxisTagLosses(s.DB(), 1); err == nil {
		t.Error("err = nil, want injected scan error")
	}
}

func TestQueryAxisTagLossesRowsErrPropagated(t *testing.T) {
	s := openMigratedBudgetStore(t)
	_ = EmitAxisTagLoss(s.DB(), 1, "stage")

	orig := rowsErrFn
	t.Cleanup(func() { rowsErrFn = orig })
	rowsErrFn = func(*sql.Rows) error {
		return errors.New("injected rows.Err error")
	}

	if _, err := QueryAxisTagLosses(s.DB(), 1); err == nil {
		t.Error("err = nil, want injected rows.Err error")
	}
}
