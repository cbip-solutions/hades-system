// SPDX-License-Identifier: MIT
// Package client — quiet.go.
//
// Three methods + supporting wire types for the daemon's
// /v1/quiet/* surface backing the operator-facing
// `zen quiet` CLI:
//
// QuietGet GET /v1/quiet — decode QuietGetResponse
// QuietUrgentPause POST /v1/quiet/urgent-pause — set UrgentPauseUntil
// QuietCancel POST /v1/quiet/cancel — clear active pause
//
// Field names + JSON tags align with the daemon-side handler in
// internal/daemon/handlers/quiet_p7.go. Times use RFC3339 over the
// wire (Go's encoding/json default for time.Time); Start/End live as
// int64 seconds-since-midnight to keep cross-language clients simple
// (a Python SDK would otherwise round-trip a Go nanosecond integer).
//
// Wire shapes are decoupled from internal/inbox.QuietConfig at the
// client layer (the QuietHours fields there are time.Duration). The
// CLI's productionQuietClient adapts wire ↔ domain at the package
// boundary so the render path consumes typed inbox.QuietConfig and
// the HTTP layer stays at portable wire types.
package client

import (
	"context"
	"fmt"
	"time"
)

type QuietHoursWire struct {
	StartSec        int64 `json:"start_sec"`
	EndSec          int64 `json:"end_sec"`
	WeekendExtended bool  `json:"weekend_extended"`
	UrgentBypass    bool  `json:"urgent_bypass"`
}

type QuietGetResponse struct {
	Default          QuietHoursWire            `json:"default"`
	PerProject       map[string]QuietHoursWire `json:"per_project"`
	UrgentPauseUntil *time.Time                `json:"urgent_pause_until,omitempty"`
}

type QuietPauseRequest struct {
	Until time.Time `json:"until"`
}

func (c *Client) QuietGet(ctx context.Context) (QuietGetResponse, error) {
	var resp QuietGetResponse
	if err := c.getJSON(ctx, "/v1/quiet", &resp); err != nil {
		return QuietGetResponse{}, err
	}
	if resp.PerProject == nil {
		resp.PerProject = map[string]QuietHoursWire{}
	}
	return resp, nil
}

// QuietUrgentPause calls POST /v1/quiet/urgent-pause with the until
// timestamp. until is converted to UTC server-side. Returns nil on 200,
// *HTTPError on non-2xx.
//
// until MUST be non-zero AND in the future at the daemon-side. The
// client asserts non-zero locally to surface operator typos without a
// round-trip; future-ness is a server-side concern (clock skew).
func (c *Client) QuietUrgentPause(ctx context.Context, until time.Time) error {
	if until.IsZero() {
		return fmt.Errorf("QuietUrgentPause: until is zero")
	}
	var resp struct {
		OK    bool      `json:"ok"`
		Until time.Time `json:"until"`
	}
	return c.postJSON(ctx, "/v1/quiet/urgent-pause", QuietPauseRequest{Until: until.UTC()}, &resp)
}

func (c *Client) QuietCancel(ctx context.Context) error {
	var resp struct {
		OK bool `json:"ok"`
	}
	return c.postJSON(ctx, "/v1/quiet/cancel", struct{}{}, &resp)
}
