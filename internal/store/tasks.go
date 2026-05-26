// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type TaskRow struct {
	ID        string
	SwarmID   string
	SpecJSON  string
	Phase     string
	Provider  string
	StartedAt int64
	EndedAt   int64
	Outcome   string
}

func (s *Store) CreateTask(row TaskRow) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) UpdateTaskPhase(id, phase string) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) UpdateTaskOutcome(id, outcome string, endedAt int64) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) GetTask(id string) (*TaskRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}

func (s *Store) ListTasksBySwarm(swarmID string) ([]TaskRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}
