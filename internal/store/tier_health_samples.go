// SPDX-License-Identifier: MIT
// tier_health_samples.go — CRUD for the tier_health_samples table (release
// ; migration 065; inv-hades-214).
//
// tier_health_samples is the per-provider health observability layer. One
// row per backend outcome — written by the dispatcher (cascade attempt
// outcomes) via the dispatcheradapter boundary bridge, and by the
// RecoveryScheduler (circuit-breaker probe outcomes). The operator-facing
// `hades orchestrator status` reads QueryTierHealthSamples to render
// per-provider success-rate + latency.
//
// Boundary (inv-hades-031): this file is in package store. The orchestrator /
// dispatcher packages MUST NOT import it directly — the
// dispatcheradapter.TierHealthSampleAdapter (Task 14) is the bridge.
//
// Concurrency InsertTierHealthSample is safe to call from multiple
// goroutines against the same *sql.DB (SQLite serialises writers via
// busy_timeout). No UNIQUE constraint — health samples are append-only and
// not idempotency-keyed.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type TierHealthSampleRow struct {
	ID           int64
	TS           time.Time
	Provider     string
	Tier         string
	Success      bool
	LatencyMS    int64
	ErrorPattern string
}

func InsertTierHealthSample(db *sql.DB, row TierHealthSampleRow) error {
	if row.Provider == "" {
		return errors.New("InsertTierHealthSample: provider is empty")
	}
	if row.Tier == "" {
		return errors.New("InsertTierHealthSample: tier is empty")
	}
	successInt := 0
	if row.Success {
		successInt = 1
	}
	_, err := db.Exec(
		`INSERT INTO tier_health_samples
			(ts, provider, tier, success, latency_ms, error_pattern)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.TS.UnixMilli(),
		row.Provider,
		row.Tier,
		successInt,
		row.LatencyMS,
		row.ErrorPattern,
	)
	if err != nil {
		return fmt.Errorf("insert tier_health_samples: %w", err)
	}
	return nil
}

func QueryTierHealthSamples(db *sql.DB, provider string, since time.Time) ([]TierHealthSampleRow, error) {
	rows, err := db.Query(
		`SELECT id, ts, provider, tier, success, latency_ms, error_pattern
		 FROM tier_health_samples
		 WHERE provider = ? AND ts >= ?
		 ORDER BY ts ASC`,
		provider, since.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("query tier_health_samples: %w", err)
	}
	defer rows.Close()
	var out []TierHealthSampleRow
	for rows.Next() {
		var r TierHealthSampleRow
		var tsMs int64
		var successInt int
		if err := rows.Scan(&r.ID, &tsMs, &r.Provider, &r.Tier, &successInt, &r.LatencyMS, &r.ErrorPattern); err != nil {
			return nil, fmt.Errorf("scan tier_health_samples row: %w", err)
		}
		r.TS = time.UnixMilli(tsMs)
		r.Success = successInt == 1
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tier_health_samples rows: %w", err)
	}
	return out, nil
}
