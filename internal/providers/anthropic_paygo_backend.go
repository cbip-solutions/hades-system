// SPDX-License-Identifier: MIT
// internal/providers/anthropic_paygo_backend.go
//
// AnthropicPaygoBackend is the TierBackend for the Anthropic /v1/messages
// pay-as-you-go API-key path (TierAnthropicPAYG). It is the operator's
// S'-fallback and D-judge (Haiku) backend (spec §3.6).
//
// Native canonical format: the dispatcher's TierRequest.Body is already
// the Anthropic Messages API JSON shape, so this backend forwards the
// body verbatim — no translate.go involvement. Auth uses x-api-key +
// anthropic-version headers (the pay-as-you-go API contract) and the
// key resolves from the shared internal/keychain package at construction.
//
// invariant: this file imports internal/keychain (boundary-neutral)
// but NOT internal/store or tier1-sidecar.
// invariant compile guard sits below the struct.
// invariant: the API key is a redact.Secret, revealed only at the
// moment of header injection.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

const anthropicAPIVersion = "2023-06-01"

var ErrAnthropicPaygoUnavailable = errors.New("anthropic-paygo unavailable")

type AnthropicPaygoBackend struct {
	endpoint   string
	model      string
	apiKey     redact.Secret
	headers    map[string]string
	httpClient *http.Client
}

var _ TierBackend = (*AnthropicPaygoBackend)(nil)

func NewAnthropicPaygoBackend(cfg ProviderConfig, kc keychain.Resolver) (*AnthropicPaygoBackend, error) {
	if kc == nil {
		return nil, fmt.Errorf("providers.NewAnthropicPaygoBackend(%q): keychain resolver is nil", cfg.Name)
	}
	key, err := kc.Lookup(cfg.APIKeyKeychain, keychainAccount)
	if err != nil {
		return nil, fmt.Errorf("providers.NewAnthropicPaygoBackend(%q): resolve api key: %w", cfg.Name, err)
	}
	return &AnthropicPaygoBackend{
		endpoint:   cfg.Endpoint,
		model:      cfg.Model,
		apiKey:     key,
		headers:    cfg.Headers,
		httpClient: &http.Client{Timeout: 90 * time.Second},
	}, nil
}

// Name returns the stable registry key. MUST NOT change across releases
// (persisted in cost_ledger.provider).
func (b *AnthropicPaygoBackend) Name() string { return "anthropic-paygo" }

func (b *AnthropicPaygoBackend) Tier() Tier { return TierAnthropicPAYG }

func (b *AnthropicPaygoBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       true,
		SupportsVision:        true,
		SupportsPromptCaching: true,
		MaxContextTokens:      200_000,
		MaxOutputTokens:       64_000,
	}
}

func (b *AnthropicPaygoBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {
	url := b.endpoint + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("anthropic-paygo: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	for k, v := range b.headers {
		httpReq.Header.Set(k, v)
	}
	for k, v := range req.Headers {
		if k == headerContentType || k == headerAuthorization {
			continue
		}
		httpReq.Header.Set(k, v)
	}
	for k, v := range req.Credentials {
		httpReq.Header.Set(k, string(v.Reveal()))
	}

	httpReq.Header.Set("x-api-key", string(b.apiKey.Reveal()))

	start := time.Now()
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic-paygo: forward: %w", err)
	}
	defer resp.Body.Close()
	latencyMs := time.Since(start).Milliseconds()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic-paygo: read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic-paygo: status %d: %s", resp.StatusCode, capBody(body))
	}

	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(body, &parsed)
	modelUsed := parsed.Model
	if modelUsed == "" {
		modelUsed = req.Model
	}
	return &TierResponse{
		Status:              resp.StatusCode,
		Body:                body,
		TierUsed:            TierAnthropicPAYG,
		ModelUsed:           modelUsed,
		LatencyMs:           latencyMs,
		InputTokens:         parsed.Usage.InputTokens,
		OutputTokens:        parsed.Usage.OutputTokens,
		CacheReadTokens:     parsed.Usage.CacheReadTokens,
		CacheCreationTokens: parsed.Usage.CacheCreationTokens,
		Headers:             responseHeaders(resp.Header),
	}, nil
}

func (b *AnthropicPaygoBackend) Probe(ctx context.Context) error {
	probeBody := fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, b.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint+"/v1/messages", bytes.NewReader([]byte(probeBody)))
	if err != nil {
		return fmt.Errorf("anthropic-paygo probe: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	httpReq.Header.Set("x-api-key", string(b.apiKey.Reveal()))
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("anthropic-paygo probe: %w: %w", ErrAnthropicPaygoUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("anthropic-paygo probe: status %d: %w", resp.StatusCode, ErrAnthropicPaygoUnavailable)
	}
	return nil
}

func (b *AnthropicPaygoBackend) Close() error {
	b.apiKey.Wipe()
	return nil
}
