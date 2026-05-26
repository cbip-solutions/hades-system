// SPDX-License-Identifier: MIT
// internal/providers/ollama_backend.go
//
// OllamaBackend forwards LLM requests to a local Ollama server via its
// OpenAI-compatible /v1/chat/completions endpoint (Plan 16 Phase B; spec
// §3.6). Ollama runs on localhost with no authentication, so unlike the
// anthropic_paygo / gemini / openai_compat backends NewOllamaBackend takes
// no keychain.Resolver (frozen contract C5).
//
// Wire translation: zen-swarm's canonical request/response shape is
// Anthropic /v1/messages. This backend translates canonical -> OpenAI
// chat-completions on the way out and OpenAI -> canonical on the way back,
// identically to openai_compat_backend.go. The two share the canonical
// <-> OpenAI translation helpers anthropicToOpenAIRequest /
// openAIToAnthropicResponse, shipped by Phase A in translate.go (same
// package).
//
// Design constraints (shared with the other HTTP-based tier backends):
//   - net/http direct, 90s timeout (Plan 2 bypass.Client convention).
//   - Token usage parsed from the response "usage" field; non-fatal on
//     absence (streaming variants omit it; Phase F treats absent as zero).
//   - Probe issues GET {endpoint}/api/tags (Ollama's model-list endpoint —
//     content-free, inv-zen-071) to confirm the server is reachable.
//
// inv-zen-067 compile guard sits below the struct.
// inv-zen-031 boundary: this file MUST NOT import internal/store.
package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrOllamaUnavailable = errors.New("ollama unavailable")

type OllamaBackend struct {
	name       string
	endpoint   string
	model      string
	httpClient *http.Client
}

var _ TierBackend = (*OllamaBackend)(nil)

func NewOllamaBackend(cfg ProviderConfig) (*OllamaBackend, error) {
	if cfg.Name == "" {
		return nil, errors.New("providers.NewOllamaBackend: cfg.Name is empty")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("providers.NewOllamaBackend(%q): cfg.Endpoint is empty", cfg.Name)
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("providers.NewOllamaBackend(%q): cfg.Model is empty", cfg.Name)
	}
	return &OllamaBackend{
		name:     cfg.Name,
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		model:    cfg.Model,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}, nil
}

// Name returns the stable registry key (== ProviderConfig.Name). MUST NOT
// change across releases — cost_ledger.provider holds this as text.
func (b *OllamaBackend) Name() string { return b.name }

func (b *OllamaBackend) Tier() Tier { return TierOllama }

func (b *OllamaBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       true,
		SupportsVision:        false,
		SupportsPromptCaching: false,
		MaxContextTokens:      32_768,
		MaxOutputTokens:       8_192,
	}
}

func (b *OllamaBackend) Close() error { return nil }

func (b *OllamaBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {

	parsedReq, parseErr := parseCanonicalRequest(req.Body)
	if parseErr != nil {
		return nil, fmt.Errorf("ollama: encode request: %w", parseErr)
	}
	if hasToolsField(parsedReq) {
		return nil, ErrToolsUnsupported
	}
	openAIBody, err := anthropicToOpenAIRequest(req.Body, b.model)
	if err != nil {
		return nil, fmt.Errorf("ollama: encode request: %w", err)
	}
	url := b.endpoint + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(openAIBody))
	if err != nil {
		return nil, fmt.Errorf("ollama: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	for k, v := range req.Headers {
		if k == "Content-Type" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: forward: %w", err)
	}
	defer resp.Body.Close()
	latencyMs := time.Since(start).Milliseconds()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		truncated := body
		if len(truncated) > 512 {
			truncated = append(truncated[:512:512], []byte("…[truncated]")...)
		}
		return nil, fmt.Errorf("ollama: status %d: %s", resp.StatusCode, string(truncated))
	}

	canonicalBody, usage, err := openAIToAnthropicResponse(body)
	if err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}

	modelUsed := b.model
	if modelUsed == "" {
		modelUsed = req.Model
	}
	return &TierResponse{
		Status:       resp.StatusCode,
		Body:         canonicalBody,
		TierUsed:     TierOllama,
		ModelUsed:    modelUsed,
		LatencyMs:    latencyMs,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		Headers:      responseHeaders(resp.Header),
	}, nil
}

func (b *OllamaBackend) Probe(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, b.endpoint+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("ollama probe: new request: %w", err)
	}
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama probe: %w: %w", ErrOllamaUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ollama probe: status %d: %w", resp.StatusCode, ErrOllamaUnavailable)
	}
	return nil
}
