// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type SwarmRow struct {
	ID          string
	Project     string
	Feature     string
	Phase       string
	StartedAt   int64
	EndedAt     int64
	Parallelism int
}

func (s *Store) CreateSwarm(row SwarmRow) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) UpdateSwarmPhase(id, phase string, endedAt int64) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) GetSwarm(id string) (*SwarmRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}

func (s *Store) ListActiveSwarms(project string) ([]SwarmRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}
