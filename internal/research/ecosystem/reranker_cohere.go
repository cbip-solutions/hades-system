// SPDX-License-Identifier: MIT
// Package ecosystem — reranker_cohere.go
//
// Cohere Rerank v4 API fallback (release Task D-4 per spec §2.6 Q6=A
// alternative C). Operator opt-in only.
//
// # Why opt-in
//
// Closed/paid API; per-query egress; violates single-egress doctrine when
// active. Default config `enable_fallback = false` (privacy posture mirrors
// VoyageCode3). The operator who explicitly opts in (via
// ~/.config/zen-swarm/providers/ecosystem-reranker.toml [fallback]) trades
// privacy for top-of-leaderboard reranker quality (ELO 1629 per ZeroEntropy
// 2025). The dispatcher prefers BGE local but cascades to Cohere when BGE
// returns no result or exceeds the per-query budget.
//
// # Egress path
//
// This file does NOT import net/http directly. HTTP egress routes through
// the narrow CohereForwarder interface, which the daemon orchestrator wires
// to a concrete *providers.Dispatcher at surface (release B-6
// narrow-interface pattern). The dispatcher owns:
//
// - URL routing → https://api.cohere.ai/v2/rerank
// - Bearer-token Authorization header injection (from cached token)
// - HTTP transport
// - Mapping HTTP non-2xx to *CohereHTTPError for the caller to branch on
//
// Tests substitute the CohereForwarder interface with fakeCohereForwarder
// (see reranker_cohere_test.go) to exercise every error/HTTP-status branch
// without any net/http import. This matches the embedder.go/VoyageCode3
// pattern (Forwarder + KeychainAccessor narrow interfaces).
//
// # Token storage
//
// Tokens stored in macOS Keychain via:
//
// security add-generic-password -a zen-swarm -s cohere-api-token -w '<tok>'
//
// CohereRerankV4Options.TokenKey ("cohere-api-token" by default) +
// TokenAccount ("zen-swarm" by default) name the Keychain entry. The
// KeychainAccessor narrow interface (defined in embedder.go) is reused —
// production wires the real macOS Keychain; tests inject fakeKeychain.
//
// invariant + privacy doctrine: ensureToken runs ONLY after the
// EnableFallback gate; ErrFallbackDisabled (defined in embedder.go and
// shared across the package) short-circuits at construction. Empty token
// → ErrKeychainTokenMissing (defense-in-depth: never invoke Forwarder
// with an empty bearer string).
//
// # Concurrency
//
// Rerank is safe for concurrent invocation. token-cache mutex prevents
// duplicate Keychain consultation; subsequent calls hit the fast path.
// The Forwarder MUST itself be goroutine-safe (release dispatcher guarantees
// this; daemon orchestrator wires it once at startup).
//
// # Latency budget
//
// MaxLatencyMs is the dispatcher-facing p95 budget surfaced for
// observability. Rerank does NOT enforce a per-call timeout itself — the
// dispatcher applies a context.WithTimeout when calling Rerank, so a
// budget breach manifests as ctx.DeadlineExceeded (which Rerank surfaces
// raw). Mirrors the BGE reranker contract.
//
// # Close semantics
//
// Close marks the reranker as closed via atomic CAS; subsequent Rerank
// calls return an error. Idempotent. No transport resources to release
// (the Forwarder owns those; daemon shutdown calls dispatcher.Close).
package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

var (
	ErrCohereRateLimit = errors.New("ecosystem: cohere rerank rate-limited (HTTP 429)")

	ErrCohereAuth = errors.New("ecosystem: cohere rerank auth failure (HTTP 401/403)")

	ErrCohereResponse = errors.New("ecosystem: cohere rerank malformed response")
)

// CohereHTTPError is the structured error type the Forwarder MUST return
// for HTTP non-2xx responses. Mirrors VoyageHTTPError. Callers use
// errors.As(err, &httpErr) to inspect StatusCode for retry semantics; the
// Rerank method translates 429 → ErrCohereRateLimit and 401/403 →
// ErrCohereAuth before returning, but still wraps the original
// *CohereHTTPError so deeper introspection (e.g., Retry-After) remains
// possible.
type CohereHTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *CohereHTTPError) Error() string {
	body := e.Body
	if len(body) > 256 {
		body = body[:256]
	}
	return fmt.Sprintf("cohere http %d: %s", e.StatusCode, string(body))
}

// CohereForwarder is the release-narrow interface dispatcher.
// The daemon orchestrator wires a concrete *providers.Dispatcher at runtime
// (which owns URL routing → api.cohere.ai/v2/rerank, bearer-token auth
// header injection, HTTP transport, and per-release audit logging); tests
// wire fakeCohereForwarder. This keeps the ecosystem package free of any
// net/http import and internal/providers import.
//
// Contract
// - On HTTP 2xx: returns (body, nil) — the raw response body for the
// caller to json.Unmarshal into cohereResponse.
// - On HTTP non-2xx: returns (nil, *CohereHTTPError) — the caller branches
// on StatusCode via errors.As. Implementers MUST surface non-2xx as
// *CohereHTTPError (errors.As-compatible); returning a generic error
// for a 401 would silently elevate to "transport blip" and could
// trigger inappropriate retry.
// - On transport-level fault (timeout, refused): returns (nil, err) where
// err is NOT a *CohereHTTPError. Caller surfaces raw.
// - On ctx.Done: returns (nil, ctx.Err()). Caller surfaces raw.
type CohereForwarder interface {
	Forward(ctx context.Context, body []byte) ([]byte, error)
}

const (
	cohereDefaultModel = "rerank-v4.0"

	cohereDefaultMaxLatencyMs = 300

	cohereDefaultTokenKey = "cohere-api-token"

	cohereDefaultTokenAccount = "zen-swarm"
)

type CohereRerankV4Options struct {
	Forwarder CohereForwarder

	Keychain KeychainAccessor

	EnableFallback bool

	Model string

	MaxLatencyMs int

	TokenKey string

	TokenAccount string
}

type CohereRerankV4 struct {
	opts    CohereRerankV4Options
	mu      sync.Mutex
	token   string
	closed  atomic.Bool
	rerankN atomic.Uint64
}

var _ Reranker = (*CohereRerankV4)(nil)

func NewCohereRerankV4(opts CohereRerankV4Options) (*CohereRerankV4, error) {
	if !opts.EnableFallback {

		return nil, ErrFallbackDisabled
	}
	if opts.Forwarder == nil {
		return nil, errors.New("ecosystem: CohereRerankV4 Forwarder required")
	}
	if opts.Keychain == nil {
		return nil, errors.New("ecosystem: CohereRerankV4 Keychain required")
	}
	if opts.Model == "" {
		opts.Model = cohereDefaultModel
	}
	if opts.MaxLatencyMs <= 0 {
		opts.MaxLatencyMs = cohereDefaultMaxLatencyMs
	}
	if opts.TokenKey == "" {
		opts.TokenKey = cohereDefaultTokenKey
	}
	if opts.TokenAccount == "" {
		opts.TokenAccount = cohereDefaultTokenAccount
	}
	return &CohereRerankV4{opts: opts}, nil
}

type cohereRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents"`
}

type cohereResponse struct {
	Results []cohereResult `json:"results"`
}

type cohereResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func (r *CohereRerankV4) ensureToken() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.token != "" {
		return nil
	}
	t, err := r.opts.Keychain.GetGenericPassword(r.opts.TokenKey, r.opts.TokenAccount)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeychainTokenMissing, err)
	}
	if t == "" {
		return ErrKeychainTokenMissing
	}
	r.token = t
	return nil
}

func (r *CohereRerankV4) Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	if r.closed.Load() {
		return nil, errors.New("cohere: reranker is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	if topK <= 0 || topK > len(candidates) {
		topK = len(candidates)
	}

	if err := r.ensureToken(); err != nil {
		return nil, err
	}

	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = c.ContentText
	}
	reqBody := cohereRequest{
		Model:           r.opts.Model,
		Query:           query,
		Documents:       docs,
		TopN:            topK,
		ReturnDocuments: false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {

		return nil, fmt.Errorf("cohere: marshal request: %w", err)
	}

	respBody, ferr := r.opts.Forwarder.Forward(ctx, body)
	if ferr != nil {

		var httpErr *CohereHTTPError
		if errors.As(ferr, &httpErr) {
			switch httpErr.StatusCode {
			case 401, 403:
				return nil, fmt.Errorf("%w: %w", ErrCohereAuth, ferr)
			case 429:
				return nil, fmt.Errorf("%w: %w", ErrCohereRateLimit, ferr)
			default:

				return nil, fmt.Errorf("cohere: forwarder: %w", ferr)
			}
		}

		return nil, ferr
	}

	var apiResp cohereResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCohereResponse, err)
	}

	out := make([]RankedResult, 0, len(apiResp.Results))
	for _, res := range apiResp.Results {
		if res.Index < 0 || res.Index >= len(candidates) {
			return nil, fmt.Errorf("%w: result index %d out of [0,%d)",
				ErrCohereResponse, res.Index, len(candidates))
		}
		out = append(out, RankedResult{
			Candidate:     candidates[res.Index],
			RerankerScore: res.RelevanceScore,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RerankerScore != out[j].RerankerScore {
			return out[i].RerankerScore > out[j].RerankerScore
		}
		return out[i].ChunkID < out[j].ChunkID
	})

	if topK < len(out) {
		out = out[:topK]
	}
	for i := range out {
		out[i].Rank = i + 1
	}

	// Counter advances only on full success (matches BGE semantics; error
	// paths do not pollute observability).
	r.rerankN.Add(1)
	return out, nil
}

func (r *CohereRerankV4) Close() error {
	r.closed.CompareAndSwap(false, true)
	return nil
}

func (r *CohereRerankV4) CountReranks() uint64 {
	return r.rerankN.Load()
}
