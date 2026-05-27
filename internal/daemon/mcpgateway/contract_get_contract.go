// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_get_contract.go.
//
// Handler for the `get_contract` MCP tool. Validates the required `endpoint`
// arg + dispatches to engine.GetContract; returns the ContractPayload as the
// MCP payload. Capa-firewall is enforced UPSTREAM in the engine call
// (Workspace.authorize() chokepoint, release M / release A); this handler
// does NOT re-enforce — separating capa-firewall (storage) from argument
// validation (surface) is the invariant split.
package mcpgateway

import "context"

func (p *CaronteProxy) handleGetContract(ctx context.Context, req CallRequest) (any, error) {
	endpointID, err := caronteStringArg(req.Args, "endpoint")
	if err != nil {
		return nil, err
	}
	payload, err := p.engine.GetContract(ctx, endpointID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
