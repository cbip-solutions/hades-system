// SPDX-License-Identifier: MIT
// Package client — merge_dto.go.
//
// Wire DTOs + the MergeClient interface for the daemon's /v1/merge/*
// surface. F-2 originally declared these in internal/cli; F-4 hoists
// them here because the production HTTP client (in this package) must
// satisfy the interface, and Go's package-isolation requires either
// the types live with the impl or with the interface — declaring them
// here with type aliases in cli (cli/merge.go) preserves the F-2 source
// surface for cli callers (hades merge, doctor merge, day brief) while
// breaking the import cycle the F-2 layout would have introduced
// (internal/client cannot import internal/cli because cli imports
// client elsewhere — see internal/cli/audit.go etc.).
//
// Wire-types decoupling vs internal/orchestrator/merge: these structs
// are pure JSON-tagged value types with NO import of merge or any
// other domain package — inv-hades-104 preserved end-to-end.
//
// Drift resolutions (carried over from F-2):
//
// - Drift C — output references Evt* constants only (no Event* /
// EventType*).
// - Drift D — anomaly list decodes EvtMergeAnomalyDetected payload
// via AnomalyDetectedPayload.Type discriminator (one EventType,
// switch on Type).
// - Drift E — cache status surfaces RebuildError from
// MergeCacheRebuiltPayload (no separate
// EvtMergeCacheRebuildFailed).
// - Drift F — Event/Payload shape consumed verbatim via
// json.Unmarshal.
package client

import (
	"context"
)

type MergeInspectResult struct {
	RequestHash    string `json:"request_hash"`
	GenerationID   int64  `json:"generation_id"`
	Mode           string `json:"mode"`
	WinnerID       string `json:"winner_id"`
	IntegrationSHA string `json:"integration_sha"`
	TestsPassed    bool   `json:"tests_passed"`
	Reverted       bool   `json:"reverted"`
}

type MergeReplayResult struct {
	SessionID      string `json:"session_id"`
	EventsReplayed int    `json:"events_replayed"`
	OutcomeMatch   bool   `json:"outcome_match"`
}

type MergeScoreExplainResult struct {
	WinnerID        string             `json:"winner_id"`
	AllScores       map[string]float64 `json:"all_scores,omitempty"`
	TiebreakApplied bool               `json:"tiebreak_applied"`
	Formula         string             `json:"formula"`
	HardRejectedIDs []string           `json:"hard_rejected_ids,omitempty"`
}

type MergeBaselineShowResult struct {
	SessionID  string   `json:"session_id"`
	BaseSHA    string   `json:"base_sha"`
	PassingSet []string `json:"passing_set"`
	DurationMs int64    `json:"duration_ms"`
}

type MergeCacheStatusResult struct {
	Size         int     `json:"size"`
	HitRatePct   float64 `json:"hit_rate_pct"`
	LastRebuilt  string  `json:"last_rebuilt"`
	RebuildError string  `json:"rebuild_error,omitempty"`
}

type MergeConfigShowResult struct {
	Doctrine          string              `json:"doctrine"`
	Scoring           MergeScoringConfig  `json:"scoring"`
	Timeouts          MergeTimeoutsConfig `json:"timeouts"`
	ModeMapping       map[string]string   `json:"mode_mapping"`
	AnomalyThresholds map[string]any      `json:"anomaly_thresholds"`
}

type MergeScoringConfig struct {
	Alpha float64 `json:"alpha"`
	Beta  float64 `json:"beta"`
	Gamma float64 `json:"gamma"`
}

type MergeTimeoutsConfig struct {
	BaselineSec   int `json:"baseline_seconds"`
	CandidateSec  int `json:"candidate_seconds"`
	FlakeRerunSec int `json:"flake_rerun_seconds"`
}

type MergeAnomalyEntry struct {
	Type            string `json:"type"`
	Severity        string `json:"severity"`
	ThresholdBreach string `json:"threshold_breach"`
	Detail          string `json:"detail"`
	Timestamp       string `json:"timestamp"`
}

type MergeAnomalyListResult struct {
	Anomalies []MergeAnomalyEntry `json:"anomalies"`
}

type MergeClient interface {
	Inspect(ctx context.Context, idOrHash string) (*MergeInspectResult, error)
	Replay(ctx context.Context, sessionID string) (*MergeReplayResult, error)
	ScoreExplain(ctx context.Context, outcomeID string) (*MergeScoreExplainResult, error)
	BaselineShow(ctx context.Context, sessionID string) (*MergeBaselineShowResult, error)
	CacheStatus(ctx context.Context) (*MergeCacheStatusResult, error)
	CacheClear(ctx context.Context) error
	ConfigShow(ctx context.Context) (*MergeConfigShowResult, error)
	AnomalyList(ctx context.Context, since string) (*MergeAnomalyListResult, error)
}
