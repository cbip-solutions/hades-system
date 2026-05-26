// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type PostmortemRow struct {
	ID              int64
	TS              int64
	Project         string
	SwarmID         string
	RootCause       string
	SuggestionsJSON string
	Outcome         string
}

func (s *Store) InsertPostmortem(row PostmortemRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan11
}

func (s *Store) GetPostmortem(swarmID string) (*PostmortemRow, error) {
	return nil, zerrors.ErrNotImplementedPlan11
}
