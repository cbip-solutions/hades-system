// SPDX-License-Identifier: MIT
// Package client — release_summary.go.
//
// Typed daemon-API helpers for the GET /v1/{workforce,research,audit,
// budget,sshexec}/summary and GET /v1/health/release endpoints. The
// morning-brief renderer (`zen day`) calls these to populate the
//
// Each helper consults daemon over the canonical UDS dialer (or the
// test base URL when set via NewWithBaseURL) and decodes a strongly-
// typed response struct. Any non-2xx status surfaces as a wrapped
// error so the caller can distinguish "endpoint unavailable" from
// "decode failure" via errors.As(*HTTPError).
package client

import (
	"context"
)

type WorkforceSummaryResponse struct {
	WorkersSpawned24h          int    `json:"workers_spawned_24h"`
	WorkersCompleted24h        int    `json:"workers_completed_24h"`
	WorkersFailed24h           int    `json:"workers_failed_24h"`
	QueueDepthShared           int    `json:"queue_depth_shared"`
	QueueDepthCheckpoint       int    `json:"queue_depth_checkpoint"`
	QueueDepthFixPrompt        int    `json:"queue_depth_fix_prompt"`
	GateState                  string `json:"gate_state"`
	PersistentContexts         int    `json:"persistent_contexts"`
	PersistentContextOldestTTL string `json:"persistent_context_oldest_ttl"`
}

type ResearchSummaryResponse struct {
	Dispatches24h        int      `json:"dispatches_24h"`
	CacheHitRate         float64  `json:"cache_hit_rate"`
	CitationsEmitted24h  int      `json:"citations_emitted_24h"`
	CitationsVerified24h int      `json:"citations_verified_24h"`
	CaronteHealth        string   `json:"caronte_health"`
	TopSources           []string `json:"top_sources"`
}

type AuditSummaryResponse struct {
	Verdicts24h            int `json:"verdicts_24h"`
	ClassificationMajor    int `json:"classification_major"`
	ClassificationMinor    int `json:"classification_minor"`
	ClassificationOK       int `json:"classification_ok"`
	FamilyDisjointPoolSize int `json:"family_disjoint_pool_size"`
	Escalations24h         int `json:"escalations_24h"`
}

type BudgetSummaryResponse struct {
	TotalCost24hUSD   float64            `json:"total_cost_24h_usd"`
	CapUSD            float64            `json:"cap_usd"`
	PerStageBreakdown map[string]float64 `json:"per_stage_breakdown"`
	Anomalies24h      int                `json:"anomalies_24h"`
	OperatorPauses24h int                `json:"operator_pauses_24h"`
	PauseModeActive   bool               `json:"pause_mode_active"`
	PauseModeName     string             `json:"pause_mode_name"`
}

type SSHExecSummaryResponse struct {
	Attempts24h           int `json:"attempts_24h"`
	Allowed24h            int `json:"allowed_24h"`
	Denied24h             int `json:"denied_24h"`
	InteractiveBlocked24h int `json:"interactive_blocked_24h"`
	DurationP50MS         int `json:"duration_p50_ms"`
	DurationP95MS         int `json:"duration_p95_ms"`
	DurationP99MS         int `json:"duration_p99_ms"`
}

type Plan4HealthSummaryResponse struct {
	DaemonUptime          string `json:"daemon_uptime"`
	MCPsLatencyP95MS      int    `json:"mcps_latency_p95_ms"`
	DoctrineValid         bool   `json:"doctrine_valid"`
	SubprocessZombieCount int    `json:"subprocess_zombie_count"`
}

func (c *Client) WorkforceSummary(ctx context.Context) (*WorkforceSummaryResponse, error) {
	var out WorkforceSummaryResponse
	if err := c.getJSON(ctx, "/v1/workforce/summary", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResearchSummary(ctx context.Context) (*ResearchSummaryResponse, error) {
	var out ResearchSummaryResponse
	if err := c.getJSON(ctx, "/v1/research/summary", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) AuditSummary(ctx context.Context) (*AuditSummaryResponse, error) {
	var out AuditSummaryResponse
	if err := c.getJSON(ctx, "/v1/audit/summary", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BudgetSummary(ctx context.Context) (*BudgetSummaryResponse, error) {
	var out BudgetSummaryResponse
	if err := c.getJSON(ctx, "/v1/budget/summary", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SSHExecSummary(ctx context.Context) (*SSHExecSummaryResponse, error) {
	var out SSHExecSummaryResponse
	if err := c.getJSON(ctx, "/v1/sshexec/summary", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Plan4HealthSummary(ctx context.Context) (*Plan4HealthSummaryResponse, error) {
	var out Plan4HealthSummaryResponse
	if err := c.getJSON(ctx, "/v1/health/plan4", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
