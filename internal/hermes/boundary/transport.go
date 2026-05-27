// SPDX-License-Identifier: MIT
package boundary

import (
	"context"
	"fmt"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/daemon/transport"
	"github.com/cbip-solutions/hades-system/internal/providers"
)

// NewHermesCliFromHadesSystemTransport adapts an existing daemon-side
// HadesSystemTransport (internal/daemon/transport/hadessystem_transport.go) into
// the boundary.HermesCli interface. Constructor preserves the inv-hades-164
// compile-anchor (the underlying transport.HadesSystemTransport file stays at
// its canonical path); the boundary package consumes it via this adapter.
//
// Per release / policy: the HadesSystemTransport is NOT moved
// (would shatter the inv-hades-164 grep). Instead, the boundary package
// wraps it so the consolidation surface (Surface interface) covers the
// existing single-egress completion path AND adds capability-feature
// detection + lifecycle hooks for future Hermes API growth.
//
// version MUST match the.hermes-version pin at repo root (consumed by
// CapabilitiesFor to compute the empirically-verified capability snapshot).
// Production wiring at cmd/hades-ctld bootstrap reads.hermes-version
// + passes the value here.
//
// Returned HermesCli:
// - SendCompletion routes via the existing HadesSystemTransport.Forward
// (single-egress preserved per inv-hades-164 + inv-hades-088).
// - RegisterStatusProvider / OnSessionStart / RenderInlinePrompt all
// return ErrCapabilityUnavailable in v0.13.x (G2/G3/G5 absent).
// - OnPreToolCall accepts handlers and stores them; production wiring
// surfaces them at the Python/Hermes integration boundary.
// - WrapMCPEnvelope delegates to the canonical WrapMCPEnvelope helper.
//
// Behaviour preservation: callers of the underlying HadesSystemTransport
// (compliance test inv-hades-164, integration tests, daemon dispatcher
// wiring) see no behaviour change. The boundary wrapping is additive — it
// gives consolidation a home without removing the existing surface.
func NewHermesCliFromHadesSystemTransport(zt *transport.HadesSystemTransport, version HermesVersion) HermesCli {
	if zt == nil {
		panic("boundary.NewHermesCliFromHadesSystemTransport: zt is required")
	}
	return &zenSwarmHermesCli{
		zt:           zt,
		cap:          CapabilitiesFor(version),
		preToolHooks: make([]PreToolCallHandler, 0, 1),
	}
}

type zenSwarmHermesCli struct {
	zt           *transport.HadesSystemTransport
	cap          Capabilities
	hooksMu      sync.RWMutex
	preToolHooks []PreToolCallHandler
}

func (c *zenSwarmHermesCli) SendCompletion(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if req.Model == "" {
		return CompletionResponse{}, fmt.Errorf("boundary.SendCompletion: req.Model is required")
	}

	tierReq := providers.TierRequest{
		Body: []byte(req.Prompt),
	}
	tierResp, err := c.zt.Forward(ctx, tierReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("boundary: %w", err)
	}
	if tierResp == nil {
		return CompletionResponse{}, fmt.Errorf("boundary.SendCompletion: nil response with nil error from transport")
	}
	return CompletionResponse{
		Text:      string(tierResp.Body),
		ModelUsed: req.Model,
		Status:    tierResp.Status,
	}, nil
}

func (c *zenSwarmHermesCli) RegisterStatusProvider(_ StatusProvider) error {
	return ErrCapabilityUnavailable
}

func (c *zenSwarmHermesCli) OnSessionStart(_ SessionStartHandler) error {
	return ErrCapabilityUnavailable
}

func (c *zenSwarmHermesCli) OnPreToolCall(handler PreToolCallHandler) error {
	c.hooksMu.Lock()
	defer c.hooksMu.Unlock()
	c.preToolHooks = append(c.preToolHooks, handler)
	return nil
}

func (c *zenSwarmHermesCli) RenderInlinePrompt(_ context.Context, _ InlinePrompt) (InlinePromptResponse, error) {
	return InlinePromptResponse{}, ErrCapabilityUnavailable
}

func (c *zenSwarmHermesCli) WrapMCPEnvelope(payload MCPPayload) MCPEnvelope {
	return WrapMCPEnvelope(payload)
}

func (c *zenSwarmHermesCli) Capabilities() Capabilities { return c.cap }

func (c *zenSwarmHermesCli) PreToolHooks() []PreToolCallHandler {
	c.hooksMu.RLock()
	defer c.hooksMu.RUnlock()
	out := make([]PreToolCallHandler, len(c.preToolHooks))
	copy(out, c.preToolHooks)
	return out
}
