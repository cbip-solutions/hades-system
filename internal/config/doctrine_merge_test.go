package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/config"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

const tomlSample = `
[merge]
candidates_default = 3
candidates_max = 5

[merge.scoring]
alpha_reviewer_weight = 1.0
beta_patch_size_penalty = 0.0
gamma_flake_penalty = 2.0
flake_rerun_budget = 2

[merge.timeouts]
baseline_seconds = 300
candidate_seconds = 600
flake_rerun_seconds = 300
straggler_kill_grace_seconds = 30

[merge.mode_mapping]
"max-scope"     = "Strict"
"default"       = "Normal"
"capa-firewall" = "Forensic"

[merge.anomaly_thresholds]
scoring_winner_vetoed_count = 1
scoring_winner_vetoed_window_hours = 24
baseline_unstable_min_divergent_tests = 1
flake_rate_threshold_pct = 5.0
flake_rate_window_sessions = 100
textual_unresolvable_rate_pct = 10.0
textual_unresolvable_window_sessions = 100
mode_degradation_pct_threshold = 40.0
mode_degradation_window_hours = 24
`

func TestLoadMergeDoctrineFromTOML(t *testing.T) {
	cfg, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CandidatesDefault != 3 {
		t.Errorf("candidates_default = %d want 3", cfg.CandidatesDefault)
	}
	if cfg.CandidatesMax != 5 {
		t.Errorf("candidates_max = %d want 5", cfg.CandidatesMax)
	}

	if cfg.Scoring.AlphaReviewerWeight != 1.0 {
		t.Errorf("alpha = %v want 1.0", cfg.Scoring.AlphaReviewerWeight)
	}
	if cfg.Scoring.GammaFlakePenalty != 2.0 {
		t.Errorf("gamma = %v want 2.0", cfg.Scoring.GammaFlakePenalty)
	}
	if cfg.Scoring.FlakeRerunBudget != 2 {
		t.Errorf("flake_rerun_budget = %d want 2", cfg.Scoring.FlakeRerunBudget)
	}

	if cfg.Timeouts.BaselineSeconds != 300 {
		t.Errorf("baseline_seconds = %d want 300", cfg.Timeouts.BaselineSeconds)
	}
	if cfg.Timeouts.CandidateSeconds != 600 {
		t.Errorf("candidate_seconds = %d want 600", cfg.Timeouts.CandidateSeconds)
	}
	if cfg.Timeouts.FlakeRerunSeconds != 300 {
		t.Errorf("flake_rerun_seconds = %d want 300", cfg.Timeouts.FlakeRerunSeconds)
	}
	if cfg.Timeouts.StragglerKillGraceSeconds != 30 {
		t.Errorf("straggler_kill_grace_seconds = %d want 30", cfg.Timeouts.StragglerKillGraceSeconds)
	}

	if got := cfg.ModeMapping["max-scope"]; got != "Strict" {
		t.Errorf("mode_mapping[max-scope] = %q want %q", got, "Strict")
	}
	if got := cfg.ModeMapping["default"]; got != "Normal" {
		t.Errorf("mode_mapping[default] = %q want %q", got, "Normal")
	}
	if got := cfg.ModeMapping["capa-firewall"]; got != "Forensic" {
		t.Errorf("mode_mapping[capa-firewall] = %q want %q", got, "Forensic")
	}

	if cfg.AnomalyThresholds.FlakeRateThresholdPct != 5.0 {
		t.Errorf("flake_rate_threshold_pct = %v want 5.0", cfg.AnomalyThresholds.FlakeRateThresholdPct)
	}
	if cfg.AnomalyThresholds.FlakeRateWindowSessions != 100 {
		t.Errorf("flake_rate_window_sessions = %d want 100", cfg.AnomalyThresholds.FlakeRateWindowSessions)
	}
	if cfg.AnomalyThresholds.ModeDegradationPctThreshold != 40.0 {
		t.Errorf("mode_degradation_pct_threshold = %v want 40.0", cfg.AnomalyThresholds.ModeDegradationPctThreshold)
	}
}

func TestLoadMergeDoctrineFromTOMLMalformed(t *testing.T) {
	_, err := config.LoadMergeDoctrineFromTOML("[merge\nthis = is not toml")
	if err == nil {
		t.Fatal("expected err on malformed TOML, got nil")
	}
}

func TestApplyOverrideAcceptsTightening(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Timeouts.BaselineSeconds = 200
	override.Timeouts.CandidateSeconds = 500
	override.Scoring.GammaFlakePenalty = 5.0
	out, err := config.ApplyOverride(base, override)
	if err != nil {
		t.Errorf("override accepted-direction returned err = %v", err)
	}
	if out.Timeouts.BaselineSeconds != 200 {
		t.Errorf("out.BaselineSeconds = %d want 200 (override applied)", out.Timeouts.BaselineSeconds)
	}
	if out.Scoring.GammaFlakePenalty != 5.0 {
		t.Errorf("out.GammaFlakePenalty = %v want 5.0", out.Scoring.GammaFlakePenalty)
	}
}

func TestApplyOverrideAcceptsEqual(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.ApplyOverride(base, base); err != nil {
		t.Errorf("ApplyOverride(equal) = %v want nil", err)
	}
}

func TestApplyOverrideRejectsLoosenTimeoutBaseline(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Timeouts.BaselineSeconds = 600
	_, err = config.ApplyOverride(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
}

func TestApplyOverrideRejectsLoosenTimeoutCandidate(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Timeouts.CandidateSeconds = 1200
	_, err = config.ApplyOverride(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
}

func TestApplyOverrideRejectsLoosenTimeoutFlakeRerun(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Timeouts.FlakeRerunSeconds = 600
	_, err = config.ApplyOverride(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
	if !strings.Contains(err.Error(), "FlakeRerunSeconds") {
		t.Errorf("err = %q does not name FlakeRerunSeconds field", err.Error())
	}
}

func TestApplyOverrideRejectsLoosenStragglerGrace(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Timeouts.StragglerKillGraceSeconds = 90
	_, err = config.ApplyOverride(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
	if !strings.Contains(err.Error(), "StragglerKillGraceSeconds") {
		t.Errorf("err = %q does not name StragglerKillGraceSeconds field", err.Error())
	}
}

func TestApplyOverrideRejectsLoosenScoring(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.Scoring.GammaFlakePenalty = 1.0
	_, err = config.ApplyOverride(base, override)
	if !errors.Is(err, merge.ErrLooseAttemptRejected) {
		t.Fatalf("err = %v want ErrLooseAttemptRejected", err)
	}
}

func TestApplyOverrideAppliesModeMapping(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.ModeMapping = map[string]string{
		"max-scope":     "Forensic",
		"default":       "Strict",
		"capa-firewall": "Forensic",
	}
	out, err := config.ApplyOverride(base, override)
	if err != nil {
		t.Fatal(err)
	}
	if out.ModeMapping["max-scope"] != "Forensic" {
		t.Errorf("ModeMapping[max-scope] = %q want %q (override applied)",
			out.ModeMapping["max-scope"], "Forensic")
	}
}

func TestApplyOverrideKeepsBaseModeMappingWhenNil(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.ModeMapping = nil
	out, err := config.ApplyOverride(base, override)
	if err != nil {
		t.Fatal(err)
	}
	if out.ModeMapping["max-scope"] != "Strict" {
		t.Errorf("ModeMapping[max-scope] = %q want %q (base preserved)",
			out.ModeMapping["max-scope"], "Strict")
	}
}

func TestApplyOverrideAppliesAnomalyThresholds(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.AnomalyThresholds.FlakeRateThresholdPct = 1.0
	override.AnomalyThresholds.FlakeRateWindowSessions = 50
	out, err := config.ApplyOverride(base, override)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnomalyThresholds.FlakeRateThresholdPct != 1.0 {
		t.Errorf("FlakeRateThresholdPct = %v want 1.0", out.AnomalyThresholds.FlakeRateThresholdPct)
	}
	if out.AnomalyThresholds.FlakeRateWindowSessions != 50 {
		t.Errorf("FlakeRateWindowSessions = %d want 50", out.AnomalyThresholds.FlakeRateWindowSessions)
	}
}

func TestApplyOverrideKeepsBaseAnomalyWhenZero(t *testing.T) {
	base, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	override := base
	override.AnomalyThresholds = config.MergeAnomalyThresholds{}
	out, err := config.ApplyOverride(base, override)
	if err != nil {
		t.Fatal(err)
	}
	if out.AnomalyThresholds.FlakeRateThresholdPct != 5.0 {
		t.Errorf("FlakeRateThresholdPct = %v want 5.0 (base preserved)",
			out.AnomalyThresholds.FlakeRateThresholdPct)
	}
}

func TestMergeDoctrineToBoot(t *testing.T) {
	cfg, err := config.LoadMergeDoctrineFromTOML(tomlSample)
	if err != nil {
		t.Fatal(err)
	}
	scoring, thresholds, runnerCfg, baselineCfg, candidateCfg := cfg.MergeDoctrineToBoot()

	if scoring.AlphaReviewerWeight != 1.0 {
		t.Errorf("scoring.Alpha = %v want 1.0", scoring.AlphaReviewerWeight)
	}
	if scoring.GammaFlakePenalty != 2.0 {
		t.Errorf("scoring.Gamma = %v want 2.0", scoring.GammaFlakePenalty)
	}

	if got, want := thresholds.ScoringWinnerVetoedWindowHours, 24*time.Hour; got != want {
		t.Errorf("thresholds.ScoringWindow = %v want %v", got, want)
	}
	if got, want := thresholds.ModeDegradationWindowHours, 24*time.Hour; got != want {
		t.Errorf("thresholds.ModeWindow = %v want %v", got, want)
	}
	if thresholds.FlakeRateThresholdPct != 5.0 {
		t.Errorf("thresholds.FlakeRatePct = %v want 5.0", thresholds.FlakeRateThresholdPct)
	}
	if thresholds.BaselineUnstableMinDivergentTests != 1 {
		t.Errorf("thresholds.BaselineMinDivergent = %d want 1", thresholds.BaselineUnstableMinDivergentTests)
	}

	if got, want := runnerCfg.StragglerKillGracePeriod, 30*time.Second; got != want {
		t.Errorf("runner.StragglerGrace = %v want %v", got, want)
	}

	if got, want := baselineCfg.Timeout, 300*time.Second; got != want {
		t.Errorf("baseline.Timeout = %v want %v", got, want)
	}
	if baselineCfg.StderrCapBytes != 512 {
		t.Errorf("baseline.StderrCapBytes = %d want 512", baselineCfg.StderrCapBytes)
	}

	if got, want := candidateCfg.Timeout, 600*time.Second; got != want {
		t.Errorf("candidate.Timeout = %v want %v", got, want)
	}
	if candidateCfg.StderrCapBytes != 512 {
		t.Errorf("candidate.StderrCapBytes = %d want 512", candidateCfg.StderrCapBytes)
	}
}

func TestMergeDoctrineToBootZeroValue(t *testing.T) {
	var cfg config.MergeDoctrineConfig
	scoring, thresholds, runnerCfg, baselineCfg, candidateCfg := cfg.MergeDoctrineToBoot()

	if scoring.AlphaReviewerWeight != 0 {
		t.Errorf("scoring.Alpha = %v want 0", scoring.AlphaReviewerWeight)
	}
	if thresholds.ScoringWinnerVetoedWindowHours != 0 {
		t.Errorf("thresholds.ScoringWindow = %v want 0", thresholds.ScoringWinnerVetoedWindowHours)
	}
	if runnerCfg.StragglerKillGracePeriod != 0 {
		t.Errorf("runner.StragglerGrace = %v want 0", runnerCfg.StragglerKillGracePeriod)
	}
	if baselineCfg.Timeout != 0 {
		t.Errorf("baseline.Timeout = %v want 0", baselineCfg.Timeout)
	}
	if candidateCfg.Timeout != 0 {
		t.Errorf("candidate.Timeout = %v want 0", candidateCfg.Timeout)
	}
}
