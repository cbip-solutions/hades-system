// SPDX-License-Identifier: MIT
// Package client — hadesday.go.
//
// Three methods + supporting wire types for the daemon's
// /v1/hades-day/* surface backing the operator-facing
// `hades day [--force | --eod | --check-pending]` CLI:
//
// DayMorning POST /v1/hades-day/morning — generate / re-render today's morning brief
// DayEOD POST /v1/hades-day/eod — generate / re-render today's EOD digest
// DayCheckPending POST /v1/hades-day/check-pending — ephemeral introspection (no archive write)
//
// Wire shape: each method returns a hadesday.BriefDoc — the rendered
// brief data, NOT the markdown. The CLI calls hadesday.Render(doc) to
// produce the markdown for stdout. This keeps the rendering logic in a
// single place (the hadesday package) and lets future consumers (TUI,
// plugin, web view) reuse the same typed BriefDoc without re-parsing
// markdown.
//
// Field names + JSON tags align with the daemon-side handlers in
// internal/daemon/handlers/hadesday.go. Times use RFC3339 over the wire
// (Go's encoding/json default for time.Time).
//
// 409 Conflict propagates as *HTTPError on idempotency violation
// (today's brief already generated and force=false); the CLI maps
// 409 to ErrRecoverable so the operator sees exit 1 + a "use --force"
// hint rather than the default exit 2 (unrecoverable infra issue).
package client

import (
	"context"

	"github.com/cbip-solutions/hades-system/internal/hadesday"
)

type DayMorningRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayEODRequest struct {
	Force bool `json:"force,omitempty"`
}

type DayCheckPendingRequest struct{}

func (c *Client) DayMorning(ctx context.Context, force bool) (hadesday.BriefDoc, error) {
	var doc hadesday.BriefDoc
	if err := c.postJSON(ctx, "/v1/hades-day/morning", DayMorningRequest{Force: force}, &doc); err != nil {
		return hadesday.BriefDoc{}, err
	}
	return doc, nil
}

func (c *Client) DayEOD(ctx context.Context, force bool) (hadesday.BriefDoc, error) {
	var doc hadesday.BriefDoc
	if err := c.postJSON(ctx, "/v1/hades-day/eod", DayEODRequest{Force: force}, &doc); err != nil {
		return hadesday.BriefDoc{}, err
	}
	return doc, nil
}

func (c *Client) DayCheckPending(ctx context.Context) (hadesday.BriefDoc, error) {
	var doc hadesday.BriefDoc
	if err := c.postJSON(ctx, "/v1/hades-day/check-pending", DayCheckPendingRequest{}, &doc); err != nil {
		return hadesday.BriefDoc{}, err
	}
	return doc, nil
}
