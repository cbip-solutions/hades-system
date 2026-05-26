// SPDX-License-Identifier: MIT
// internal/orchestrator/seam_blastradius.go
//
//
// Declares the Verdict value type and BlastRadiusProvider interface that the
// four "Doctrina de Máximo Rigor" hooks (G-7 HRA escalation, G-8 confirmation
// policy pause, G-9/G-10 merge mode override) consume — without importing
// internal/caronte.
//
// Layering rationale (§"Why orchestrator-side, not caronte-side"):
// The four consumers live in internal/orchestrator. If they referenced
// evolution.RiskScore, internal/orchestrator would import internal/caronte —
// a layering inversion (orchestrator is a higher layer; caronte is infra).
// Declaring the seam here means caronte depends on nobody and the daemon (the
// composition root, which already imports both) maps evolution.RiskScore →
// orchestrator.Verdict at wiring time. Mirrors the master C-2 single-egress
// pattern exactly.
//
// inv-zen-031 family: the inv-zen-235 compliance scan asserts that
// internal/orchestrator has ZERO internal/caronte imports. This file's design
// is the reason that invariant can be asserted — the interface here is the
// narrow seam the scanner validates.

package orchestrator

import "context"

type Verdict struct {
	Level       string
	Score       float64
	TopAffected []string
}

func (v Verdict) IsHigh() bool { return v.Level == "high" }

type BlastRadiusProvider interface {
	// BlastRadius scores a change (changed fully-qualified symbols + their
	// files) for projectID. An error means the score is unavailable; callers
	// degrade (they do NOT block the build on a scorer failure — mirrors the
	// proxy escalate() degradation in Plan 11/J).
	BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (Verdict, error)
}
