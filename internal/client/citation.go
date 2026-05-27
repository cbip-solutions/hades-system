// SPDX-License-Identifier: MIT
// Package client — citation.go
//
// audit-event daemon endpoints shipped by (audit_event handler).
// only needs the audit-event resolver here; the structured
// citation rendering lives behind the daemon-side renderer set (
// ships the 6 platform renderers; ships only the
// markdown_fallback).
package client

import (
	"context"
	"net/url"
)

type AuditEventResolveResponse struct {
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	TessLeaf      string         `json:"tessera_leaf"`
	ProjectAlias  string         `json:"project_alias,omitempty"`
	DoctrineName  string         `json:"doctrine_name,omitempty"`
	TimestampUnix int64          `json:"timestamp_unix"`
	Detail        map[string]any `json:"detail,omitempty"`
}

func (c *Client) AuditEventResolve(ctx context.Context, id string) (*AuditEventResolveResponse, error) {
	var resp AuditEventResolveResponse
	if err := c.getJSON(ctx, "/v1/audit/event/"+id, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type CitationProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (c *Client) CitationProbe(ctx context.Context, check string) (*CitationProbeResp, error) {
	u := "/v1/citation/probe?check=" + url.QueryEscape(check)
	var resp CitationProbeResp
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
