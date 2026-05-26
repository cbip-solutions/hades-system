// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type SessionRow struct {
	ID        string
	Project   string
	Runtime   string
	StartedAt int64
	EndedAt   int64
}

func (s *Store) RegisterSession(row SessionRow) error {
	return zerrors.ErrNotImplementedPlan7
}

func (s *Store) EndSession(id string, endedAt int64) error {
	return zerrors.ErrNotImplementedPlan7
}

func (s *Store) GetSession(id string) (*SessionRow, error) {
	return nil, zerrors.ErrNotImplementedPlan7
}

func (s *Store) ListActiveSessions(project string) ([]SessionRow, error) {
	return nil, zerrors.ErrNotImplementedPlan7
}
