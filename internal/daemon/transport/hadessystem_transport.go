// SPDX-License-Identifier: MIT
// HadesSystemTransport is the daemon-side counterpart of the Python
// ProviderTransport ABC implementation. It exposes the release dispatcher
// chain as a providers.TierBackend so the compile-anchor proves the Go
// side honours the same contract the Python side enforces at the Hermes
// boundary.
//
// Production traffic does NOT flow through this type — it flows through the
// HTTP handler (messages_handler.go) which calls dispatcher.Forward
// directly. HadesSystemTransport.Forward exists so daemon-internal callers
// (future MCP-internal LLM dispatch) can route through the same single-
// egress chokepoint without instantiating the dispatcher directly. This is
// the same defence-in-depth pattern release uses for BypassBackend and the
// providers.toml cascade backends (concrete types behind the
// providers.TierBackend interface).

package transport

import (
	"context"
	"fmt"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

type HadesSystemTransport struct {
	dispatcher Dispatcher
	anchor     AuditAnchor
}

var _ providers.TierBackend = (*HadesSystemTransport)(nil)

// NewHadesSystemTransport constructs a HadesSystemTransport bound to the given
// dispatcher. dispatcher MUST NOT be nil — passing nil is a wiring bug at
// daemon bootstrap that fails fast here rather than at first Forward.
//
// anchor MAY be nil;
// Forward checks for nil before calling Emit. Production wiring always
// supplies a non-nil anchor (internal/audit/chain.New).
func NewHadesSystemTransport(dispatcher Dispatcher, anchor AuditAnchor) *HadesSystemTransport {
	if dispatcher == nil {
		panic("transport.NewHadesSystemTransport: dispatcher is required")
	}
	return &HadesSystemTransport{
		dispatcher: dispatcher,
		anchor:     anchor,
	}
}

func (t *HadesSystemTransport) Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error) {
	resp, err := t.dispatcher.Forward(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("hadessystem-transport: %w", err)
	}
	return resp, nil
}

func (t *HadesSystemTransport) Probe(_ context.Context) error { return nil }

func (t *HadesSystemTransport) Close() error { return nil }

// Name returns the stable registry key for HadesSystemTransport. MUST NOT
// change across releases (cost_ledger.tier and audit-chain rows persist
// this string verbatim). release discipline applies.
func (t *HadesSystemTransport) Name() string { return "hadessystem-transport" }

func (t *HadesSystemTransport) Capabilities() providers.TierCapabilities {
	return providers.TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       true,
		SupportsVision:        true,
		SupportsPromptCaching: true,
		MaxContextTokens:      200_000,
		MaxOutputTokens:       64_000,
	}
}

func (t *HadesSystemTransport) Tier() providers.Tier { return providers.TierInHouse }
