// SPDX-License-Identifier: MIT
// Package zenday — autonomous-mode state contract.
//
// AutonomyStateReader is the boundary between zenday and release's
// orchestrator-state reader. Production wiring adapts the orchestrator
// snapshot; tests substitute fakes (per invariant, zenday/ never
// imports internal/store).
package zenday

import (
	"context"
	"time"
)

type AutonomySnapshot struct {
	ProjectID string

	ProjectAlias string

	State string

	PauseReason string

	LastMilestone string

	LastMilestoneAt time.Time
}

type AutonomyStateReader interface {
	// Snapshot returns one snapshot per known project. Implementations
	// MUST return rows sorted by ProjectAlias asc for deterministic
	// downstream rendering (cap selection picks newest LastMilestoneAt
	// across all rows; alias order does NOT determine cap winner).
	Snapshot(ctx context.Context) ([]AutonomySnapshot, error)
}
