// SPDX-License-Identifier: MIT
// Package client — hermes.go
//
// /v1/hermes/probe?check=<name> route. Daemon side ships in
// (mcpgateway). Probe contract matches the BypassDoctor pattern
// : per-check name → {status, detail} response.
package client

import (
	"context"
	"net/url"
)

type HermesProbeResp struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func (c *Client) HermesProbe(ctx context.Context, check string) (*HermesProbeResp, error) {
	u := "/v1/hermes/probe?check=" + url.QueryEscape(check)
	var resp HermesProbeResp
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
