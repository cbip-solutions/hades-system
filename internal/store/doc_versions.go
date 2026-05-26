// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type DocVersionRow struct {
	ID      int64
	TS      int64
	Project string
	Feature string
	DocPath string
	Content string
	Author  string
}

func (s *Store) InsertDocVersion(row DocVersionRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan9
}

func (s *Store) ListDocVersions(project, feature, docPath string) ([]DocVersionRow, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}
