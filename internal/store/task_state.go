// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type TaskStateRow struct {
	ID            int64
	TS            int64
	TaskID        string
	SwarmID       string
	AttemptN      int
	PriorErrors   string
	FilesEdited   string
	CurrentPhase  string
	ApproachAvoid string
}

func (s *Store) InsertTaskState(row TaskStateRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan5
}

func (s *Store) LatestTaskState(taskID string) (*TaskStateRow, error) {
	return nil, zerrors.ErrNotImplementedPlan5
}
