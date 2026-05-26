// SPDX-License-Identifier: MIT
// noop.go — production fallback adapters for cases where a daemon-routed
// adapter is unavailable (e.g. CI without the daemon running, or the
// standalone research MCP that has no in-process code-graph engine). These
// are NOT stubs in the "no stubs, código completo" doctrine sense: they are
// intentional fallback adapters with documented degradation semantics. Each
// one is minimal, complete, and behaves correctly within its degradation
// contract.
//
// Renamed from "Stub*" → "NoOp*" (post-review I-2 NIT) for semantic
// consistency: NoOp captures the intent ("does nothing observable")
// without overlap with the doctrine's "stub = unfinished half-method".
//
// PreCall always allows; Record/Emit silently drop; the code-graph fallback
// returns "not configured" errors.
package research

import (
	"context"
	"errors"
)

var errGitnexusNotConfigured = errors.New("research: code-graph backend not configured (no in-process engine)")

type NoOpGitnexus struct{}

func (NoOpGitnexus) CodeGraph(_ context.Context, _, _ string) (CodeGraphResult, error) {
	return CodeGraphResult{}, errGitnexusNotConfigured
}

func (NoOpGitnexus) Close() error { return nil }

type NoOpBudget struct{}

func (NoOpBudget) PreCall(_ context.Context, _, _ string, _ float64) (bool, string, error) {
	return true, "", nil
}

func (NoOpBudget) Record(_ context.Context, _ string, _ map[string]string) error { return nil }

type NoOpAudit struct{}

func (NoOpAudit) Emit(_ context.Context, _ string, _ []byte) error { return nil }
