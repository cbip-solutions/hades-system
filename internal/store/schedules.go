// SPDX-License-Identifier: MIT
package store

//
// Schema lives in migrations/063_schedules.sql (Phase D-1 ships the
// migration alongside this CRUD). Master plan §"Migration numbering
// coordination" originally reserved slot 059 for Phase D under an
// A→C→B→D→E execution sequence; reality at HEAD on 2026-05-07 has
// 057 / 060 / 062 already taken (Phase A / B-6 / C-11) so Phase D-1
// picks 063 — the next free number on the daemon.db chain. Slot 059
// is reserved for Phase E inbox storage; slot 061 for Phase G
// knowledge-index DB (separate SQLite file, no daemon.db
// schemaVersion bump). schemaVersion bump path: 26 (Phase C-11) →
// 27 (this migration).
//
// inv-zen-031 boundary: internal/scheduler MUST NEVER import
// internal/store. The interface scheduler.Store + scheduler.HistoryStore
// (declared in D-2..D-12) are bridged to *store.Store by
// internal/daemon/scheduleradapter — that package's import list is
// the single legitimate co-location of internal/scheduler and
// internal/store anywhere in the codebase, enforced by the inv-zen-122
// compliance test (extended in Phase K).
//
// Time handling:
//   - INTEGER columns store UTC unix seconds (per inv-zen-005).
//   - The Go-side ScheduleRow / ScheduleHistoryRow use time.Time.
//   - last_run_at_unix / next_run_at_unix accept NULL: the Go layer
//     translates 0 unix-seconds → time.Time{} so callers can use
//     IsZero() consistently. Insert maps zero-value time.Time → NULL.
//
// Defense-in-depth pattern (mirrors tmux_session_state.go):
//   - SQL CHECK enforces enum range at the storage floor.
//   - Go-side validateScheduleTier / validateScheduleTriggerType /
//     validateScheduleMissPolicy / validateScheduleStatus reject
//     out-of-range values BEFORE the SQL CHECK fires (better error
//     message + faster short-circuit).
//   - validateScheduleTriggerConfig asserts JSON validity (rejects
//     empty + malformed) so a hand-edited DB row or logic bug
//     surfaces at the call site rather than at the next tick decode.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
)

var ErrDuplicateScheduleID = errors.New("schedules: id already recorded")

var ErrScheduleNotFound = errors.New("schedules: row not found")

type ScheduleRow struct {
	ID                    string
	Tier                  int
	ProjectAlias          string
	Action                string
	TriggerType           int
	TriggerConfig         string
	MissPolicy            int
	MissLookbackSeconds   int64
	CoalesceWindowSeconds int64
	LastRunAt             time.Time
	NextRunAt             time.Time
	Status                int
	CreatedAt             time.Time
	BearerTokenHash       string
}

type ScheduleHistoryRow struct {
	ID         int64
	ScheduleID string
	FiredAt    time.Time
	Outcome    int
	Reason     string
	CostUSD    float64
	DurationMs int64
}

func validateScheduleTier(t int) error {
	if t < 0 || t > 2 {
		return fmt.Errorf("schedules: tier %d out of range [0,2]", t)
	}
	return nil
}

func validateScheduleTriggerType(t int) error {
	if t < 0 || t > 2 {
		return fmt.Errorf("schedules: trigger_type %d out of range [0,2]", t)
	}
	return nil
}

func validateScheduleMissPolicy(p int) error {
	if p < 0 || p > 3 {
		return fmt.Errorf("schedules: miss_policy %d out of range [0,3]", p)
	}
	return nil
}

func validateScheduleStatus(s int) error {
	if s < 0 || s > 2 {
		return fmt.Errorf("schedules: status %d out of range [0,2]", s)
	}
	return nil
}

func validateHistoryOutcome(o int) error {
	if o < 0 || o > 3 {
		return fmt.Errorf("schedule_history: outcome %d out of range [0,3]", o)
	}
	return nil
}

func validateScheduleTriggerConfig(s string) error {
	if s == "" {
		return errors.New("schedules: trigger_config is empty")
	}
	if !json.Valid([]byte(s)) {
		return fmt.Errorf("schedules: trigger_config is not valid JSON: %q", s)
	}
	return nil
}

func scheduleTimeToUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Unix()
}

func scheduleUnixToTime(u sql.NullInt64) time.Time {
	if !u.Valid || u.Int64 == 0 {
		return time.Time{}
	}
	return time.Unix(u.Int64, 0).UTC()
}

func nullableBearer(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func InsertSchedule(db *sql.DB, row ScheduleRow) error {
	if row.ID == "" {
		return errors.New("InsertSchedule: id is empty")
	}
	if row.ProjectAlias == "" {
		return errors.New("InsertSchedule: project_alias is empty")
	}
	if row.Action == "" {
		return errors.New("InsertSchedule: action is empty")
	}
	if err := validateScheduleTier(row.Tier); err != nil {
		return fmt.Errorf("InsertSchedule: %w", err)
	}
	if err := validateScheduleTriggerType(row.TriggerType); err != nil {
		return fmt.Errorf("InsertSchedule: %w", err)
	}
	if err := validateScheduleTriggerConfig(row.TriggerConfig); err != nil {
		return fmt.Errorf("InsertSchedule: %w", err)
	}
	if err := validateScheduleMissPolicy(row.MissPolicy); err != nil {
		return fmt.Errorf("InsertSchedule: %w", err)
	}
	if err := validateScheduleStatus(row.Status); err != nil {
		return fmt.Errorf("InsertSchedule: %w", err)
	}
	if row.MissLookbackSeconds < 0 {
		return fmt.Errorf("InsertSchedule: miss_lookback_seconds %d must be >= 0", row.MissLookbackSeconds)
	}
	if row.CoalesceWindowSeconds < 0 {
		return fmt.Errorf("InsertSchedule: coalesce_window_seconds %d must be >= 0", row.CoalesceWindowSeconds)
	}
	created := row.CreatedAt
	if created.IsZero() {
		return errors.New("InsertSchedule: created_at is zero (must be set)")
	}
	_, err := db.Exec(
		`INSERT INTO schedules
		 (id, tier, project_alias, action, trigger_type, trigger_config,
		  miss_policy, miss_lookback_seconds, coalesce_window_seconds,
		  last_run_at_unix, next_run_at_unix, status, created_at_unix,
		  bearer_token_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Tier, row.ProjectAlias, row.Action, row.TriggerType,
		row.TriggerConfig, row.MissPolicy, row.MissLookbackSeconds,
		row.CoalesceWindowSeconds,
		scheduleTimeToUnix(row.LastRunAt), scheduleTimeToUnix(row.NextRunAt),
		row.Status, created.UTC().Unix(), nullableBearer(row.BearerTokenHash),
	)
	if err != nil {
		if isScheduleIDPKViolation(err) {
			return fmt.Errorf("%w: %v", ErrDuplicateScheduleID, err)
		}
		return fmt.Errorf("insert schedules: %w", err)
	}
	return nil
}

func isScheduleIDPKViolation(err error) bool {
	if errors.Is(err, sqlite3.CONSTRAINT_PRIMARYKEY) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "schedules.id") {
		return true
	}
	if strings.Contains(msg, "PRIMARY KEY") {
		return true
	}
	return false
}

func GetSchedule(db *sql.DB, id string) (*ScheduleRow, error) {
	if id == "" {
		return nil, errors.New("GetSchedule: id is empty")
	}
	var (
		row              ScheduleRow
		createdAtUnix    int64
		lastRunAtUnix    sql.NullInt64
		nextRunAtUnix    sql.NullInt64
		bearerTokenNullS sql.NullString
	)
	err := db.QueryRow(
		`SELECT id, tier, project_alias, action, trigger_type, trigger_config,
		        miss_policy, miss_lookback_seconds, coalesce_window_seconds,
		        last_run_at_unix, next_run_at_unix, status, created_at_unix,
		        bearer_token_hash
		 FROM schedules
		 WHERE id = ?`, id,
	).Scan(
		&row.ID, &row.Tier, &row.ProjectAlias, &row.Action, &row.TriggerType,
		&row.TriggerConfig, &row.MissPolicy, &row.MissLookbackSeconds,
		&row.CoalesceWindowSeconds, &lastRunAtUnix, &nextRunAtUnix,
		&row.Status, &createdAtUnix, &bearerTokenNullS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedules: %w", err)
	}
	row.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	row.LastRunAt = scheduleUnixToTime(lastRunAtUnix)
	row.NextRunAt = scheduleUnixToTime(nextRunAtUnix)
	if bearerTokenNullS.Valid {
		row.BearerTokenHash = bearerTokenNullS.String
	}
	return &row, nil
}

func ListSchedules(db *sql.DB) ([]ScheduleRow, error) {
	rows, err := db.Query(
		`SELECT id, tier, project_alias, action, trigger_type, trigger_config,
		        miss_policy, miss_lookback_seconds, coalesce_window_seconds,
		        last_run_at_unix, next_run_at_unix, status, created_at_unix,
		        bearer_token_hash
		 FROM schedules
		 ORDER BY created_at_unix DESC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	out := make([]ScheduleRow, 0)
	for rows.Next() {
		row, err := scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}
	return out, nil
}

func ListSchedulesDue(db *sql.DB, now time.Time) ([]ScheduleRow, error) {
	rows, err := db.Query(
		`SELECT id, tier, project_alias, action, trigger_type, trigger_config,
		        miss_policy, miss_lookback_seconds, coalesce_window_seconds,
		        last_run_at_unix, next_run_at_unix, status, created_at_unix,
		        bearer_token_hash
		 FROM schedules
		 WHERE status = 0 AND next_run_at_unix IS NOT NULL AND next_run_at_unix <= ?
		 ORDER BY next_run_at_unix ASC, id ASC`,
		now.UTC().Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("list schedules due: %w", err)
	}
	defer rows.Close()
	out := make([]ScheduleRow, 0)
	for rows.Next() {
		row, err := scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules due: %w", err)
	}
	return out, nil
}

func scanScheduleRow(rows *sql.Rows) (ScheduleRow, error) {
	var (
		row              ScheduleRow
		createdAtUnix    int64
		lastRunAtUnix    sql.NullInt64
		nextRunAtUnix    sql.NullInt64
		bearerTokenNullS sql.NullString
	)
	if err := rows.Scan(
		&row.ID, &row.Tier, &row.ProjectAlias, &row.Action, &row.TriggerType,
		&row.TriggerConfig, &row.MissPolicy, &row.MissLookbackSeconds,
		&row.CoalesceWindowSeconds, &lastRunAtUnix, &nextRunAtUnix,
		&row.Status, &createdAtUnix, &bearerTokenNullS,
	); err != nil {
		return ScheduleRow{}, fmt.Errorf("scan schedules: %w", err)
	}
	row.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	row.LastRunAt = scheduleUnixToTime(lastRunAtUnix)
	row.NextRunAt = scheduleUnixToTime(nextRunAtUnix)
	if bearerTokenNullS.Valid {
		row.BearerTokenHash = bearerTokenNullS.String
	}
	return row, nil
}

func UpdateSchedule(db *sql.DB, row ScheduleRow) error {
	if row.ID == "" {
		return errors.New("UpdateSchedule: id is empty")
	}
	if row.ProjectAlias == "" {
		return errors.New("UpdateSchedule: project_alias is empty")
	}
	if row.Action == "" {
		return errors.New("UpdateSchedule: action is empty")
	}
	if err := validateScheduleTier(row.Tier); err != nil {
		return fmt.Errorf("UpdateSchedule: %w", err)
	}
	if err := validateScheduleTriggerType(row.TriggerType); err != nil {
		return fmt.Errorf("UpdateSchedule: %w", err)
	}
	if err := validateScheduleTriggerConfig(row.TriggerConfig); err != nil {
		return fmt.Errorf("UpdateSchedule: %w", err)
	}
	if err := validateScheduleMissPolicy(row.MissPolicy); err != nil {
		return fmt.Errorf("UpdateSchedule: %w", err)
	}
	if err := validateScheduleStatus(row.Status); err != nil {
		return fmt.Errorf("UpdateSchedule: %w", err)
	}
	if row.MissLookbackSeconds < 0 {
		return fmt.Errorf("UpdateSchedule: miss_lookback_seconds %d must be >= 0", row.MissLookbackSeconds)
	}
	if row.CoalesceWindowSeconds < 0 {
		return fmt.Errorf("UpdateSchedule: coalesce_window_seconds %d must be >= 0", row.CoalesceWindowSeconds)
	}
	res, err := db.Exec(
		`UPDATE schedules SET
		    tier = ?, project_alias = ?, action = ?, trigger_type = ?,
		    trigger_config = ?, miss_policy = ?, miss_lookback_seconds = ?,
		    coalesce_window_seconds = ?, last_run_at_unix = ?,
		    next_run_at_unix = ?, status = ?, bearer_token_hash = ?
		 WHERE id = ?`,
		row.Tier, row.ProjectAlias, row.Action, row.TriggerType,
		row.TriggerConfig, row.MissPolicy, row.MissLookbackSeconds,
		row.CoalesceWindowSeconds,
		scheduleTimeToUnix(row.LastRunAt), scheduleTimeToUnix(row.NextRunAt),
		row.Status, nullableBearer(row.BearerTokenHash),
		row.ID,
	)
	if err != nil {
		return fmt.Errorf("update schedules: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update schedules: rows affected: %w", err)
	}
	if n == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func DeleteSchedule(db *sql.DB, id string) error {
	if id == "" {
		return errors.New("DeleteSchedule: id is empty")
	}
	res, err := db.Exec(`DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedules: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete schedules: rows affected: %w", err)
	}
	if n == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func AppendScheduleHistory(db *sql.DB, row ScheduleHistoryRow) error {
	if row.ScheduleID == "" {
		return errors.New("AppendScheduleHistory: schedule_id is empty")
	}
	if err := validateHistoryOutcome(row.Outcome); err != nil {
		return fmt.Errorf("AppendScheduleHistory: %w", err)
	}
	if row.CostUSD < 0 {
		return fmt.Errorf("AppendScheduleHistory: cost_usd %v must be >= 0", row.CostUSD)
	}
	if row.DurationMs < 0 {
		return fmt.Errorf("AppendScheduleHistory: duration_ms %d must be >= 0", row.DurationMs)
	}
	if row.FiredAt.IsZero() {
		return errors.New("AppendScheduleHistory: fired_at is zero (must be set)")
	}
	_, err := db.Exec(
		`INSERT INTO schedule_history
		 (schedule_id, fired_at_unix, outcome, reason, cost_usd, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.ScheduleID, row.FiredAt.UTC().Unix(), row.Outcome, row.Reason,
		row.CostUSD, row.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("insert schedule_history: %w", err)
	}
	return nil
}

func QueryScheduleHistory(db *sql.DB, scheduleID string, from, to time.Time) ([]ScheduleHistoryRow, error) {
	if scheduleID == "" {
		return nil, errors.New("QueryScheduleHistory: schedule_id is empty")
	}
	if from.After(to) {
		return nil, fmt.Errorf("QueryScheduleHistory: from %v is after to %v", from, to)
	}
	rows, err := db.Query(
		`SELECT id, schedule_id, fired_at_unix, outcome, reason, cost_usd, duration_ms
		 FROM schedule_history
		 WHERE schedule_id = ? AND fired_at_unix BETWEEN ? AND ?
		 ORDER BY fired_at_unix ASC, id ASC`,
		scheduleID, from.UTC().Unix(), to.UTC().Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("query schedule_history: %w", err)
	}
	defer rows.Close()
	out := make([]ScheduleHistoryRow, 0)
	for rows.Next() {
		var (
			r           ScheduleHistoryRow
			firedAtUnix int64
		)
		if err := rows.Scan(&r.ID, &r.ScheduleID, &firedAtUnix, &r.Outcome,
			&r.Reason, &r.CostUSD, &r.DurationMs); err != nil {
			return nil, fmt.Errorf("scan schedule_history: %w", err)
		}
		r.FiredAt = time.Unix(firedAtUnix, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule_history: %w", err)
	}
	return out, nil
}
