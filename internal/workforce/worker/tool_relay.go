// SPDX-License-Identifier: MIT
package worker

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolRelay routes a tool_use event from the OpenClaude subprocess to
// the corresponding MCP backend (research, ssh_exec, audit_review,
// budget). ships this interface + the unavailableRelay default;
// Plans 4 Phases I/J/K/L wire concrete implementations.
//
// Concurrency implementations MUST be safe for concurrent Dispatch calls
// from a single Worker. Multi-worker concurrency is the relay's
// responsibility (Plans I/J/K/L document fan-out behaviour).
type ToolRelay interface {
	Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}

type ToolRelayFunc func(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)

func (f ToolRelayFunc) Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return f(ctx, name, input)
}

type unavailableRelay struct{}

func NewUnavailableRelay() ToolRelay { return unavailableRelay{} }

func (unavailableRelay) Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: tool=%q", ErrToolNotAvailable, name)
}
