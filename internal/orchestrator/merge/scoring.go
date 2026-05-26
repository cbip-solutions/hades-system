// SPDX-License-Identifier: MIT
// internal/orchestrator/merge/scoring.go
package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var ErrNoSurvivors = errors.New("merge: no surviving candidates after Stage 1 filter")

var ErrLooseAttemptRejected = errors.New("merge: doctrine override attempts to loosen a tightening constraint (TIGHTEN-only per inv-zen-111)")

type Scorer interface {
	Rank(ctx context.Context, outcomes []CandidateOutcome, votes map[string]int, cfg ScoringConfig) (ScoringResult, error)
}

type ScoringConfig struct {
	AlphaReviewerWeight  float64 `toml:"alpha_reviewer_weight"`
	BetaPatchSizePenalty float64 `toml:"beta_patch_size_penalty"`
	GammaFlakePenalty    float64 `toml:"gamma_flake_penalty"`

	DeltaBlastRadiusPenalty float64 `toml:"delta_blast_radius_penalty"`
}

type ScoringResult struct {
	WinnerID        string
	AllScores       map[string]float64
	TiebreakApplied bool
	HardRejectedIDs []string
}

type realScorer struct{}

func NewScorer() Scorer {
	return &realScorer{}
}

func (s *realScorer) Rank(ctx context.Context, outcomes []CandidateOutcome, votes map[string]int, cfg ScoringConfig) (ScoringResult, error) {
	if err := ctx.Err(); err != nil {
		return ScoringResult{}, err
	}
	if votes == nil {
		votes = map[string]int{}
	}

	var (
		survivors []CandidateOutcome
		rejected  []string
	)
	for _, o := range outcomes {
		if o.HardRejected {
			rejected = append(rejected, o.Candidate.HeadSHA)
			continue
		}
		survivors = append(survivors, o)
	}

	if len(survivors) == 0 {
		return ScoringResult{HardRejectedIDs: rejected}, fmt.Errorf("%w: %d candidates hard-rejected", ErrNoSurvivors, len(rejected))
	}

	maxPass := -1
	for _, o := range survivors {
		if o.TestPassCount > maxPass {
			maxPass = o.TestPassCount
		}
	}
	tied := make([]CandidateOutcome, 0, len(survivors))
	for _, o := range survivors {
		if o.TestPassCount == maxPass {
			tied = append(tied, o)
		}
	}

	if len(tied) == 1 {
		return ScoringResult{
			WinnerID:        tied[0].Candidate.HeadSHA,
			HardRejectedIDs: rejected,
		}, nil
	}

	scores := make(map[string]float64, len(tied))
	for _, o := range tied {
		v := votes[o.Candidate.Branch]
		score := cfg.AlphaReviewerWeight*float64(v) -
			cfg.BetaPatchSizePenalty*float64(o.PatchSizeLines) -
			cfg.GammaFlakePenalty*float64(o.FlakeCount) -
			cfg.DeltaBlastRadiusPenalty*o.BlastRadius
		scores[o.Candidate.HeadSHA] = score
	}

	winnerID := ""
	winnerScore := 0.0
	first := true

	ids := make([]string, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		sc := scores[id]
		if first || sc > winnerScore {
			winnerID = id
			winnerScore = sc
			first = false
		}
	}

	return ScoringResult{
		WinnerID:        winnerID,
		AllScores:       scores,
		TiebreakApplied: true,
		HardRejectedIDs: rejected,
	}, nil
}

type ScoringCompletePayload struct {
	WinnerID        string             `json:"winner_id"`
	AllScores       map[string]float64 `json:"all_scores,omitempty"`
	TiebreakApplied bool               `json:"tiebreak_applied"`
	HardRejectedIDs []string           `json:"hard_rejected_ids,omitempty"`
	Formula         string             `json:"formula"`
	OperatorVetoed  bool               `json:"operator_vetoed,omitempty"`
}

func ValidateTightenOnly(base, override ScoringConfig) error {
	if override.BetaPatchSizePenalty < base.BetaPatchSizePenalty {
		return fmt.Errorf("%w: BetaPatchSizePenalty: base=%.4f override=%.4f (override would loosen)",
			ErrLooseAttemptRejected, base.BetaPatchSizePenalty, override.BetaPatchSizePenalty)
	}
	if override.GammaFlakePenalty < base.GammaFlakePenalty {
		return fmt.Errorf("%w: GammaFlakePenalty: base=%.4f override=%.4f (override would loosen)",
			ErrLooseAttemptRejected, base.GammaFlakePenalty, override.GammaFlakePenalty)
	}

	if override.DeltaBlastRadiusPenalty < base.DeltaBlastRadiusPenalty {
		return fmt.Errorf("%w: DeltaBlastRadiusPenalty: base=%.4f override=%.4f (override would loosen)",
			ErrLooseAttemptRejected, base.DeltaBlastRadiusPenalty, override.DeltaBlastRadiusPenalty)
	}

	return nil
}

func MarshalScoringComplete(res ScoringResult, cfg ScoringConfig) []byte {
	formula := fmt.Sprintf("argmax(test_pass) → tiebreak(α=%.2f β=%.2f γ=%.2f δ=%.2f)",
		cfg.AlphaReviewerWeight, cfg.BetaPatchSizePenalty, cfg.GammaFlakePenalty, cfg.DeltaBlastRadiusPenalty)
	b, _ := json.Marshal(ScoringCompletePayload{
		WinnerID:        res.WinnerID,
		AllScores:       res.AllScores,
		TiebreakApplied: res.TiebreakApplied,
		HardRejectedIDs: res.HardRejectedIDs,
		Formula:         formula,
	})
	return b
}

var _ Scorer = (*realScorer)(nil)
