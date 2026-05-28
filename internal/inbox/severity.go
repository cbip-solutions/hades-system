// SPDX-License-Identifier: MIT
// Package inbox owns the per-project notification storage substrate plus
// the 2-stage outbox bridge to the daemon-level aggregator cache (HADES design
// , spec §1 design choice/design choice).
//
// Severity, dedup, batch window, cross-project collapse, and quiet-hours
// gating all live here. The package NEVER imports internal/store
// (invariant); the inboxadapter package in internal/daemon is the only
// crossing point between this domain layer and the SQL layer.
package inbox

import (
	"errors"
	"fmt"
)

type Severity string

const (
	SeverityUrgent Severity = "urgent"

	SeverityActionNeeded Severity = "action-needed"

	SeverityInfoImmediate Severity = "info-immediate"

	SeverityInfoDigest Severity = "info-digest"
)

var ErrInvalidSeverity = errors.New("inbox: invalid severity")

var ErrSeverity4TierAnchor = errors.New("inbox: severity 4-tier enum anchor")

func AllSeverities() []Severity {
	return []Severity{
		SeverityUrgent,
		SeverityActionNeeded,
		SeverityInfoImmediate,
		SeverityInfoDigest,
	}
}

func ValidSeverity(s string) bool {
	switch Severity(s) {
	case SeverityUrgent, SeverityActionNeeded, SeverityInfoImmediate, SeverityInfoDigest:
		return true
	default:
		return false
	}
}

func ParseSeverity(s string) (Severity, error) {
	if !ValidSeverity(s) {
		return "", fmt.Errorf("%w: %q", ErrInvalidSeverity, s)
	}
	return Severity(s), nil
}

func (s Severity) String() string { return string(s) }
