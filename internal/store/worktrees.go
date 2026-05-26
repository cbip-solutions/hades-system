// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type WorktreeRow struct {
	ID        int64
	Project   string
	Feature   string
	TaskID    string
	Path      string
	Branch    string
	Status    string
	CreatedAt int64
	RemovedAt int64
}

func (s *Store) InsertWorktree(row WorktreeRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan5
}

func (s *Store) UpdateWorktreeStatus(id int64, status string, removedAt int64) error {
	return zerrors.ErrNotImplementedPlan5
}

func (s *Store) ListActiveWorktrees(project string) ([]WorktreeRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}
