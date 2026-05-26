// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type DecisionRow struct {
	ID            int64
	TS            int64
	Project       string
	Scope         string
	Decision      string
	Justification string
	Actor         string
}

type DecisionQuery struct {
	Project string
	Scope   string
	Actor   string
	SinceTS int64
	UntilTS int64
	Limit   int
	Offset  int
}

func (s *Store) InsertDecision(row DecisionRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan9
}

func (s *Store) ListDecisions(q DecisionQuery) ([]DecisionRow, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}
