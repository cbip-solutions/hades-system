// SPDX-License-Identifier: MIT
// Package client — inbox.go (Plan 7 Phase E Task E-12).
//
// Three methods + supporting wire types for the daemon's
// /v1/inbox/* surface backing the operator-facing
// `zen inbox` CLI:
//
//	InboxList    POST /v1/inbox/list    — filter + decode []InboxCacheRow
//	InboxAck     POST /v1/inbox/ack     — set AckedAt = now on id
//	InboxSnooze  POST /v1/inbox/snooze  — set SnoozedUntil = until on id
//
// Field names + JSON tags align with the daemon-side handler in
// internal/daemon/handlers/inbox_p7.go. Times use RFC3339 over the
// wire (Go's encoding/json default for time.Time).
//
// Wire shapes are decoupled from internal/inbox.CacheRow at the
// client layer (the typed Severity field there is a domain enum).
// The CLI re-hydrates rows back into inbox.CacheRow via a helper
// for rendering — keeping the wire boundary at strings, the
// domain layer at typed enums.
//
// 4-tier severity enum referenced (validated server-side):
//
//	urgent | action-needed | info-immediate | info-digest
package client

import (
	"context"
	"fmt"
	"time"
)

type InboxListRequest struct {
	Severity     string `json:"severity,omitempty"`
	Project      string `json:"project,omitempty"`
	SinceUnix    int64  `json:"since_unix,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	IncludeAcked bool   `json:"include_acked,omitempty"`
}

type InboxListResponse struct {
	Rows []InboxCacheRow `json:"rows"`
}

type InboxCacheRow struct {
	CacheID        int64      `json:"cache_id"`
	ProjectID      string     `json:"project_id"`
	ProjectAlias   string     `json:"project_alias"`
	NotificationID int64      `json:"notification_id"`
	Severity       string     `json:"severity"`
	EventType      string     `json:"event_type"`
	ContentHash    string     `json:"content_hash"`
	CreatedAt      time.Time  `json:"created_at"`
	AckedAt        *time.Time `json:"acked_at,omitempty"`
}

type InboxAckRequest struct {
	ID int64 `json:"id"`
}

type InboxSnoozeRequest struct {
	ID    int64     `json:"id"`
	Until time.Time `json:"until"`
}

func (c *Client) InboxList(ctx context.Context, req InboxListRequest) ([]InboxCacheRow, error) {
	var resp InboxListResponse
	if err := c.postJSON(ctx, "/v1/inbox/list", req, &resp); err != nil {
		return nil, err
	}
	if resp.Rows == nil {
		return []InboxCacheRow{}, nil
	}
	return resp.Rows, nil
}

// InboxAck calls POST /v1/inbox/ack with the id. Returns nil on 200,
// *HTTPError on non-2xx (404 → recoverable mapped at CLI layer).
//
// id MUST be > 0; passing 0 returns a client-side error without
// hitting the network.
func (c *Client) InboxAck(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("InboxAck: id must be positive (got %d)", id)
	}
	var resp struct {
		OK bool  `json:"ok"`
		ID int64 `json:"id"`
	}
	return c.postJSON(ctx, "/v1/inbox/ack", InboxAckRequest{ID: id}, &resp)
}

// InboxSnooze calls POST /v1/inbox/snooze with id+until. until is
// converted to UTC server-side. Returns nil on 200, *HTTPError on
// non-2xx.
//
// id MUST be > 0; until MUST be non-zero. Both are checked client-side
// to surface operator typos without a round-trip.
func (c *Client) InboxSnooze(ctx context.Context, id int64, until time.Time) error {
	if id <= 0 {
		return fmt.Errorf("InboxSnooze: id must be positive (got %d)", id)
	}
	if until.IsZero() {
		return fmt.Errorf("InboxSnooze: until is zero")
	}
	var resp struct {
		OK    bool      `json:"ok"`
		ID    int64     `json:"id"`
		Until time.Time `json:"until"`
	}
	return c.postJSON(ctx, "/v1/inbox/snooze", InboxSnoozeRequest{ID: id, Until: until.UTC()}, &resp)
}
