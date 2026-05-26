// SPDX-License-Identifier: MIT
package workforceadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/store"
	"github.com/cbip-solutions/hades-system/internal/workforce/gate"
)

type GateAdapter struct {
	s *store.Store

	loadStateQueryFn func(ctx context.Context) (gate.State, error)
	saveStateExecFn  func(ctx context.Context, s gate.State, reason string) error

	scanStateResultFn func() (string, error)
}

func NewGateAdapter(s *store.Store) *GateAdapter {
	if s == nil {
		panic("workforceadapter.NewGateAdapter: store is nil")
	}
	return &GateAdapter{s: s}
}

func (a *GateAdapter) LoadState(ctx context.Context) (gate.State, error) {
	if a.loadStateQueryFn != nil {
		return a.loadStateQueryFn(ctx)
	}
	var stateStr string
	var scanErr error
	if a.scanStateResultFn != nil {
		stateStr, scanErr = a.scanStateResultFn()
	} else {
		scanErr = a.s.DB().QueryRowContext(ctx,
			`SELECT state FROM operator_gate_state WHERE id=1`,
		).Scan(&stateStr)
	}
	if errors.Is(scanErr, sql.ErrNoRows) {
		return gate.StateRunning, nil
	}
	if scanErr != nil {
		return gate.StateRunning, fmt.Errorf("gate_adapter.LoadState: %w", scanErr)
	}
	s := gate.State(stateStr)
	switch s {
	case gate.StateRunning, gate.StatePausedDescriptive, gate.StatePausedQuiet, gate.StatePausedAfterApply:
		return s, nil
	default:

		return gate.StateRunning, nil
	}
}

func (a *GateAdapter) SaveState(ctx context.Context, s gate.State, reason string) error {
	if a.saveStateExecFn != nil {
		return a.saveStateExecFn(ctx, s, reason)
	}
	_, err := a.s.DB().ExecContext(ctx,
		`INSERT INTO operator_gate_state (id, state, reason, updated_at)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     state=excluded.state,
		     reason=excluded.reason,
		     updated_at=excluded.updated_at`,
		string(s), reason, time.Now().UTC().Unix(),
	)
	if err != nil {
		return fmt.Errorf("gate_adapter.SaveState: %w", err)
	}
	return nil
}
