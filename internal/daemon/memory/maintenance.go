// SPDX-License-Identifier: MIT
package memory

import zerrors "github.com/cbip-solutions/hades-system/internal/errors"

type HygieneReport struct {
	Duplicates          []string
	Stale               []string
	Contradictions      []string
	ElevationCandidates []string
}

func Audit(memDir string, privacyLocked bool) (*HygieneReport, error) {
	return nil, zerrors.ErrNotImplementedPlan9
}
