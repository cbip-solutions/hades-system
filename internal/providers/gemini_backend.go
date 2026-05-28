// SPDX-License-Identifier: MIT
// internal/providers/gemini_backend.go
//
// GeminiBackend is the TierBackend for Google AI Studio's
// generateContent API (TierGemini). It is the operator's S'-fallback,
// A1c, B, and D backend (spec §3.6).
//
// The Gemini API differs from the OpenAI-compat shape in three ways this
// backend handles: (1) the model is part of the URL path
// (/v1beta/models/<model>:generateContent); (2) the API key is a ?key=
// query parameter, not a header; (3) the request/response bodies are the
// Gemini contents/candidates shape — translate.go bridges them to/from
// the canonical Anthropic shape.
//
// invariant: imports internal/keychain (boundary-neutral), not
// internal/store / tier1-sidecar.
// invariant compile guard below the struct. invariant: API key is a
// redact.Secret revealed only at the instant of URL construction.
package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cbip-solutions/hades-system/internal/keychain"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

const geminiAPIPath = "/v1beta/models/"

var ErrGeminiUnavailable = errors.New("gemini unavailable")

type GeminiBackend struct {
	name       string
	endpoint   string
	model      string
	apiKey     redact.Secret
	headers    map[string]string
	httpClient *http.Client
}

var _ TierBackend = (*GeminiBackend)(nil)

func NewGeminiBackend(cfg ProviderConfig, kc keychain.Resolver) (*GeminiBackend, error) {
	if kc == nil {
		return nil, fmt.Errorf("providers.NewGeminiBackend(%q): keychain resolver is nil", cfg.Name)
	}
	key, err := kc.Lookup(cfg.APIKeyKeychain, keychainAccount)
	if err != nil {
		return nil, fmt.Errorf("providers.NewGeminiBackend(%q): resolve api key: %w", cfg.Name, err)
	}
	return &GeminiBackend{
		name:       cfg.Name,
		endpoint:   cfg.Endpoint,
		model:      cfg.Model,
		apiKey:     key,
		headers:    cfg.Headers,
		httpClient: &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func (b *GeminiBackend) Name() string { return b.name }

func (b *GeminiBackend) Tier() Tier { return TierGemini }

func (b *GeminiBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       false,
		SupportsVision:        false,
		SupportsPromptCaching: false,
		MaxContextTokens:      1_000_000,
		MaxOutputTokens:       8_192,
	}
}

func (b *GeminiBackend) generateContentURL() string {
	q := url.Values{}
	q.Set("key", string(b.apiKey.Reveal()))
	return b.endpoint + geminiAPIPath + b.model + ":generateContent?" + q.Encode()
}

func (b *GeminiBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {

	parsedReq, parseErr := parseCanonicalRequest(req.Body)
	if parseErr != nil {
		return nil, fmt.Errorf("gemini(%s): translate request: %w", b.name, parseErr)
	}
	if hasToolsField(parsedReq) {
		return nil, ErrToolsUnsupported
	}
	geminiBody, err := anthropicToGeminiRequest(req.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini(%s): translate request: %w", b.name, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.generateContentURL(), bytes.NewReader(geminiBody))
	if err != nil {
		return nil, fmt.Errorf("gemini(%s): new request: %w", b.name, err)
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

	start := time.Now()
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini(%s): forward: %w", b.name, err)
	}
	defer resp.Body.Close()
	latencyMs := time.Since(start).Milliseconds()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini(%s): read body: %w", b.name, err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini(%s): status %d: %s", b.name, resp.StatusCode, capBody(rawBody))
	}

	canonBody, usage, err := geminiToAnthropicResponse(rawBody, b.model)
	if err != nil {
		return nil, fmt.Errorf("gemini(%s): translate response: %w", b.name, err)
	}
	return &TierResponse{
		Status:       resp.StatusCode,
		Body:         canonBody,
		TierUsed:     TierGemini,
		ModelUsed:    b.model,
		LatencyMs:    latencyMs,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		Headers:      responseHeaders(resp.Header),
	}, nil
}

func (b *GeminiBackend) Probe(ctx context.Context) error {
	probeBody := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":1}}`)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.generateContentURL(), bytes.NewReader(probeBody))
	if err != nil {
		return fmt.Errorf("gemini(%s) probe: new request: %w", b.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	for k, v := range b.headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("gemini(%s) probe: %w: %w", b.name, ErrGeminiUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("gemini(%s) probe: status %d: %w", b.name, resp.StatusCode, ErrGeminiUnavailable)
	}
	return nil
}

func (b *GeminiBackend) Close() error {
	b.apiKey.Wipe()
	return nil
}
