// SPDX-License-Identifier: MIT
// Package client — bypass.go (Plan 2 Phase L Task L-2).
//
// Typed helper methods for /v1/bypass/* endpoints exercised by the zen
// CLI subcommands.
package client

import (
	"context"
	"fmt"
)

func (c *Client) BypassStatus(ctx context.Context) (*BypassStatusResp, error) {
	var out BypassStatusResp
	if err := c.getJSON(ctx, "/v1/bypass/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassProbe(ctx context.Context) (*BypassProbeResp, error) {
	var out BypassProbeResp
	if err := c.postJSON(ctx, "/v1/bypass/probe", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassAudit(ctx context.Context, q BypassAuditQuery) (*BypassAuditResp, error) {
	path := "/v1/bypass/audit"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var out BypassAuditResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassDoctor(ctx context.Context, check string) (*BypassDoctorResp, error) {
	var out BypassDoctorResp
	if err := c.getJSON(ctx, "/v1/bypass/doctor?check="+check, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassRefreshNow(ctx context.Context) (*BypassRefreshNowResp, error) {
	var out BypassRefreshNowResp
	if err := c.postJSON(ctx, "/v1/bypass/refresh-now", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassTest(ctx context.Context) (*BypassTestResp, error) {
	var out BypassTestResp
	if err := c.postJSON(ctx, "/v1/bypass/test", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassUpdateConfig(ctx context.Context, opts BypassUpdateOpts) (*BypassUpdateResp, error) {
	var out BypassUpdateResp
	if err := c.postJSON(ctx, "/v1/bypass/update-config", opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassExtractConfig(ctx context.Context, opts ExtractOpts) (*BypassExtractResp, error) {
	var out BypassExtractResp
	if err := c.postJSON(ctx, "/v1/bypass/extract-config", opts, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassCrossValidate(ctx context.Context, plugin string) (*BypassCrossValidateResp, error) {
	var out BypassCrossValidateResp
	body := struct {
		Plugin string `json:"plugin"`
	}{Plugin: plugin}
	if err := c.postJSON(ctx, "/v1/bypass/cross-validate", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassAnomalies(ctx context.Context) ([]BypassAnomaly, error) {
	var out []BypassAnomaly
	if err := c.getJSON(ctx, "/v1/bypass/anomalies", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) BypassAnomaliesAck(ctx context.Context, field string) error {
	body := struct {
		Field string `json:"field"`
	}{Field: field}
	return c.postJSON(ctx, "/v1/bypass/anomalies/ack", body, nil)
}

func (c *Client) BypassPin(ctx context.Context, conversationID string) error {
	body := struct {
		ConversationID string `json:"conversation_id"`
		Reason         string `json:"reason"`
	}{ConversationID: conversationID, Reason: "operator"}
	return c.postJSON(ctx, "/v1/bypass/pin", body, nil)
}

func (c *Client) BypassUnpin(ctx context.Context, conversationID string) error {
	body := struct {
		ConversationID string `json:"conversation_id"`
	}{ConversationID: conversationID}
	return c.postJSON(ctx, "/v1/bypass/unpin", body, nil)
}

func (c *Client) BypassPurge(ctx context.Context, apply bool) (*BypassPurgeResp, error) {
	path := "/v1/bypass/purge"
	if apply {
		path += "?apply=1"
	}
	var out BypassPurgeResp
	if err := c.postJSON(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassCertsShow(ctx context.Context) (*BypassCertsShowResp, error) {
	var out BypassCertsShowResp
	if err := c.getJSON(ctx, "/v1/bypass/certs", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) BypassCertsRotate(ctx context.Context, sha256 string) error {
	body := struct {
		SHA256 string `json:"sha256"`
	}{SHA256: sha256}
	return c.postJSON(ctx, "/v1/bypass/certs/rotate", body, nil)
}

func (c *Client) BypassCFRange(ctx context.Context, refresh bool) (*BypassCFRangeResp, error) {
	path := "/v1/bypass/cf-range"
	if refresh {
		path += "?refresh=1"
	}
	var out BypassCFRangeResp
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) NotificationsList(ctx context.Context, limit int, unacked bool) ([]NotificationRow, error) {
	path := "/v1/notifications"
	q := ""
	if limit > 0 {
		q = fmt.Sprintf("limit=%d", limit)
	}
	if unacked {
		if q != "" {
			q += "&"
		}
		q += "unacked=1"
	}
	if q != "" {
		path += "?" + q
	}
	var out []NotificationRow
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) NotificationsDismiss(ctx context.Context, id int64) error {
	return c.postJSON(ctx, fmt.Sprintf("/v1/notifications/%d/dismiss", id), nil, nil)
}
