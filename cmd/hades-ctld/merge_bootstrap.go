// SPDX-License-Identifier: MIT
package main

import (
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/orchestrator/merge"
)

type MergeBootstrapConfig struct {
	EngineVersion     string
	PoolCapacity      int
	ScoringConfig     merge.ScoringConfig
	AnomalyThresholds merge.AnomalyThresholds
	BaselineConfig    merge.BaselineConfig
	CandidateConfig   merge.CandidateConfig
	RunnerConfig      merge.RunnerConfig
}

func NewMergeEngineFromConfig(
	cfg MergeBootstrapConfig,
	pool merge.WorktreePool,
	emitter merge.EventEmitter,
	clk merge.AnomalyClock,
	gitClient merge.GitClient,
	executor merge.TestExecutor,
) (merge.MergeEngine, error) {
	gc := &merge.GenerationCounter{}

	baseline, err := merge.NewBaselineRunner(merge.BaselineDeps{
		Pool:     pool,
		Executor: executor,
		Emitter:  emitter,
		Git:      gitClient,
		GenCtr:   gc,
	}, cfg.BaselineConfig)
	if err != nil {
		return nil, fmt.Errorf("merge bootstrap: baseline: %w", err)
	}

	candidate, err := merge.NewCandidateRunner(merge.CandidateDeps{
		Pool:     pool,
		Executor: executor,
		Emitter:  emitter,
		Git:      gitClient,
		GenCtr:   gc,
	}, cfg.CandidateConfig)
	if err != nil {
		return nil, fmt.Errorf("merge bootstrap: candidate: %w", err)
	}

	runner, err := merge.NewRunner(merge.RunnerDeps{
		Candidate: candidate,
		Emitter:   emitter,
		GenCtr:    gc,
		Clock:     clk,
	}, cfg.RunnerConfig)
	if err != nil {
		return nil, fmt.Errorf("merge bootstrap: runner: %w", err)
	}

	scorer := merge.NewScorer()
	cache := merge.NewCache()

	anomaly, err := merge.NewAnomalyDetector(merge.AnomalyDeps{
		Emitter: emitter,
		Clock:   clk,
		GenCtr:  gc,
	}, cfg.AnomalyThresholds)
	if err != nil {
		return nil, fmt.Errorf("merge bootstrap: anomaly: %w", err)
	}

	engine, err := merge.NewEngine(merge.Deps{
		Pool:     pool,
		Emitter:  emitter,
		Clock:    clk,
		Baseline: baseline,
		Runner:   runner,
		Scorer:   scorer,
		Cache:    cache,
		Anomaly:  anomaly,
		Git:      gitClient,
		Config: merge.EngineConfig{
			Scoring:       cfg.ScoringConfig,
			EngineVersion: cfg.EngineVersion,
			PoolCapacity:  cfg.PoolCapacity,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("merge bootstrap: engine: %w", err)
	}
	return engine, nil
}
