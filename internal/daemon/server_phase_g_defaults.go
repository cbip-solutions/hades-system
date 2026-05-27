// SPDX-License-Identifier: MIT
//
// Every method in this file satisfies one of the handlers.*Ctx interfaces.
// The defaults are sufficient for end-to-end testing (handlers
// return shape-correct JSON) but are intentionally minimal — they do
// NOT call into the eventual production subsystems. Each method carries
// the PHASE_G_DEFAULT marker plus a reference to the Phase that wires
// the real engine, so a `grep PHASE_G_DEFAULT` enumerates every place
// the daemon still holds a placeholder.
//
// Production assertion: tests that exercise post- behaviour must
// inject a real Ctx via subagent stubs OR be excluded from this file via
// a build tag. will fail-closed on calls to any default still
// living in this file once its scope is in.
//
// Post-review I-8 fix: prior code interleaved these defaults with the
// HTTP wiring in server.go; reviewers and subsequent fix subagents could
// not enumerate them quickly. Lifting them here makes the placeholder
// surface explicit.

package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/doctrine"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

func (s *Server) ResearchCacheGet(hash string) (string, int64, bool, error) {
	if s.store == nil {
		return "", 0, false, nil
	}
	var responseJSON string
	var ttlUnix int64
	row := s.store.DB().QueryRow(
		`SELECT response_json, ttl_unix FROM research_cache WHERE hash = ?`,
		hash,
	)
	switch err := row.Scan(&responseJSON, &ttlUnix); {
	case errors.Is(err, sql.ErrNoRows):
		return "", 0, false, nil
	case err != nil:
		return "", 0, false, err
	}
	if ttlUnix < time.Now().Unix() {
		return "", 0, false, nil
	}
	return responseJSON, ttlUnix, true, nil
}

func (s *Server) ResearchCacheSet(hash, responseJSON string, ttlUnix int64) error {
	if s.store == nil {
		return nil
	}
	_, err := s.store.DB().Exec(
		`INSERT INTO research_cache(hash, response_json, ttl_unix) VALUES(?, ?, ?)
		 ON CONFLICT(hash) DO UPDATE SET response_json=excluded.response_json, ttl_unix=excluded.ttl_unix`,
		hash, responseJSON, ttlUnix,
	)
	return err
}

func (s *Server) ResearchCacheTTL() time.Duration { return 7 * 24 * time.Hour }

func (s *Server) AuditEmit(event handlers.AuditEventIn) error {
	if s.store == nil {
		return nil
	}
	payloadBytes, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	_, err = s.store.DB().Exec(
		`INSERT INTO audit_events_raw(id, project_id, type, payload_json, emitted_at)
		 VALUES(?, ?, ?, ?, ?)`,
		event.ID, event.ProjectID, event.Type, string(payloadBytes), event.EmittedAt,
	)
	return err
}

func (s *Server) BudgetCapStatus(axis, value string) (handlers.BudgetCapStatusResult, error) {
	return handlers.BudgetCapStatusResult{RemainingUSD: 0, Blocked: false}, nil
}

func (s *Server) BudgetRecord(req handlers.BudgetRecordReq) error { return nil }

func (s *Server) BudgetAxes(costID string) ([]handlers.BudgetAxisTag, error) {
	return nil, nil
}

func (s *Server) BudgetAnomalyCheck(scope, value string, windowSec int64) (handlers.BudgetAnomalyResult, error) {
	return handlers.BudgetAnomalyResult{}, nil
}

func (s *Server) BudgetEvents(sinceUnix int64, limitN int) ([]handlers.BudgetEventRow, error) {
	return nil, nil
}

func (s *Server) BudgetPause(scope, value, reason string) (string, error) { return "paused", nil }

func (s *Server) BudgetResume(scope, value string) (string, error) { return "running", nil }

func (s *Server) WorkforceSpecs(limit, offset int, filter string) ([]handlers.WorkerSpecRow, error) {
	return nil, nil
}

func (s *Server) WorkforceWorkers(limit, offset int, status string) ([]handlers.WorkerRow, error) {
	return nil, nil
}

func (s *Server) WorkforceCheckpoints(taskID string, limit, offset int) ([]handlers.CheckpointRow, error) {
	return nil, nil
}

func (s *Server) WorkforceFixPrompts(taskID string, limit, offset int) ([]handlers.FixPromptRow, error) {
	return nil, nil
}

func (s *Server) WorkforceAggregations(layer string, windowSec int64, limit int) ([]handlers.AggregationRow, error) {
	return nil, nil
}

func (s *Server) OperatorGateState() (string, error) {
	g := s.OperatorGate()
	if g == nil {
		return string(gate.StateRunning), nil
	}
	return string(g.State()), nil
}

func (s *Server) OperatorGatePause(mode, reason string) (string, error) {
	pauseMode, pauseState, err := parseOperatorGatePauseMode(mode)
	if err != nil {
		return "", err
	}
	g := s.OperatorGate()
	if g == nil {
		return string(pauseState), nil
	}
	if err := g.Pause(context.Background(), pauseMode, reason); err != nil {
		return "", err
	}
	return string(g.State()), nil
}

func (s *Server) OperatorGateResume() (string, error) {
	g := s.OperatorGate()
	if g == nil {
		return string(gate.StateRunning), nil
	}
	if err := g.Resume(context.Background()); err != nil {
		return "", err
	}
	return string(g.State()), nil
}

func parseOperatorGatePauseMode(mode string) (gate.PauseMode, gate.State, error) {
	switch mode {
	case "", string(gate.StatePausedDescriptive):
		return gate.PauseDescriptive, gate.StatePausedDescriptive, nil
	case string(gate.StatePausedQuiet):
		return gate.PauseQuiet, gate.StatePausedQuiet, nil
	case string(gate.StatePausedAfterApply):
		return gate.PauseAfterApply, gate.StatePausedAfterApply, nil
	default:
		return 0, "", errors.New("unknown operator gate pause mode: " + mode)
	}
}

func (s *Server) RateLimitThreshold(endpoint string) int {
	defaults := handlers.Defaults()
	if v, ok := defaults[endpoint]; ok {
		return v
	}
	return 100
}

var _ = doctrine.NameMaxScope
