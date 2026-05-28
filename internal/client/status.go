// SPDX-License-Identifier: MIT
package client

import "context"

type RuntimeStatus struct {
	Daemon      RuntimeDaemonStatus     `json:"daemon"`
	Config      RuntimeConfigStatus     `json:"config"`
	Project     RuntimeProjectStatus    `json:"project"`
	Provider    RuntimeProviderStatus   `json:"provider"`
	Cascade     RuntimeCascadeStatus    `json:"cascade"`
	Cost        RuntimeCostStatus       `json:"cost"`
	Context     *RuntimeContextStatus   `json:"context,omitempty"`
	Profile     RuntimeProfileStatus    `json:"profile"`
	Caronte     *RuntimeSubsystemStatus `json:"caronte,omitempty"`
	Federation  *RuntimeSubsystemStatus `json:"federation,omitempty"`
	NextActions []string                `json:"next_actions"`
}

type RuntimeDaemonStatus struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	PID           int    `json:"pid"`
	UDSPath       string `json:"uds_path"`
	ActiveModel   string `json:"active_model"`
}

type RuntimeConfigStatus struct {
	SocketPath string `json:"socket_path"`
}

type RuntimeProjectStatus struct {
	CWD string `json:"cwd"`
}

type RuntimeProviderStatus struct {
	ActiveModel   string `json:"active_model"`
	ProviderCount int    `json:"provider_count"`
}

type RuntimeCascadeStatus struct {
	ActiveTier    int    `json:"active_tier"`
	TierName      string `json:"tier_name"`
	ProviderCount int    `json:"provider_count"`
}

type RuntimeCostStatus struct {
	Spend24hUSD     float64  `json:"spend_24h_usd"`
	SpendSessionUSD *float64 `json:"spend_session_usd,omitempty"`
}

type RuntimeContextStatus struct {
	UsedTokens int `json:"used_tokens"`
	MaxTokens  int `json:"max_tokens"`
}

type RuntimeProfileStatus struct {
	ProfileName string `json:"profile_name"`
	Kind        string `json:"kind"`
}

type RuntimeSubsystemStatus struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (c *Client) RuntimeStatus(ctx context.Context) (*RuntimeStatus, error) {
	var out RuntimeStatus
	if err := c.getJSON(ctx, "/v1/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
