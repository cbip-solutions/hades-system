// SPDX-License-Identifier: MIT
// Package ecosystem — reranker_bge.go
//
// BGE-reranker-v2-m3 cross-encoder reranker ( Task D-3 per
// spec §2.6 Q6=A + invariant).
//
// # Model
//
// BAAI/bge-reranker-v2-m3 is a 149M-param multilingual cross-encoder
// distributed by Beijing Academy of Artificial Intelligence (BAAI) under
// the MIT license; per the ZeroEntropy 2025 leaderboard it is the leading
// open cross-encoder. The reranker is the ONLY pipeline stage that sees
// (query, document) pairs together, so it resolves cross-ecosystem
// score-space drift implicitly (Bruch 2023; ZeroEntropy 2025).
//
// # Backends
//
// BGEBackendMock — deterministic Go-only scorer used in unit tests.
// Scores candidates by lexical overlap (Jaccard on
// whitespace-split, lowercased tokens) between query
// and content, mixed 90:10 with the pre-rerank
// SimilarityScore. Pure-Go, no external deps.
// BGEBackendMPS — ONNX Runtime on Apple Silicon (CoreML execution
// provider). Tokenizer: HuggingFace WordPiece
// (tokenizer.json co-located with the ONNX file).
// Backed by an injected onnxRunner instance; production
// wiring lives in reranker_bge_onnx.go (build tag `cgo`)
// — daemon supplies the runner from config.
// BGEBackendCPU — same as MPS but with CPUExecutionProvider. Used as
// fallback when MPS provider unavailable (e.g., x86).
//
// # Latency invariants
//
// invariant: p95 ≤300ms for 100 candidates on M4 MPS. Enforced via
// reranker_bge_bench_integration_test.go (build tag `integration`).
//
// # Goroutine safety
//
// One *BGEReRankerV2M3 holds one backend instance. ONNX Runtime sessions
// are NOT goroutine-safe; Rerank takes an internal sync.Mutex to serialize
// forward passes. Dispatcher.Query may have 4 fan-out goroutines querying
// ecosystem.db concurrently, but they all converge on a single reranker
// post-fusion (spec §4.2 step 8) — single-session serialization matches
// the data flow exactly.
//
// # Close semantics
//
// Close releases backend resources (ONNX session, file handles) and is
// idempotent. Caller MUST call Close before discarding (the dispatcher
// does this via Dispatcher.Close() at daemon shutdown).
//
// # Dependency injection seam
//
// The bgeBackend interface decouples scoring strategy from this file's
// orchestration. The mock backend lives here (pure-Go). The ONNX backend
// lives in a cgo-tagged file that imports the ONNX Runtime Go bindings.
// A nocgo build returns ErrCGORequired from newONNXBackendImpl, matching
// the chunker.go / chunker_nocgo.go pair pattern.
package ecosystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type BGEBackend string

const (
	BGEBackendMock BGEBackend = "mock"

	BGEBackendMPS BGEBackend = "mps"

	BGEBackendCPU BGEBackend = "cpu"
)

type BGEConfig struct {
	Backend BGEBackend

	ModelPath string

	TokenizerPath string

	MaxLatencyMs int

	MaxSeqLen int

	BatchSize int
}

type BGEReRankerV2M3 struct {
	cfg     BGEConfig
	backend bgeBackend
	closed  atomic.Bool
	mu      sync.Mutex
	rerankN atomic.Uint64
}

var _ Reranker = (*BGEReRankerV2M3)(nil)

const (
	bgeDefaultMaxLatencyMs = 300
	bgeDefaultMaxSeqLen    = 512
	bgeDefaultBatchSize    = 32
)

var bgeDefaultMaxLatency = time.Duration(bgeDefaultMaxLatencyMs) * time.Millisecond

func NewBGEReRankerV2M3(cfg BGEConfig) (*BGEReRankerV2M3, error) {
	if cfg.Backend == "" {
		return nil, errors.New("bge: BGEConfig.Backend required")
	}
	if cfg.MaxLatencyMs == 0 {
		cfg.MaxLatencyMs = bgeDefaultMaxLatencyMs
	}
	if cfg.MaxSeqLen == 0 {
		cfg.MaxSeqLen = bgeDefaultMaxSeqLen
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = bgeDefaultBatchSize
	}

	var be bgeBackend
	var err error
	switch cfg.Backend {
	case BGEBackendMock:
		be = &mockBGEBackend{}
	case BGEBackendMPS:
		be, err = newONNXBackendImpl(cfg, "mps")
	case BGEBackendCPU:
		be, err = newONNXBackendImpl(cfg, "cpu")
	default:
		return nil, fmt.Errorf("bge: unsupported Backend %q (want mock|mps|cpu)", cfg.Backend)
	}
	if err != nil {
		return nil, fmt.Errorf("bge: backend init: %w", err)
	}
	return &BGEReRankerV2M3{cfg: cfg, backend: be}, nil
}

func (r *BGEReRankerV2M3) Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	if r.closed.Load() {
		return nil, errors.New("bge: reranker is closed")
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

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scores, err := r.backend.Score(ctx, query, candidates)
	if err != nil {
		return nil, fmt.Errorf("bge: backend score: %w", err)
	}
	if len(scores) != len(candidates) {
		return nil, fmt.Errorf("bge: backend returned %d scores for %d candidates", len(scores), len(candidates))
	}
	r.rerankN.Add(1)

	out := make([]RankedResult, len(candidates))
	for i, c := range candidates {
		out[i] = RankedResult{Candidate: c, RerankerScore: scores[i]}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RerankerScore != out[j].RerankerScore {
			return out[i].RerankerScore > out[j].RerankerScore
		}
		if out[i].SimilarityScore != out[j].SimilarityScore {
			return out[i].SimilarityScore > out[j].SimilarityScore
		}
		return out[i].ChunkID < out[j].ChunkID
	})
	if topK < len(out) {
		out = out[:topK]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	return out, nil
}

func (r *BGEReRankerV2M3) Close() error {
	if !r.closed.CompareAndSwap(false, true) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.backend.Close()
}

func (r *BGEReRankerV2M3) CountReranks() uint64 {
	return r.rerankN.Load()
}

type bgeBackend interface {
	Score(ctx context.Context, query string, candidates []Candidate) ([]float64, error)
	Close() error
}

type mockBGEBackend struct{}

func (m *mockBGEBackend) Score(ctx context.Context, query string, cands []Candidate) ([]float64, error) {
	queryToks := bgeTokenize(query)
	if len(queryToks) == 0 {

		out := make([]float64, len(cands))
		for i, c := range cands {
			out[i] = c.SimilarityScore
		}
		return out, nil
	}
	qSet := tokenSet(queryToks)
	out := make([]float64, len(cands))
	for i, c := range cands {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		docToks := bgeTokenize(c.ContentText)
		out[i] = 0.9*jaccardSet(qSet, tokenSet(docToks)) + 0.1*c.SimilarityScore
	}
	return out, nil
}

func (m *mockBGEBackend) Close() error { return nil }

func bgeTokenize(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(strings.ToLower(s))
}

func tokenSet(toks []string) map[string]struct{} {
	out := make(map[string]struct{}, len(toks))
	for _, t := range toks {
		out[t] = struct{}{}
	}
	return out
}

func jaccardSet(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var inter int

	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	for k := range small {
		if _, ok := large[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func bgeModelPathFromEnv() (string, bool) {
	v := os.Getenv("ZEN_BGE_MODEL_PATH")
	return v, v != ""
}

func buildRealisticCandidates(n int) []Candidate {
	templates := []string{
		"synthetic content body with various tokens like goroutine context channel select case",
		"package-level documentation describing a function that accepts a context.Context for cancellation",
		"example usage demonstrating channel-based fan-out with sync.WaitGroup and context.Done propagation",
		"reference page covering the standard library context package and its cancellation semantics",
		"tutorial walking through a worker pool pattern using goroutines bounded by runtime.NumCPU",
	}
	out := make([]Candidate, n)
	for i := 0; i < n; i++ {
		out[i] = Candidate{
			ChunkID:         int64(i + 1),
			Ecosystem:       EcoGo,
			ContentText:     templates[i%len(templates)],
			SymbolPath:      "pkg.Symbol" + strconv.Itoa(i),
			SimilarityScore: 0.5 + 0.001*float64(i),
		}
	}
	return out
}

func resolveBGEModelPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("ZEN_BGE_MODEL_PATH"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "zen-swarm", "models", "bge-reranker-v2-m3.onnx")
}

func ResolveBGEModelPath(explicit string) string {
	return resolveBGEModelPath(explicit)
}
