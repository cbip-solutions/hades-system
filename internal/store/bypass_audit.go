// SPDX-License-Identifier: MIT
package store

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type BypassAuditRow struct {
	ID           int64
	TS           int64
	RequestHash  string
	ResponseHash string
	Success      bool
	LatencyMs    int64
	ErrorCode    string
	ErrorPattern string
	TierUsed     string
}

type BypassAuditQuery struct {
	SinceTS int64
	UntilTS int64
	Success *bool
	Tier    string
	Limit   int
	Offset  int
}

func (s *Store) InsertBypassAudit(row BypassAuditRow) (int64, error) {
	return 0, zerrors.ErrNotImplementedPlan2
}

func (s *Store) ListBypassAudit(q BypassAuditQuery) ([]BypassAuditRow, error) {
	return nil, zerrors.ErrNotImplementedPlan2
}

func (s *Store) BypassSuccessRate(q BypassAuditQuery) (float64, error) {
	return 0, zerrors.ErrNotImplementedPlan2
}
