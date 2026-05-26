// SPDX-License-Identifier: MIT
// cost_ledger.go — CRUD for the cost_ledger table (Plan 3 Phase C, Task F-1).
//
// The cost_ledger table is the persistent source of truth for every LLM
// request's cost. Layer 2 of orchestrator observability (Layer 1 is
// bypass_audit). The in-memory CostCounters in
// internal/daemon/orchestrator/cost_counters.go (Task F-4) materialise
// rolling 30d / 24h / session-lifetime windows on top of this table and
// are rebuilt on daemon restart via QueryAllRecentCosts (inv-zen-065).
//
// Invariant inv-zen-062 (no double-charge) is anchored two ways:
//
//   - SQL-side: idempotency_key UNIQUE in migration 040. SQLite refuses
//     a second insert with the same key under any contention regime
//     (busy_timeout serialises writers).
//   - Go-side: the noDoubleCharge sentinel + ErrDuplicateIdempotency
//     translation. Removing the sentinel breaks `var _ = noDoubleCharge`
//     and any test using errors.Is — the compile-time guard.
//
// The error translation is defense-in-depth:
//
//  1. errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) — the typed extended
//     error code from the ncruces/go-sqlite3 driver. This is the
//     authoritative match.
//  2. strings.Contains on the error text — fallback if the driver ever
//     fails to thread the typed code (defense in depth; matches the
//     plan's defensive shape).
//
// Concurrency InsertCostLedger is safe to call from multiple goroutines
// against the same *sql.DB. Under the Open() pragmas (busy_timeout=5000,
// WAL journal) SQLite serialises writers transparently, and the UNIQUE
// constraint enforces no-double-charge across goroutines (verified by
// TestInsertCostLedgerConcurrent under -race).

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
)

var ErrDuplicateIdempotency = errors.New("cost_ledger: idempotency key already recorded")

type CostLedgerRow struct {
	ID                  int64
	IdempotencyKey      string
	TS                  time.Time
	Project             string
	Profile             string
	Provider            string
	Tier                string
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUSD             float64
	ConversationID      string
	SessionID           string
	RequestHash         []byte
}

func InsertCostLedger(db *sql.DB, row CostLedgerRow) (int64, error) {
	if row.IdempotencyKey == "" {
		return 0, errors.New("InsertCostLedger: idempotency_key is empty")
	}
	if row.Project == "" || row.Profile == "" || row.Tier == "" || row.Model == "" {
		return 0, errors.New("InsertCostLedger: project/profile/tier/model are required")
	}
	res, err := db.Exec(
		`INSERT INTO cost_ledger (
			idempotency_key, ts, project, profile, provider, tier, model,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
			cost_usd, conversation_id, session_id, request_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.IdempotencyKey,
		row.TS.UnixMilli(),
		row.Project,
		row.Profile,
		row.Provider,
		row.Tier,
		row.Model,
		row.InputTokens,
		row.OutputTokens,
		row.CacheReadTokens,
		row.CacheCreationTokens,
		row.CostUSD,
		nullableString(row.ConversationID),
		nullableString(row.SessionID),
		nullableBytes(row.RequestHash),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("%w: %v", ErrDuplicateIdempotency, err)
		}
		return 0, fmt.Errorf("insert cost_ledger: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("LastInsertId: %w", err)
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	if errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return true
	}
	if strings.Contains(msg, "constraint failed: cost_ledger.idempotency_key") {
		return true
	}
	return false
}

func QueryCostInWindow(db *sql.DB, project, profile, tier string, since time.Time) (totalUSD float64, count int, err error) {
	var sumNull sql.NullFloat64
	err = db.QueryRow(
		`SELECT COALESCE(SUM(cost_usd), 0), COUNT(*) FROM cost_ledger
		 WHERE project = ? AND profile = ? AND tier = ? AND ts >= ?`,
		project, profile, tier, since.UnixMilli(),
	).Scan(&sumNull, &count)
	if err != nil {
		return 0, 0, fmt.Errorf("query cost_in_window: %w", err)
	}
	return sumNull.Float64, count, nil
}

func QueryCostBySession(db *sql.DB, sessionID string) (totalUSD float64, count int, err error) {
	var sumNull sql.NullFloat64
	err = db.QueryRow(
		`SELECT COALESCE(SUM(cost_usd), 0), COUNT(*) FROM cost_ledger
		 WHERE session_id = ?`,
		sessionID,
	).Scan(&sumNull, &count)
	if err != nil {
		return 0, 0, fmt.Errorf("query cost_by_session: %w", err)
	}
	return sumNull.Float64, count, nil
}

func QueryAllRecentCosts(db *sql.DB, since time.Time) ([]CostLedgerRow, error) {
	rows, err := db.Query(
		`SELECT id, idempotency_key, ts, project, profile, provider, tier, model,
		        input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
		        cost_usd, COALESCE(conversation_id, ''), COALESCE(session_id, ''),
		        request_hash
		 FROM cost_ledger WHERE ts >= ? ORDER BY ts ASC`,
		since.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("query all_recent_costs: %w", err)
	}
	defer rows.Close()
	var out []CostLedgerRow
	for rows.Next() {
		var r CostLedgerRow
		var tsMs int64
		if err := rows.Scan(
			&r.ID, &r.IdempotencyKey, &tsMs, &r.Project, &r.Profile, &r.Provider, &r.Tier, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheCreationTokens,
			&r.CostUSD, &r.ConversationID, &r.SessionID, &r.RequestHash,
		); err != nil {
			return nil, fmt.Errorf("scan cost_ledger row: %w", err)
		}
		r.TS = time.UnixMilli(tsMs)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cost_ledger rows: %w", err)
	}
	return out, nil
}

func noDoubleCharge() error { return ErrDuplicateIdempotency }

var _ = noDoubleCharge
