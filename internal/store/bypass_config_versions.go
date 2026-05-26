// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type BypassConfigVersionRow struct {
	Version     string
	AppliedAt   int64
	DiffSummary string
	AppliedBy   string
}

func (s *Store) RecordBypassConfigVersion(version, diffSummary, appliedBy string) error {
	return zerrors.ErrNotImplementedPlan2
}

func (s *Store) CurrentBypassConfigVersion() (string, error) {
	return "", zerrors.ErrNotImplementedPlan2
}

func (s *Store) ListBypassConfigVersions(limit int) ([]BypassConfigVersionRow, error) {
	return nil, zerrors.ErrNotImplementedPlan2
}
