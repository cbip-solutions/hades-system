// SPDX-License-Identifier: MIT
// internal/providers/openai_compat_backend.go
//
// OpenAICompatBackend is the TierBackend for any provider speaking the
// OpenAI chat-completions API (TierGenericOpenAICompat). One backend
// type, N config-driven instances: the release roster routes DeepSeek,
// Moonshot, Zhipu, Perplexity, SiliconFlow, and OpenRouter through it
// (spec §3.6) — each is a distinct [[providers]] entry with its own
// endpoint, model, family, and Keychain key.
//
// Because the dispatcher's TierRequest.Body is canonical (Anthropic
// Messages shape), this backend uses translate.go on BOTH legs:
// anthropicToOpenAIRequest before the call, openAIToAnthropicResponse
// after — so the dispatcher and cost ledger always see canonical bytes.
//
// inv-hades-031: imports internal/keychain (boundary-neutral), not
// internal/store / private-tier1-module.
// inv-hades-067 compile guard below the struct. inv-hades-068: API key is a
// redact.Secret revealed only at header-injection time.
package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

var ErrOpenAICompatUnavailable = errors.New("openai-compat unavailable")

type OpenAICompatBackend struct {
	name       string
	endpoint   string
	model      string
	apiKey     redact.Secret
	headers    map[string]string
	httpClient *http.Client
}

var _ TierBackend = (*OpenAICompatBackend)(nil)

func NewOpenAICompatBackend(cfg ProviderConfig, kc keychain.Resolver) (*OpenAICompatBackend, error) {
	if kc == nil {
		return nil, fmt.Errorf("providers.NewOpenAICompatBackend(%q): keychain resolver is nil", cfg.Name)
	}
	key, err := kc.Lookup(cfg.APIKeyKeychain, keychainAccount)
	if err != nil {
		return nil, fmt.Errorf("providers.NewOpenAICompatBackend(%q): resolve api key: %w", cfg.Name, err)
	}
	return &OpenAICompatBackend{
		name:       cfg.Name,
		endpoint:   cfg.Endpoint,
		model:      cfg.Model,
		apiKey:     key,
		headers:    cfg.Headers,
		httpClient: &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func (b *OpenAICompatBackend) Name() string { return b.name }

func (b *OpenAICompatBackend) Tier() Tier { return TierGenericOpenAICompat }

func (b *OpenAICompatBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       false,
		SupportsVision:        false,
		SupportsPromptCaching: false,
		MaxContextTokens:      128_000,
		MaxOutputTokens:       8_192,
	}
}

func (b *OpenAICompatBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {

	parsedReq, parseErr := parseCanonicalRequest(req.Body)
	if parseErr != nil {
		return nil, fmt.Errorf("openai-compat(%s): translate request: %w", b.name, parseErr)
	}
	if hasToolsField(parsedReq) {
		return nil, ErrToolsUnsupported
	}
	oaiBody, err := anthropicToOpenAIRequest(req.Body, b.model)
	if err != nil {
		return nil, fmt.Errorf("openai-compat(%s): translate request: %w", b.name, err)
	}
	url := b.endpoint + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(oaiBody))
	if err != nil {
		return nil, fmt.Errorf("openai-compat(%s): new request: %w", b.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
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

	httpReq.Header.Set("Authorization", "Bearer "+string(b.apiKey.Reveal()))

	start := time.Now()
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai-compat(%s): forward: %w", b.name, err)
	}
	defer resp.Body.Close()
	latencyMs := time.Since(start).Milliseconds()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai-compat(%s): read body: %w", b.name, err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-compat(%s): status %d: %s", b.name, resp.StatusCode, capBody(rawBody))
	}

	canonBody, usage, err := openAIToAnthropicResponse(rawBody)
	if err != nil {
		return nil, fmt.Errorf("openai-compat(%s): translate response: %w", b.name, err)
	}
	return &TierResponse{
		Status:       resp.StatusCode,
		Body:         canonBody,
		TierUsed:     TierGenericOpenAICompat,
		ModelUsed:    b.model,
		LatencyMs:    latencyMs,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		Headers:      responseHeaders(resp.Header),
	}, nil
}

func (b *OpenAICompatBackend) Probe(ctx context.Context) error {
	probeBody := fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, b.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint+"/v1/chat/completions", bytes.NewReader([]byte(probeBody)))
	if err != nil {
		return fmt.Errorf("openai-compat(%s) probe: new request: %w", b.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	for k, v := range b.headers {
		httpReq.Header.Set(k, v)
	}
	httpReq.Header.Set("Authorization", "Bearer "+string(b.apiKey.Reveal()))
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("openai-compat(%s) probe: %w: %w", b.name, ErrOpenAICompatUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("openai-compat(%s) probe: status %d: %w", b.name, resp.StatusCode, ErrOpenAICompatUnavailable)
	}
	return nil
}

func (b *OpenAICompatBackend) Close() error {
	b.apiKey.Wipe()
	return nil
}
