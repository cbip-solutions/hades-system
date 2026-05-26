// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type PAYGSpendRow struct {
	ID        int64
	TS        int64
	SessionID string
	Project   string
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
	Capped    bool
}

func (s *Store) InsertPAYGSpend(row PAYGSpendRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan5
}

func (s *Store) SumPAYGSpend(project string, sinceTS, untilTS int64) (float64, error) {
	return 0, zerrors.ErrNotImplementedPlan5
}
