// SPDX-License-Identifier: MIT
// Package client — types.go.
//
// Wire-shape mirrors of the daemon's bypass + notifications + orchestrator
// JSON responses. Defined here so the CLI can decode without importing
// internal/daemon (handlers package).
package client

import (
	"net/url"
	"time"
)

type BypassStatusResp struct {
	ActiveTier          string  `json:"active_tier"`
	Health              string  `json:"health"`
	HealthReason        string  `json:"health_reason"`
	SuccessRate24h      float64 `json:"success_rate_24h"`
	InFlight            int64   `json:"in_flight"`
	QueueDepth          int     `json:"queue_depth"`
	RefreshExpiresIn    string  `json:"refresh_expires_in"`
	AnomaliesUnacked    int     `json:"anomalies_unacked"`
	AnomalyTopField     string  `json:"anomaly_top_field"`
	AnomalyTopPct       float64 `json:"anomaly_top_pct"`
	PinnedConversations int     `json:"pinned_conversations"`
	PinnedOldestAge     string  `json:"pinned_oldest_age"`
	ConfigVersion       string  `json:"config_version"`
	LatestConfigVersion string  `json:"latest_config_version"`
	PaygSpentUSD        float64 `json:"payg_spent_usd"`
	PaygMonthlyUSD      float64 `json:"payg_monthly_usd"`
	RecentEscalations   int     `json:"recent_escalations"`
}

type BypassProbeResp struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latency_ms"`
	TierUsed  string `json:"tier_used"`
	Error     string `json:"error,omitempty"`
}

type BypassAuditQuery struct {
	Range   string
	Inspect string
	Since   string
}

func (q BypassAuditQuery) Encode() string {
	v := url.Values{}
	if q.Range != "" {
		v.Set("range", q.Range)
	}
	if q.Inspect != "" {
		v.Set("inspect", q.Inspect)
	}
	if q.Since != "" {
		v.Set("since", q.Since)
	}
	return v.Encode()
}

type BypassAuditAggRow struct {
	Tier     string  `json:"tier"`
	Count    int     `json:"count"`
	P50Ms    int64   `json:"p50_ms"`
	ErrorPct float64 `json:"error_pct"`
	TopError string  `json:"top_error"`
}

type BypassAuditResp struct {
	Aggregated []BypassAuditAggRow `json:"aggregated"`
	Row        any                 `json:"row,omitempty"`
}

type BypassUpdateOpts struct {
	DiffOnly  bool `json:"diff_only"`
	CheckOnly bool `json:"check_only"`
}

type BypassUpdateResp struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Diff           string `json:"diff"`
	Applied        bool   `json:"applied"`
}

type BypassTestProbe struct {
	Name      string `json:"name"`
	Passed    bool   `json:"passed"`
	LatencyMs int64  `json:"latency_ms"`
	Detail    string `json:"detail"`
}

type BypassTestResp struct {
	AllPassed bool              `json:"all_passed"`
	Probes    []BypassTestProbe `json:"probes"`
}

type ExtractOpts struct {
	CaptureOnly bool `json:"capture_only"`
}

type BypassExtractResp struct {
	CapturedRequests int    `json:"captured_requests"`
	OutputPath       string `json:"output_path"`
	Detail           string `json:"detail"`
}

type BypassCrossValidateResp struct {
	Plugin string `json:"plugin"`
	Report string `json:"report"`
}

type BypassAnomaly struct {
	Field        string    `json:"field"`
	Count        int       `json:"count"`
	FirstSeen    time.Time `json:"first_seen"`
	ThresholdPct float64   `json:"threshold_pct"`
}

type BypassRefreshNowResp struct {
	OK        string `json:"ok"`
	ExpiresIn string `json:"expires_in"`
}

type BypassPurgeResp struct {
	Candidates int   `json:"candidates"`
	BytesFreed int64 `json:"bytes_freed"`
	Applied    bool  `json:"applied"`
}

type BypassCertsShowResp struct {
	SHA256    string `json:"sha256"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
}

type BypassCFRangeResp struct {
	Refreshed bool     `json:"refreshed"`
	V4Count   int      `json:"v4_count"`
	V6Count   int      `json:"v6_count"`
	V4        []string `json:"v4"`
	V6        []string `json:"v6"`
	Age       string   `json:"age"`
}

type BypassDoctorResp struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type NotificationRow struct {
	ID           int64     `json:"id"`
	Severity     string    `json:"severity"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Source       string    `json:"source"`
	TS           time.Time `json:"ts"`
	Acknowledged bool      `json:"acknowledged"`
}

type OrchestratorTierState struct {
	Tier  string `json:"tier"`
	State string `json:"state"`
}

type OrchestratorPinSummary struct {
	ID        int64      `json:"id"`
	Scope     string     `json:"scope"`
	ScopeID   string     `json:"scope_id,omitempty"`
	Tier      string     `json:"tier"`
	Provider  string     `json:"provider,omitempty"`
	SetAt     time.Time  `json:"set_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Reason    string     `json:"reason,omitempty"`
}

type OrchestratorCostRow struct {
	Tier   string  `json:"tier"`
	Total  float64 `json:"total_usd_30d"`
	Window string  `json:"window"`
}

type OrchestratorStatusResp struct {
	Tiers []OrchestratorTierState  `json:"tiers"`
	Pins  []OrchestratorPinSummary `json:"pins"`
	Costs []OrchestratorCostRow    `json:"costs"`
}

type OrchestratorPinReq struct {
	Scope    string `json:"scope"`
	Project  string `json:"project,omitempty"`
	Session  string `json:"session,omitempty"`
	Tier     string `json:"tier"`
	Provider string `json:"provider,omitempty"`
	TTL      string `json:"ttl,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type OrchestratorUnpinReq struct {
	Scope   string `json:"scope,omitempty"`
	Project string `json:"project,omitempty"`
	Session string `json:"session,omitempty"`
	All     bool   `json:"all,omitempty"`
}

type OrchestratorPinsResp struct {
	Pins []OrchestratorPinSummary `json:"pins"`
}

type OrchestratorProbeResp struct {
	Tiers []OrchestratorTierState `json:"tiers"`
}

type OrchestratorHistoryResp struct {
	Tiers []OrchestratorTierState `json:"tiers"`
	Note  string                  `json:"note"`
}

type BudgetTierSpend struct {
	Project  string  `json:"project"`
	Profile  string  `json:"profile"`
	Tier     string  `json:"tier"`
	SpendUSD float64 `json:"spend_usd"`
}

type BudgetSummaryResp struct {
	Range    string            `json:"range"`
	TotalUSD float64           `json:"total_usd"`
	ByTier   []BudgetTierSpend `json:"by_tier"`
}
