// SPDX-License-Identifier: MIT
package scheduleradapter

//
// Bridges *store.Store CRUD primitives (internal/store/schedules.go)
// for the schedules + schedule_history tables (migration 063) to the
// scheduler subsystem. ships the adapter operating directly
// on store.ScheduleRow / store.ScheduleHistoryRow value types; Phase
// D-2 lands the internal/scheduler package with its own Schedule +
// HistoryEntry types and adds a thin translation layer on top.
//
// invariant boundary: internal/scheduler MUST NEVER import
// internal/store. This adapter's import list is the single legitimate
// co-location point — verified by the compliance test
// inv_zen_122_inv_zen_031_plan7_packages_test.go.
//
// Constructor pattern: New(*store.Store) panics on nil. Same defensive
// contract as bypassadapter / quotaadapter / projectctxadapter — daemon
// wiring guarantees a real store; a nil here is a programming error
// caught at boot rather than at first method call.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	s *store.Store
}

func New(s *store.Store) *Adapter {
	if s == nil {
		panic("scheduleradapter.New: store is nil")
	}
	return &Adapter{s: s}
}

func (a *Adapter) Insert(ctx context.Context, row store.ScheduleRow) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scheduleradapter.Insert: %w", err)
	}
	if err := store.InsertSchedule(a.s.DB(), row); err != nil {
		return fmt.Errorf("scheduleradapter.Insert(%q): %w", row.ID, err)
	}
	return nil
}

func (a *Adapter) Get(ctx context.Context, id string) (*store.ScheduleRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scheduleradapter.Get: %w", err)
	}
	row, err := store.GetSchedule(a.s.DB(), id)
	if err != nil {
		return nil, fmt.Errorf("scheduleradapter.Get(%q): %w", id, err)
	}
	return row, nil
}

func (a *Adapter) List(ctx context.Context) ([]store.ScheduleRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scheduleradapter.List: %w", err)
	}
	rows, err := store.ListSchedules(a.s.DB())
	if err != nil {
		return nil, fmt.Errorf("scheduleradapter.List: %w", err)
	}
	return rows, nil
}

func (a *Adapter) ListDue(ctx context.Context, now time.Time) ([]store.ScheduleRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scheduleradapter.ListDue: %w", err)
	}
	rows, err := store.ListSchedulesDue(a.s.DB(), now)
	if err != nil {
		return nil, fmt.Errorf("scheduleradapter.ListDue: %w", err)
	}
	return rows, nil
}

func (a *Adapter) Update(ctx context.Context, row store.ScheduleRow) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scheduleradapter.Update: %w", err)
	}
	if err := store.UpdateSchedule(a.s.DB(), row); err != nil {

		if errors.Is(err, store.ErrScheduleNotFound) {
			return err
		}
		return fmt.Errorf("scheduleradapter.Update(%q): %w", row.ID, err)
	}
	return nil
}

func (a *Adapter) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scheduleradapter.Delete: %w", err)
	}
	if err := store.DeleteSchedule(a.s.DB(), id); err != nil {
		if errors.Is(err, store.ErrScheduleNotFound) {
			return err
		}
		return fmt.Errorf("scheduleradapter.Delete(%q): %w", id, err)
	}
	return nil
}

func (a *Adapter) AppendHistory(ctx context.Context, row store.ScheduleHistoryRow) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("scheduleradapter.AppendHistory: %w", err)
	}
	if err := store.AppendScheduleHistory(a.s.DB(), row); err != nil {
		return fmt.Errorf("scheduleradapter.AppendHistory(%q): %w", row.ScheduleID, err)
	}
	return nil
}

func (a *Adapter) QueryHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]store.ScheduleHistoryRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scheduleradapter.QueryHistory: %w", err)
	}
	rows, err := store.QueryScheduleHistory(a.s.DB(), scheduleID, from, to)
	if err != nil {
		return nil, fmt.Errorf("scheduleradapter.QueryHistory(%q): %w", scheduleID, err)
	}
	return rows, nil
}
