// SPDX-License-Identifier: MIT
// Package client — zenday.go (Plan 7 Phase F Task F-10).
//
// Three methods + supporting wire types for the daemon's
// /v1/zen-day/* surface backing the operator-facing
// `zen day [--force | --eod | --check-pending]` CLI:
//
//	DayMorning      POST /v1/zen-day/morning        — generate / re-render today's morning brief
//	DayEOD          POST /v1/zen-day/eod            — generate / re-render today's EOD digest
//	DayCheckPending POST /v1/zen-day/check-pending  — ephemeral introspection (no archive write)
//
// Wire shape: each method returns a zenday.BriefDoc — the rendered
// brief data, NOT the markdown. The CLI calls zenday.Render(doc) to
// produce the markdown for stdout. This keeps the rendering logic in a
// single place (the zenday package) and lets future consumers (TUI,
// plugin, web view) reuse the same typed BriefDoc without re-parsing
// markdown.
//
// Field names + JSON tags align with the daemon-side handlers in
// internal/daemon/handlers/zenday.go. Times use RFC3339 over the wire
// (Go's encoding/json default for time.Time).
//
// 409 Conflict propagates as *HTTPError on idempotency violation
// (today's brief already generated and force=false); the CLI maps
// 409 to ErrRecoverable so the operator sees exit 1 + a "use --force"
// hint rather than the default exit 2 (unrecoverable infra issue).
package client

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/zenday"
)

type DayMorningRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayEODRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayCheckPendingRequest struct{}

func (c *Client) DayMorning(ctx context.Context, force bool) (zenday.BriefDoc, error) {
	var doc zenday.BriefDoc
	if err := c.postJSON(ctx, "/v1/zen-day/morning", DayMorningRequest{Force: force}, &doc); err != nil {
		return zenday.BriefDoc{}, err
	}
	return doc, nil
}

func (c *Client) DayEOD(ctx context.Context, force bool) (zenday.BriefDoc, error) {
	var doc zenday.BriefDoc
	if err := c.postJSON(ctx, "/v1/zen-day/eod", DayEODRequest{Force: force}, &doc); err != nil {
		return zenday.BriefDoc{}, err
	}
	return doc, nil
}

func (c *Client) DayCheckPending(ctx context.Context) (zenday.BriefDoc, error) {
	var doc zenday.BriefDoc
	if err := c.postJSON(ctx, "/v1/zen-day/check-pending", DayCheckPendingRequest{}, &doc); err != nil {
		return zenday.BriefDoc{}, err
	}
	return doc, nil
}
