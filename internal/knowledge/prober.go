// SPDX-License-Identifier: MIT
// Package knowledge — prober.go
//
// task adapter: exposes a slim Prober implementation that the
// cli/doctor_knowledge.go layer consumes (cli.KnowledgeProber). The split
// keeps invariant clean (internal/cli imports internal/knowledge; this
// package does NOT import internal/store; the daemon assembles the
// runtime via the projectctxadapter pattern).
//
// The Prober is read-only: it queries the index DB and an in-memory
// budget/watcher snapshot owned by the daemon. No mutations.
//
// Design split:
//
// - ships the FTS5 + knowledge_meta schema, the IndexDoc / Open
// / Init / Reindex hot path, and the fsnotify Watcher loop with
// debounce + CPU throttle (internal/knowledge/watcher.go).
//
// - adds the Prober that exposes that subsystem state to the
// CLI doctor probe. Rather than retrofitting Watcher with a public
// LastHeartbeat() accessor (which would surface internal scheduling
// state), the Prober consumes injected closures: HeartbeatFn returns
// the watcher tick timestamp, BudgetSnapshotFn returns the cpu
// usage/thresholds. The daemon main loop wires these closures over
// the actual Watcher / Budget instances at startup, keeping
// surface narrow.
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// HeartbeatFn returns the timestamp of the watcher's most recent loop
// tick. Zero time signals "watcher never started" (e.g. fsnotify init
// failed; daemon downgraded to cron-only mode).
//
// MUST be safe for concurrent use — multiple operators may run
// `hades doctor knowledge` concurrently.
type HeartbeatFn func() time.Time

// BudgetSnapshotFn returns (used, warn, fail) for the indexer CPU budget,
// resolved against the active doctrine at call time. If the budget
// tracker is unavailable returns
// (0, 0, 0, nil) which RunKnowledgeProbe interprets as ProbeOK with a
// passive "no budget tracker" message — the absent tracker is a
// degraded-mode signal, not a failure.
//
// MUST be safe for concurrent use.
type BudgetSnapshotFn func(ctx context.Context) (used, warn, fail int, err error)

type Prober struct {
	db *sql.DB

	heartbeat HeartbeatFn

	budget BudgetSnapshotFn

	mu sync.Mutex
}

var ErrProberNilArg = errors.New("knowledge.NewProber: nil argument")

// NewProber wires a Prober. Each constructor argument is either a
// borrowed DB handle (db) or a closure over live components owned by
// the daemon (heartbeat, budget). The Prober keeps borrowed references;
// caller owns the lifecycle of every dependency.
//
// Caller MUST NOT call Close on the Prober — there is no Close. The
// underlying components own their cleanup. The closures MUST be
// goroutine-safe (multiple operators may run `hades doctor` concurrently).
//
// Panics on nil db, nil heartbeat, or nil budget — these are programmer
// errors at boot, not recoverable runtime conditions. The daemon's main
// loop is the single canonical caller; if it boots with a nil component
// the operator is in a partial-bootstrap state and the loud panic is
// the correct signal.
func NewProber(db *sql.DB, heartbeat HeartbeatFn, budget BudgetSnapshotFn) *Prober {
	if db == nil {
		panic(fmt.Errorf("%w: db", ErrProberNilArg))
	}
	if heartbeat == nil {
		panic(fmt.Errorf("%w: heartbeat", ErrProberNilArg))
	}
	if budget == nil {
		panic(fmt.Errorf("%w: budget", ErrProberNilArg))
	}
	return &Prober{
		db:        db,
		heartbeat: heartbeat,
		budget:    budget,
	}
}

func (p *Prober) IntegrityCheck(ctx context.Context) (string, error) {
	rows, err := p.db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return "", fmt.Errorf("knowledge.Prober.IntegrityCheck: %w", err)
	}
	defer rows.Close()
	var lines []byte
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", fmt.Errorf("knowledge.Prober.IntegrityCheck scan: %w", err)
		}
		if len(lines) > 0 {
			lines = append(lines, '\n')
		}
		lines = append(lines, s...)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("knowledge.Prober.IntegrityCheck rows: %w", err)
	}
	return string(lines), nil
}

func (p *Prober) LastIndexedAt(ctx context.Context) (time.Time, error) {
	var ts sql.NullInt64
	err := p.db.QueryRowContext(ctx,
		`SELECT MAX(last_indexed) FROM knowledge_meta`).Scan(&ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("knowledge.Prober.LastIndexedAt: %w", err)
	}
	if !ts.Valid {
		return time.Time{}, nil
	}
	return time.Unix(0, ts.Int64), nil
}

func (p *Prober) IndexerCPUBudget(ctx context.Context) (used, warn, fail int, err error) {
	return p.budget(ctx)
}

func (p *Prober) WatcherHeartbeat(ctx context.Context) (time.Time, error) {
	return p.heartbeat(), nil
}

func (p *Prober) ExtensionHookNullCount(ctx context.Context) (nullCount, totalCount int, err error) {
	row := p.db.QueryRowContext(ctx,
		`SELECT
			SUM(CASE WHEN audit_chain_anchor IS NULL THEN 1 ELSE 0 END),
			COUNT(*)
		 FROM knowledge_meta`)
	var nullCol sql.NullInt64
	var total int
	if err = row.Scan(&nullCol, &total); err != nil {
		return 0, 0, fmt.Errorf("knowledge.Prober.ExtensionHookNullCount: %w", err)
	}
	if !nullCol.Valid {

		return 0, total, nil
	}
	return int(nullCol.Int64), total, nil
}
