// SPDX-License-Identifier: MIT
// Package client — specs_sync.go.
//
// SpecsSync wires the operator-facing `hades specs sync` subcommand to the
// daemon's POST /v1/knowledge/ecosystem/specs-sync route. The daemon-side
// handler is wired in ; F-5 declares the client-side
// method + wire types so the CLI surface is final-shape day 1 per project
// doctrine (build the final product, not the stages — no MVP-then-extend).
//
// Wire contract (spec §0.2): specs are read-only at the CLI surface;
// sync triggers a re-index of openspec/specs/ into ecosystem.db
// for RAG retrieval. Write-back of specs is deferred to a future
// release.
//
// Error mapping at the CLI layer (see classifySpecsError in internal/cli/
// specs_sync.go):
// - 404 → ErrRecoverable (route not yet wired in ; transient
// operator-recoverable state — phase F can ship before lands
// the handler).
// - 422 → ErrRecoverable (daemon rejected operator input).
// - 5xx / transport / decode → bare err (exit 2 unrecoverable).
package client

import "context"

type SpecsSyncRequest struct {
	SpecsDir string `json:"specs_dir,omitempty"`
	Full     bool   `json:"full"`
}

type SpecsSyncResponse struct {
	ChunksIndexed int    `json:"chunks_indexed"`
	SpecsScanned  int    `json:"specs_scanned"`
	ElapsedMs     int64  `json:"elapsed_ms"`
	Message       string `json:"message,omitempty"`
}

func (c *Client) SpecsSync(ctx context.Context, req SpecsSyncRequest) (*SpecsSyncResponse, error) {
	var resp SpecsSyncResponse
	if err := c.postJSON(ctx, "/v1/knowledge/ecosystem/specs-sync", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
