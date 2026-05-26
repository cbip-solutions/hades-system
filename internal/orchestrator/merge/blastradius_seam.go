// SPDX-License-Identifier: MIT
package merge

import "context"

type Verdict struct {
	Level       string
	Score       float64
	TopAffected []string
}

type BlastRadiusScorer interface {
	BlastRadius(ctx context.Context, projectID string, changedSymbols, changedFiles []string) (Verdict, error)
}
