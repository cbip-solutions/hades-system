// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_federation_health.go.
//
// Handler for the `federation_health` MCP tool. The `workspace` arg is
// OPTIONAL empty → daemon-wide health (aggregate over all workspaces);
// non-empty → workspace-scoped health. Dispatches to
// engine.FederationHealth; returns the FederationHealthReport payload.
package mcpgateway

import "context"

func (p *CaronteProxy) handleFederationHealth(ctx context.Context, req CallRequest) (any, error) {
	workspaceID, _ := caronteStringArgOpt(req.Args, "workspace")
	payload, err := p.engine.FederationHealth(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
