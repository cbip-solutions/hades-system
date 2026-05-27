// SPDX-License-Identifier: MIT
// internal/providers/bypass_backend.go
//
// BypassBackend wraps bypass.Client (private-tier1-module) as a
// TierBackend so the dispatcher can select between Tier 1 (bypass,
// this file) and the providers.toml cascade uniformly.
//
// Substrate decision (ADR-0008 plan-3-rescope):
// - Tier 1 (TierInHouse) = bypass.Client, which speaks directly to Anthropic
// via Max-subscription OAuth. It authenticates, cert-pins, and manages
// concurrency + idempotency internally.
// - The dispatcher selects Tier 1 when bypass-config exists and circuit
// breaker permits; falls back to the providers.toml cascade otherwise.
//
// Import boundary:
// - This file MUST NOT import private-tier1-module directly.
// - The BypassClient interface defined here uses only context + stdlib types.
// - Daemon bootstrap constructs a thin adapter
// that wraps *bypass.Client and satisfies BypassClient.
//
// Token usage:
// - bypass.Client.Forward returns the raw Anthropic response body.
// - BypassBackend parses "usage.input_tokens" / "usage.output_tokens" from
// the response body (canonical Anthropic wire format).
// - Missing usage field → zero token counts (non-fatal; treats as
// zero-cost — the convention every cascade backend follows).
//
// Credential forwarding:
// - TierRequest.Credentials are revealed at the last moment before the
// ForwardRaw call and injected into the headers map passed to the client.
// - TierRequest.Headers minus managed keys (Content-Type, Authorization)
// are also forwarded so X-Zen-* metadata reaches bypass.Client.Render.
//
// Probe:
// - Delegates to BypassClient.Health — content-free, no Anthropic Max sub
// turn consumed.
// - circuit breaker recovery scheduler calls Probe to detect Open →
// Closed transition.
//
// invariant compile guard (TierBackend interface) sits below the struct.
// invariant boundary: this file must NOT import internal/store or
// private-tier1-module (enforced by the providers package-level doc.go).
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrBypassUnavailable = errors.New("bypass unavailable")

type BypassClient interface {
	ForwardRaw(ctx context.Context, body []byte, headers map[string]string, conversationID string) (respBody []byte, status int, retryAfter time.Duration, err error)

	Health(ctx context.Context) error
}

type BypassBackend struct {
	client BypassClient
}

var _ TierBackend = (*BypassBackend)(nil)

func NewBypassBackend(client BypassClient) *BypassBackend {
	return &BypassBackend{client: client}
}

// Name returns the stable registry key for BypassBackend. MUST NOT change
// across releases — cost_ledger.tier and pin_overrides.provider hold this as text.
func (b *BypassBackend) Name() string { return "bypass" }

func (b *BypassBackend) Tier() Tier { return TierInHouse }

func (b *BypassBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       true,
		SupportsVision:        true,
		SupportsPromptCaching: true,
		MaxContextTokens:      200_000,
		MaxOutputTokens:       64_000,
	}
}

func (b *BypassBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {

	headers := make(map[string]string, len(req.Headers)+len(req.Credentials))
	for k, v := range req.Headers {
		if k == headerContentType || k == headerAuthorization {

			continue
		}
		headers[k] = v
	}
	for k, v := range req.Credentials {
		headers[k] = string(v.Reveal())
	}

	start := time.Now()
	respBody, status, retryAfter, err := b.client.ForwardRaw(ctx, req.Body, headers, req.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("bypass: forward: %w", err)
	}
	latencyMs := time.Since(start).Milliseconds()

	if status >= 300 {

		if status == 429 {
			return nil, &RateLimitedError{Provider: "bypass", RetryAfter: retryAfter}
		}

		truncated := respBody
		if len(truncated) > 512 {
			truncated = append(truncated[:512:512], []byte("…[truncated]")...)
		}
		return nil, fmt.Errorf("bypass: status %d: %s", status, string(truncated))
	}

	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(respBody, &parsed)

	modelUsed := parsed.Model
	if modelUsed == "" {
		modelUsed = req.Model
	}

	return &TierResponse{
		Status:       status,
		Body:         respBody,
		TierUsed:     TierInHouse,
		ModelUsed:    modelUsed,
		LatencyMs:    latencyMs,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		Headers:      map[string]string{},
	}, nil
}

func (b *BypassBackend) Probe(ctx context.Context) error {
	if err := b.client.Health(ctx); err != nil {
		return fmt.Errorf("bypass probe: %w: %w", ErrBypassUnavailable, err)
	}
	return nil
}

func (b *BypassBackend) Close() error { return nil }
