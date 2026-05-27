// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_get_consumers.go.
//
// Handler for the `get_consumers` MCP tool. Validates required `endpoint` +
// `workspace` args + dispatches to engine.GetConsumers; returns the
// ConsumerList payload. Capa-firewall enforced upstream (inv-hades-031).
package mcpgateway

import "context"

func (p *CaronteProxy) handleGetConsumers(ctx context.Context, req CallRequest) (any, error) {
	endpointID, err := caronteStringArg(req.Args, "endpoint")
	if err != nil {
		return nil, err
	}
	workspaceID, err := caronteStringArg(req.Args, "workspace")
	if err != nil {
		return nil, err
	}
	payload, err := p.engine.GetConsumers(ctx, endpointID, workspaceID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
