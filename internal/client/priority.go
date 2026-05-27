// SPDX-License-Identifier: MIT
// Package client — priority.go.
//
// Three methods that mirror the daemon's priority-override surface:
//
// PriorityBoost POST /v1/priority/boost
// PriorityReset POST /v1/priority/reset
// PriorityList GET /v1/priority/list
//
// Wire types align field-for-field with the daemon-side handler in
// internal/daemon/handlers/priority.go (PriorityBoostRequest,
// PriorityListResponse, PriorityOverrideRow). Times use RFC3339 over
// the wire (Go's encoding/json default for time.Time).
package client

import (
	"context"
	"time"
)

type PriorityOverrideRow struct {
	Alias      string    `json:"alias"`
	Multiplier float64   `json:"multiplier"`
	ExpiresAt  time.Time `json:"expires_at"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

type priorityListResponse struct {
	Overrides []PriorityOverrideRow `json:"overrides"`
}

type priorityBoostRequest struct {
	Alias      string    `json:"alias"`
	Multiplier float64   `json:"multiplier"`
	ExpiresAt  time.Time `json:"expires_at"`
	Reason     string    `json:"reason"`
}

func (c *Client) PriorityBoost(ctx context.Context, alias string, multiplier float64, expiresAt time.Time, reason string) error {
	body := priorityBoostRequest{
		Alias:      alias,
		Multiplier: multiplier,
		ExpiresAt:  expiresAt,
		Reason:     reason,
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, "/v1/priority/boost", body, &resp)
}

func (c *Client) PriorityReset(ctx context.Context, alias string) error {
	body := map[string]string{"alias": alias}
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, "/v1/priority/reset", body, &resp)
}

func (c *Client) PriorityList(ctx context.Context) ([]PriorityOverrideRow, error) {
	var resp priorityListResponse
	if err := c.getJSON(ctx, "/v1/priority/list", &resp); err != nil {
		return nil, err
	}
	if resp.Overrides == nil {
		return []PriorityOverrideRow{}, nil
	}
	return resp.Overrides, nil
}
