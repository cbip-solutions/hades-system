//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var ErrONNXRuntimeNotProvisioned = errors.New("ecosystem: no ONNX runner factory registered; run `zen docs reindex --bootstrap-models` or install onnxruntime shared library")

// ONNXRunner is the narrow ONNX Runtime interface BGEReRankerV2M3 depends
// on. The daemon registers a factory that constructs a concrete
// runner against github.com/yalue/onnxruntime_go (or any chosen library);
// the daemon owns the dependency, this file is library-agnostic.
//
// Inputs are flat int64 slices laid out [batch × seqLen]; the
// session has been initialized with the model loaded at construction.
// Outputs are flat float32 logits, one per row in the batch.
//
// Implementations MUST be goroutine-unsafe for Run (the caller of this
// interface, mockBGEBackend / onnxBGEBackend, serializes via the parent
// Reranker's mutex).
type ONNXRunner interface {
	Run(ctx context.Context, inputIDs, attentionMask []int64, batch, seqLen int) ([]float32, error)

	Close() error
}

type ONNXRunnerFactory func(modelPath, device string) (ONNXRunner, error)

var (
	onnxRunnerFactory   ONNXRunnerFactory
	onnxRunnerFactoryMu sync.RWMutex
)

func SetONNXRunnerFactory(f ONNXRunnerFactory) {
	onnxRunnerFactoryMu.Lock()
	onnxRunnerFactory = f
	onnxRunnerFactoryMu.Unlock()
}

func getONNXRunnerFactory() ONNXRunnerFactory {
	onnxRunnerFactoryMu.RLock()
	defer onnxRunnerFactoryMu.RUnlock()
	return onnxRunnerFactory
}

type onnxBGEBackend struct {
	cfg           BGEConfig
	device        string
	modelPath     string
	tokenizerPath string

	initOnce sync.Once
	initErr  error
	runner   ONNXRunner
	tok      *bgeTokenizer
}

func newONNXBackendImpl(cfg BGEConfig, device string) (bgeBackend, error) {
	modelPath := resolveBGEModelPath(cfg.ModelPath)
	if modelPath == "" {
		return nil, errors.New("bge onnx: ModelPath unresolved; set BGEConfig.ModelPath, ZEN_BGE_MODEL_PATH env, or place model at ~/.local/share/zen-swarm/models/bge-reranker-v2-m3.onnx")
	}
	if _, err := os.Stat(modelPath); err != nil {
		return nil, fmt.Errorf("bge onnx: model file unreadable at %q: %w", modelPath, err)
	}
	tokenizerPath := cfg.TokenizerPath
	if tokenizerPath == "" {
		tokenizerPath = filepath.Join(filepath.Dir(modelPath), "tokenizer.json")
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		return nil, fmt.Errorf("bge onnx: tokenizer file unreadable at %q: %w", tokenizerPath, err)
	}
	if device != "mps" && device != "cpu" {
		return nil, fmt.Errorf("bge onnx: unsupported device %q (want mps|cpu)", device)
	}
	return &onnxBGEBackend{
		cfg:           cfg,
		device:        device,
		modelPath:     modelPath,
		tokenizerPath: tokenizerPath,
	}, nil
}

func (b *onnxBGEBackend) initLocked(ctx context.Context) {

	if err := ctx.Err(); err != nil {
		b.initErr = fmt.Errorf("bge onnx: init aborted: %w", err)
		return
	}
	factory := getONNXRunnerFactory()
	if factory == nil {
		b.initErr = ErrONNXRuntimeNotProvisioned
		return
	}
	runner, err := factory(b.modelPath, b.device)
	if err != nil {
		b.initErr = fmt.Errorf("bge onnx: ONNXRunnerFactory: %w", err)
		return
	}
	tokenizerBytes, err := os.ReadFile(b.tokenizerPath)
	if err != nil {
		_ = runner.Close()
		b.initErr = fmt.Errorf("bge onnx: read tokenizer: %w", err)
		return
	}
	tok, err := parseBGETokenizer(tokenizerBytes, b.cfg.MaxSeqLen)
	if err != nil {
		_ = runner.Close()
		b.initErr = fmt.Errorf("bge onnx: parse tokenizer: %w", err)
		return
	}
	b.runner = runner
	b.tok = tok
}

func (b *onnxBGEBackend) Score(ctx context.Context, query string, cands []Candidate) ([]float64, error) {
	b.initOnce.Do(func() { b.initLocked(ctx) })
	if b.initErr != nil {
		return nil, b.initErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make([]float64, len(cands))
	batchSize := b.cfg.BatchSize
	if batchSize <= 0 {
		batchSize = bgeDefaultBatchSize
	}

	for start := 0; start < len(cands); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + batchSize
		if end > len(cands) {
			end = len(cands)
		}

		batch := end - start
		encs := make([]bgeEncoding, batch)
		maxLen := 0
		for i := 0; i < batch; i++ {
			encs[i] = b.tok.encodePair(query, cands[start+i].ContentText)
			if len(encs[i].ids) > maxLen {
				maxLen = len(encs[i].ids)
			}
		}
		if maxLen == 0 {

			continue
		}

		ids := make([]int64, batch*maxLen)
		mask := make([]int64, batch*maxLen)
		for i, enc := range encs {
			rowOff := i * maxLen
			for j, tokID := range enc.ids {
				ids[rowOff+j] = tokID
				mask[rowOff+j] = 1
			}

		}
		logits, err := b.runner.Run(ctx, ids, mask, batch, maxLen)
		if err != nil {
			return nil, fmt.Errorf("bge onnx: forward pass: %w", err)
		}
		if len(logits) != batch {
			return nil, fmt.Errorf("bge onnx: runner returned %d logits for %d batch rows", len(logits), batch)
		}
		for i, logit := range logits {
			out[start+i] = sigmoid(float64(logit))
		}
	}
	return out, nil
}

func (b *onnxBGEBackend) Close() error {
	if b.runner == nil {
		return nil
	}
	err := b.runner.Close()
	b.runner = nil
	return err
}

type bgeEncoding struct {
	ids []int64
}

type bgeTokenizer struct {
	vocab      map[string]int64
	maxSeqLen  int
	clsTokenID int64
	sepTokenID int64
	padTokenID int64
	unkTokenID int64
}

func parseBGETokenizer(raw []byte, maxSeqLen int) (*bgeTokenizer, error) {
	if maxSeqLen <= 0 {
		maxSeqLen = bgeDefaultMaxSeqLen
	}
	var doc struct {
		Model struct {
			Vocab map[string]int64 `json:"vocab"`
		} `json:"model"`
		AddedTokens []struct {
			ID      int64  `json:"id"`
			Content string `json:"content"`
		} `json:"added_tokens"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("tokenizer.json: %w", err)
	}
	vocab := doc.Model.Vocab
	if vocab == nil {
		vocab = make(map[string]int64)
	}

	for _, at := range doc.AddedTokens {
		vocab[at.Content] = at.ID
	}

	cls := firstID(vocab, "<s>", "[CLS]")
	sep := firstID(vocab, "</s>", "[SEP]")
	pad := firstID(vocab, "<pad>", "[PAD]")
	unk := firstID(vocab, "<unk>", "[UNK]")
	if cls < 0 || sep < 0 {
		return nil, errors.New("tokenizer.json: missing required special tokens (<s>/[CLS] and </s>/[SEP])")
	}
	if pad < 0 {

		pad = 0
	}
	if unk < 0 {

		unk = pad
	}
	return &bgeTokenizer{
		vocab:      vocab,
		maxSeqLen:  maxSeqLen,
		clsTokenID: cls,
		sepTokenID: sep,
		padTokenID: pad,
		unkTokenID: unk,
	}, nil
}

func firstID(vocab map[string]int64, keys ...string) int64 {
	for _, k := range keys {
		if id, ok := vocab[k]; ok {
			return id
		}
	}
	return -1
}

func (t *bgeTokenizer) encodePair(query, doc string) bgeEncoding {
	qIDs := t.tokenize(query)
	dIDs := t.tokenize(doc)

	budget := t.maxSeqLen - 3
	if budget < 0 {
		budget = 0
	}

	const queryCap = 64
	if len(qIDs) > queryCap {
		qIDs = qIDs[:queryCap]
	}
	remaining := budget - len(qIDs)
	if remaining < 0 {
		qIDs = qIDs[:budget]
		remaining = 0
	}
	if len(dIDs) > remaining {
		dIDs = dIDs[:remaining]
	}
	ids := make([]int64, 0, 3+len(qIDs)+len(dIDs))
	ids = append(ids, t.clsTokenID)
	ids = append(ids, qIDs...)
	ids = append(ids, t.sepTokenID)
	ids = append(ids, dIDs...)
	ids = append(ids, t.sepTokenID)
	return bgeEncoding{ids: ids}
}

func (t *bgeTokenizer) tokenize(s string) []int64 {
	if s == "" {
		return nil
	}
	words := strings.Fields(strings.ToLower(s))
	if len(words) == 0 {
		return nil
	}
	out := make([]int64, 0, len(words))
	for _, w := range words {
		out = append(out, t.tokenizeWord(w)...)
	}
	return out
}

func (t *bgeTokenizer) tokenizeWord(w string) []int64 {
	if w == "" {
		return nil
	}

	if id, ok := t.vocab[w]; ok {
		return []int64{id}
	}

	if id, ok := t.vocab["▁"+w]; ok {
		return []int64{id}
	}

	var out []int64
	start := 0
	for start < len(w) {
		end := len(w)
		var matchedID int64 = -1
		var matchedEnd int
		for end > start {
			sub := w[start:end]
			if start > 0 {
				sub = "##" + sub
			}
			if id, ok := t.vocab[sub]; ok {
				matchedID = id
				matchedEnd = end
				break
			}
			end--
		}
		if matchedID < 0 {
			out = append(out, t.unkTokenID)
			return out
		}
		out = append(out, matchedID)
		start = matchedEnd
	}
	return out
}

func sigmoid(x float64) float64 {
	if x >= 0 {
		ex := math.Exp(-x)
		return 1.0 / (1.0 + ex)
	}
	ex := math.Exp(x)
	return ex / (1.0 + ex)
}

func (t *bgeTokenizer) vocabKeysSorted() []string {
	out := make([]string, 0, len(t.vocab))
	for k := range t.vocab {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
