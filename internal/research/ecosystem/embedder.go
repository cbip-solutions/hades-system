// SPDX-License-Identifier: MIT
package ecosystem

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Embedder produces both retrieval-stage embeddings for a text input.
//
// Concurrency implementations MUST be safe for concurrent EmbedBinary256d /
// EmbedFP32_1536d / EmbedBoth / EmbedBatch calls (Phase B ingester batches
// N goroutines per package; Phase D dispatcher embeds the query string
// + reranker candidates concurrently).
//
// Lifecycle Close releases native ONNX runtime + GPU memory (for jina-code
// impl) or HTTP client (for voyage-code-3 impl). Idempotent — multiple
// Close calls are safe.
type Embedder interface {
	EmbedBinary256d(ctx context.Context, text string) ([]byte, error)

	EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error)

	// EmbedBoth is a convenience returning both shapes in one call. Phase
	// B chunker MUST call this (not separate Binary + FP32 calls) — the
	// jina-code impl computes FP32 once + derives binary via single
	// quantization pass, avoiding 2× compute.
	EmbedBoth(ctx context.Context, text string) (bin []byte, fp32 []float32, err error)

	EmbedBatch(ctx context.Context, texts []string) (bin [][]byte, fp32 [][]float32, err error)

	Close() error
}

type EmbedderConfig struct {
	Model string

	Backend string

	BatchSize int

	APITokenKey string
}

// =============================================================================
// NoopEmbedder — test-helper implementation
//
// Returns zero-valued embeddings of the correct shape (32 bytes / 1536 floats).
// Production wiring of Dispatcher MUST NOT receive NoopEmbedder — Phase D
// Task D-9 asserts the Embedder is non-noop at NewDispatcher time. This
// type exists for tests that exercise non-embedder logic (router /
// abstention / citation / verifier) in isolation.
// =============================================================================

type NoopEmbedder struct{}

func (NoopEmbedder) EmbedBinary256d(ctx context.Context, text string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return make([]byte, 32), nil
}

func (NoopEmbedder) EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return make([]float32, 1536), nil
}

func (NoopEmbedder) EmbedBoth(ctx context.Context, text string) ([]byte, []float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return make([]byte, 32), make([]float32, 1536), nil
}

func (NoopEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]byte, [][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	bin := make([][]byte, len(texts))
	fp := make([][]float32, len(texts))
	for i := range texts {
		bin[i] = make([]byte, 32)
		fp[i] = make([]float32, 1536)
	}
	return bin, fp, nil
}

func (NoopEmbedder) Close() error { return nil }

// ErrEmbedderNoopInProduction is returned by Dispatcher's init path
// (Phase D Task D-9) if a NoopEmbedder is passed in Options.Embedder
// without an explicit test-mode override. Tests that exercise
// non-embedder logic MUST set Options.AllowNoopEmbedder = true (or use
// the test-only `newTestDispatcher` helper at Phase D).
var ErrEmbedderNoopInProduction = fmt.Errorf("research/ecosystem: NoopEmbedder used in production wiring (Phase D D-9 guard)")

type JinaCodeEmbeddings struct {
	opts   JinaCodeEmbeddingsOptions
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	closed bool
}

type JinaCodeEmbeddingsOptions struct {
	PythonPath string

	ScriptPath string

	BatchSize int
	// ShimMode runs the script with ZEN_JINA_SHIM=1 so unit tests can run
	// without sentence-transformers / torch installed. Production callers
	// MUST leave ShimMode=false (the daemon orchestrator passes false).
	ShimMode bool

	RequestHook func(texts []string, shape string)
}

func NewJinaCodeEmbeddings(opts JinaCodeEmbeddingsOptions) (*JinaCodeEmbeddings, error) {
	if opts.PythonPath == "" {
		opts.PythonPath = "python3"
	}
	if opts.ScriptPath == "" {
		return nil, errors.New("research/ecosystem: JinaCodeEmbeddings ScriptPath required")
	}
	if _, err := os.Stat(opts.ScriptPath); err != nil {
		return nil, fmt.Errorf("research/ecosystem: JinaCodeEmbeddings script %s: %w", opts.ScriptPath, err)
	}
	if opts.BatchSize < 0 {
		return nil, fmt.Errorf("research/ecosystem: JinaCodeEmbeddings BatchSize=%d must be >= 0", opts.BatchSize)
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = 64
	}
	cmd := exec.Command(opts.PythonPath, opts.ScriptPath)
	if opts.ShimMode {
		cmd.Env = append(os.Environ(), "ZEN_JINA_SHIM=1")
	}
	stdin, err := cmd.StdinPipe()

	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: JinaCodeEmbeddings stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()

	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("research/ecosystem: JinaCodeEmbeddings stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("research/ecosystem: JinaCodeEmbeddings start subprocess: %w", err)
	}
	scanner := bufio.NewScanner(stdout)

	scanner.Buffer(make([]byte, 1024*1024), 1024*1024*16)
	return &JinaCodeEmbeddings{
		opts:   opts,
		cmd:    cmd,
		stdin:  stdin,
		stdout: scanner,
	}, nil
}

type jinaResponse struct {
	BinsB64 []string    `json:"bins_b64"`
	FP32s   [][]float32 `json:"fp32s"`
	Error   string      `json:"error"`
}

func (e *JinaCodeEmbeddings) EmbedBinary256d(ctx context.Context, text string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := e.request(ctx, []string{text}, "bin")
	if err != nil {
		return nil, err
	}
	if len(resp.BinsB64) != 1 {
		return nil, fmt.Errorf("research/ecosystem: EmbedBinary256d: subprocess returned %d bins, want 1", len(resp.BinsB64))
	}
	bin, err := base64.StdEncoding.DecodeString(resp.BinsB64[0])
	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: EmbedBinary256d decode b64: %w", err)
	}
	if len(bin) != 32 {
		return nil, fmt.Errorf("research/ecosystem: EmbedBinary256d returned %d bytes, want 32", len(bin))
	}
	return bin, nil
}

func (e *JinaCodeEmbeddings) EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := e.request(ctx, []string{text}, "fp32")
	if err != nil {
		return nil, err
	}
	if len(resp.FP32s) != 1 {
		return nil, fmt.Errorf("research/ecosystem: EmbedFP32_1536d: subprocess returned %d vecs, want 1", len(resp.FP32s))
	}
	if len(resp.FP32s[0]) != 1536 {
		return nil, fmt.Errorf("research/ecosystem: EmbedFP32_1536d returned len=%d, want 1536", len(resp.FP32s[0]))
	}
	return resp.FP32s[0], nil
}

func (e *JinaCodeEmbeddings) EmbedBoth(ctx context.Context, text string) ([]byte, []float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	resp, err := e.request(ctx, []string{text}, "both")
	if err != nil {
		return nil, nil, err
	}
	if len(resp.BinsB64) != 1 || len(resp.FP32s) != 1 {
		return nil, nil, fmt.Errorf("research/ecosystem: EmbedBoth: subprocess returned bins=%d fp32s=%d, want 1 each",
			len(resp.BinsB64), len(resp.FP32s))
	}
	bin, err := base64.StdEncoding.DecodeString(resp.BinsB64[0])
	if err != nil {
		return nil, nil, fmt.Errorf("research/ecosystem: EmbedBoth decode b64: %w", err)
	}
	if len(bin) != 32 {
		return nil, nil, fmt.Errorf("research/ecosystem: EmbedBoth bin len=%d, want 32", len(bin))
	}
	fp32 := resp.FP32s[0]
	if len(fp32) != 1536 {
		return nil, nil, fmt.Errorf("research/ecosystem: EmbedBoth fp32 len=%d, want 1536", len(fp32))
	}
	return bin, fp32, nil
}

func (e *JinaCodeEmbeddings) EmbedBatch(ctx context.Context, texts []string) ([][]byte, [][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(texts) == 0 {
		return nil, nil, nil
	}
	bins := make([][]byte, 0, len(texts))
	fp32s := make([][]float32, 0, len(texts))
	batchSize := e.opts.BatchSize

	if batchSize <= 0 {
		batchSize = 64
	}
	for start := 0; start < len(texts); start += batchSize {

		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		resp, err := e.request(ctx, texts[start:end], "both")
		if err != nil {
			return nil, nil, fmt.Errorf("research/ecosystem: EmbedBatch chunk [%d:%d]: %w", start, end, err)
		}
		if len(resp.BinsB64) != end-start || len(resp.FP32s) != end-start {
			return nil, nil, fmt.Errorf("research/ecosystem: EmbedBatch [%d:%d] subprocess returned bins=%d fp32s=%d, want %d",
				start, end, len(resp.BinsB64), len(resp.FP32s), end-start)
		}
		for i := 0; i < end-start; i++ {
			bin, err := base64.StdEncoding.DecodeString(resp.BinsB64[i])
			if err != nil {
				return nil, nil, fmt.Errorf("research/ecosystem: EmbedBatch [%d] decode b64: %w", start+i, err)
			}
			if len(bin) != 32 {
				return nil, nil, fmt.Errorf("research/ecosystem: EmbedBatch [%d] bin len=%d, want 32", start+i, len(bin))
			}
			fp32 := resp.FP32s[i]
			if len(fp32) != 1536 {
				return nil, nil, fmt.Errorf("research/ecosystem: EmbedBatch [%d] fp32 len=%d, want 1536", start+i, len(fp32))
			}
			bins = append(bins, bin)
			fp32s = append(fp32s, fp32)
		}
	}
	return bins, fp32s, nil
}

func (e *JinaCodeEmbeddings) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if e.stdin != nil {
		_ = e.stdin.Close()
	}
	if e.cmd != nil && e.cmd.Process != nil {

		_ = e.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- e.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):

			_ = e.cmd.Process.Kill()
			<-done
		}
	}
	return nil
}

func (e *JinaCodeEmbeddings) request(ctx context.Context, texts []string, shape string) (*jinaResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, errors.New("research/ecosystem: JinaCodeEmbeddings closed")
	}
	if e.opts.RequestHook != nil {
		e.opts.RequestHook(texts, shape)
	}
	reqBytes, err := json.Marshal(map[string]any{
		"texts": texts,
		"shape": shape,
	})

	if err != nil {
		return nil, fmt.Errorf("research/ecosystem: marshal req: %w", err)
	}
	if _, err := e.stdin.Write(append(reqBytes, '\n')); err != nil {
		return nil, fmt.Errorf("research/ecosystem: write req: %w", err)
	}
	type readResult struct {
		resp jinaResponse
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		if !e.stdout.Scan() {
			scanErr := e.stdout.Err()
			if scanErr == nil {
				scanErr = errors.New("subprocess stdout EOF")
			}
			ch <- readResult{err: fmt.Errorf("research/ecosystem: read resp: %w", scanErr)}
			return
		}
		var resp jinaResponse
		if err := json.Unmarshal(e.stdout.Bytes(), &resp); err != nil {
			ch <- readResult{err: fmt.Errorf("research/ecosystem: unmarshal resp: %w", err)}
			return
		}
		ch <- readResult{resp: resp}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		if r.resp.Error != "" {
			return nil, fmt.Errorf("research/ecosystem: subprocess error: %s", r.resp.Error)
		}
		return &r.resp, nil
	}
}

// quantizeBinary256 packs 256 fp32 values into 32 bytes via sign-bit.
// Bit i (MSB-first within each byte) = 1 if fp32[i] >= 0 else 0.
// Wire format MUST match sqlite-vec's BIT[256] convention (verified
// against zen_jina_embed.py's quantize_binary_256 plus sqlite-vec docs).
//
// Panics on input length != 256 — callers MUST slice properly. This is a
// defense-in-depth guard: silent miscompression would corrupt the Stage 1
// retrieval index and surface as undetectable recall loss downstream.
func quantizeBinary256(fp32 []float32) []byte {
	if len(fp32) != 256 {
		panic(fmt.Sprintf("research/ecosystem: quantizeBinary256 requires 256 floats, got %d", len(fp32)))
	}
	out := make([]byte, 32)
	for i, v := range fp32 {
		if v >= 0 {
			out[i>>3] |= 1 << (7 - uint(i&7))
		}
	}
	return out
}

// =============================================================================
// VoyageCode3 — operator-opt-in API fallback embedder (Task C-2).
//
// Spec §2.4 Q4=A alternative path: voyage-code-3 hosted API (Anthropic-blessed
// per docs.voyageai.com). Routes ALL HTTP egress through Plan 3 dispatcher via
// the narrow Forwarder interface declared below (Plan 10 B-6 narrow-interface
// pattern). Tokens come from macOS Keychain at (service="voyage-api-token",
// account="zen-swarm") by default — never accept tokens via struct field or
// env var (defense-in-depth: prevents accidental token leakage through logs
// or process listings).
//
// Privacy doctrine (LOAD-BEARING):
//   - EnableFallback defaults to FALSE. Operator must opt in via
//     ~/.config/zen-swarm/providers/ecosystem-embedder.toml [fallback]
//     enable_fallback = true. Without opt-in, every Embed* method returns
//     ErrFallbackDisabled before any Keychain lookup or Forwarder call.
//   - Tokens are cached in-memory after first Keychain fetch to avoid
//     repeated unlock prompts on high-throughput batches.
//
// Boundary doctrine:
//   - inv-zen-191 forward-compat: this type does NOT import net/http
//     directly; HTTP egress flows through Forwarder which the daemon
//     orchestrator wires to a concrete *providers.Dispatcher at runtime.
//   - inv-zen-031: no internal/providers import here — the narrow Forwarder
//     interface keeps the boundary clean (mirrors the bypassadapter +
//     dispatcheradapter split in internal/daemon/).
//
// Cross-shape invariant (preserves recall parity with the primary jina
// path): EmbedBoth / EmbedBinary256d / EmbedBatch all produce the binary
// 256-d via the SAME quantizeBinary256 helper applied to fp32[:256] of a
// single output_dimension=1536 round-trip. We deliberately do NOT use
// Voyage's native binary path (output_dtype=binary @ output_dimension=256)
// because mixing native-binary vs derived-binary across embedders risks
// silent divergence at retrieval time (different sign convention, different
// byte order, etc.).
// =============================================================================

var ErrFallbackDisabled = errors.New("ecosystem: voyage-code-3 fallback disabled (operator opt-in required)")

var ErrKeychainTokenMissing = errors.New("ecosystem: voyage-api-token missing from macOS Keychain")

type Forwarder interface {
	// Forward sends the marshalled Voyage request body and returns the
	// raw response body. The Forwarder is responsible for: URL routing
	// (api.voyageai.com/v1/embeddings), bearer-token auth header
	// injection, HTTP transport, and per-Plan-3 single-egress audit
	// logging. Returns either:
	//   - (body, nil) on 2xx
	//   - (nil, *VoyageHTTPError) on a Voyage HTTP non-2xx response, so
	//     the caller can branch on StatusCode for retry semantics
	//   - (nil, transportErr) on a transport-level fault (timeout, refused)
	//
	// IMPORTANT implementers MUST surface HTTP non-2xx as *VoyageHTTPError
	// (errors.As-compatible) so the retry semantics in fetchBatch can
	// distinguish "429 retry" vs "401 don't retry" vs "5xx retry" vs
	// "transport blip retry". Returning a generic error for an HTTP 401
	// will silently trigger retry, which is incorrect behavior.
	Forward(ctx context.Context, body []byte) ([]byte, error)
}

type KeychainAccessor interface {
	GetGenericPassword(service, account string) (string, error)
}

type VoyageCode3 struct {
	opts  VoyageCode3Options
	mu    sync.Mutex
	token string
}

type VoyageCode3Options struct {
	Forwarder Forwarder

	Keychain KeychainAccessor

	EnableFallback bool

	BatchSize int

	MaxRetries int

	RetryBackoff time.Duration

	TokenKey string

	TokenAccount string
}

func NewVoyageCode3(opts VoyageCode3Options) (*VoyageCode3, error) {
	if opts.Forwarder == nil {
		return nil, errors.New("ecosystem: VoyageCode3 Forwarder required")
	}
	if opts.Keychain == nil {
		return nil, errors.New("ecosystem: VoyageCode3 Keychain required")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 32
	}

	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	} else if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = 1 * time.Second
	}
	if opts.TokenKey == "" {
		opts.TokenKey = "voyage-api-token"
	}
	if opts.TokenAccount == "" {
		opts.TokenAccount = "zen-swarm"
	}
	return &VoyageCode3{opts: opts}, nil
}

// voyageRequest mirrors the Voyage AI /v1/embeddings POST body.
// JSON tags MUST match the API spec exactly (no rename, no omitempty —
// the API expects literal field names).
type voyageRequest struct {
	Input           []string `json:"input"`
	Model           string   `json:"model"`
	OutputDimension int      `json:"output_dimension"`
	OutputDtype     string   `json:"output_dtype"`
	InputType       string   `json:"input_type"`
}

type voyageEmbedding struct {
	Object    string    `json:"object"`
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type voyageResponse struct {
	Object string            `json:"object"`
	Data   []voyageEmbedding `json:"data"`
	Model  string            `json:"model"`
	Usage  struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// VoyageHTTPError is the structured error type the Forwarder MUST return
// for any Voyage HTTP non-2xx response. fetchBatch uses errors.As to
// branch on StatusCode for retry semantics:
//   - 429 (rate-limit) + 5xx: retried with exponential backoff
//   - 401 (auth) + other 4xx: NOT retried (permanent until creds change)
//
// EXPORTED so the real Plan 3 dispatcher (in internal/providers, a
// separate package wired by the daemon orchestrator) can construct this
// type when an upstream HTTP error reaches it. The narrow Forwarder
// interface contract is: "non-2xx HTTP responses MUST be returned as
// *VoyageHTTPError so retry semantics work; transport-level errors
// (connection refused, timeout) MAY be returned as any other error type
// — they will be retried as transient." See narrow-interface pattern in
type VoyageHTTPError struct {
	StatusCode int
	Body       string
}

func (e *VoyageHTTPError) Error() string {
	return fmt.Sprintf("voyage api HTTP %d: %s", e.StatusCode, e.Body)
}

func (v *VoyageCode3) ensureToken() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.token != "" {
		return nil
	}
	t, err := v.opts.Keychain.GetGenericPassword(v.opts.TokenKey, v.opts.TokenAccount)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrKeychainTokenMissing, err)
	}
	if t == "" {
		return ErrKeychainTokenMissing
	}
	v.token = t
	return nil
}

func (v *VoyageCode3) EmbedBinary256d(ctx context.Context, text string) ([]byte, error) {
	if !v.opts.EnableFallback {
		return nil, ErrFallbackDisabled
	}
	if err := v.ensureToken(); err != nil {
		return nil, err
	}
	fp32, err := v.fetchOne(ctx, text, 1536, "float")
	if err != nil {
		return nil, err
	}
	return quantizeBinary256(fp32[:256]), nil
}

func (v *VoyageCode3) EmbedFP32_1536d(ctx context.Context, text string) ([]float32, error) {
	if !v.opts.EnableFallback {
		return nil, ErrFallbackDisabled
	}
	if err := v.ensureToken(); err != nil {
		return nil, err
	}
	return v.fetchOne(ctx, text, 1536, "float")
}

func (v *VoyageCode3) EmbedBoth(ctx context.Context, text string) (bin []byte, fp32 []float32, err error) {
	if !v.opts.EnableFallback {
		return nil, nil, ErrFallbackDisabled
	}
	if err := v.ensureToken(); err != nil {
		return nil, nil, err
	}
	fp32, err = v.fetchOne(ctx, text, 1536, "float")
	if err != nil {
		return nil, nil, err
	}
	return quantizeBinary256(fp32[:256]), fp32, nil
}

func (v *VoyageCode3) EmbedBatch(ctx context.Context, texts []string) (bins [][]byte, fp32s [][]float32, err error) {
	if !v.opts.EnableFallback {
		return nil, nil, ErrFallbackDisabled
	}
	if err := v.ensureToken(); err != nil {
		return nil, nil, err
	}
	if len(texts) == 0 {
		return nil, nil, nil
	}
	bins = make([][]byte, 0, len(texts))
	fp32s = make([][]float32, 0, len(texts))
	bs := v.opts.BatchSize
	for start := 0; start < len(texts); start += bs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		end := start + bs
		if end > len(texts) {
			end = len(texts)
		}
		batch, ferr := v.fetchBatch(ctx, texts[start:end], 1536, "float")
		if ferr != nil {
			return nil, nil, fmt.Errorf("ecosystem: VoyageCode3.EmbedBatch [%d:%d]: %w", start, end, ferr)
		}
		for _, fp := range batch {
			fp32s = append(fp32s, fp)
			bins = append(bins, quantizeBinary256(fp[:256]))
		}
	}
	return bins, fp32s, nil
}

func (v *VoyageCode3) Close() error { return nil }

func (v *VoyageCode3) fetchOne(ctx context.Context, text string, dim int, dtype string) ([]float32, error) {
	batch, err := v.fetchBatch(ctx, []string{text}, dim, dtype)
	if err != nil {
		return nil, err
	}
	if len(batch) != 1 {
		return nil, fmt.Errorf("ecosystem: VoyageCode3 returned %d vecs, want 1", len(batch))
	}
	return batch[0], nil
}

func (v *VoyageCode3) fetchBatch(ctx context.Context, texts []string, dim int, dtype string) ([][]float32, error) {
	body, err := json.Marshal(voyageRequest{
		Input:           texts,
		Model:           "voyage-code-3",
		OutputDimension: dim,
		OutputDtype:     dtype,
		InputType:       "document",
	})
	if err != nil {

		return nil, fmt.Errorf("marshal voyage req: %w", err)
	}

	var lastErr error
	backoff := v.opts.RetryBackoff
	for attempt := 0; attempt <= v.opts.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		respBody, ferr := v.opts.Forwarder.Forward(ctx, body)
		if ferr == nil {
			var resp voyageResponse
			if uerr := json.Unmarshal(respBody, &resp); uerr != nil {
				return nil, fmt.Errorf("unmarshal voyage resp: %w", uerr)
			}
			out := make([][]float32, len(resp.Data))
			for _, e := range resp.Data {
				if e.Index < 0 || e.Index >= len(out) {
					return nil, fmt.Errorf("voyage embedding index %d out of range [0,%d)", e.Index, len(out))
				}
				if len(e.Embedding) != dim {
					return nil, fmt.Errorf("voyage embedding[%d] len=%d, want %d", e.Index, len(e.Embedding), dim)
				}
				out[e.Index] = e.Embedding
			}
			return out, nil
		}
		lastErr = ferr
		var httpErr *VoyageHTTPError
		if errors.As(ferr, &httpErr) {
			// Retry on 429 + 5xx; do not retry on 4xx other than 429.
			if httpErr.StatusCode != 429 && httpErr.StatusCode < 500 {
				return nil, ferr
			}
		}

		if attempt == v.opts.MaxRetries {

			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
	return nil, fmt.Errorf("ecosystem: VoyageCode3 exhausted retries: %w", lastErr)
}
