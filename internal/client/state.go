// SPDX-License-Identifier: MIT
// Package client — state.go.
//
// 5 typed wrappers for the release system-state endpoints declared in
// internal/daemon/handlers/state.go. Wire types mirror the handler declarations;
// duplication is intentional (client compiles standalone without importing
// internal/daemon — release N convention).
//
// GET /v1/state/show — StateShow
// POST /v1/state/regenerate — StateRegenerate
// POST /v1/state/verify — StateVerify
// POST /v1/state/pin — StatePin
// GET /v1/state/history — StateHistory
//
// invariant: this file imports stdlib only (context, net/url).
// No internal/daemon, internal/store, or internal/state imports.
//
// Type-name note: handler types use the P9 suffix (StateManifestP9,
// StateDiffP9, etc.) to coexist with potential legacy types inside the
// daemon package. At the client layer, P9 suffix is dropped per the
// knowledge_p9.go convention — the client types are the authoritative
// client-side names with no collision risk.
package client

import (
	"context"
	"net/url"
)

type StateManifest struct {
	LastRegenerateUnix int64  `json:"last_regenerate_unix"`
	ManualFieldCount   int    `json:"manual_field_count"`
	MissingSourceCount int    `json:"missing_source_count,omitempty"`
	TomlContent        string `json:"toml_content"`
}

type StateRegenerateReq struct {
	DryRun bool `json:"dry_run"`
}

type StateRegenerateResp struct {
	DryRun        bool     `json:"dry_run"`
	ChangedFields []string `json:"changed_fields"`
	Diff          string   `json:"diff,omitempty"`
}

type StateDiff struct {
	Match bool   `json:"match"`
	Diff  string `json:"diff,omitempty"`
}

// StatePinReq is the body of POST /v1/state/pin.
// Field and Value are both required. Reason MUST be non-empty.
// OperatorID is optional and overridden by peer-cred in production.
type StatePinReq struct {
	Field      string `json:"field"`
	Value      string `json:"value"`
	Reason     string `json:"reason"`
	OperatorID string `json:"operator_id,omitempty"`
}

type StateChange struct {
	Field      string `json:"field"`
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	Reason     string `json:"reason"`
	At         int64  `json:"at_unix"`
	OperatorID string `json:"operator_id"`
}

func (c *Client) StateShow(ctx context.Context) (StateManifest, error) {
	var out StateManifest
	if err := c.getJSON(ctx, "/v1/state/show", &out); err != nil {
		return StateManifest{}, err
	}
	return out, nil
}

func (c *Client) StateRegenerate(ctx context.Context, req StateRegenerateReq) (StateRegenerateResp, error) {
	var out StateRegenerateResp
	if err := c.postJSON(ctx, "/v1/state/regenerate", req, &out); err != nil {
		return StateRegenerateResp{}, err
	}
	return out, nil
}

func (c *Client) StateVerify(ctx context.Context) (StateDiff, error) {
	var out StateDiff
	if err := c.postJSON(ctx, "/v1/state/verify", nil, &out); err != nil {
		return StateDiff{}, err
	}
	return out, nil
}

func (c *Client) StatePin(ctx context.Context, req StatePinReq) error {
	return c.postJSON(ctx, "/v1/state/pin", req, nil)
}

func (c *Client) StateHistory(ctx context.Context, field string) ([]StateChange, error) {
	q := url.Values{}
	if field != "" {
		q.Set("field", field)
	}
	path := "/v1/state/history"
	if e := q.Encode(); e != "" {
		path += "?" + e
	}
	var out struct {
		Items []StateChange `json:"items"`
		Count int           `json:"count"`
	}
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []StateChange{}
	}
	return out.Items, nil
}
