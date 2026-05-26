// SPDX-License-Identifier: MIT
// Package knowledge — ranker for the hybrid query pipeline (Plan 7 Phase G
// Task G-7).
//
// Three components combine into a single deterministic score:
//
//  1. BM25 base — supplied by SQLite FTS5's bm25() function, already
//     sign-flipped by query.go::scanResults so higher = better. Zero
//     for the structured-only path (no FTS5 MATCH performed).
//  2. Recency boost — exponential decay over wall-clock age:
//     boost = exp(-Δt / τ) with τ = 168 h (≈ one week scale).
//     boost(Δt=0) = 1.0; boost(168h) ≈ 0.368; boost(720h) ≈ 0.053.
//     Fresh docs float without obliterating older still-relevant ones.
//  3. Project-match boost — additive constant (`projectMatchBoostWeight`,
//     0.5) when the doc's project alias is one of the requested filter
//     values. Lifts the operator's targeted-project results above
//     comparable cross-project hits at equal BM25 + recency.
//
// The combination is additive (BM25 + recency + project-match), not
// multiplicative — additive preserves ordering when one component
// dominates and contributes only the documented constant otherwise.
//
// Determinism the function reads no global state (no internal
// time.Now()), so identical RankParams always yield identical scores.
// query.go::scanResults captures `time.Now()` once at scan time and
// passes it on every row so all rows inside one Execute() see the same
// reference instant.
//
// Per spec §1 Q16: "ranking = BM25 base + recency boost (exponential
// decay) + project-match boost". Coefficients are constants here;
// spec §6.6 reserves operator-tunable knobs for a future Phase L
// config-file extension (out of scope for Phase G).
package knowledge

import (
	"math"
	"time"
)

const recencyDecayTauHours = 168.0

const projectMatchBoostWeight = 0.5

// RankParams aggregates the inputs to ComputeScore. Filled by query.go's
// scanResults (one RankParams per scanned row); consumed by the ranker.
//
// Fields
//   - BaseBM25: per-row BM25 score from FTS5's bm25(), already negated
//     by scanResults (SQLite returns lower=better; callers flip to
//     higher=better). Zero for the structured-only path.
//   - LastModified: the doc's mtime at last index. Drives the recency
//     boost (exponential decay). MUST be in the same time-zone domain
//     as Now (typically both UTC).
//   - Now: wall-clock at query time, captured once by scanResults so
//     all rows inside one Execute() share a reference instant — that
//     is what makes ordering deterministic across rows.
//   - ProjectMatchBonus: 1.0 when the doc's project alias is one of the
//     requested filter projects, else 0.0. Drives the project-match
//     boost (additive `projectMatchBoostWeight`).
//
// Clock-skew defence: if LastModified > Now (future mtime, e.g. clock
// skew on the host that wrote the file), the age is clamped to 0 inside
// ComputeScore — never negative. A negative age would feed exp(+x) and
// produce an arbitrarily inflated score; clamp keeps scores bounded.
type RankParams struct {
	BaseBM25          float64
	LastModified      time.Time
	Now               time.Time
	ProjectMatchBonus float64
}

func ComputeScore(p RankParams) float64 {
	ageHours := p.Now.Sub(p.LastModified).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	recency := math.Exp(-ageHours / recencyDecayTauHours)
	project := projectMatchBoostWeight * p.ProjectMatchBonus
	return p.BaseBM25 + recency + project
}

func computeScore(p RankParams) float64 {
	return ComputeScore(p)
}
