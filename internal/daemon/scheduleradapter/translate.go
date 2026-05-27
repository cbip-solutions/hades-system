// SPDX-License-Identifier: MIT
// Package scheduleradapter — translate.go.
//
// Translation layer between scheduler.Schedule (canonical Go domain
// type with typed enums) and store.ScheduleRow (SQLite-flat
// representation with int enums + JSON-encoded TriggerConfig).
//
// invariant boundary: this is the SINGLE bridge point between the
// scheduler domain and SQLite persistence. internal/scheduler/* never
// imports internal/store; the daemon-side handler stack consumes
// scheduler.Schedule through the *Adapter satisfying the
// handlers.ScheduleStore interface (defined in
// internal/daemon/handlers/schedule_p7.go).
//
// Why a method-set on *Adapter (not free functions): the methods MUST
// satisfy handlers.ScheduleStore at the type level so the daemon can
// inject the *Adapter directly via Server.SetScheduleStore — the
// boundary is preserved AT THE INTERFACE LEVEL, not via runtime cast.
//
// Encoding choices (load-bearing):
// - TriggerConfig is marshalled as JSON with empty-field elision
// (the zero scheduler.TriggerConfig must round-trip cleanly).
// - Time conversions are direct: scheduler.Schedule uses time.Time
// unzoned; store.ScheduleRow stores unix-seconds. Both adapters
// normalise to UTC.
// - Enum casts are int(scheduler.Tier) etc.; the constants share
// wire integer values across both packages.
//
// On miss semantics (callers branch on nil):
// - Get returns (nil, nil) when the underlying store row is absent.
// - SoftDelete returns nil when the row is already gone (idempotent).
// - List + ListDue return ([]*scheduler.Schedule{}, nil) on no rows
// (empty non-nil slice — stable len() semantics for handler render).

package scheduleradapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/scheduler"
	"github.com/cbip-solutions/hades-system/internal/store"
)

func (a *Adapter) InsertSchedule(ctx context.Context, s *scheduler.Schedule) error {
	if s == nil {
		return fmt.Errorf("scheduleradapter.InsertSchedule: nil Schedule")
	}
	row, err := scheduleToStoreRow(s)
	if err != nil {
		return fmt.Errorf("scheduleradapter.InsertSchedule: %w", err)
	}
	return a.Insert(ctx, row)
}

func (a *Adapter) GetSchedule(ctx context.Context, id string) (*scheduler.Schedule, error) {
	row, err := a.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return storeRowToSchedule(row)
}

func (a *Adapter) ListSchedules(ctx context.Context, alias string) ([]*scheduler.Schedule, error) {
	rows, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*scheduler.Schedule, 0, len(rows))
	for i := range rows {
		if alias != "" && rows[i].ProjectAlias != alias {
			continue
		}
		s, terr := storeRowToSchedule(&rows[i])
		if terr != nil {
			return nil, terr
		}
		out = append(out, s)
	}
	return out, nil
}

func (a *Adapter) SoftDeleteSchedule(ctx context.Context, id string) error {
	row, err := a.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("scheduleradapter.SoftDeleteSchedule: %w", err)
	}
	if row == nil {

		return nil
	}

	row.Status = int(scheduler.StatusDisabled)
	_ = a.Update(ctx, *row)
	if err := a.Delete(ctx, id); err != nil {
		if errors.Is(err, store.ErrScheduleNotFound) {
			return nil
		}
		return fmt.Errorf("scheduleradapter.SoftDeleteSchedule(%q): %w", id, err)
	}
	return nil
}

func (a *Adapter) ListDueSchedules(ctx context.Context, until time.Time) ([]*scheduler.Schedule, error) {
	rows, err := a.ListDue(ctx, until)
	if err != nil {
		return nil, err
	}
	out := make([]*scheduler.Schedule, 0, len(rows))
	for i := range rows {
		s, terr := storeRowToSchedule(&rows[i])
		if terr != nil {
			return nil, terr
		}
		out = append(out, s)
	}
	return out, nil
}

func (a *Adapter) QueryScheduleHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]scheduler.HistoryEntry, error) {
	rows, err := a.QueryHistory(ctx, scheduleID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]scheduler.HistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, scheduler.HistoryEntry{
			ScheduleID: r.ScheduleID,
			FiredAt:    r.FiredAt,
			Outcome:    scheduler.Outcome(r.Outcome),
			Reason:     r.Reason,
			CostUSD:    r.CostUSD,
			DurationMs: r.DurationMs,
		})
	}
	return out, nil
}

type HandlerStore struct {
	a *Adapter
}

func NewHandlerStore(a *Adapter) *HandlerStore {
	if a == nil {
		panic("scheduleradapter.NewHandlerStore: adapter is nil")
	}
	return &HandlerStore{a: a}
}

func (h *HandlerStore) Insert(ctx context.Context, s *scheduler.Schedule) error {
	return h.a.InsertSchedule(ctx, s)
}

func (h *HandlerStore) Get(ctx context.Context, id string) (*scheduler.Schedule, error) {
	return h.a.GetSchedule(ctx, id)
}

func (h *HandlerStore) List(ctx context.Context, alias string) ([]*scheduler.Schedule, error) {
	return h.a.ListSchedules(ctx, alias)
}

func (h *HandlerStore) SoftDelete(ctx context.Context, id string) error {
	return h.a.SoftDeleteSchedule(ctx, id)
}

func (h *HandlerStore) QueryHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]scheduler.HistoryEntry, error) {
	return h.a.QueryScheduleHistory(ctx, scheduleID, from, to)
}

func (h *HandlerStore) ListDue(ctx context.Context, until time.Time) ([]*scheduler.Schedule, error) {
	return h.a.ListDueSchedules(ctx, until)
}

func scheduleToStoreRow(s *scheduler.Schedule) (store.ScheduleRow, error) {
	if err := s.Validate(); err != nil {
		return store.ScheduleRow{}, err
	}
	cfgJSON, err := json.Marshal(s.TriggerConfig)
	if err != nil {
		return store.ScheduleRow{}, fmt.Errorf("scheduleradapter: marshal TriggerConfig: %w", err)
	}
	return store.ScheduleRow{
		ID:                    s.ID,
		Tier:                  int(s.Tier),
		ProjectAlias:          s.ProjectAlias,
		Action:                s.Action,
		TriggerType:           int(s.TriggerType),
		TriggerConfig:         string(cfgJSON),
		MissPolicy:            int(s.MissPolicy),
		MissLookbackSeconds:   int64(s.MissLookback / time.Second),
		CoalesceWindowSeconds: int64(s.CoalesceWindow / time.Second),
		LastRunAt:             s.LastRunAt,
		NextRunAt:             s.NextRunAt,
		Status:                int(s.Status),
		CreatedAt:             s.CreatedAt,
		BearerTokenHash:       s.BearerTokenHash,
	}, nil
}

// storeRowToSchedule translates a store.ScheduleRow back to a
// scheduler.Schedule. Decodes TriggerConfig from JSON; rejects rows
// whose TriggerConfig blob is unparseable (caller surfaces as
// 500 — operator can grep for malformed rows in daemon.db).
//
// The BearerTokenHash field is mirrored onto both s.BearerTokenHash
// (adapter convenience) and s.TriggerConfig.BearerTokenHash (validate-
// gate authority). The two MUST stay in sync; the duplication is
// inherited from scheduler.Schedule's documented structure.
func storeRowToSchedule(row *store.ScheduleRow) (*scheduler.Schedule, error) {
	if row == nil {
		return nil, nil
	}
	var cfg scheduler.TriggerConfig
	if row.TriggerConfig != "" {
		if err := json.Unmarshal([]byte(row.TriggerConfig), &cfg); err != nil {
			return nil, fmt.Errorf("scheduleradapter: unmarshal TriggerConfig for id=%q: %w", row.ID, err)
		}
	}

	if cfg.BearerTokenHash == "" && row.BearerTokenHash != "" {
		cfg.BearerTokenHash = row.BearerTokenHash
	}
	return &scheduler.Schedule{
		ID:              row.ID,
		Tier:            scheduler.Tier(row.Tier),
		ProjectAlias:    row.ProjectAlias,
		Action:          row.Action,
		TriggerType:     scheduler.TriggerType(row.TriggerType),
		TriggerConfig:   cfg,
		MissPolicy:      scheduler.MissPolicy(row.MissPolicy),
		MissLookback:    time.Duration(row.MissLookbackSeconds) * time.Second,
		CoalesceWindow:  time.Duration(row.CoalesceWindowSeconds) * time.Second,
		LastRunAt:       row.LastRunAt,
		NextRunAt:       row.NextRunAt,
		Status:          scheduler.Status(row.Status),
		CreatedAt:       row.CreatedAt,
		BearerTokenHash: row.BearerTokenHash,
	}, nil
}
