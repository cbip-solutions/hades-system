// SPDX-License-Identifier: MIT
// Package client — orchestrator.go.
//
// Typed helper methods for /v1/orchestrator/* endpoints exercised by the
// `zen orchestrator` CLI subcommands. Replaces the release stub set
// (OrchestratorStatus + OrchestratorPin/Unpin against /switch) with a
// six-method surface backed by the +C+D+E components.
package client

import "context"

func (c *Client) OrchestratorStatus(ctx context.Context) (*OrchestratorStatusResp, error) {
	var out OrchestratorStatusResp
	if err := c.getJSON(ctx, "/v1/orchestrator/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorPin(ctx context.Context, req OrchestratorPinReq) error {
	return c.postJSON(ctx, "/v1/orchestrator/pin", req, nil)
}

func (c *Client) OrchestratorUnpin(ctx context.Context, req OrchestratorUnpinReq) error {
	return c.postJSON(ctx, "/v1/orchestrator/unpin", req, nil)
}

func (c *Client) OrchestratorPins(ctx context.Context) (*OrchestratorPinsResp, error) {
	var out OrchestratorPinsResp
	if err := c.getJSON(ctx, "/v1/orchestrator/pins", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorProbe(ctx context.Context) (*OrchestratorProbeResp, error) {
	var out OrchestratorProbeResp
	if err := c.postJSON(ctx, "/v1/orchestrator/probe", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) OrchestratorHistory(ctx context.Context) (*OrchestratorHistoryResp, error) {
	var out OrchestratorHistoryResp
	if err := c.getJSON(ctx, "/v1/orchestrator/history", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
