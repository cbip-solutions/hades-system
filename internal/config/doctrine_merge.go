// SPDX-License-Identifier: MIT
// internal/config/doctrine_merge.go
//
// Doctrine TOML loader extension for the merge engine (Plan 6 Phase F-5).
//
// This file extends the existing doctrine TOML schema with the [merge.*]
// sub-tree per spec §2.5 (zen-swarm-plan-6-merge-engine-design). It supports:
//
//   - Loading a [merge] block from a TOML blob (LoadMergeDoctrineFromTOML).
//   - Per-project TIGHTEN-only overrides (ApplyOverride):
//   - Scoring fields delegate to merge.ValidateTightenOnly
//     (Beta/Gamma penalties: tighter == higher).
//   - Timeouts: tighter == lower (shorter timeout == more strict).
//     ApplyOverride wraps merge.ErrLooseAttemptRejected on any loosening
//     attempt so callers (CLI, daemon doctor, bootstrap) can errors.Is
//     them and surface a doctrine-misconfig diagnostic.
//   - Adapter to Phase F-1 bootstrap inputs (MergeDoctrineToBoot):
//     int-seconds → time.Duration; the doctrine TOML schema deliberately
//     stores integers (operator ergonomics) and the converter keeps the
//     duration math out of every consumer.
//
// inv-zen-111: TIGHTEN-only doctrine override on per-project overrides.
// inv-zen-112: ADR range — see docs/decisions/0030-0038 for the substantive
// + reservation ADRs covering Plan 6 doctrine-tunable surfaces.
package config

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type MergeDoctrineConfig struct {
	CandidatesDefault int                    `toml:"candidates_default"`
	CandidatesMax     int                    `toml:"candidates_max"`
	Scoring           MergeScoringSection    `toml:"scoring"`
	Timeouts          MergeTimeoutsSection   `toml:"timeouts"`
	ModeMapping       map[string]string      `toml:"mode_mapping"`
	AnomalyThresholds MergeAnomalyThresholds `toml:"anomaly_thresholds"`
}

type MergeScoringSection struct {
	AlphaReviewerWeight  float64 `toml:"alpha_reviewer_weight"`
	BetaPatchSizePenalty float64 `toml:"beta_patch_size_penalty"`
	GammaFlakePenalty    float64 `toml:"gamma_flake_penalty"`
	FlakeRerunBudget     int     `toml:"flake_rerun_budget"`
}

type MergeTimeoutsSection struct {
	BaselineSeconds           int `toml:"baseline_seconds"`
	CandidateSeconds          int `toml:"candidate_seconds"`
	FlakeRerunSeconds         int `toml:"flake_rerun_seconds"`
	StragglerKillGraceSeconds int `toml:"straggler_kill_grace_seconds"`
}

type MergeAnomalyThresholds struct {
	ScoringWinnerVetoedCount       int     `toml:"scoring_winner_vetoed_count"`
	ScoringWinnerVetoedWindowHours int     `toml:"scoring_winner_vetoed_window_hours"`
	BaselineUnstableMinDivergent   int     `toml:"baseline_unstable_min_divergent_tests"`
	FlakeRateThresholdPct          float64 `toml:"flake_rate_threshold_pct"`
	FlakeRateWindowSessions        int     `toml:"flake_rate_window_sessions"`
	TextualUnresolvableRatePct     float64 `toml:"textual_unresolvable_rate_pct"`
	TextualUnresolvableWindow      int     `toml:"textual_unresolvable_window_sessions"`
	ModeDegradationPctThreshold    float64 `toml:"mode_degradation_pct_threshold"`
	ModeDegradationWindowHours     int     `toml:"mode_degradation_window_hours"`
}

func LoadMergeDoctrineFromTOML(tomlBlob string) (MergeDoctrineConfig, error) {
	var cfg struct {
		Merge MergeDoctrineConfig `toml:"merge"`
	}
	if _, err := decodeTOML(tomlBlob, &cfg); err != nil {
		return MergeDoctrineConfig{}, fmt.Errorf("config: load [merge]: %w", err)
	}
	return cfg.Merge, nil
}

func ApplyOverride(base, override MergeDoctrineConfig) (MergeDoctrineConfig, error) {

	baseScoring := merge.ScoringConfig{
		AlphaReviewerWeight:  base.Scoring.AlphaReviewerWeight,
		BetaPatchSizePenalty: base.Scoring.BetaPatchSizePenalty,
		GammaFlakePenalty:    base.Scoring.GammaFlakePenalty,
	}
	overrideScoring := merge.ScoringConfig{
		AlphaReviewerWeight:  override.Scoring.AlphaReviewerWeight,
		BetaPatchSizePenalty: override.Scoring.BetaPatchSizePenalty,
		GammaFlakePenalty:    override.Scoring.GammaFlakePenalty,
	}
	if err := merge.ValidateTightenOnly(baseScoring, overrideScoring); err != nil {
		return MergeDoctrineConfig{}, err
	}

	if override.Timeouts.BaselineSeconds > base.Timeouts.BaselineSeconds {
		return MergeDoctrineConfig{}, fmt.Errorf("%w: BaselineSeconds: base=%d override=%d (override would loosen)",
			merge.ErrLooseAttemptRejected, base.Timeouts.BaselineSeconds, override.Timeouts.BaselineSeconds)
	}
	if override.Timeouts.CandidateSeconds > base.Timeouts.CandidateSeconds {
		return MergeDoctrineConfig{}, fmt.Errorf("%w: CandidateSeconds: base=%d override=%d (override would loosen)",
			merge.ErrLooseAttemptRejected, base.Timeouts.CandidateSeconds, override.Timeouts.CandidateSeconds)
	}

	if override.Timeouts.FlakeRerunSeconds > base.Timeouts.FlakeRerunSeconds {
		return MergeDoctrineConfig{}, fmt.Errorf("%w: FlakeRerunSeconds: base=%d override=%d (override would loosen)",
			merge.ErrLooseAttemptRejected, base.Timeouts.FlakeRerunSeconds, override.Timeouts.FlakeRerunSeconds)
	}

	if override.Timeouts.StragglerKillGraceSeconds > base.Timeouts.StragglerKillGraceSeconds {
		return MergeDoctrineConfig{}, fmt.Errorf("%w: StragglerKillGraceSeconds: base=%d override=%d (override would loosen)",
			merge.ErrLooseAttemptRejected, base.Timeouts.StragglerKillGraceSeconds, override.Timeouts.StragglerKillGraceSeconds)
	}

	out := base
	out.Scoring = override.Scoring
	out.Timeouts = override.Timeouts
	if override.AnomalyThresholds != (MergeAnomalyThresholds{}) {
		out.AnomalyThresholds = override.AnomalyThresholds
	}
	if override.ModeMapping != nil {
		out.ModeMapping = override.ModeMapping
	}
	return out, nil
}

func decodeTOML(data string, out any) (toml.MetaData, error) {
	return toml.Decode(data, out)
}

func (c MergeDoctrineConfig) MergeDoctrineToBoot() (
	scoring merge.ScoringConfig,
	thresholds merge.AnomalyThresholds,
	runnerCfg merge.RunnerConfig,
	baselineCfg merge.BaselineConfig,
	candidateCfg merge.CandidateConfig,
) {
	scoring = merge.ScoringConfig{
		AlphaReviewerWeight:  c.Scoring.AlphaReviewerWeight,
		BetaPatchSizePenalty: c.Scoring.BetaPatchSizePenalty,
		GammaFlakePenalty:    c.Scoring.GammaFlakePenalty,
	}
	thresholds = merge.AnomalyThresholds{
		ScoringWinnerVetoedCount:          c.AnomalyThresholds.ScoringWinnerVetoedCount,
		ScoringWinnerVetoedWindowHours:    time.Duration(c.AnomalyThresholds.ScoringWinnerVetoedWindowHours) * time.Hour,
		BaselineUnstableMinDivergentTests: c.AnomalyThresholds.BaselineUnstableMinDivergent,
		FlakeRateThresholdPct:             c.AnomalyThresholds.FlakeRateThresholdPct,
		FlakeRateWindowSessions:           c.AnomalyThresholds.FlakeRateWindowSessions,
		TextualUnresolvableRatePct:        c.AnomalyThresholds.TextualUnresolvableRatePct,
		TextualUnresolvableWindowSessions: c.AnomalyThresholds.TextualUnresolvableWindow,
		ModeDegradationPctThreshold:       c.AnomalyThresholds.ModeDegradationPctThreshold,
		ModeDegradationWindowHours:        time.Duration(c.AnomalyThresholds.ModeDegradationWindowHours) * time.Hour,
	}
	runnerCfg = merge.RunnerConfig{
		StragglerKillGracePeriod: time.Duration(c.Timeouts.StragglerKillGraceSeconds) * time.Second,
	}
	baselineCfg = merge.BaselineConfig{
		Timeout:        time.Duration(c.Timeouts.BaselineSeconds) * time.Second,
		StderrCapBytes: 512,
	}
	candidateCfg = merge.CandidateConfig{
		Timeout:        time.Duration(c.Timeouts.CandidateSeconds) * time.Second,
		StderrCapBytes: 512,
	}
	return
}
