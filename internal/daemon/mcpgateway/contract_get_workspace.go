// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_get_workspace.go.
//
// Handler for the `get_workspace` MCP tool. Validates required `workspace`
// arg + dispatches to engine.GetWorkspace; returns the WorkspaceSnapshot
// payload (composite of GetWorkspace + ListWorkspaceMembers +
// GetWorkspacePolicy per the engine-method delegation contract).
package mcpgateway

import "context"

func (p *CaronteProxy) handleGetWorkspace(ctx context.Context, req CallRequest) (any, error) {
	workspaceID, err := caronteStringArg(req.Args, "workspace")
	if err != nil {
		return nil, err
	}
	payload, err := p.engine.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
