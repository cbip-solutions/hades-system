// SPDX-License-Identifier: MIT
// Package ecosystem — router.go
//
// Local query classifier + heuristic token-presence pre-pass for cross-ecosystem
// fan-out routing per spec §2.6 Q6=A. Output: RoutingDecision determining
// which of EcoGo/EcoPython/EcoTypeScript/EcoRust databases to query.
//
// # Why no LLM in router
//
// Per Q6=A: a Haiku-class router would add 200-400ms p95 RTT, breaking the
// P50 ≤350ms / P95 ≤700ms budget for the ENTIRE query path. RAGRoute
// (EuroMLSys 2025; arXiv 2502.19280) demonstrates that local classifiers reach
// 85-90% routing accuracy at sub-ms inference cost. We achieve this by:
//  1. Heuristic token-presence pass (~hundreds of ns) handles the high-
//     precision majority case (canonical ecosystem-specific tokens).
//  2. Local logistic classifier (~1-2ms) over query embedding for ambiguous
//     cases.
//  3. Margin-based mode selection (single / top-2 / broadcast) provides
//     graceful degradation: when confident, target one ecosystem; when
//     uncertain, broadcast across all four with RRF weighted by confidence.
//
// inv-zen-200: deterministic ordering — given identical (query, embedding,
// classifier checkpoint, RouterConfig), Classify returns byte-identical
// RoutingDecision. This is load-bearing for cache hits + reproducible audit.
// The router itself enforces ordering determinism via stable-sort +
// alphabetical tie-break in rankSoftmax; full cross-eco fan-out determinism
// (the merged dispatcher.go path) lands in D-9 + property-tests in Phase H.
package ecosystem

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type RoutingMethod string

const (
	RoutingMethodSingle RoutingMethod = "single"

	RoutingMethodTop2 RoutingMethod = "top-2"

	RoutingMethodBroadcast RoutingMethod = "broadcast"
)

type RoutingDecision struct {
	Ecosystems          []Ecosystem
	ConfidenceWeights   map[Ecosystem]float64
	Method              RoutingMethod
	ClassifierLatencyMs float64
	HeuristicLatencyMs  float64
	HeuristicMatched    string
}

type HeuristicRule struct {
	Substring string
	Implies   Ecosystem

	weight float64
}

// QueryClassifier is the abstraction over the logistic regression head.
//
// Production impl: trained logistic model loaded from
// ~/.local/share/zen-swarm/router/classifier.bin (see D-2).
// Test impl: fixed-score or uniform classifier injected via newTest*.
//
// Implementations MUST be safe for concurrent ScoreSoftmax calls (Router is
// goroutine-safe per inv-zen-200; classifier is a hot-path member).
type QueryClassifier interface {
	// ScoreSoftmax returns a softmax distribution over the 4 ecosystems given
	// a 1536-d FP32 query embedding (from jina-code-embeddings-1.5B per Q4=A).
	// Sum of returned values is 1.0 ± 1e-3. Implementations MUST return one
	// entry per canonical Ecosystem in AllEcosystems; missing or extra entries
	// are rejected by the Router as invariant violations.
	ScoreSoftmax(ctx context.Context, embedding []float32) (map[Ecosystem]float64, error)

	CheckpointHash() string
}

type RouterConfig struct {
	Heuristics      []HeuristicRule
	Classifier      QueryClassifier
	MarginBroadcast float64
	MarginTop2      float64
}

type Router struct {
	cfg              RouterConfig
	classificationsN atomic.Uint64
}

func NewRouter(cfg RouterConfig) (*Router, error) {
	if len(AllEcosystems) < 2 {
		return nil, fmt.Errorf("router: package requires len(AllEcosystems) ≥2, have %d", len(AllEcosystems))
	}
	if len(cfg.Heuristics) == 0 {
		return nil, errors.New("router: empty Heuristics; use defaultHeuristics()")
	}
	if cfg.Classifier == nil {
		return nil, errors.New("router: nil Classifier")
	}
	if cfg.MarginBroadcast <= 0 || cfg.MarginTop2 <= cfg.MarginBroadcast || cfg.MarginTop2 >= 1.0 {
		return nil, fmt.Errorf("router: invalid margins (broadcast=%g, top2=%g); require 0 < broadcast < top2 < 1.0", cfg.MarginBroadcast, cfg.MarginTop2)
	}
	return &Router{cfg: cfg}, nil
}

func (r *Router) Classify(ctx context.Context, query string, embedding []float32) (RoutingDecision, error) {
	if err := ctx.Err(); err != nil {
		return RoutingDecision{}, err
	}
	r.classificationsN.Add(1)
	start := time.Now()
	if strings.TrimSpace(query) == "" {

		return r.broadcastUniform(start), nil
	}

	heuristicHits, matched := r.applyHeuristics(query)
	hLatency := durationMs(time.Since(start))
	if len(heuristicHits) == 1 && matched != "" {
		eco := singleFromSet(heuristicHits)
		return RoutingDecision{
			Ecosystems:         []Ecosystem{eco},
			ConfidenceWeights:  map[Ecosystem]float64{eco: 0.95},
			Method:             RoutingMethodSingle,
			HeuristicMatched:   matched,
			HeuristicLatencyMs: hLatency,
		}, nil
	}

	cStart := time.Now()
	softmax, err := r.cfg.Classifier.ScoreSoftmax(ctx, embedding)
	cLatency := durationMs(time.Since(cStart))
	if err != nil {

		return RoutingDecision{
			HeuristicLatencyMs:  hLatency,
			ClassifierLatencyMs: cLatency,
		}, fmt.Errorf("router classifier: %w", err)
	}
	if err := validateSoftmax(softmax); err != nil {
		return RoutingDecision{
			HeuristicLatencyMs:  hLatency,
			ClassifierLatencyMs: cLatency,
		}, fmt.Errorf("router classifier invariant: %w", err)
	}

	ranked := rankSoftmax(softmax)
	top1, top2 := ranked[0], ranked[1]
	margin := top1.score - top2.score

	switch {
	case margin >= r.cfg.MarginTop2:
		return RoutingDecision{
			Ecosystems:          []Ecosystem{top1.eco},
			ConfidenceWeights:   map[Ecosystem]float64{top1.eco: 1.0},
			Method:              RoutingMethodSingle,
			HeuristicMatched:    matched,
			HeuristicLatencyMs:  hLatency,
			ClassifierLatencyMs: cLatency,
		}, nil
	case margin >= r.cfg.MarginBroadcast:
		renormed := renormalizeTop2(top1, top2)
		return RoutingDecision{
			Ecosystems:          []Ecosystem{top1.eco, top2.eco},
			ConfidenceWeights:   renormed,
			Method:              RoutingMethodTop2,
			HeuristicMatched:    matched,
			HeuristicLatencyMs:  hLatency,
			ClassifierLatencyMs: cLatency,
		}, nil
	default:
		return RoutingDecision{
			Ecosystems:          orderedEcosystems(softmax),
			ConfidenceWeights:   softmax,
			Method:              RoutingMethodBroadcast,
			HeuristicMatched:    matched,
			HeuristicLatencyMs:  hLatency,
			ClassifierLatencyMs: cLatency,
		}, nil
	}
}

func (r *Router) CountClassifications() uint64 {
	return r.classificationsN.Load()
}

func (r *Router) ClassifierCheckpointHash() string {
	return r.cfg.Classifier.CheckpointHash()
}

func (r *Router) broadcastUniform(start time.Time) RoutingDecision {
	weights := map[Ecosystem]float64{}
	for _, e := range AllEcosystems {
		weights[e] = 1.0 / float64(len(AllEcosystems))
	}
	return RoutingDecision{
		Ecosystems:         append([]Ecosystem(nil), AllEcosystems...),
		ConfidenceWeights:  weights,
		Method:             RoutingMethodBroadcast,
		HeuristicLatencyMs: durationMs(time.Since(start)),
	}
}

func durationMs(d time.Duration) float64 {
	return float64(d.Nanoseconds()) / 1e6
}

func (r *Router) applyHeuristics(query string) (map[Ecosystem]bool, string) {
	lowered := strings.ToLower(query)
	matches := map[Ecosystem]bool{}
	var firstMatch string
	for _, rule := range r.cfg.Heuristics {
		if strings.Contains(lowered, rule.Substring) {
			matches[rule.Implies] = true
			if firstMatch == "" {
				firstMatch = rule.Substring
			}
		}
	}
	return matches, firstMatch
}

func singleFromSet(s map[Ecosystem]bool) Ecosystem {
	for k := range s {
		return k
	}
	return ""
}

type scored struct {
	eco   Ecosystem
	score float64
}

func rankSoftmax(softmax map[Ecosystem]float64) []scored {
	out := make([]scored, 0, len(softmax))
	for e, s := range softmax {
		out = append(out, scored{e, s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}

		return out[i].eco < out[j].eco
	})
	return out
}

func orderedEcosystems(softmax map[Ecosystem]float64) []Ecosystem {
	ranked := rankSoftmax(softmax)
	out := make([]Ecosystem, len(ranked))
	for i, r := range ranked {
		out[i] = r.eco
	}
	return out
}

func renormalizeTop2(top1, top2 scored) map[Ecosystem]float64 {
	total := top1.score + top2.score
	if total == 0 {
		return map[Ecosystem]float64{top1.eco: 0.5, top2.eco: 0.5}
	}
	return map[Ecosystem]float64{
		top1.eco: top1.score / total,
		top2.eco: top2.score / total,
	}
}

func validateSoftmax(s map[Ecosystem]float64) error {
	if len(s) != len(AllEcosystems) {
		return fmt.Errorf("softmax must cover %d ecosystems, got %d", len(AllEcosystems), len(s))
	}
	sum := 0.0
	for _, e := range AllEcosystems {
		v, ok := s[e]
		if !ok {
			return fmt.Errorf("softmax missing ecosystem %s", e)
		}
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("softmax has invalid value %v for %s", v, e)
		}
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-3 {
		return fmt.Errorf("softmax does not sum to 1.0 (got %v)", sum)
	}
	return nil
}

// defaultHeuristics returns the canonical 4-ecosystem token table per spec §2.6.
//
// Each ecosystem has ~8-10 high-precision tokens. Tokens are lowercased; the
// scanner matches case-insensitively (query is lowercased pre-scan).
//
// Tokens follow the spec's canonical short forms ("pip", "npm" rather than
// "pip install" / "npm install"): inclusivity over precision (max-scope
// "no gaps, nothing left behind"). Operators discussing pip / npm package
// management without typing "install" — e.g. "pip check requests version",
// "npm run build with webpack" — still resolve to the right ecosystem. Any
// over-trigger risk (a Python answer when "npm" appears in a polyglot query)
// is caught by the heuristic-conflict fall-through to classifier.
//
// The list is deliberately conservative on overlap: any token here MUST be
// specific to one ecosystem (no false-positive overlap across the 4). When
// operators need recall over precision, the classifier handles it.
//
// Slice order is load-bearing: it determines `HeuristicMatched` field for
// audit (first-match-wins) and contributes to inv-zen-200 determinism.
func defaultHeuristics() []HeuristicRule {
	eco := func(s string, e Ecosystem) HeuristicRule {
		return HeuristicRule{Substring: s, Implies: e, weight: 1.0}
	}
	return []HeuristicRule{

		eco("goroutine", EcoGo), eco("chan ", EcoGo), eco("go.mod", EcoGo),
		eco("go.sum", EcoGo), eco("gofmt", EcoGo), eco("go vet", EcoGo),
		eco("go.work", EcoGo), eco("context.context", EcoGo),
		eco("net/http", EcoGo), eco("encoding/json", EcoGo),

		eco("asyncio", EcoPython), eco("numpy", EcoPython), eco("pandas", EcoPython),
		eco("pip", EcoPython), eco("pyproject.toml", EcoPython),
		eco("requirements.txt", EcoPython), eco("__init__.py", EcoPython),
		eco("def ", EcoPython), eco("import torch", EcoPython),

		eco("tsconfig", EcoTypeScript), eco("npm", EcoTypeScript),
		eco("usestate", EcoTypeScript), eco("useeffect", EcoTypeScript),
		eco("package.json", EcoTypeScript), eco("d.ts", EcoTypeScript),
		eco("eslint", EcoTypeScript), eco("vite", EcoTypeScript),

		eco("cargo build", EcoRust), eco("cargo run", EcoRust),
		eco("cargo test", EcoRust), eco("cargo doc", EcoRust),
		eco("crate ", EcoRust), eco("&'static", EcoRust),
		eco("impl trait", EcoRust), eco("rustfmt", EcoRust),
		eco("clippy", EcoRust), eco("unsafe ", EcoRust),
	}
}

// =============================================================================
// LogisticClassifier — production QueryClassifier impl
// =============================================================================
//
// Multinomial logistic regression over a frozen 1536-d query embedding from
// jina-code-embeddings-1.5B (per Q4=A). 4-way softmax over Ecosystems with
// cross-entropy loss + L2 weight regularization, trained via mini-batch SGD.
//
// # Why this shape
//
// Spec §2.6 Q6=A: a Haiku-class LLM router would add 200-400ms p95 RTT,
// breaking the P50 ≤350ms / P95 ≤700ms budget for the entire query path.
// RAGRoute (EuroMLSys 2025; arXiv 2502.19280) shows local linear classifiers
// reach 85-90% routing accuracy at sub-ms inference cost over frozen query
// embeddings — no embedder fine-tuning, just a linear head + softmax.
//
// # Storage
//
// Weights are [K × D] float32 + per-class bias [K] float32, where
// K = len(Ecosystems) = 4 and D = EmbeddingDim = 1536. Total payload:
// 4*1536*4 + 4*4 = 24,592 bytes raw, plus magic + version + shape header
// + sha256 trailer. Fits trivially in memory; classifier.bin is ~24 KB.
//
// # Checkpoint format (little-endian)
//
//	magic    [4]byte    "ZSRC" (Zen-Swarm Router Classifier)
//	version  uint8      1
//	K        uint8      number of ecosystems
//	D        uint32     embedding dimension
//	W        [K*D]float32 row-major weights (per ecosystem then per dim)
//	B        [K]float32 per-class bias
//	hash     [32]byte   sha256 over (W || B) bytes
//
// The hash is included for two reasons:
//  1. Authenticate the on-disk checkpoint against tampering / partial writes.
//  2. Provide a stable CheckpointHash() string for inv-zen-200 audit identity
//     (RoutingDecision determinism contract; EvtRAGQuery audit payload).
//
// # Concurrency
//
// ScoreSoftmax is read-only over the weight slices and may be called from
// multiple goroutines (matches the QueryClassifier interface contract).
// Fit + ReadCheckpoint MUTATE in place and MUST NOT race with ScoreSoftmax.
// The Router's lifecycle guarantees this: a classifier is constructed +
// trained at daemon startup, then frozen for the daemon's lifetime;
// retraining produces a NEW classifier and atomically swaps the Router's
// reference. The classifier itself does not embed a mutex — that would be
// a hot-path tax on every Classify call.

const (
	classifierMagic = "ZSRC"
	// classifierVersion identifies the on-disk schema. Bump on any layout
	// change; readers MUST reject unknown versions (no silent backward-compat).
	classifierVersion = uint8(1)

	classifierFileMode = 0o600

	classifierEmbeddingDim = 1536

	defaultLearningRate = 0.05

	defaultL2Regularization = 1e-4

	defaultSyntheticCorpusSize = 5000
)

const (
	DefaultEpochs = 30

	DefaultBatchSize = 64
)

const (
	defaultEpochs    = DefaultEpochs
	defaultBatchSize = DefaultBatchSize
)

// LabeledSample is one (embedding, ecosystem) training pair. Embedding length
// MUST equal LogisticClassifierConfig.EmbeddingDim at training time.
type LabeledSample struct {
	Embedding []float32
	Label     Ecosystem
}

type LogisticClassifierConfig struct {
	EmbeddingDim int
	Ecosystems   []Ecosystem
	LearningRate float64
	Epochs       int
	BatchSize    int
	L2           float64
	Seed         int64
}

type LogisticClassifier struct {
	cfg            LogisticClassifierConfig
	weights        [][]float32
	biases         []float32
	checkpointHash string
}

func NewLogisticClassifier(cfg LogisticClassifierConfig) *LogisticClassifier {
	if cfg.EmbeddingDim == 0 {
		cfg.EmbeddingDim = classifierEmbeddingDim
	}
	if len(cfg.Ecosystems) == 0 {
		cfg.Ecosystems = AllEcosystems
	}
	if cfg.LearningRate == 0 {
		cfg.LearningRate = defaultLearningRate
	}
	if cfg.Epochs == 0 {
		cfg.Epochs = defaultEpochs
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.L2 == 0 {
		cfg.L2 = defaultL2Regularization
	}
	weights := make([][]float32, len(cfg.Ecosystems))
	for i := range weights {
		weights[i] = make([]float32, cfg.EmbeddingDim)
	}
	return &LogisticClassifier{
		cfg:     cfg,
		weights: weights,
		biases:  make([]float32, len(cfg.Ecosystems)),
	}
}

func (c *LogisticClassifier) Fit(ctx context.Context, samples []LabeledSample) error {
	return c.fit(ctx, samples)
}

func (c *LogisticClassifier) ScoreSoftmax(ctx context.Context, embedding []float32) (map[Ecosystem]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(embedding) != c.cfg.EmbeddingDim {
		return nil, fmt.Errorf("router classifier: embedding dim %d != expected %d", len(embedding), c.cfg.EmbeddingDim)
	}
	logits := make([]float64, len(c.cfg.Ecosystems))
	for k := range c.cfg.Ecosystems {
		var dot float64
		for i, w := range c.weights[k] {
			dot += float64(w) * float64(embedding[i])
		}
		logits[k] = dot + float64(c.biases[k])
	}

	maxLogit := logits[0]
	for _, l := range logits[1:] {
		if l > maxLogit {
			maxLogit = l
		}
	}

	if math.IsNaN(maxLogit) || math.IsInf(maxLogit, 0) {
		out := make(map[Ecosystem]float64, len(c.cfg.Ecosystems))
		uniform := 1.0 / float64(len(c.cfg.Ecosystems))
		for _, e := range c.cfg.Ecosystems {
			out[e] = uniform
		}
		return out, nil
	}
	var sum float64
	for k := range logits {
		logits[k] = math.Exp(logits[k] - maxLogit)
		sum += logits[k]
	}
	if sum == 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {

		out := make(map[Ecosystem]float64, len(c.cfg.Ecosystems))
		uniform := 1.0 / float64(len(c.cfg.Ecosystems))
		for _, e := range c.cfg.Ecosystems {
			out[e] = uniform
		}
		return out, nil
	}
	out := make(map[Ecosystem]float64, len(c.cfg.Ecosystems))
	for k, e := range c.cfg.Ecosystems {
		out[e] = logits[k] / sum
	}
	return out, nil
}

func (c *LogisticClassifier) CheckpointHash() string {
	return c.checkpointHash
}

func (c *LogisticClassifier) WriteCheckpoint(w io.Writer) error {
	return c.writeCheckpoint(w)
}

func (c *LogisticClassifier) ReadCheckpoint(r io.Reader) error {
	return c.readCheckpoint(r)
}

type RetrainOptions struct {
	BootstrapCorpusPath string
	OutputPath          string
	Seed                int64
	Epochs              int
	BatchSize           int
}

func RetrainAndPersist(ctx context.Context, opts RetrainOptions) error {
	return retrainAndPersist(ctx, opts)
}

func LoadLogisticClassifier(path string) (*LogisticClassifier, error) {
	return loadLogisticClassifier(path)
}

func (c *LogisticClassifier) fit(ctx context.Context, samples []LabeledSample) error {
	if len(samples) == 0 {
		return errors.New("classifier: empty training set")
	}
	for i, s := range samples {
		if len(s.Embedding) != c.cfg.EmbeddingDim {
			return fmt.Errorf("classifier: sample[%d] dim %d != expected %d", i, len(s.Embedding), c.cfg.EmbeddingDim)
		}
	}
	labelIdx := map[Ecosystem]int{}
	for i, e := range c.cfg.Ecosystems {
		labelIdx[e] = i
	}
	rng := newRNG(c.cfg.Seed)

	K := len(c.cfg.Ecosystems)
	D := c.cfg.EmbeddingDim
	gradW := make([][]float64, K)
	for k := range gradW {
		gradW[k] = make([]float64, D)
	}
	gradB := make([]float64, K)

	logits := make([]float64, K)

	for epoch := 0; epoch < c.cfg.Epochs; epoch++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		shuffled := append([]LabeledSample(nil), samples...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		for start := 0; start < len(shuffled); start += c.cfg.BatchSize {
			end := start + c.cfg.BatchSize
			if end > len(shuffled) {
				end = len(shuffled)
			}
			batch := shuffled[start:end]

			for k := range gradW {
				row := gradW[k]
				for i := range row {
					row[i] = 0
				}
			}
			for k := range gradB {
				gradB[k] = 0
			}
			for _, s := range batch {

				for k := range c.cfg.Ecosystems {
					var dot float64
					for i, w := range c.weights[k] {
						dot += float64(w) * float64(s.Embedding[i])
					}
					logits[k] = dot + float64(c.biases[k])
				}

				maxL := logits[0]
				for _, l := range logits[1:] {
					if l > maxL {
						maxL = l
					}
				}
				var sum float64
				for k := range logits {
					logits[k] = math.Exp(logits[k] - maxL)
					sum += logits[k]
				}
				for k := range logits {
					logits[k] /= sum
				}

				targetK := labelIdx[s.Label]
				for k := range c.cfg.Ecosystems {
					grad := logits[k]
					if k == targetK {
						grad -= 1.0
					}
					for i := range gradW[k] {
						gradW[k][i] += grad * float64(s.Embedding[i])
					}
					gradB[k] += grad
				}
			}
			n := float64(len(batch))
			for k := range c.weights {
				for i := range c.weights[k] {
					avgGrad := gradW[k][i]/n + c.cfg.L2*float64(c.weights[k][i])
					c.weights[k][i] -= float32(c.cfg.LearningRate * avgGrad)
				}
				c.biases[k] -= float32(c.cfg.LearningRate * gradB[k] / n)
			}
		}
	}
	c.checkpointHash = c.computeCheckpointHash()
	return nil
}

func (c *LogisticClassifier) computeCheckpointHash() string {
	h := sha256.New()
	for k := range c.weights {
		for _, w := range c.weights[k] {
			_ = binary.Write(h, binary.LittleEndian, w)
		}
	}
	for _, b := range c.biases {
		_ = binary.Write(h, binary.LittleEndian, b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (c *LogisticClassifier) writeCheckpoint(w io.Writer) error {
	if _, err := w.Write([]byte(classifierMagic)); err != nil {
		return fmt.Errorf("write magic: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, classifierVersion); err != nil {
		return fmt.Errorf("write version: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint8(len(c.cfg.Ecosystems))); err != nil {
		return fmt.Errorf("write K: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(c.cfg.EmbeddingDim)); err != nil {
		return fmt.Errorf("write D: %w", err)
	}
	for k := range c.weights {
		for _, v := range c.weights[k] {
			if err := binary.Write(w, binary.LittleEndian, v); err != nil {
				return fmt.Errorf("write weights[%d]: %w", k, err)
			}
		}
	}
	for _, v := range c.biases {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return fmt.Errorf("write biases: %w", err)
		}
	}

	hashBytes, err := hex.DecodeString(c.computeCheckpointHash())
	if err != nil {
		return fmt.Errorf("encode hash: %w", err)
	}
	if _, err := w.Write(hashBytes); err != nil {
		return fmt.Errorf("write hash: %w", err)
	}
	return nil
}

func (c *LogisticClassifier) readCheckpoint(r io.Reader) error {
	magic := make([]byte, len(classifierMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return fmt.Errorf("read magic: %w", err)
	}
	if string(magic) != classifierMagic {
		return fmt.Errorf("classifier: invalid magic %q (want %q)", magic, classifierMagic)
	}
	var version uint8
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if version != classifierVersion {
		return fmt.Errorf("classifier: unsupported version %d (want %d)", version, classifierVersion)
	}
	var k uint8
	var d uint32
	if err := binary.Read(r, binary.LittleEndian, &k); err != nil {
		return fmt.Errorf("read K: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &d); err != nil {
		return fmt.Errorf("read D: %w", err)
	}
	if int(k) != len(c.cfg.Ecosystems) || int(d) != c.cfg.EmbeddingDim {
		return fmt.Errorf("classifier: shape mismatch K=%d D=%d (want K=%d D=%d)", k, d, len(c.cfg.Ecosystems), c.cfg.EmbeddingDim)
	}
	for ki := uint8(0); ki < k; ki++ {
		for i := uint32(0); i < d; i++ {
			if err := binary.Read(r, binary.LittleEndian, &c.weights[ki][i]); err != nil {
				return fmt.Errorf("read weights[%d][%d]: %w", ki, i, err)
			}
		}
	}
	for ki := uint8(0); ki < k; ki++ {
		if err := binary.Read(r, binary.LittleEndian, &c.biases[ki]); err != nil {
			return fmt.Errorf("read biases[%d]: %w", ki, err)
		}
	}
	hashBytes := make([]byte, sha256.Size)
	if _, err := io.ReadFull(r, hashBytes); err != nil {
		return fmt.Errorf("read hash: %w", err)
	}

	computed := c.computeCheckpointHash()
	if hex.EncodeToString(hashBytes) != computed {
		return fmt.Errorf("classifier: checkpoint integrity check failed (file=%s computed=%s)",
			hex.EncodeToString(hashBytes), computed)
	}
	c.checkpointHash = computed
	return nil
}

type fileSink interface {
	io.Writer

	Sync() error

	Close() error
}

type fileSinkFactory func(path string) (fileSink, string, error)

type renameFunc func(oldPath, newPath string) error

type removeFunc func(path string) error

type retrainPersistDeps struct {
	openSink fileSinkFactory
	rename   renameFunc
	remove   removeFunc
}

func openFileSink(path string) (fileSink, string, error) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, classifierFileMode)
	if err != nil {
		return nil, "", err
	}
	return f, tmp, nil
}

func defaultRetrainPersistDeps() retrainPersistDeps {
	return retrainPersistDeps{
		openSink: openFileSink,
		rename:   os.Rename,
		remove:   os.Remove,
	}
}

func retrainAndPersist(ctx context.Context, opts RetrainOptions) error {
	return retrainAndPersistWithDeps(ctx, opts, defaultRetrainPersistDeps())
}

func retrainAndPersistWithDeps(ctx context.Context, opts RetrainOptions, deps retrainPersistDeps) error {
	if opts.OutputPath == "" {
		return errors.New("retrain: empty OutputPath")
	}
	if opts.Epochs == 0 {
		opts.Epochs = defaultEpochs
	}
	if opts.BatchSize == 0 {
		opts.BatchSize = defaultBatchSize
	}

	var samples []LabeledSample
	if opts.BootstrapCorpusPath == "" {

		rng := newRNG(opts.Seed)
		samples = generateSyntheticCorpus(rng, defaultSyntheticCorpusSize)
	} else {
		var err error
		samples, err = loadBootstrapCorpus(opts.BootstrapCorpusPath)
		if err != nil {
			return fmt.Errorf("retrain: load corpus: %w", err)
		}
	}

	cfg := LogisticClassifierConfig{
		EmbeddingDim: classifierEmbeddingDim,
		Ecosystems:   AllEcosystems,
		LearningRate: defaultLearningRate,
		Epochs:       opts.Epochs,
		BatchSize:    opts.BatchSize,
		L2:           defaultL2Regularization,
		Seed:         opts.Seed,
	}
	cls := NewLogisticClassifier(cfg)
	if err := cls.Fit(ctx, samples); err != nil {
		return fmt.Errorf("retrain: fit: %w", err)
	}

	sink, tmp, err := deps.openSink(opts.OutputPath)
	if err != nil {
		return fmt.Errorf("retrain: open %s.tmp: %w", opts.OutputPath, err)
	}
	if err := cls.WriteCheckpoint(sink); err != nil {
		_ = sink.Close()
		_ = deps.remove(tmp)
		return fmt.Errorf("retrain: write checkpoint: %w", err)
	}
	if err := sink.Sync(); err != nil {
		_ = sink.Close()
		_ = deps.remove(tmp)
		return fmt.Errorf("retrain: fsync: %w", err)
	}
	if err := sink.Close(); err != nil {
		_ = deps.remove(tmp)
		return fmt.Errorf("retrain: close: %w", err)
	}
	if err := deps.rename(tmp, opts.OutputPath); err != nil {
		_ = deps.remove(tmp)
		return fmt.Errorf("retrain: rename %s → %s: %w", tmp, opts.OutputPath, err)
	}
	return nil
}

func loadLogisticClassifier(path string) (*LogisticClassifier, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	cls := NewLogisticClassifier(LogisticClassifierConfig{
		EmbeddingDim: classifierEmbeddingDim,
		Ecosystems:   AllEcosystems,
	})
	if err := cls.ReadCheckpoint(f); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return cls, nil
}

func generateSyntheticCorpus(rng *rand.Rand, n int) []LabeledSample {
	out := make([]LabeledSample, n)
	for i := 0; i < n; i++ {
		eco := AllEcosystems[i%len(AllEcosystems)]
		out[i] = LabeledSample{
			Embedding: syntheticEmbedding(rng, eco),
			Label:     eco,
		}
	}
	return out
}

func syntheticEmbedding(rng *rand.Rand, eco Ecosystem) []float32 {
	emb := make([]float32, classifierEmbeddingDim)

	blockSize := classifierEmbeddingDim / len(AllEcosystems)
	var ownIdx int
	for i, e := range AllEcosystems {
		if e == eco {
			ownIdx = i
			break
		}
	}
	ownStart := ownIdx * blockSize
	ownEnd := ownStart + blockSize
	for j := range emb {
		noise := rng.NormFloat64() * 0.3
		if j >= ownStart && j < ownEnd {
			emb[j] = float32(1.0 + noise)
		} else {
			emb[j] = float32(noise)
		}
	}
	return emb
}

func loadBootstrapCorpus(path string) ([]LabeledSample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	known := map[string]Ecosystem{}
	for _, e := range AllEcosystems {
		known[string(e)] = e
	}

	var samples []LabeledSample
	scanner := bufio.NewScanner(f)

	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tab := strings.LastIndexByte(line, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("corpus %s line %d: missing tab separator", path, lineNo)
		}
		embPart := line[:tab]
		labelPart := strings.TrimSpace(line[tab+1:])
		eco, ok := known[labelPart]
		if !ok {
			return nil, fmt.Errorf("corpus %s line %d: unknown ecosystem label %q", path, lineNo, labelPart)
		}
		fields := strings.Fields(embPart)
		if len(fields) != classifierEmbeddingDim {
			return nil, fmt.Errorf("corpus %s line %d: embedding dim %d != expected %d", path, lineNo, len(fields), classifierEmbeddingDim)
		}
		emb := make([]float32, classifierEmbeddingDim)
		for i, tok := range fields {
			v, perr := strconv.ParseFloat(tok, 32)
			if perr != nil {
				return nil, fmt.Errorf("corpus %s line %d: parse float[%d] %q: %w", path, lineNo, i, tok, perr)
			}
			emb[i] = float32(v)
		}
		samples = append(samples, LabeledSample{Embedding: emb, Label: eco})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("corpus %s: scan: %w", path, err)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("corpus %s: no samples loaded", path)
	}
	return samples, nil
}

func newRNG(seed int64) *rand.Rand {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	return rand.New(rand.NewSource(seed))
}
