// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_contract_diff.go (Plan 20 Phase I).
//
// Handler for the `contract_diff` MCP tool. Validates required `endpoint` +
// `since` (REQUIRED here — a diff with no anchor is meaningless; per Task
// I-9 the validator is strict, NOT default-to-0 because diffing-against-
// nothing is a logic bug at the caller). Dispatches to engine.ContractDiff.
package mcpgateway

import (
	"context"
	"fmt"
)

// handleContractDiff implements the `contract_diff` tool dispatch. Required
// args: "endpoint" (endpoint_id) + "since" (unix-seconds; MUST be present —
// unlike get_breaking_changes' optional since, contract_diff REQUIRES an
// anchor because diffing-against-nothing is not a meaningful operation).
func (p *CaronteProxy) handleContractDiff(ctx context.Context, req CallRequest) (any, error) {
	endpointID, err := caronteStringArg(req.Args, "endpoint")
	if err != nil {
		return nil, err
	}
	if req.Args == nil {
		return nil, fmt.Errorf("mcpgateway: caronte args nil")
	}
	if _, ok := req.Args["since"]; !ok {
		return nil, fmt.Errorf(`mcpgateway: caronte args missing "since"`)
	}
	since := int64(caronteIntArg(req.Args, "since", 0))
	payload, err := p.engine.ContractDiff(ctx, endpointID, since)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
