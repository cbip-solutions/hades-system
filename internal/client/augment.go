// SPDX-License-Identifier: MIT
// Package client — augment.go
//
// /v1/augment/probe?check=<name>. ships the daemon-side
// handler; only consumes for doctor checks.
//
// also adds AugmentSummary (consumed by zen day's Augmentation
// section) wrapping the already-shipped /v1/augment/summary endpoint.
package client

import (
	"context"
	"net/url"
)

type AugmentProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (c *Client) AugmentProbe(ctx context.Context, check string) (*AugmentProbeResp, error) {
	u := "/v1/augment/probe?check=" + url.QueryEscape(check)
	var resp AugmentProbeResp
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type AugmentSummaryResponse struct {
	Date               string  `json:"date"`
	TotalCost          float64 `json:"total_cost_usd"`
	TokensConsumed     int     `json:"tokens_consumed"`
	TokensCeiling      int     `json:"tokens_ceiling"`
	KGQueriesFired     int     `json:"kg_queries_fired"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	LastIndexedRFC3339 string  `json:"last_indexed,omitempty"`
}

func (c *Client) AugmentSummary(ctx context.Context, dateYYYYMMDD string) (*AugmentSummaryResponse, error) {
	u := "/v1/augment/summary"
	if dateYYYYMMDD != "" {
		u += "?date=" + url.QueryEscape(dateYYYYMMDD)
	}
	var resp AugmentSummaryResponse
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
