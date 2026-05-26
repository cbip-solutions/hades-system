// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_trace_api_call.go (Plan 20 Phase I).
//
// Handler for the `trace_api_call` MCP tool. Validates required `call` +
// `workspace` args + dispatches to engine.TraceAPICall; returns the
// APICallTrace payload.
package mcpgateway

import "context"

func (p *CaronteProxy) handleTraceAPICall(ctx context.Context, req CallRequest) (any, error) {
	callID, err := caronteStringArg(req.Args, "call")
	if err != nil {
		return nil, err
	}
	workspaceID, err := caronteStringArg(req.Args, "workspace")
	if err != nil {
		return nil, err
	}
	payload, err := p.engine.TraceAPICall(ctx, callID, workspaceID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
