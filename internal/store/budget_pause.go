// SPDX-License-Identifier: MIT
// budget_pauses + budget_anomalies + budget_anomaly_samples. Engine
// access is mediated via internal/daemon/dispatcheradapter/budget_hooks.go;
// internal/budget/ never imports this package directly (invariant).
//
// This file is the storage-layer counterpart to internal/budget/pause.go.
// Migration 052 declared the three tables;
// these wrappers expose typed Go-level access.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"
)

var ErrAnomalyNaN = errors.New("store: budget_anomalies numeric input is NaN/Inf")

type BudgetPauseRow struct {
	Scope        string
	ScopeValue   string
	Reason       string
	StartedAt    time.Time
	AutoResumeAt time.Time
}

func UpsertBudgetPause(db *sql.DB, scope, scopeValue, reason string, startedAtMs, autoResumeAtMs int64) error {
	if scope == "" || scopeValue == "" {
		return errors.New("UpsertBudgetPause: scope and scopeValue required")
	}
	_, err := db.Exec(
		`INSERT INTO budget_pauses (scope, scope_value, reason, started_at, auto_resume_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(scope, scope_value) DO UPDATE
		   SET reason = excluded.reason,
		       started_at = excluded.started_at,
		       auto_resume_at = excluded.auto_resume_at`,
		scope, scopeValue, reason, startedAtMs, autoResumeAtMs,
	)
	if err != nil {
		return fmt.Errorf("upsert budget_pauses: %w", err)
	}
	return nil
}

func GetBudgetPause(db *sql.DB, scope, scopeValue string) (active bool, autoResumeAtMs int64, err error) {
	row := db.QueryRow(
		`SELECT auto_resume_at FROM budget_pauses WHERE scope = ? AND scope_value = ?`,
		scope, scopeValue,
	)
	if err = row.Scan(&autoResumeAtMs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("scan budget_pauses: %w", err)
	}
	return true, autoResumeAtMs, nil
}

func DeleteBudgetPause(db *sql.DB, scope, scopeValue string) error {
	_, err := db.Exec(`DELETE FROM budget_pauses WHERE scope = ? AND scope_value = ?`, scope, scopeValue)
	if err != nil {
		return fmt.Errorf("delete budget_pauses: %w", err)
	}
	return nil
}

func DeleteBudgetPauseIfExpired(db *sql.DB, scope, scopeValue string, beforeMs int64) error {
	_, err := db.Exec(
		`DELETE FROM budget_pauses
		 WHERE scope = ? AND scope_value = ?
		   AND auto_resume_at > 0
		   AND auto_resume_at <= ?`,
		scope, scopeValue, beforeMs,
	)
	if err != nil {
		return fmt.Errorf("delete-if-expired budget_pauses: %w", err)
	}
	return nil
}

func ListActiveBudgetPauses(db *sql.DB) ([]BudgetPauseRow, error) {
	rows, err := db.Query(
		`SELECT scope, scope_value, reason, started_at, auto_resume_at
		 FROM budget_pauses ORDER BY started_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query budget_pauses: %w", err)
	}
	defer rows.Close()
	var out []BudgetPauseRow
	for rows.Next() {
		var r BudgetPauseRow
		var startedMs, autoMs int64
		if err := scanFn(rows, &r.Scope, &r.ScopeValue, &r.Reason, &startedMs, &autoMs); err != nil {
			return nil, fmt.Errorf("scan budget_pauses row: %w", err)
		}
		r.StartedAt = time.UnixMilli(startedMs)
		if autoMs > 0 {
			r.AutoResumeAt = time.UnixMilli(autoMs)
		}
		out = append(out, r)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate budget_pauses rows: %w", err)
	}
	return out, nil
}

func InsertBudgetAnomaly(db *sql.DB, scope, scopeValue string, zScore, mean, std float64, windowSize int, detectedAtMs int64) error {
	if scope == "" || scopeValue == "" {
		return errors.New("InsertBudgetAnomaly: scope and scopeValue required")
	}
	if math.IsNaN(zScore) || math.IsInf(zScore, 0) {
		return fmt.Errorf("%w: z_score=%v", ErrAnomalyNaN, zScore)
	}
	if math.IsNaN(mean) || math.IsInf(mean, 0) {
		return fmt.Errorf("%w: mean=%v", ErrAnomalyNaN, mean)
	}
	if math.IsNaN(std) || math.IsInf(std, 0) {
		return fmt.Errorf("%w: std=%v", ErrAnomalyNaN, std)
	}
	_, err := db.Exec(
		`INSERT INTO budget_anomalies (scope, scope_value, z_score, mean, std, window_size, detected_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scope, scopeValue, zScore, mean, std, windowSize, detectedAtMs,
	)
	if err != nil {
		return fmt.Errorf("insert budget_anomalies: %w", err)
	}
	return nil
}

type BudgetAnomalyRow struct {
	ID         int64
	Scope      string
	ScopeValue string
	ZScore     float64
	Mean       float64
	Std        float64
	WindowSize int
	DetectedAt time.Time
}

func ListBudgetAnomalies(db *sql.DB, limit int) ([]BudgetAnomalyRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT id, scope, scope_value, z_score, mean, std, window_size, detected_at
		 FROM budget_anomalies ORDER BY detected_at DESC, id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query budget_anomalies: %w", err)
	}
	defer rows.Close()
	var out []BudgetAnomalyRow
	for rows.Next() {
		var r BudgetAnomalyRow
		var ms int64
		if err := scanFn(rows, &r.ID, &r.Scope, &r.ScopeValue, &r.ZScore, &r.Mean, &r.Std, &r.WindowSize, &ms); err != nil {
			return nil, fmt.Errorf("scan budget_anomalies row: %w", err)
		}
		r.DetectedAt = time.UnixMilli(ms)
		out = append(out, r)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate budget_anomalies rows: %w", err)
	}
	return out, nil
}

func AppendAnomalySample(db *sql.DB, scope, scopeValue string, sampleUSD float64, sampledAtMs int64) error {
	return AppendAnomalySampleByCostID(db, scope, scopeValue, uniqueLegacyCostIDFn(), sampleUSD, sampledAtMs)
}

var uniqueLegacyCostIDFn = nextLegacyCostID

var legacyCostIDCounter int64

func nextLegacyCostID() int64 {

	const start = -int64(1) << 32
	v := atomic.AddInt64(&legacyCostIDCounter, -1)
	return start + v
}

// AppendAnomalySampleByCostID appends one cost-delta sample keyed by
// cost_id for retry idempotency. Uses INSERT OR IGNORE so a partial-
// failure retry of PostCallWithCost with the same cost_id collapses to
// one row at the SQL layer (mirrors cost_axis_tags pattern).
//
// cost_id MUST be > 0 for the idempotency guarantee. cost_id = 0 is the
// opt-out sentinel; legacy migration rows use negative ids to avoid
// colliding with both the sentinel and real positive ids.
//
// Post-review C-2 fix: this is the writer PostCallWithCost now uses;
// without it, retries inflated the rolling-window denominator and
// corrupted the z-score baseline.
func AppendAnomalySampleByCostID(db *sql.DB, scope, scopeValue string, costID int64, sampleUSD float64, sampledAtMs int64) error {
	if scope == "" || scopeValue == "" {
		return errors.New("AppendAnomalySampleByCostID: scope and scopeValue required")
	}
	_, err := db.Exec(
		`INSERT OR IGNORE INTO budget_anomaly_samples
		   (scope, scope_value, cost_id, sample_usd, sampled_at)
		 VALUES (?, ?, ?, ?, ?)`,
		scope, scopeValue, costID, sampleUSD, sampledAtMs,
	)
	if err != nil {
		return fmt.Errorf("insert budget_anomaly_samples: %w", err)
	}
	return nil
}

func QueryAnomalyWindow(db *sql.DB, scope, scopeValue string, limit int) ([]float64, error) {
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.Query(
			`SELECT sample_usd FROM (
			   SELECT sample_usd, sampled_at FROM budget_anomaly_samples
			   WHERE scope = ? AND scope_value = ?
			   ORDER BY sampled_at DESC LIMIT ?
			 ) ORDER BY sampled_at ASC`,
			scope, scopeValue, limit,
		)
	} else {
		rows, err = db.Query(
			`SELECT sample_usd FROM budget_anomaly_samples
			 WHERE scope = ? AND scope_value = ? ORDER BY sampled_at ASC`,
			scope, scopeValue,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query budget_anomaly_samples: %w", err)
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := scanFn(rows, &v); err != nil {
			return nil, fmt.Errorf("scan budget_anomaly_samples row: %w", err)
		}
		out = append(out, v)
	}
	if err := rowsErrFn(rows); err != nil {
		return nil, fmt.Errorf("iterate budget_anomaly_samples rows: %w", err)
	}
	return out, nil
}

func PruneAnomalySamplesOlderThan(db *sql.DB, cutoffMs int64) (int64, error) {
	res, err := db.Exec(`DELETE FROM budget_anomaly_samples WHERE sampled_at < ?`, cutoffMs)
	if err != nil {
		return 0, fmt.Errorf("prune budget_anomaly_samples: %w", err)
	}
	n, err := rowsAffectedFn(res)
	if err != nil {
		return 0, fmt.Errorf("rows-affected prune: %w", err)
	}
	return n, nil
}

var rowsAffectedFn = func(res sql.Result) (int64, error) {
	return res.RowsAffected()
}
