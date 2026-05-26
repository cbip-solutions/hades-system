// SPDX-License-Identifier: MIT
package budget

import "time"

type RollupRequest struct {
	Axis  string    `json:"axis"`
	Value string    `json:"value"`
	Since time.Time `json:"since"`
}

type RollupResponse struct {
	TotalUSD  float64            `json:"total_usd"`
	Breakdown map[string]float64 `json:"breakdown"`
}

type CapStatusRequest struct {
	Axis  string `json:"axis"`
	Value string `json:"value"`
}

type CapStatusResponse struct {
	RemainingUSD float64 `json:"remaining_usd"`
	Blocked      bool    `json:"blocked"`
	BlockedScope string  `json:"blocked_scope"`
}

type TagRequest struct {
	CostID   string            `json:"cost_id"`
	AxisTags map[string]string `json:"axis_tags"`
}

type TagResponse struct {
	OK bool `json:"ok"`
}

type AnomalyCheckRequest struct {
	Scope  string `json:"scope"`
	Window string `json:"window,omitempty"`
}

type AnomalyCheckResponse struct {
	ZScore  float64 `json:"z_score"`
	Mean    float64 `json:"mean"`
	Std     float64 `json:"std"`
	Samples int     `json:"samples"`
}

type PauseRequest struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

type PauseStateResponse struct {
	Scope     string `json:"scope"`
	Active    bool   `json:"active"`
	PauseMode string `json:"pause_mode"`
	Reason    string `json:"reason,omitempty"`
}

type ResumeRequest struct {
	Scope string `json:"scope"`
}

type EventsRequest struct {
	Since time.Time `json:"since"`
}

type Event struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Scope     string         `json:"scope"`
	Payload   map[string]any `json:"payload,omitempty"`
	EmittedAt time.Time      `json:"emitted_at"`
}

type EventsResponse struct {
	Events []Event `json:"events"`
}
