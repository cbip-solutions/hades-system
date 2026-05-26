// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type LLMCallRow struct {
	ID        int64
	TS        int64
	Project   string
	SwarmID   string
	TaskID    string
	Provider  string
	Model     string
	TokensIn  int64
	TokensOut int64
	LatencyMs int64
	CostUSD   float64
}

type LLMCallQuery struct {
	Project  string
	Provider string
	SwarmID  string
	TaskID   string
	SinceTS  int64
	UntilTS  int64
	Limit    int
	Offset   int
}

func (s *Store) InsertLLMCall(row LLMCallRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan4
}

func (s *Store) ListLLMCalls(q LLMCallQuery) ([]LLMCallRow, error) {
	return nil, zerrors.ErrNotImplementedPlan4
}

func (s *Store) SumCostUSD(q LLMCallQuery) (float64, error) {
	return 0, zerrors.ErrNotImplementedPlan4
}
