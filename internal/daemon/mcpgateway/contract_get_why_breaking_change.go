// SPDX-License-Identifier: MIT
// Package mcpgateway — contract_get_why_breaking_change.go.
//
// Handler for the `get_why_breaking_change` MCP tool. Validates required
// `change` arg + dispatches to engine.GetWhyBreakingChange. Returns the
// WhyBreakingChange payload (D7 Lore-attribution evidence chain).
//
// The underlying engine call surfaces bcdetect.LoreAttribution (
// type; the package is `bcdetect`, NOT `intent` — `intent.LoreAttribution`
// does not exist and would be a fictional reference). The engine maps the
// attribution into the WhyBreakingChange value type before returning.
package mcpgateway

import "context"

func (p *CaronteProxy) handleGetWhyBreakingChange(ctx context.Context, req CallRequest) (any, error) {
	changeID, err := caronteStringArg(req.Args, "change")
	if err != nil {
		return nil, err
	}
	payload, err := p.engine.GetWhyBreakingChange(ctx, changeID)
	if err != nil {
		return nil, err
	}
	return payload, nil
}
