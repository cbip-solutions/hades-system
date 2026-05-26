// SPDX-License-Identifier: MIT
// Package client — sessions.go (Plan 7 Phase C Task C-12).
//
// Three methods that mirror the daemon's tmux-session surface (Phase I
// ships handlers; Phase C ships client + CLI so the surface is final-
// shape day 1 per project doctrine "build the final product, not the
// stages"):
//
//	AttachSession  POST /v1/sessions/{alias}/attach
//	ListSessions   GET  /v1/sessions
//	RepaintLayout  POST /v1/sessions/{alias}/layout/repaint
//
// In the Phase I gap the daemon returns HTTP 503 with a remediation
// hint (consistent with the existing /v1/messages graceful-degradation
// pattern from Plan 2). 503 is unrecoverable from the operator's
// perspective at this command level — exit 2 — because "daemon route
// not yet shipped" is an infra concern, not an input typo.
//
// Wire types align field-for-field with the daemon-side handler that
// will land in Phase I (`internal/daemon/handlers/sessions_p7.go`):
// SessionRow / AttachResponse / RepaintResponse. Times use RFC3339
// over the wire (Go's encoding/json default for time.Time).
package client

import (
	"context"
	"net/url"
	"time"
)

type SessionRow struct {
	Alias      string    `json:"alias"`
	Sha8       string    `json:"sha8"`
	Status     string    `json:"status"`
	LastAttach time.Time `json:"last_attach"`
	PaneCount  int       `json:"pane_count"`
}

type sessionsListResponse struct {
	Sessions []SessionRow `json:"sessions"`
}

type attachRequest struct {
	Window string `json:"window"`
}

// attachResponse is the body of POST /v1/sessions/{alias}/attach.
// TmuxCmd is the exact command-line the CLI MUST exec to inherit the
// operator's TTY — daemon-controlled (enforces inv-zen-117 via the -S
// SocketPath flag in the rendered tokens). Returned as a single string
// the CLI splits via strings.Fields; the daemon renders the canonical
// form with no shell metacharacters.
type attachResponse struct {
	TmuxCmd string `json:"tmux_cmd"`
}

type repaintResponse struct {
	OK               bool     `json:"ok"`
	WindowsRepainted []string `json:"windows_repainted"`
	ScratchPreserved bool     `json:"scratch_preserved"`
	DurationMs       int64    `json:"duration_ms"`
}

func (c *Client) AttachSession(ctx context.Context, alias, window string) (string, error) {
	body := attachRequest{Window: window}
	var resp attachResponse
	path := "/v1/sessions/" + url.PathEscape(alias) + "/attach"
	if err := c.postJSON(ctx, path, body, &resp); err != nil {
		return "", err
	}
	return resp.TmuxCmd, nil
}

func (c *Client) ListSessions(ctx context.Context) ([]SessionRow, error) {
	var resp sessionsListResponse
	if err := c.getJSON(ctx, "/v1/sessions", &resp); err != nil {
		return nil, err
	}
	if resp.Sessions == nil {
		return []SessionRow{}, nil
	}
	return resp.Sessions, nil
}

func (c *Client) RepaintLayout(ctx context.Context, alias string) error {
	var resp repaintResponse
	path := "/v1/sessions/" + url.PathEscape(alias) + "/layout/repaint"
	if err := c.postJSON(ctx, path, struct{}{}, &resp); err != nil {
		return err
	}
	return nil
}
