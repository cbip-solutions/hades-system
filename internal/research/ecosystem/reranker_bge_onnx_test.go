//go:build cgo
// +build cgo

package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func syntheticTokenizerJSON(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{

				"[CLS]": 1,
				"[SEP]": 2,
				"[PAD]": 0,
				"[UNK]": 3,

				"hello":     10,
				"world":     11,
				"context":   12,
				"goroutine": 13,
				"channel":   14,
				"cancel":    15,

				"▁hello":     20,
				"▁world":     21,
				"▁goroutine": 22,

				"go":       30,
				"gor":      31,
				"##rou":    32,
				"##tine":   33,
				"##ing":    34,
				"##er":     35,
				"##s":      36,
				"sync":     40,
				"chan":     41,
				"##nel":    42,
				"##eduler": 43,
				"sch":      44,
			},
		},
		"added_tokens": []map[string]any{

			{"id": 1, "content": "[CLS]"},
			{"id": 2, "content": "[SEP]"},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal synthetic tokenizer.json: %v", err)
	}
	return raw
}

func xlmStyleTokenizerJSON(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"<s>":   0,
				"</s>":  2,
				"<pad>": 1,
				"<unk>": 3,
				"hello": 10,
				"world": 11,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	return raw
}

func writeFakeModelFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(p, []byte("ONNX-PLACEHOLDER"), 0o644); err != nil {
		t.Fatalf("write model.onnx: %v", err)
	}
	return p
}

func TestBGEONNXBackend_ParseTokenizer_LoadsVocabAndSpecials_BERTStyle(t *testing.T) {
	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.clsTokenID != 1 {
		t.Errorf("clsTokenID = %d, want 1 ([CLS])", tok.clsTokenID)
	}
	if tok.sepTokenID != 2 {
		t.Errorf("sepTokenID = %d, want 2 ([SEP])", tok.sepTokenID)
	}
	if tok.padTokenID != 0 {
		t.Errorf("padTokenID = %d, want 0 ([PAD])", tok.padTokenID)
	}
	if tok.unkTokenID != 3 {
		t.Errorf("unkTokenID = %d, want 3 ([UNK])", tok.unkTokenID)
	}
	if id, ok := tok.vocab["hello"]; !ok || id != 10 {
		t.Errorf("vocab[hello] = (%d, %v), want (10, true)", id, ok)
	}
	if tok.maxSeqLen != 128 {
		t.Errorf("maxSeqLen = %d, want 128", tok.maxSeqLen)
	}
}

func TestBGEONNXBackend_ParseTokenizer_LoadsVocabAndSpecials_XLMStyle(t *testing.T) {
	tok, err := parseBGETokenizer(xlmStyleTokenizerJSON(t), 256)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.clsTokenID != 0 {
		t.Errorf("clsTokenID = %d, want 0 (<s>)", tok.clsTokenID)
	}
	if tok.sepTokenID != 2 {
		t.Errorf("sepTokenID = %d, want 2 (</s>)", tok.sepTokenID)
	}
	if tok.padTokenID != 1 {
		t.Errorf("padTokenID = %d, want 1 (<pad>)", tok.padTokenID)
	}
	if tok.unkTokenID != 3 {
		t.Errorf("unkTokenID = %d, want 3 (<unk>)", tok.unkTokenID)
	}
}

func TestBGEONNXBackend_ParseTokenizer_DefaultsMaxSeqLen(t *testing.T) {
	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 0)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.maxSeqLen != bgeDefaultMaxSeqLen {
		t.Errorf("maxSeqLen with 0 input = %d, want default %d", tok.maxSeqLen, bgeDefaultMaxSeqLen)
	}
	tok2, err := parseBGETokenizer(syntheticTokenizerJSON(t), -7)
	if err != nil {
		t.Fatalf("parseBGETokenizer (negative): %v", err)
	}
	if tok2.maxSeqLen != bgeDefaultMaxSeqLen {
		t.Errorf("maxSeqLen with -7 input = %d, want default %d", tok2.maxSeqLen, bgeDefaultMaxSeqLen)
	}
}

func TestBGEONNXBackend_ParseTokenizer_MalformedJSON(t *testing.T) {
	_, err := parseBGETokenizer([]byte("{not json"), 128)
	if err == nil {
		t.Fatalf("expected error for malformed JSON; got nil")
	}
	if !strings.Contains(err.Error(), "tokenizer.json") {
		t.Errorf("error message should mention tokenizer.json; got %q", err.Error())
	}
}

func TestBGEONNXBackend_ParseTokenizer_MissingCLSAndSEP(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"foo": 1,
				"bar": 2,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	_, err := parseBGETokenizer(raw, 128)
	if err == nil {
		t.Fatalf("expected error for missing CLS/SEP; got nil")
	}
	if !strings.Contains(err.Error(), "required special tokens") {
		t.Errorf("error should mention required special tokens; got %q", err.Error())
	}
}

func TestBGEONNXBackend_ParseTokenizer_PadFallback(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]": 1,
				"[SEP]": 2,
				"[UNK]": 3,
				"hello": 10,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 128)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.padTokenID != 0 {
		t.Errorf("pad fallback = %d, want 0", tok.padTokenID)
	}
}

func TestBGEONNXBackend_ParseTokenizer_UnkFallbackToPad(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]": 1,
				"[SEP]": 2,
				"[PAD]": 5,
				"hello": 10,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 128)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.unkTokenID != tok.padTokenID {
		t.Errorf("unk fallback = %d, want pad = %d", tok.unkTokenID, tok.padTokenID)
	}
}

func TestBGEONNXBackend_ParseTokenizer_AddedTokensFoldedIn(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{},
		},
		"added_tokens": []map[string]any{
			{"id": 1, "content": "[CLS]"},
			{"id": 2, "content": "[SEP]"},
			{"id": 0, "content": "[PAD]"},
			{"id": 3, "content": "[UNK]"},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 64)
	if err != nil {
		t.Fatalf("parseBGETokenizer: %v", err)
	}
	if tok.clsTokenID != 1 || tok.sepTokenID != 2 || tok.padTokenID != 0 || tok.unkTokenID != 3 {
		t.Errorf("added_tokens-only fold-in: got cls=%d sep=%d pad=%d unk=%d; want 1,2,0,3",
			tok.clsTokenID, tok.sepTokenID, tok.padTokenID, tok.unkTokenID)
	}
}

func TestBGEONNXBackend_ParseTokenizer_NilVocabSurvives(t *testing.T) {

	raw := []byte(`{"model":{},"added_tokens":[]}`)
	_, err := parseBGETokenizer(raw, 128)
	if err == nil {
		t.Fatalf("expected error for absent vocab AND absent special tokens; got nil")
	}
}

func TestBGEONNXBackend_FirstID_HappyPathAndMissing(t *testing.T) {
	vocab := map[string]int64{"<s>": 0, "[CLS]": 1, "foo": 99}
	if got := firstID(vocab, "<s>", "[CLS]"); got != 0 {
		t.Errorf("firstID(<s>,[CLS]) = %d, want 0 (first hit wins)", got)
	}
	if got := firstID(vocab, "[CLS]", "<s>"); got != 1 {
		t.Errorf("firstID([CLS],<s>) = %d, want 1", got)
	}
	if got := firstID(vocab, "foo"); got != 99 {
		t.Errorf("firstID(foo) = %d, want 99", got)
	}
	if got := firstID(vocab, "nope", "neither"); got != -1 {
		t.Errorf("firstID(none) = %d, want -1", got)
	}
	if got := firstID(vocab); got != -1 {
		t.Errorf("firstID(empty) = %d, want -1", got)
	}
}

func TestBGEONNXBackend_TokenizeWord_FullWordMatch(t *testing.T) {
	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := tok.tokenizeWord("hello")
	if len(got) != 1 || got[0] != 10 {
		t.Errorf("tokenizeWord(hello) = %v, want [10]", got)
	}
}

func TestBGEONNXBackend_TokenizeWord_SentencePiecePrefix(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]":    1,
				"[SEP]":    2,
				"[UNK]":    3,
				"▁foreign": 50,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := tok.tokenizeWord("foreign")
	if len(got) != 1 || got[0] != 50 {
		t.Errorf("tokenizeWord(foreign) via ▁ prefix = %v, want [50]", got)
	}
}

func TestBGEONNXBackend_TokenizeWord_LongestPrefixWordPiece(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]":   1,
				"[SEP]":   2,
				"[UNK]":   3,
				"go":      30,
				"##rou":   32,
				"##tine":  33,
				"##works": 60,
				"##er":    35,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := tok.tokenizeWord("goroutine")
	want := []int64{30, 32, 33}
	if !equalInt64Slice(got, want) {
		t.Errorf("tokenizeWord(goroutine) = %v, want %v (go + ##rou + ##tine)", got, want)
	}
}

func TestBGEONNXBackend_TokenizeWord_UnknownFallsBackToUNK(t *testing.T) {
	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := tok.tokenizeWord("zzzzz")
	if len(got) != 1 || got[0] != tok.unkTokenID {
		t.Errorf("tokenizeWord(zzzzz) = %v, want single UNK (=%d)", got, tok.unkTokenID)
	}
}

func TestBGEONNXBackend_TokenizeWord_EmptyReturnsNil(t *testing.T) {
	tok, _ := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if got := tok.tokenizeWord(""); got != nil {
		t.Errorf("tokenizeWord(empty) = %v, want nil", got)
	}
}

func TestBGEONNXBackend_Tokenize_FullStringRoundTrip(t *testing.T) {

	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := tok.tokenize("hello world")
	want := []int64{10, 11}
	if !equalInt64Slice(got, want) {
		t.Errorf("tokenize(hello world) = %v, want %v", got, want)
	}

	if got := tok.tokenize(""); got != nil {
		t.Errorf("tokenize(empty) = %v, want nil", got)
	}

	got = tok.tokenize("HELLO")
	if len(got) != 1 || got[0] != 10 {
		t.Errorf("tokenize(HELLO) = %v, want [10] (lowercased)", got)
	}

	if got := tok.tokenize("   \t\n"); got != nil {
		t.Errorf("tokenize(whitespace) = %v, want nil", got)
	}
}

func TestBGEONNXBackend_EncodePair_ConcatsAndAddsSpecials(t *testing.T) {
	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	enc := tok.encodePair("hello", "world")

	want := []int64{1, 10, 2, 11, 2}
	if !equalInt64Slice(enc.ids, want) {
		t.Errorf("encodePair shape = %v, want %v", enc.ids, want)
	}
}

func TestBGEONNXBackend_EncodePair_EmptyQueryAndDoc(t *testing.T) {
	tok, _ := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	enc := tok.encodePair("", "")

	want := []int64{1, 2, 2}
	if !equalInt64Slice(enc.ids, want) {
		t.Errorf("encodePair empty = %v, want %v", enc.ids, want)
	}
}

func TestBGEONNXBackend_EncodePair_QueryCapAt64(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]": 1,
				"[SEP]": 2,
				"[PAD]": 0,
				"[UNK]": 3,
				"q":     100,
				"d":     101,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 512)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := strings.Repeat("q ", 80)
	enc := tok.encodePair(query, "d")

	qCount := 0
	for _, id := range enc.ids {
		if id == 100 {
			qCount++
		}
	}
	if qCount != 64 {
		t.Errorf("query tokens after queryCap=64: got %d, want 64", qCount)
	}

	docCount := 0
	for _, id := range enc.ids {
		if id == 101 {
			docCount++
		}
	}
	if docCount != 1 {
		t.Errorf("doc tokens: got %d, want 1", docCount)
	}
}

func TestBGEONNXBackend_EncodePair_DocTruncatedToBudget(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]": 1, "[SEP]": 2, "[PAD]": 0, "[UNK]": 3,
				"q": 100, "d": 101,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 10)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := "q q q"
	docText := strings.Repeat("d ", 20)
	enc := tok.encodePair(query, docText)
	if len(enc.ids) != 10 {
		t.Errorf("len(enc.ids) = %d, want 10 (maxSeqLen)", len(enc.ids))
	}
	docCount := 0
	for _, id := range enc.ids {
		if id == 101 {
			docCount++
		}
	}
	if docCount != 4 {
		t.Errorf("doc tokens after budget truncation: got %d, want 4 (budget=7 - query=3)", docCount)
	}
}

func TestBGEONNXBackend_EncodePair_QueryExceedsBudget(t *testing.T) {

	doc := map[string]any{
		"model": map[string]any{
			"vocab": map[string]int64{
				"[CLS]": 1, "[SEP]": 2, "[PAD]": 0, "[UNK]": 3,
				"q": 100, "d": 101,
			},
		},
	}
	raw, _ := json.Marshal(doc)
	tok, err := parseBGETokenizer(raw, 6)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := strings.Repeat("q ", 10)
	docText := strings.Repeat("d ", 5)
	enc := tok.encodePair(query, docText)
	if len(enc.ids) != 6 {
		t.Errorf("len(enc.ids) = %d, want 6 (=maxSeqLen)", len(enc.ids))
	}
	qCount, dCount := 0, 0
	for _, id := range enc.ids {
		if id == 100 {
			qCount++
		}
		if id == 101 {
			dCount++
		}
	}
	if qCount != 3 {
		t.Errorf("query tokens = %d, want 3 (budget)", qCount)
	}
	if dCount != 0 {
		t.Errorf("doc tokens = %d, want 0 (no room left)", dCount)
	}
}

func TestBGEONNXBackend_EncodePair_NegativeBudgetClampedToZero(t *testing.T) {

	tok, err := parseBGETokenizer(syntheticTokenizerJSON(t), 2)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	enc := tok.encodePair("hello world", "context")

	want := []int64{1, 2, 2}
	if !equalInt64Slice(enc.ids, want) {
		t.Errorf("negative-budget encodePair = %v, want %v", enc.ids, want)
	}
}

func TestBGEONNXBackend_Sigmoid_TableDriven(t *testing.T) {
	type tc struct {
		name string
		in   float64
		want float64

		approxEpsilon float64
		expectNaN     bool
	}
	cases := []tc{
		{name: "zero_yields_half", in: 0, want: 0.5, approxEpsilon: 1e-12},
		{name: "large_positive_saturates_to_one", in: 50, want: 1.0, approxEpsilon: 1e-9},
		{name: "large_negative_underflows_to_zero", in: -50, want: 0.0, approxEpsilon: 1e-9},
		{name: "neg_inf_returns_zero", in: math.Inf(-1), want: 0.0, approxEpsilon: 1e-12},
		{name: "pos_inf_returns_one", in: math.Inf(1), want: 1.0, approxEpsilon: 1e-12},
		{name: "NaN_propagates", in: math.NaN(), expectNaN: true},
		{name: "positive_branch", in: 1.0, want: 1.0 / (1.0 + math.Exp(-1.0)), approxEpsilon: 1e-12},
		{name: "negative_branch", in: -1.0, want: math.Exp(-1.0) / (1.0 + math.Exp(-1.0)), approxEpsilon: 1e-12},
		{name: "small_positive", in: 0.5, want: 1.0 / (1.0 + math.Exp(-0.5)), approxEpsilon: 1e-12},
		{name: "small_negative", in: -0.5, want: math.Exp(-0.5) / (1.0 + math.Exp(-0.5)), approxEpsilon: 1e-12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sigmoid(c.in)
			if c.expectNaN {
				if !math.IsNaN(got) {
					t.Errorf("sigmoid(NaN) = %v, want NaN", got)
				}
				return
			}
			if math.Abs(got-c.want) > c.approxEpsilon {
				t.Errorf("sigmoid(%v) = %v, want ≈%v (ε=%v)", c.in, got, c.want, c.approxEpsilon)
			}
		})
	}
}

func TestBGEONNXBackend_Sigmoid_MonotonicAndBounded(t *testing.T) {

	prev := sigmoid(-10)
	if prev < 0 || prev > 1 {
		t.Errorf("sigmoid(-10)=%v out of [0,1]", prev)
	}
	for _, x := range []float64{-5, -1, -0.1, 0, 0.1, 1, 5, 10} {
		got := sigmoid(x)
		if got < 0 || got > 1 {
			t.Errorf("sigmoid(%v)=%v out of [0,1]", x, got)
		}
		if got < prev {
			t.Errorf("sigmoid not monotonic at x=%v: prev=%v got=%v", x, prev, got)
		}
		prev = got
	}
}

func TestBGEONNXBackend_NewONNXBackendImpl_ModelPathMissing(t *testing.T) {

	t.Setenv("ZEN_BGE_MODEL_PATH", "")
	t.Setenv("HOME", t.TempDir())
	cfg := BGEConfig{Backend: BGEBackendMPS}
	_, err := newONNXBackendImpl(cfg, "mps")
	if err == nil {
		t.Fatalf("expected error for missing model file; got nil")
	}
	if !strings.Contains(err.Error(), "model file unreadable") {
		t.Errorf("error should mention model file unreadable; got %q", err.Error())
	}
}

func TestBGEONNXBackend_NewONNXBackendImpl_TokenizerMissing(t *testing.T) {

	modelPath := writeFakeModelFile(t)
	cfg := BGEConfig{Backend: BGEBackendCPU, ModelPath: modelPath}
	_, err := newONNXBackendImpl(cfg, "cpu")
	if err == nil {
		t.Fatalf("expected error for missing tokenizer; got nil")
	}
	if !strings.Contains(err.Error(), "tokenizer file unreadable") {
		t.Errorf("error should mention tokenizer unreadable; got %q", err.Error())
	}
}

func TestBGEONNXBackend_NewONNXBackendImpl_InvalidDevice(t *testing.T) {

	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(modelPath, []byte("ONNX"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	tokPath := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(tokPath, syntheticTokenizerJSON(t), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	cfg := BGEConfig{ModelPath: modelPath, TokenizerPath: tokPath}
	_, err := newONNXBackendImpl(cfg, "tpu")
	if err == nil {
		t.Fatalf("expected error for unsupported device; got nil")
	}
	if !strings.Contains(err.Error(), "unsupported device") {
		t.Errorf("error should mention unsupported device; got %q", err.Error())
	}
}

func TestBGEONNXBackend_NewONNXBackendImpl_ExplicitTokenizerPath(t *testing.T) {

	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "model.onnx")
	if err := os.WriteFile(modelPath, []byte("ONNX"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	tokDir := t.TempDir()
	tokPath := filepath.Join(tokDir, "alt_tokenizer.json")
	if err := os.WriteFile(tokPath, syntheticTokenizerJSON(t), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	cfg := BGEConfig{ModelPath: modelPath, TokenizerPath: tokPath}
	be, err := newONNXBackendImpl(cfg, "cpu")
	if err != nil {
		t.Fatalf("newONNXBackendImpl: %v", err)
	}
	concrete, ok := be.(*onnxBGEBackend)
	if !ok {
		t.Fatalf("backend is %T, want *onnxBGEBackend", be)
	}
	if concrete.modelPath != modelPath {
		t.Errorf("modelPath = %q, want %q", concrete.modelPath, modelPath)
	}
	if concrete.tokenizerPath != tokPath {
		t.Errorf("tokenizerPath = %q, want %q", concrete.tokenizerPath, tokPath)
	}
	if concrete.device != "cpu" {
		t.Errorf("device = %q, want cpu", concrete.device)
	}
}

func TestBGEONNXBackend_InitLocked_FactoryNotProvisioned(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	be := buildScoredBackend(t)
	be.initLocked(context.Background())
	if !errors.Is(be.initErr, ErrONNXRuntimeNotProvisioned) {
		t.Errorf("initErr = %v, want ErrONNXRuntimeNotProvisioned", be.initErr)
	}
	if be.runner != nil {
		t.Errorf("runner should be nil when factory missing; got %v", be.runner)
	}
}

func TestBGEONNXBackend_InitLocked_ContextCancel(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{}, nil
	})

	be := buildScoredBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	be.initLocked(ctx)
	if be.initErr == nil {
		t.Fatalf("initErr should be non-nil for cancelled ctx")
	}
	if !errors.Is(be.initErr, context.Canceled) {
		t.Errorf("initErr = %v, want wrapping context.Canceled", be.initErr)
	}
	if be.runner != nil {
		t.Errorf("runner must remain nil after cancelled-ctx init")
	}
}

func TestBGEONNXBackend_InitLocked_FactoryErrorPropagates(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	factoryErr := errors.New("synthetic factory boom")
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return nil, factoryErr
	})

	be := buildScoredBackend(t)
	be.initLocked(context.Background())
	if be.initErr == nil {
		t.Fatalf("initErr should be non-nil when factory errored")
	}
	if !errors.Is(be.initErr, factoryErr) {
		t.Errorf("initErr = %v, want wrapping %v", be.initErr, factoryErr)
	}
}

func TestBGEONNXBackend_InitLocked_TokenizerUnreadable(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	dummyRunner := &dummyONNXRunner{}
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return dummyRunner, nil
	})

	be := buildScoredBackend(t)

	if err := os.Remove(be.tokenizerPath); err != nil {
		t.Fatalf("rm tokenizer: %v", err)
	}
	be.initLocked(context.Background())
	if be.initErr == nil {
		t.Fatalf("initErr should be non-nil when tokenizer missing")
	}
	if !strings.Contains(be.initErr.Error(), "read tokenizer") {
		t.Errorf("initErr = %v, want mentioning 'read tokenizer'", be.initErr)
	}
	if !dummyRunner.closed.Load() {
		t.Errorf("runner.Close must be called when tokenizer read fails")
	}
}

func TestBGEONNXBackend_InitLocked_TokenizerParseFails(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	dummyRunner := &dummyONNXRunner{}
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return dummyRunner, nil
	})

	be := buildScoredBackend(t)

	if err := os.WriteFile(be.tokenizerPath, []byte("{not json}"), 0o644); err != nil {
		t.Fatalf("write malformed tokenizer: %v", err)
	}
	be.initLocked(context.Background())
	if be.initErr == nil {
		t.Fatalf("initErr should be non-nil when tokenizer parse fails")
	}
	if !strings.Contains(be.initErr.Error(), "parse tokenizer") {
		t.Errorf("initErr = %v, want mentioning 'parse tokenizer'", be.initErr)
	}
	if !dummyRunner.closed.Load() {
		t.Errorf("runner.Close must be called when tokenizer parse fails")
	}
}

func TestBGEONNXBackend_InitLocked_HappyPath(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	dummyRunner := &dummyONNXRunner{}
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return dummyRunner, nil
	})

	be := buildScoredBackend(t)
	be.initLocked(context.Background())
	if be.initErr != nil {
		t.Fatalf("happy-path initErr = %v, want nil", be.initErr)
	}
	if be.runner == nil {
		t.Errorf("runner should be assigned on success")
	}
	if be.tok == nil {
		t.Errorf("tokenizer should be parsed on success")
	}
}

func TestBGEONNXBackend_Score_EmptyCandidates(t *testing.T) {
	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{returnLogits: func(batch int) []float32 { return make([]float32, batch) }}, nil
	})
	be := buildScoredBackend(t)
	scores, err := be.Score(context.Background(), "q", nil)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("Score(empty) = %v, want empty slice", scores)
	}
}

func TestBGEONNXBackend_Score_InitErrorReplays(t *testing.T) {
	// No factory → first Score returns ErrONNXRuntimeNotProvisioned; the
	// second call MUST return the same sticky error (initOnce semantics).
	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	be := buildScoredBackend(t)
	_, err1 := be.Score(context.Background(), "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if !errors.Is(err1, ErrONNXRuntimeNotProvisioned) {
		t.Fatalf("first Score err = %v, want ErrONNXRuntimeNotProvisioned", err1)
	}
	_, err2 := be.Score(context.Background(), "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if !errors.Is(err2, ErrONNXRuntimeNotProvisioned) {
		t.Fatalf("second Score err = %v, want sticky ErrONNXRuntimeNotProvisioned", err2)
	}
}

func TestBGEONNXBackend_Score_RunnerError(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	runErr := errors.New("synthetic ONNX forward boom")
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{runErr: runErr}, nil
	})
	be := buildScoredBackend(t)
	_, err := be.Score(context.Background(), "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if err == nil {
		t.Fatalf("Score should error when runner.Run errors")
	}
	if !errors.Is(err, runErr) {
		t.Errorf("Score err = %v, want wrapping %v", err, runErr)
	}
}

func TestBGEONNXBackend_Score_BadLogitsCount(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{
			returnLogits: func(batch int) []float32 { return make([]float32, batch+1) },
		}, nil
	})
	be := buildScoredBackend(t)
	_, err := be.Score(context.Background(), "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if err == nil {
		t.Fatalf("Score should error on bad logits count")
	}
	if !strings.Contains(err.Error(), "logits for") {
		t.Errorf("err msg = %q, want containing 'logits for'", err.Error())
	}
}

func TestBGEONNXBackend_Score_HappyPath(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {

		return &dummyONNXRunner{
			returnLogits: func(batch int) []float32 {
				out := make([]float32, batch)
				for i := range out {
					out[i] = float32(i)
				}
				return out
			},
		}, nil
	})
	be := buildScoredBackend(t)
	cands := []Candidate{
		{ChunkID: 1, ContentText: "hello"},
		{ChunkID: 2, ContentText: "world"},
	}
	scores, err := be.Score(context.Background(), "hello", cands)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if len(scores) != 2 {
		t.Fatalf("len(scores) = %d, want 2", len(scores))
	}
	if math.Abs(scores[0]-0.5) > 1e-6 {
		t.Errorf("scores[0] = %v, want ≈0.5 (sigmoid(0))", scores[0])
	}
	if math.Abs(scores[1]-(1.0/(1.0+math.Exp(-1.0)))) > 1e-6 {
		t.Errorf("scores[1] = %v, want sigmoid(1)", scores[1])
	}
}

func TestBGEONNXBackend_Score_ContextCancelledMidLoop(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	var batchCount atomic.Int32
	cancelSignal := make(chan struct{})
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{
			returnLogits: func(batch int) []float32 {
				n := batchCount.Add(1)
				if n == 1 {
					close(cancelSignal)
				}
				return make([]float32, batch)
			},
		}, nil
	})

	be := buildScoredBackendWithBatchSize(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := be.Score(ctx, "q", []Candidate{
			{ChunkID: 1, ContentText: "hello"},
			{ChunkID: 2, ContentText: "world"},
		})
		done <- err
	}()

	<-cancelSignal
	cancel()

	err := <-done
	// Score may return either ctx.Err directly OR succeed if it raced
	// faster than the cancel. The contract is: when ctx is cancelled,
	// Score MUST eventually return without a panic. We accept either err
	// is nil (raced through) OR err is context.Canceled. The race-free
	// guarantee is "no panic" — verified by the test not panicking.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Score err = %v, want nil or context.Canceled", err)
	}
}

func TestBGEONNXBackend_Score_PreEntryContextCancel(t *testing.T) {

	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) {
		return &dummyONNXRunner{}, nil
	})

	be := buildScoredBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := be.Score(ctx, "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Score with pre-cancelled ctx err = %v, want wrapping context.Canceled", err)
	}
}

func TestBGEONNXBackend_Close_IdempotentAndReleasesRunner(t *testing.T) {
	restoreFactory := withCleanFactory(t)
	defer restoreFactory()
	dummyRunner := &dummyONNXRunner{returnLogits: func(b int) []float32 { return make([]float32, b) }}
	SetONNXRunnerFactory(func(modelPath, device string) (ONNXRunner, error) { return dummyRunner, nil })

	be := buildScoredBackend(t)

	_, err := be.Score(context.Background(), "q", []Candidate{{ChunkID: 1, ContentText: "hello"}})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if be.runner == nil {
		t.Fatalf("runner should be set after Score")
	}

	if err := be.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !dummyRunner.closed.Load() {
		t.Errorf("runner.Close should have fired")
	}
	if be.runner != nil {
		t.Errorf("runner should be nil after Close")
	}

	if err := be.Close(); err != nil {
		t.Errorf("second Close errored: %v", err)
	}
}

func TestBGEONNXBackend_Close_NilRunnerEarlyReturn(t *testing.T) {

	be := buildScoredBackend(t)
	if err := be.Close(); err != nil {
		t.Errorf("Close on uninitialized backend should not error; got %v", err)
	}
}

func TestBGEONNXBackend_SetGetONNXRunnerFactory_RoundTrip(t *testing.T) {
	restoreFactory := withCleanFactory(t)
	defer restoreFactory()

	if got := getONNXRunnerFactory(); got != nil {
		t.Fatalf("getONNXRunnerFactory before set = %v, want nil", got)
	}
	dummy := func(modelPath, device string) (ONNXRunner, error) { return &dummyONNXRunner{}, nil }
	SetONNXRunnerFactory(dummy)
	got := getONNXRunnerFactory()
	if got == nil {
		t.Fatalf("getONNXRunnerFactory after set = nil, want non-nil")
	}

	runner, err := got("/tmp/foo", "cpu")
	if err != nil {
		t.Fatalf("invoking returned factory: %v", err)
	}
	if runner == nil {
		t.Errorf("factory returned nil runner")
	}

	SetONNXRunnerFactory(nil)
	if got := getONNXRunnerFactory(); got != nil {
		t.Errorf("getONNXRunnerFactory after reset = %v, want nil", got)
	}
}

func TestBGEONNXBackend_ResolveBGEModelPath_ExplicitWins(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "/env/path/model.onnx")
	got := resolveBGEModelPath("/explicit/path/model.onnx")
	if got != "/explicit/path/model.onnx" {
		t.Errorf("explicit should win: got %q", got)
	}
}

func TestBGEONNXBackend_ResolveBGEModelPath_EnvUsedWhenExplicitEmpty(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "/env/path/model.onnx")
	got := resolveBGEModelPath("")
	if got != "/env/path/model.onnx" {
		t.Errorf("env should be used when explicit empty: got %q", got)
	}
}

func TestBGEONNXBackend_ResolveBGEModelPath_DefaultPath(t *testing.T) {
	t.Setenv("ZEN_BGE_MODEL_PATH", "")
	t.Setenv("HOME", "/synthetic/home")
	got := resolveBGEModelPath("")
	if !strings.Contains(got, "bge-reranker-v2-m3.onnx") {
		t.Errorf("default path = %q, want containing model filename", got)
	}
	if !strings.Contains(got, "/synthetic/home") {
		t.Errorf("default path = %q, want containing $HOME", got)
	}
}

func TestBGEONNXBackend_VocabKeysSorted_DeterministicOrder(t *testing.T) {
	tok, _ := parseBGETokenizer(syntheticTokenizerJSON(t), 128)
	k1 := tok.vocabKeysSorted()
	k2 := tok.vocabKeysSorted()
	if !equalStringSlice(k1, k2) {
		t.Errorf("vocabKeysSorted not deterministic: %v vs %v", k1, k2)
	}
	for i := 1; i < len(k1); i++ {
		if k1[i-1] >= k1[i] {
			t.Errorf("not sorted at index %d: %q >= %q", i, k1[i-1], k1[i])
		}
	}
}

func buildScoredBackend(t *testing.T) *onnxBGEBackend {
	t.Helper()
	return buildScoredBackendWithBatchSize(t, 0)
}

func buildScoredBackendWithBatchSize(t *testing.T, batchSize int) *onnxBGEBackend {
	t.Helper()
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(modelPath, []byte("ONNX"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}
	tokPath := filepath.Join(dir, "tokenizer.json")
	if err := os.WriteFile(tokPath, syntheticTokenizerJSON(t), 0o644); err != nil {
		t.Fatalf("write tokenizer: %v", err)
	}
	cfg := BGEConfig{
		Backend:       BGEBackendCPU,
		ModelPath:     modelPath,
		TokenizerPath: tokPath,
		MaxSeqLen:     128,
		BatchSize:     batchSize,
	}
	be, err := newONNXBackendImpl(cfg, "cpu")
	if err != nil {
		t.Fatalf("newONNXBackendImpl: %v", err)
	}
	return be.(*onnxBGEBackend)
}

// withCleanFactory snapshots the global factory, clears it, and returns a
// restore func. Tests that mutate the factory MUST defer the restore so
// other tests get a clean global.
func withCleanFactory(t *testing.T) func() {
	t.Helper()
	prev := getONNXRunnerFactory()
	SetONNXRunnerFactory(nil)
	return func() { SetONNXRunnerFactory(prev) }
}

func equalInt64Slice(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type dummyONNXRunner struct {
	mu           sync.Mutex
	returnLogits func(batch int) []float32
	runErr       error
	closed       atomicBoolish
}

func (d *dummyONNXRunner) Run(ctx context.Context, inputIDs, attentionMask []int64, batch, seqLen int) ([]float32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runErr != nil {
		return nil, d.runErr
	}
	if d.returnLogits != nil {
		return d.returnLogits(batch), nil
	}
	return make([]float32, batch), nil
}

func (d *dummyONNXRunner) Close() error {
	d.closed.Set(true)
	return nil
}

type atomicBoolish struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBoolish) Set(v bool) { a.mu.Lock(); a.v = v; a.mu.Unlock() }
func (a *atomicBoolish) Load() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.v }
