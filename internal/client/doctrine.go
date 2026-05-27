// SPDX-License-Identifier: MIT
// Package client — doctrine.go.
//
// Typed wrappers for the doctrine HTTP surface ( handlers
// in internal/daemon/handlers/doctrine.go):
//
// GET /v1/doctrine/state — active doctrine config snapshot
// POST /v1/doctrine/validate — static-check a TOML candidate
// POST /v1/doctrine/reload — atomic-swap reload from files
//
// The CLI in internal/cli/doctrine.go layers list / which / diff / schema
// on top using the in-process internal/doctrine package (no extra daemon
// routes needed).
package client

import (
	"context"
)

type DoctrineState map[string]any

type DoctrineValidateReq struct {
	TOMLContent string `json:"toml_content"`
}

type DoctrineValidateResp struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

type DoctrineReloadResp struct {
	Reloaded bool          `json:"reloaded"`
	State    DoctrineState `json:"state,omitempty"`
	Errors   []string      `json:"errors,omitempty"`
	Error    string        `json:"error,omitempty"`
}

func (c *Client) DoctrineStateCall(ctx context.Context) (DoctrineState, error) {
	var out DoctrineState
	if err := c.getJSON(ctx, "/v1/doctrine/state", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DoctrineValidateCall(ctx context.Context, req DoctrineValidateReq) (*DoctrineValidateResp, error) {
	var out DoctrineValidateResp
	if err := c.postJSON(ctx, "/v1/doctrine/validate", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DoctrineReloadCall(ctx context.Context) (*DoctrineReloadResp, error) {
	var out DoctrineReloadResp
	if err := c.postJSON(ctx, "/v1/doctrine/reload", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
