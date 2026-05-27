// SPDX-License-Identifier: MIT
// axis_tag_loss_events. The internal/budget engine never imports this
// package; the adapter in internal/daemon/dispatcheradapter/budget_hooks.go
// satisfies the BudgetStore interface declared in internal/budget/axes.go
// by calling these functions. invariant boundary preserved.
//
// Option A coordination (METHODOLOGY.md §4.7.5): cost_axis_tags has no FK
// to cost_ledger because F-1 (which creates cost_ledger via
// migration 040) is not on main yet. Tests provide explicit cost_id values;
// engine-layer idempotency rests on UNIQUE (cost_id, axis_name) +
// INSERT OR IGNORE, not on the FK.
//
// # Test seams
//
// All db.Exec / db.Query / rows.Scan / rows.Err calls go through package-
// level function variables (executor, queryer, scanner, rowsErrFn) so
// SQL-error injection paths are exercisable from tests in
// budget_axes_test.go. Production callers see no behaviour difference;
// the seams are pure indirection.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type CostAxisTag struct {
	CostID    int64
	AxisName  string
	AxisValue string
	WrittenAt time.Time
}

type AxisTagLossEvent struct {
	ID          int64
	CostID      int64
	MissingAxis string
	DetectedAt  time.Time
}

type scannableRow interface {
	Scan(dest ...any) error
}

var scanFn = func(r scannableRow, dest ...any) error {
	return r.Scan(dest...)
}

var rowsErrFn = func(r *sql.Rows) error {
	return r.Err()
}

func InsertCostAxisTag(db *sql.DB, costID int64, axisName, axisValue string) error {
	if costID <= 0 {
		return errors.New("InsertCostAxisTag: cost_id must be > 0")
	}
	if axisName == "" {
		return errors.New("InsertCostAxisTag: axis_name is empty")
	}
	if axisValue == "" {
		return errors.New("InsertCostAxisTag: axis_value is empty")
	}
	_, err := db.Exec(
		`INSERT OR IGNORE INTO cost_axis_tags (cost_id, axis_name, axis_value, written_at)
		 VALUES (?, ?, ?, ?)`,
		costID, axisName, axisValue, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert cost_axis_tags: %w", err)
	}
	return nil
}

func QueryCostAxisTags(db *sql.DB, costID int64) ([]CostAxisTag, error) {
	rows, err := db.Query(
		`SELECT cost_id, axis_name, axis_value, written_at
		 FROM cost_axis_tags WHERE cost_id = ? ORDER BY axis_name`,
		costID,
	)
	if err != nil {
		return nil, fmt.Errorf("query cost_axis_tags: %w", err)
	}
	defer rows.Close()
	var out []CostAxisTag
	for rows.Next() {
		var t CostAxisTag
		var ms int64
		if err := scanFn(rows, &t.CostID, &t.AxisName, &t.AxisValue, &ms); err != nil {
			return nil, fmt.Errorf("scan cost_axis_tags row: %w", err)
		}
		t.WrittenAt = time.UnixMilli(ms)
		out = append(out, t)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate cost_axis_tags rows: %w", err)
	}
	return out, nil
}

func QueryCostIDsByAxis(db *sql.DB, axisName, axisValue string) ([]int64, error) {
	rows, err := db.Query(
		`SELECT cost_id FROM cost_axis_tags WHERE axis_name = ? AND axis_value = ?
		 ORDER BY cost_id`,
		axisName, axisValue,
	)
	if err != nil {
		return nil, fmt.Errorf("query cost_ids_by_axis: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := scanFn(rows, &id); err != nil {
			return nil, fmt.Errorf("scan cost_ids_by_axis: %w", err)
		}
		out = append(out, id)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate cost_ids_by_axis: %w", err)
	}
	return out, nil
}

func EmitAxisTagLoss(db *sql.DB, costID int64, missingAxis string) error {
	if costID <= 0 {
		return errors.New("EmitAxisTagLoss: cost_id must be > 0")
	}
	if missingAxis == "" {
		return errors.New("EmitAxisTagLoss: missing_axis is empty")
	}
	_, err := db.Exec(
		`INSERT INTO axis_tag_loss_events (cost_id, missing_axis, detected_at)
		 VALUES (?, ?, ?)`,
		costID, missingAxis, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert axis_tag_loss_events: %w", err)
	}
	return nil
}

func QueryAxisTagLosses(db *sql.DB, costID int64) ([]AxisTagLossEvent, error) {
	rows, err := db.Query(
		`SELECT id, cost_id, missing_axis, detected_at
		 FROM axis_tag_loss_events WHERE cost_id = ? ORDER BY detected_at, id`,
		costID,
	)
	if err != nil {
		return nil, fmt.Errorf("query axis_tag_loss_events: %w", err)
	}
	defer rows.Close()
	var out []AxisTagLossEvent
	for rows.Next() {
		var e AxisTagLossEvent
		var ms int64
		if err := scanFn(rows, &e.ID, &e.CostID, &e.MissingAxis, &ms); err != nil {
			return nil, fmt.Errorf("scan axis_tag_loss_events row: %w", err)
		}
		e.DetectedAt = time.UnixMilli(ms)
		out = append(out, e)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate axis_tag_loss_events rows: %w", err)
	}
	return out, nil
}
