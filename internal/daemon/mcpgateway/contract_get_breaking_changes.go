// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_get_breaking_changes.go.
//
// Handler for the `get_breaking_changes` MCP tool. Validates required
// `workspace` arg + optional `since` (int unix-seconds; default 0 = unbounded)
// + dispatches to engine.GetBreakingChanges. Returns the
// []BreakingChangePayload slice as the MCP payload.
package mcpgateway

import "context"

func (p *CaronteProxy) handleGetBreakingChanges(ctx context.Context, req CallRequest) (any, error) {
	workspaceID, err := caronteStringArg(req.Args, "workspace")
	if err != nil {
		return nil, err
	}
	since := int64(caronteIntArg(req.Args, "since", 0))
	payload, err := p.engine.GetBreakingChanges(ctx, workspaceID, since)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
