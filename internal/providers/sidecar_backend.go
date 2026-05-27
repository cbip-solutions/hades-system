// SPDX-License-Identifier: MIT
// internal/providers/sidecar_backend.go
//
// SidecarBackend is the release Tier 1 HTTP TierBackend that talks
// to the private hades-bypass-tier1 sidecar process running on loopback.
//
// Substrate split: the in-process bypass.Client
// (BypassBackend, private-tier1-module) is being relocated to a
// separate private binary (hades-bypass-tier1) speaking the HTTP contract
// defined in cmd/hades-bypass — the public daemon (this dev repo)
// communicates with the private sidecar over loopback so:
// - the Anthropic-Max-subscription OAuth bypass code never ships in the
// public Apache-2.0 distribution;
// - the dispatcher (this package's consumer) keeps a uniform
// name-based cascade (inv-hades-066 frozen contract C8): "bypass-sidecar"
// is the first entry in the operator's profiles.toml cascade, and a
// missing/unhealthy sidecar surfaces as ErrSidecarUnavailable →
// cascade proceeds to the release direct backends (Anthropic paygo,
// Gemini, OpenRouter, etc. per ADR-0093 + reference_provider_roster).
//
// HTTP contract:
// - POST /v1/messages: forwards the Anthropic Messages body verbatim;
// returns the upstream response body verbatim. The sidecar owns
// auth, cert pinning, idempotency, concurrency, OAuth refresh — this
// backend is a thin HTTP client only.
// - GET /health: content-free probe (inv-hades-071); 200 == healthy.
// - GET /v1/sidecar/info: capability vector (versions + feature flags);
// not consumed by Forward — orchestrator surfaces these via `hades status`.
//
// Graceful degradation (inv-hades-280):
// - Connection-refused / DNS-fail / timeout / ctx-cancel → ErrSidecarUnavailable.
// - 5xx HTTP status → ErrSidecarDegraded.
// - 4xx HTTP status → a real upstream contract violation; NOT a fallback
// sentinel (returned wrapped; dispatcher's attempt() records failure +
// emits CostEvent like any other error). 4xx-as-Unavailable would mask
// genuine request-shape bugs.
// - 2xx → success; usage tokens parsed from the canonical Anthropic
// response envelope (zero on parse-failure — same convention as
// BypassBackend, AnthropicPaygoBackend, et al).
//
// Concurrency NewSidecarBackend constructs an http.Client with a per-request
// Timeout; the client is goroutine-safe so the SidecarBackend itself is also
// safe for concurrent Forward / Probe invocation.
//
// Boundary discipline (inv-hades-031): this file lives in internal/providers/
// (the canonical home for every concrete TierBackend impl); it imports only
// stdlib. NO import of internal/store, private-tier1-module, or
// internal/config — registration wiring (RegisterSidecars) lives in
// internal/daemon/dispatcheradapter/ and reads the config there.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrSidecarUnavailable = errors.New("sidecar unavailable")

var ErrSidecarDegraded = errors.New("sidecar degraded")

type SidecarBackend struct {
	baseURL string
	client  *http.Client
}

var _ TierBackend = (*SidecarBackend)(nil)

func NewSidecarBackend(baseURL string, timeout time.Duration) *SidecarBackend {
	return &SidecarBackend{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: timeout},
	}
}

// Name returns the stable registry key. MUST NOT change across releases
// (cost_ledger.provider holds this as text; operator profiles.toml
// cascades reference it).
func (b *SidecarBackend) Name() string { return "bypass-sidecar" }

func (b *SidecarBackend) Tier() Tier { return TierInHouse }

func (b *SidecarBackend) Capabilities() TierCapabilities {
	return TierCapabilities{
		SupportsStreaming:     false,
		SupportsToolUse:       true,
		SupportsVision:        true,
		SupportsPromptCaching: true,
		MaxContextTokens:      200_000,
		MaxOutputTokens:       64_000,
	}
}

func (b *SidecarBackend) Forward(ctx context.Context, req TierRequest) (*TierResponse, error) {
	endpoint, err := url.JoinPath(b.baseURL, "/v1/messages")
	if err != nil {
		return nil, fmt.Errorf("%w: url join: %w", ErrSidecarUnavailable, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.Body))
	if err != nil {

		return nil, fmt.Errorf("%w: new request: %w", ErrSidecarUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}

	start := time.Now()
	resp, err := b.client.Do(httpReq)
	if err != nil {

		return nil, fmt.Errorf("%w: forward: %w", ErrSidecarUnavailable, err)
	}
	defer resp.Body.Close()
	latencyMs := time.Since(start).Milliseconds()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {

		return nil, fmt.Errorf("%w: read body: %w", ErrSidecarDegraded, readErr)
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: status %d body %s", ErrSidecarDegraded, resp.StatusCode, capSidecarBody(body))
	}
	if resp.StatusCode >= 400 {

		return nil, fmt.Errorf("sidecar 4xx: status %d body %s", resp.StatusCode, capSidecarBody(body))
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
		TierUsed:            TierInHouse,
		ModelUsed:           modelUsed,
		LatencyMs:           latencyMs,
		InputTokens:         parsed.Usage.InputTokens,
		OutputTokens:        parsed.Usage.OutputTokens,
		CacheReadTokens:     parsed.Usage.CacheReadTokens,
		CacheCreationTokens: parsed.Usage.CacheCreationTokens,
		Headers:             responseHeaders(resp.Header),
	}, nil
}

func (b *SidecarBackend) Probe(ctx context.Context) error {
	endpoint, err := url.JoinPath(b.baseURL, "/health")
	if err != nil {
		return fmt.Errorf("%w: probe url join: %w", ErrSidecarUnavailable, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%w: probe new request: %w", ErrSidecarUnavailable, err)
	}
	resp, err := b.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: probe: %w", ErrSidecarUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: probe status %d", ErrSidecarUnavailable, resp.StatusCode)
	}
	return nil
}

func (b *SidecarBackend) Close() error { return nil }

// capSidecarBody is the 512-byte-cap helper for sidecar error messages.
// Mirrors providers/translate.go::capBody but kept local so a future
// refactor of capBody does not silently change SidecarBackend's leak
// posture (the cap is a security property, not a formatting choice).
func capSidecarBody(body []byte) string {
	const limit = 512
	if len(body) > limit {
		return string(append(body[:limit:limit], []byte("…[truncated]")...))
	}
	return string(body)
}
