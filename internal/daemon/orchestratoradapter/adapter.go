// SPDX-License-Identifier: MIT
// Package orchestratoradapter is the inv-hades-089 boundary bridge between
// internal/orchestrator/* and internal/store. It is the ONLY package in
// the daemon binary that imports BOTH internal/orchestrator/... AND
// internal/store. The orchestrator subsystems consume small persistence-
// shaped interfaces; this adapter satisfies them by routing to SQL on
// the supplied *store.Store.
//
// What the adapter satisfies (post-M-1 final shapes):
//
// - eventlog.RawEmitter — durable seam for *eventlog.Log
// (audit_events_raw round-trip; wrapping payload_json with
// reserved __session_id / __event_id / __ts_ns metadata so QueryRaw
// can rehydrate eventlog.Record instances cleanly).
// - safetynet.HealthWriter — durable seam for *safetynet.Regression
// (substrate_health Insert OR IGNORE for idempotency + Recent
// window queries).
// - amendment.EventEmitter — narrow interface returned by
// AmendmentEventEmitter() that wraps a *eventlog.Log constructed
// with this adapter as the RawEmitter and discards the int64 id.
// - amendment.ReloadSignal — returned by AmendmentReloadSignal()
// which delegates to the canonical *amendment.HTTPReloadSignal
// (POST /v1/doctrine/reload with retry semantics).
//
// What the adapter explicitly does NOT satisfy (the orchestrator never
// asked for these — kept here to prevent future drift):
//
// - amendment.Repository — there is NO such interface; ADRs
// are markdown files under architecture records
// materialised by amendment.{Proposer,Applier,Reverter}.
// - worktreepool.LeaseStore — there is NO such interface;
// leases are in-memory only (warm slice + leased map).
//
// Inv-hades-102 cost-ledger isolation: this adapter MUST NOT write the
// cost_ledger table directly. All cost-ledger writes flow through release
// dispatcher → internal/daemon/dispatcheradapter. Compliance test in
// tests/compliance/inv_hades_102_cost_ledger_isolation_test.go.
//
// Concurrency every public method takes ctx; the adapter holds a
// per-session counter behind a sync.Mutex for the int64 event_id
// monotonic-per-session contract; SQLite serialises writers underneath
// via its WAL/busy_timeout machinery. Close is idempotent; subsequent
// EmitRaw / Insert calls fail fast with a "closed" error.
package orchestratoradapter

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/amendment"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/clock"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/safetynet"
	"github.com/cbip-solutions/hades-system/internal/store"
)

type Adapter struct {
	store *store.Store

	mu                             sync.Mutex
	perSessionCounter              map[string]int64
	substrateHealthUniqueInstalled bool

	closed atomic.Bool
}

func New(s *store.Store) (*Adapter, error) {
	if s == nil {
		return nil, errors.New("orchestratoradapter: store must not be nil")
	}
	return &Adapter{
		store:             s,
		perSessionCounter: make(map[string]int64),
	}, nil
}

func (a *Adapter) Close() error {
	a.closed.Store(true)
	return nil
}

func (a *Adapter) EmitRaw(ctx context.Context, projectID, sessionID string, eventType int, payload []byte, ts int64) (int64, error) {
	if a.closed.Load() {
		return 0, errors.New("orchestratoradapter: closed")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if sessionID == "" {
		return 0, errors.New("orchestratoradapter: empty session_id")
	}
	if ts <= 0 {
		return 0, fmt.Errorf("orchestratoradapter: ts must be > 0, got %d", ts)
	}

	a.mu.Lock()
	a.perSessionCounter[sessionID]++
	id := a.perSessionCounter[sessionID]
	a.mu.Unlock()

	wrapped := map[string]any{
		"__session_id": sessionID,
		"__event_id":   id,
		"__ts_ns":      ts,
	}
	if len(payload) > 0 {
		var caller any
		if err := json.Unmarshal(payload, &caller); err == nil {
			wrapped["payload"] = caller
		} else {
			wrapped["payload_raw"] = string(payload)
		}
	}
	wrappedJSON, err := json.Marshal(wrapped)
	if err != nil {
		return 0, fmt.Errorf("orchestratoradapter: marshal wrapped payload: %w", err)
	}

	rowID, err := newRowID()
	if err != nil {
		return 0, fmt.Errorf("orchestratoradapter: generate row id: %w", err)
	}

	emittedAtSec := ts / int64(time.Second)
	if emittedAtSec <= 0 {

		emittedAtSec = 1
	}

	typeStr := eventlog.EventType(eventType).String()
	if typeStr == "" || typeStr == "EvtUnknown" {
		typeStr = fmt.Sprintf("%d", eventType)
	}

	if _, err := a.store.DB().ExecContext(ctx,
		`INSERT INTO audit_events_raw(id, project_id, type, payload_json, emitted_at)
		 VALUES (?, ?, ?, ?, ?)`,
		rowID, projectID, typeStr, string(wrappedJSON), emittedAtSec,
	); err != nil {
		return 0, fmt.Errorf("orchestratoradapter: insert audit_events_raw: %w", err)
	}
	return id, nil
}

func (a *Adapter) QueryRaw(ctx context.Context, sessionID string, since int64) ([]eventlog.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT type, payload_json, emitted_at FROM audit_events_raw
		 WHERE json_extract(payload_json, '$.__session_id') = ?
		 ORDER BY CAST(json_extract(payload_json, '$.__event_id') AS INTEGER) ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("orchestratoradapter: query audit_events_raw: %w", err)
	}
	defer rows.Close()

	var out []eventlog.Record
	for rows.Next() {
		var typeStr, payloadJSON string
		var emittedAtSec int64
		if err := rows.Scan(&typeStr, &payloadJSON, &emittedAtSec); err != nil {
			return nil, fmt.Errorf("orchestratoradapter: scan: %w", err)
		}
		var wrapped struct {
			SessionID  string          `json:"__session_id"`
			EventID    int64           `json:"__event_id"`
			TsNs       int64           `json:"__ts_ns"`
			ProjectID  string          `json:"__project_id"`
			Payload    json.RawMessage `json:"payload"`
			PayloadRaw string          `json:"payload_raw"`
		}
		if err := json.Unmarshal([]byte(payloadJSON), &wrapped); err != nil {
			return nil, fmt.Errorf("orchestratoradapter: unmarshal wrapped: %w", err)
		}
		if wrapped.EventID <= since {
			continue
		}
		et, _ := parseEventType(typeStr)

		var payloadBytes []byte
		if len(wrapped.Payload) > 0 && string(wrapped.Payload) != "null" {
			payloadBytes = []byte(wrapped.Payload)
		} else if wrapped.PayloadRaw != "" {
			payloadBytes = []byte(wrapped.PayloadRaw)
		}

		out = append(out, eventlog.Record{
			EventID:   wrapped.EventID,
			SessionID: wrapped.SessionID,
			ProjectID: wrapped.ProjectID,
			EventType: et,
			Payload:   payloadBytes,
			Timestamp: wrapped.TsNs,
		})
	}
	return out, rows.Err()
}

func parseEventType(s string) (eventlog.EventType, error) {
	for _, et := range eventlog.AllEventTypes() {
		if et.String() == s {
			return et, nil
		}
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		return eventlog.EventType(n), nil
	}
	return eventlog.EvtUnknown, fmt.Errorf("orchestratoradapter: unknown event type %q", s)
}

var randReader io.Reader = rand.Reader

func newRowID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(randReader, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (a *Adapter) Insert(ctx context.Context, r safetynet.HealthRecord) error {
	if a.closed.Load() {
		return errors.New("orchestratoradapter: closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.ensureSubstrateHealthUnique(ctx); err != nil {
		return err
	}
	findingsJSON := r.DoctrineLintFindingsJSON
	if findingsJSON == "" {
		findingsJSON = "[]"
	}
	_, err := a.store.DB().ExecContext(ctx,
		`INSERT OR IGNORE INTO substrate_health
		 (commit_sha, authored_by, test_pass_rate, test_total, test_passed,
		  doctrine_lint_pass, doctrine_lint_findings_json, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.CommitSHA, r.AuthoredBy, r.TestPassRate, r.TestTotal, r.TestPassed,
		boolToInt(r.DoctrineLintPass), findingsJSON, r.RecordedAt,
	)
	if err != nil {
		return fmt.Errorf("orchestratoradapter: insert substrate_health: %w", err)
	}
	return nil
}

func (a *Adapter) ensureSubstrateHealthUnique(ctx context.Context) error {
	a.mu.Lock()
	if a.substrateHealthUniqueInstalled {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if _, err := a.store.DB().ExecContext(ctx,
		`CREATE UNIQUE INDEX IF NOT EXISTS
		 idx_substrate_health_commit_recorded
		 ON substrate_health (commit_sha, recorded_at)`,
	); err != nil {
		return fmt.Errorf("orchestratoradapter: ensure substrate_health unique index: %w", err)
	}
	a.mu.Lock()
	a.substrateHealthUniqueInstalled = true
	a.mu.Unlock()
	return nil
}

func (a *Adapter) Recent(ctx context.Context, author string, since time.Time) ([]safetynet.HealthRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT commit_sha, authored_by, test_pass_rate, test_total, test_passed,
		        doctrine_lint_pass, COALESCE(doctrine_lint_findings_json, '[]'),
		        recorded_at
		 FROM substrate_health
		 WHERE authored_by = ? AND recorded_at > ?
		 ORDER BY recorded_at DESC`,
		author, since.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("orchestratoradapter: query substrate_health: %w", err)
	}
	defer rows.Close()

	var out []safetynet.HealthRecord
	for rows.Next() {
		var rec safetynet.HealthRecord

		var lintPassRaw sql.RawBytes
		if err := rows.Scan(
			&rec.CommitSHA, &rec.AuthoredBy, &rec.TestPassRate,
			&rec.TestTotal, &rec.TestPassed, &lintPassRaw,
			&rec.DoctrineLintFindingsJSON, &rec.RecordedAt,
		); err != nil {
			return nil, fmt.Errorf("orchestratoradapter: scan substrate_health: %w", err)
		}
		rec.DoctrineLintPass = parseBoolish(lintPassRaw)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (a *Adapter) AmendmentEventEmitter() amendment.EventEmitter {
	log := eventlog.New(a, clock.Real{})
	return &amendmentEmitter{log: log}
}

type amendmentEmitter struct{ log *eventlog.Log }

func (e *amendmentEmitter) Append(ctx context.Context, ev eventlog.Event) error {
	_, err := e.log.Append(ctx, ev)
	return err
}

func (a *Adapter) AmendmentReloadSignal(daemonURL string) amendment.ReloadSignal {
	return amendment.NewHTTPReloadSignal(daemonURL, 5*time.Second)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseBoolish(raw sql.RawBytes) bool {
	if len(raw) == 0 {
		return false
	}
	switch string(raw) {
	case "1", "true", "TRUE", "True", "t", "T":
		return true
	}
	return false
}

type EventLogRow struct {
	PayloadJSON   []byte
	EmittedAtUnix int64
}

func (a *Adapter) QueryEventsByType(ctx context.Context, typeStr string, sinceUnix int64) ([]EventLogRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := a.store.DB().QueryContext(ctx,
		`SELECT payload_json, emitted_at FROM audit_events_raw
		 WHERE type = ? AND emitted_at >= ?
		 ORDER BY emitted_at ASC`,
		typeStr, sinceUnix,
	)
	if err != nil {
		return nil, fmt.Errorf("orchestratoradapter: query audit_events_raw by type: %w", err)
	}
	defer rows.Close()
	var out []EventLogRow
	for rows.Next() {
		var payload string
		var emittedAt int64
		if err := rows.Scan(&payload, &emittedAt); err != nil {
			return nil, fmt.Errorf("orchestratoradapter: scan event row: %w", err)
		}
		out = append(out, EventLogRow{
			PayloadJSON:   []byte(payload),
			EmittedAtUnix: emittedAt,
		})
	}
	return out, rows.Err()
}

func (a *Adapter) CountEventsByType(ctx context.Context, typeStr string, sinceNs int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	sinceSec := sinceNs / int64(time.Second)
	if sinceSec < 0 {
		sinceSec = 0
	}
	row := a.store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_events_raw
		 WHERE type = ? AND emitted_at >= ?`,
		typeStr, sinceSec,
	)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("orchestratoradapter: count events by type: %w", err)
	}
	return n, nil
}

func (a *Adapter) LastEventByTypeUnix(ctx context.Context, typeStr string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	row := a.store.DB().QueryRowContext(ctx,
		`SELECT emitted_at FROM audit_events_raw
		 WHERE type = ?
		 ORDER BY emitted_at DESC
		 LIMIT 1`,
		typeStr,
	)
	var ts int64
	if err := row.Scan(&ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("orchestratoradapter: last event by type: %w", err)
	}
	return ts, nil
}

var _ *sql.DB

var (
	_ eventlog.RawEmitter    = (*Adapter)(nil)
	_ safetynet.HealthWriter = (*Adapter)(nil)
)
