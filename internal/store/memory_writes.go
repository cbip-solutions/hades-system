// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type MemoryWriteRow struct {
	ID          int64
	TS          int64
	Project     string
	FilePath    string
	Action      string
	ContentHash string
	Runtime     string
}

func (s *Store) InsertMemoryWrite(row MemoryWriteRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan9
}

func (s *Store) ListMemoryWrites(project string, sinceTS int64, limit int) ([]MemoryWriteRow, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}
