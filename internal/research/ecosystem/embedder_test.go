package ecosystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNoopEmbedderImplementsEmbedderInterface(t *testing.T) {
	var _ Embedder = (*NoopEmbedder)(nil)
}

func TestNoopEmbedderEmbedBinary256dShape(t *testing.T) {
	e := &NoopEmbedder{}
	bin, err := e.EmbedBinary256d(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("EmbedBinary256d: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("len(bin) = %d; want 32 (256 bits = 32 bytes)", len(bin))
	}
}

func TestNoopEmbedderEmbedFP32_1536dShape(t *testing.T) {
	e := &NoopEmbedder{}
	fp, err := e.EmbedFP32_1536d(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("EmbedFP32_1536d: %v", err)
	}
	if len(fp) != 1536 {
		t.Errorf("len(fp) = %d; want 1536", len(fp))
	}
}

func TestNoopEmbedderEmbedBothReturnsBothShapes(t *testing.T) {
	e := &NoopEmbedder{}
	bin, fp, err := e.EmbedBoth(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("EmbedBoth: %v", err)
	}
	if len(bin) != 32 || len(fp) != 1536 {
		t.Errorf("EmbedBoth len = (%d, %d); want (32, 1536)", len(bin), len(fp))
	}
}

func TestNoopEmbedderEmbedBatchReturnsBatchedShapes(t *testing.T) {
	e := &NoopEmbedder{}
	texts := []string{"a", "b", "c"}
	bins, fps, err := e.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(bins) != 3 || len(fps) != 3 {
		t.Fatalf("EmbedBatch lens = (%d, %d); want (3, 3)", len(bins), len(fps))
	}
	for i := range bins {
		if len(bins[i]) != 32 {
			t.Errorf("bins[%d] len = %d; want 32", i, len(bins[i]))
		}
		if len(fps[i]) != 1536 {
			t.Errorf("fps[%d] len = %d; want 1536", i, len(fps[i]))
		}
	}
}

func TestNoopEmbedderContextCancel(t *testing.T) {
	e := &NoopEmbedder{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.EmbedBinary256d(ctx, "x"); err == nil {
		t.Errorf("EmbedBinary256d(cancelled): want error; got nil")
	}
	if _, err := e.EmbedFP32_1536d(ctx, "x"); err == nil {
		t.Errorf("EmbedFP32_1536d(cancelled): want error; got nil")
	}
	if _, _, err := e.EmbedBoth(ctx, "x"); err == nil {
		t.Errorf("EmbedBoth(cancelled): want error; got nil")
	}
	if _, _, err := e.EmbedBatch(ctx, []string{"x"}); err == nil {
		t.Errorf("EmbedBatch(cancelled): want error; got nil")
	}
}

func TestNoopEmbedderClose(t *testing.T) {
	e := &NoopEmbedder{}
	if err := e.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestEmbedderConfigFields(t *testing.T) {
	c := EmbedderConfig{
		Model:       "jina-code-embeddings-1.5b",
		Backend:     "mps",
		BatchSize:   32,
		APITokenKey: "voyage-code-3-token",
	}
	if c.Model == "" || c.Backend == "" || c.BatchSize <= 0 {
		t.Errorf("EmbedderConfig field-set mismatch: %+v", c)
	}
}

func TestJinaCodeEmbeddings_InterfaceConformance(t *testing.T) {
	var _ Embedder = (*JinaCodeEmbeddings)(nil)
}

func TestJinaCodeEmbeddings_ShimMode_EmbedBinary256d(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	opts := JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: scriptPath,
		ShimMode:   true,
	}
	emb, err := NewJinaCodeEmbeddings(opts)
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := emb.EmbedBinary256d(ctx, "func main() {}")
	if err != nil {
		t.Fatalf("EmbedBinary256d err: %v", err)
	}
	if len(got) != 32 {
		t.Errorf("EmbedBinary256d len=%d, want 32", len(got))
	}

	got2, err := emb.EmbedBinary256d(ctx, "func main() {}")
	if err != nil {
		t.Fatalf("second EmbedBinary256d err: %v", err)
	}
	if string(got) != string(got2) {
		t.Errorf("shim non-deterministic: %x vs %x", got, got2)
	}
}

func TestJinaCodeEmbeddings_ShimMode_EmbedFP32_1536d(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	ctx := context.Background()
	got, err := emb.EmbedFP32_1536d(ctx, "import sys")
	if err != nil {
		t.Fatalf("EmbedFP32_1536d err: %v", err)
	}
	if len(got) != 1536 {
		t.Errorf("EmbedFP32_1536d len=%d, want 1536", len(got))
	}
}

func TestJinaCodeEmbeddings_ShimMode_EmbedBoth(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	ctx := context.Background()
	bin, fp32, err := emb.EmbedBoth(ctx, "let x = 42")
	if err != nil {
		t.Fatalf("EmbedBoth err: %v", err)
	}
	if len(bin) != 32 {
		t.Errorf("bin len=%d, want 32", len(bin))
	}
	if len(fp32) != 1536 {
		t.Errorf("fp32 len=%d, want 1536", len(fp32))
	}
	// Per-call cross-shape consistency: the binary-256d MUST be derivable from
	// the first 256 dims of fp32 (Matryoshka slicing + binary quantization).
	wantBin := quantizeBinary256(fp32[:256])
	if string(bin) != string(wantBin) {
		t.Errorf("EmbedBoth cross-shape inconsistent:\n  got  bin = %x\n  want bin = %x", bin, wantBin)
	}
}

func TestJinaCodeEmbeddings_ShimMode_EmbedBatch(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: 4,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	ctx := context.Background()
	texts := []string{
		"package main",
		"def foo(): pass",
		"console.log()",
		"fn main() {}",
		"struct Foo {}",
	}
	bins, fp32s, err := emb.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch err: %v", err)
	}
	if len(bins) != len(texts) || len(fp32s) != len(texts) {
		t.Fatalf("EmbedBatch len mismatch: bins=%d fp32s=%d texts=%d", len(bins), len(fp32s), len(texts))
	}
	for i := range texts {
		if len(bins[i]) != 32 {
			t.Errorf("bins[%d] len=%d, want 32", i, len(bins[i]))
		}
		if len(fp32s[i]) != 1536 {
			t.Errorf("fp32s[%d] len=%d, want 1536", i, len(fp32s[i]))
		}
	}
	// Order preservation: batch output[i] MUST correspond to input[i].
	// Verify via single-call comparison for first text.
	singleBin, _, err := emb.EmbedBoth(ctx, texts[0])
	if err != nil {
		t.Fatalf("single EmbedBoth err: %v", err)
	}
	if string(bins[0]) != string(singleBin) {
		t.Errorf("batch order broken: bins[0] != single EmbedBoth(texts[0])")
	}
}

func TestJinaCodeEmbeddings_ShimMode_EmbedBatchEmpty(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	bins, fps, err := emb.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil) err: %v", err)
	}
	if len(bins) != 0 || len(fps) != 0 {
		t.Errorf("EmbedBatch(nil) returned non-empty: bins=%d fps=%d", len(bins), len(fps))
	}
}

func TestJinaCodeEmbeddings_ContextCancellation_EmbedBinary(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = emb.EmbedBinary256d(ctx, "anything")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("EmbedBinary256d on cancelled ctx err=%v, want context.Canceled", err)
	}
}

func TestJinaCodeEmbeddings_ContextCancellation_AllMethods(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := emb.EmbedFP32_1536d(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("EmbedFP32_1536d cancelled err=%v, want context.Canceled", err)
	}
	if _, _, err := emb.EmbedBoth(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("EmbedBoth cancelled err=%v, want context.Canceled", err)
	}
	if _, _, err := emb.EmbedBatch(ctx, []string{"x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("EmbedBatch cancelled err=%v, want context.Canceled", err)
	}
}

func TestJinaCodeEmbeddings_CloseIdempotent(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	if err := emb.Close(); err != nil {
		t.Errorf("first Close err: %v", err)
	}
	if err := emb.Close(); err != nil {
		t.Errorf("second Close err: %v (should be idempotent)", err)
	}
}

// TestJinaCodeEmbeddings_AfterClose verifies that every public method
// errors after Close (defense-in-depth: callers MUST NOT use a closed
// embedder; surface the misuse).
func TestJinaCodeEmbeddings_AfterClose(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	_ = emb.Close()
	if _, err := emb.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Error("EmbedBinary256d after Close should error")
	}
	if _, err := emb.EmbedFP32_1536d(context.Background(), "x"); err == nil {
		t.Error("EmbedFP32_1536d after Close should error")
	}
	if _, _, err := emb.EmbedBoth(context.Background(), "x"); err == nil {
		t.Error("EmbedBoth after Close should error")
	}
	if _, _, err := emb.EmbedBatch(context.Background(), []string{"x"}); err == nil {
		t.Error("EmbedBatch after Close should error")
	}
}

func TestJinaCodeEmbeddings_MissingScript(t *testing.T) {
	_, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: "/does/not/exist.py",
		ShimMode:   true,
	})
	if err == nil {
		t.Error("NewJinaCodeEmbeddings with missing script should error")
	}
}

func TestJinaCodeEmbeddings_EmptyScript(t *testing.T) {
	_, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3",
		ScriptPath: "",
		ShimMode:   true,
	})
	if err == nil {
		t.Error("NewJinaCodeEmbeddings with empty ScriptPath should error")
	}
}

func TestJinaCodeEmbeddings_InvalidBatchSize(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	_, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: -1,
	})
	if err == nil {
		t.Error("NewJinaCodeEmbeddings with BatchSize=-1 should error")
	}
}

func TestJinaCodeEmbeddings_DefaultPython(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{

		ScriptPath: scriptPath,
		ShimMode:   true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings (empty PythonPath default) err: %v", err)
	}
	defer emb.Close()
	if _, err := emb.EmbedBinary256d(context.Background(), "y"); err != nil {
		t.Errorf("EmbedBinary256d after default-python init: %v", err)
	}
}

func TestJinaCodeEmbeddings_DefaultBatchSize(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: 0,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()

	texts := make([]string, 3)
	for i := range texts {
		texts[i] = "x"
	}
	if _, _, err := emb.EmbedBatch(context.Background(), texts); err != nil {
		t.Errorf("EmbedBatch with default BatchSize: %v", err)
	}
}

func TestQuantizeBinary256_SignBit(t *testing.T) {
	fp32 := make([]float32, 256)

	for i := range fp32 {
		fp32[i] = 1.0
	}
	got := quantizeBinary256(fp32)
	if len(got) != 32 {
		t.Fatalf("quantizeBinary256 all-positive len=%d, want 32", len(got))
	}
	for i, b := range got {
		if b != 0xFF {
			t.Errorf("all-positive byte[%d]=0x%02X, want 0xFF", i, b)
		}
	}

	for i := range fp32 {
		fp32[i] = -1.0
	}
	got = quantizeBinary256(fp32)
	for i, b := range got {
		if b != 0x00 {
			t.Errorf("all-negative byte[%d]=0x%02X, want 0x00", i, b)
		}
	}

	for i := range fp32 {
		fp32[i] = 0
	}
	got = quantizeBinary256(fp32)
	for i, b := range got {
		if b != 0xFF {
			t.Errorf("zero byte[%d]=0x%02X, want 0xFF (zero is non-negative)", i, b)
		}
	}

	for i := range fp32 {
		fp32[i] = -1.0
	}
	fp32[0] = 1.0
	got = quantizeBinary256(fp32)
	if got[0] != 0x80 {
		t.Errorf("MSB-first: byte[0]=0x%02X, want 0x80", got[0])
	}
	for i := 1; i < 32; i++ {
		if got[i] != 0x00 {
			t.Errorf("MSB-first: byte[%d]=0x%02X, want 0x00", i, got[i])
		}
	}

	for i := range fp32 {
		fp32[i] = -1.0
	}
	fp32[7] = 1.0
	got = quantizeBinary256(fp32)
	if got[0] != 0x01 {
		t.Errorf("byte[0] for fp32[7] only =0x%02X, want 0x01", got[0])
	}

	for i := range fp32 {
		fp32[i] = -1.0
	}
	fp32[8] = 1.0
	got = quantizeBinary256(fp32)
	if got[1] != 0x80 {
		t.Errorf("byte[1] for fp32[8] only =0x%02X, want 0x80", got[1])
	}
}

// TestQuantizeBinary256_WrongLengthPanics confirms the helper guards its
// 256-float precondition (defense-in-depth; callers MUST slice properly).
func TestQuantizeBinary256_WrongLengthPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("quantizeBinary256(short input) did not panic")
		}
	}()
	quantizeBinary256(make([]float32, 100))
}

// TestJinaCodeEmbeddings_ConcurrentEmbedSerialized verifies mutex-serialised
// concurrent calls return correct shapes (subprocess is single-threaded;
// concurrent callers MUST NOT corrupt each other's responses).
func TestJinaCodeEmbeddings_ConcurrentEmbedSerialized(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings err: %v", err)
	}
	defer emb.Close()
	const N = 10
	type result struct {
		bin []byte
		err error
	}
	out := make(chan result, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			bin, err := emb.EmbedBinary256d(context.Background(),
				"text_"+string(rune('A'+i)))
			out <- result{bin: bin, err: err}
		}(i)
	}
	for i := 0; i < N; i++ {
		r := <-out
		if r.err != nil {
			t.Errorf("concurrent EmbedBinary256d err: %v", r.err)
			continue
		}
		if len(r.bin) != 32 {
			t.Errorf("concurrent EmbedBinary256d len=%d, want 32", len(r.bin))
		}
	}
}

func newJinaMalformEmbedder(t *testing.T, mode string) *JinaCodeEmbeddings {
	t.Helper()
	scriptPath := findJinaShimScript(t)
	t.Setenv("ZEN_JINA_MALFORM", mode)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings(malform=%s): %v", mode, err)
	}
	t.Cleanup(func() { _ = emb.Close() })
	return emb
}

func TestJinaCodeEmbeddings_Malform_WrongBinCount(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "wrong_bin_count")
	ctx := context.Background()
	if _, err := emb.EmbedBinary256d(ctx, "x"); err == nil {
		t.Error("EmbedBinary256d under wrong_bin_count should error")
	}
	if _, _, err := emb.EmbedBoth(ctx, "x"); err == nil {
		t.Error("EmbedBoth under wrong_bin_count should error")
	}
	if _, _, err := emb.EmbedBatch(ctx, []string{"a", "b"}); err == nil {
		t.Error("EmbedBatch under wrong_bin_count should error")
	}
}

func TestJinaCodeEmbeddings_Malform_WrongFpCount(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "wrong_fp_count")
	if _, err := emb.EmbedFP32_1536d(context.Background(), "x"); err == nil {
		t.Error("EmbedFP32_1536d under wrong_fp_count should error")
	}
}

func TestJinaCodeEmbeddings_Malform_WrongBinLen(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "wrong_bin_len")
	if _, err := emb.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Error("EmbedBinary256d under wrong_bin_len should error")
	}
	if _, _, err := emb.EmbedBoth(context.Background(), "x"); err == nil {
		t.Error("EmbedBoth under wrong_bin_len should error")
	}
	if _, _, err := emb.EmbedBatch(context.Background(), []string{"a"}); err == nil {
		t.Error("EmbedBatch under wrong_bin_len should error")
	}
}

func TestJinaCodeEmbeddings_Malform_WrongFpLen(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "wrong_fp_len")
	if _, err := emb.EmbedFP32_1536d(context.Background(), "x"); err == nil {
		t.Error("EmbedFP32_1536d under wrong_fp_len should error")
	}
	if _, _, err := emb.EmbedBoth(context.Background(), "x"); err == nil {
		t.Error("EmbedBoth under wrong_fp_len should error")
	}
	if _, _, err := emb.EmbedBatch(context.Background(), []string{"a"}); err == nil {
		t.Error("EmbedBatch under wrong_fp_len should error")
	}
}

func TestJinaCodeEmbeddings_Malform_BadB64(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "bad_b64")
	if _, err := emb.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Error("EmbedBinary256d under bad_b64 should error")
	}
	if _, _, err := emb.EmbedBoth(context.Background(), "x"); err == nil {
		t.Error("EmbedBoth under bad_b64 should error")
	}
	if _, _, err := emb.EmbedBatch(context.Background(), []string{"a"}); err == nil {
		t.Error("EmbedBatch under bad_b64 should error")
	}
}

func TestJinaCodeEmbeddings_Malform_SubprocessError(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "subprocess_err")
	_, err := emb.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("EmbedBinary256d under subprocess_err should error")
	}
	if want := "synthetic subprocess error"; !strings.Contains(err.Error(), want) {
		t.Errorf("error msg %q does not contain %q", err.Error(), want)
	}
}

func TestJinaCodeEmbeddings_Malform_MalformedJSON(t *testing.T) {
	emb := newJinaMalformEmbedder(t, "malformed_json")
	if _, err := emb.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Error("EmbedBinary256d with malformed_json should error")
	}
}

func TestJinaCodeEmbeddings_EmbedBatch_CtxCancelledMidLoop(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	texts := []string{"a", "b", "c", "d", "e", "f"}
	_, _, err = emb.EmbedBatch(ctx, texts)

	if err != nil {
		t.Logf("EmbedBatch returned error (likely ctx canceled): %v", err)
	} else {
		t.Logf("EmbedBatch raced and completed before cancellation; that's also fine")
	}
}

func TestJinaCodeEmbeddings_AfterCloseRequest(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	if err := emb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = emb.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Error("EmbedBinary256d after Close should error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error msg %q should mention 'closed'", err.Error())
	}
}

// TestJinaCodeEmbeddings_SubprocessDeath covers the stdout-EOF branch in
// request(): the subprocess is killed externally; the next call MUST surface
// a read error rather than hang or panic.
func TestJinaCodeEmbeddings_SubprocessDeath(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()

	if emb.cmd != nil && emb.cmd.Process != nil {
		if err := emb.cmd.Process.Kill(); err != nil {
			t.Fatalf("kill subprocess: %v", err)
		}

		_ = emb.cmd.Wait()
	}
	if _, err := emb.EmbedBinary256d(context.Background(), "x"); err == nil {
		t.Error("EmbedBinary256d after subprocess kill should error")
	}
}

func findJinaShimScript(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	root := wd
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(root, "internal", "research", "ecosystem", "scripts", "zen_jina_embed.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		root = filepath.Dir(root)
	}
	t.Fatalf("could not locate scripts/zen_jina_embed.py from %s", wd)
	return ""
}

func TestVoyageCode3_InterfaceConformance(t *testing.T) {
	var _ Embedder = (*VoyageCode3)(nil)
}

func TestVoyageCode3_ConstructorValidatesRequiredFields(t *testing.T) {
	if _, err := NewVoyageCode3(VoyageCode3Options{Keychain: &fakeKeychain{}}); err == nil {
		t.Errorf("NewVoyageCode3 with nil Forwarder = nil err; want error")
	}
	if _, err := NewVoyageCode3(VoyageCode3Options{Forwarder: &fakeForwarder{}}); err == nil {
		t.Errorf("NewVoyageCode3 with nil Keychain = nil err; want error")
	}
}

func TestVoyageCode3_ConstructorAppliesDefaults(t *testing.T) {
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder: &fakeForwarder{},
		Keychain:  &fakeKeychain{},
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if v.opts.EnableFallback {
		t.Errorf("EnableFallback default = true; want false (privacy doctrine)")
	}
	if v.opts.BatchSize != 32 {
		t.Errorf("BatchSize default = %d; want 32 (Voyage API max)", v.opts.BatchSize)
	}
	if v.opts.MaxRetries != 3 {
		t.Errorf("MaxRetries default = %d; want 3", v.opts.MaxRetries)
	}
	if v.opts.RetryBackoff != 1*time.Second {
		t.Errorf("RetryBackoff default = %v; want 1s", v.opts.RetryBackoff)
	}
	if v.opts.TokenKey != "voyage-api-token" {
		t.Errorf("TokenKey default = %q; want voyage-api-token", v.opts.TokenKey)
	}
	if v.opts.TokenAccount != "zen-swarm" {
		t.Errorf("TokenAccount default = %q; want zen-swarm", v.opts.TokenAccount)
	}
}

func TestVoyageCode3_FallbackDisabled(t *testing.T) {
	fwd := &fakeForwarder{}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "should-never-be-read"},
		EnableFallback: false,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()

	if _, err := v.EmbedBinary256d(context.Background(), "x"); !errors.Is(err, ErrFallbackDisabled) {
		t.Errorf("EmbedBinary256d err=%v, want ErrFallbackDisabled", err)
	}
	if _, err := v.EmbedFP32_1536d(context.Background(), "x"); !errors.Is(err, ErrFallbackDisabled) {
		t.Errorf("EmbedFP32_1536d err=%v, want ErrFallbackDisabled", err)
	}
	if _, _, err := v.EmbedBoth(context.Background(), "x"); !errors.Is(err, ErrFallbackDisabled) {
		t.Errorf("EmbedBoth err=%v, want ErrFallbackDisabled", err)
	}
	if _, _, err := v.EmbedBatch(context.Background(), []string{"x"}); !errors.Is(err, ErrFallbackDisabled) {
		t.Errorf("EmbedBatch err=%v, want ErrFallbackDisabled", err)
	}
	if fwd.callCount != 0 {
		t.Errorf("Forwarder called %d times under fallback-disabled; want 0", fwd.callCount)
	}
}

func TestVoyageCode3_KeychainMissing(t *testing.T) {
	fwd := &fakeForwarder{}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{err: errors.New("item not found")},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedBinary256d(context.Background(), "x"); !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("EmbedBinary256d err=%v, want ErrKeychainTokenMissing", err)
	}
	if _, err := v.EmbedFP32_1536d(context.Background(), "x"); !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("EmbedFP32_1536d err=%v, want ErrKeychainTokenMissing", err)
	}
	if _, _, err := v.EmbedBoth(context.Background(), "x"); !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("EmbedBoth err=%v, want ErrKeychainTokenMissing", err)
	}
	if _, _, err := v.EmbedBatch(context.Background(), []string{"x"}); !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("EmbedBatch err=%v, want ErrKeychainTokenMissing", err)
	}
	if fwd.callCount != 0 {
		t.Errorf("Forwarder called %d times under missing-token; want 0", fwd.callCount)
	}
}

func TestVoyageCode3_KeychainEmptyTokenIsMissing(t *testing.T) {
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      &fakeForwarder{},
		Keychain:       &fakeKeychain{token: ""},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedBinary256d(context.Background(), "x"); !errors.Is(err, ErrKeychainTokenMissing) {
		t.Errorf("err=%v, want ErrKeychainTokenMissing", err)
	}
}

func TestVoyageCode3_KeychainTokenCached(t *testing.T) {
	kc := &fakeKeychain{token: "cached-token"}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder: &fakeForwarder{
			resp: voyageResponse{Data: []voyageEmbedding{{Embedding: makeTestFP32(1536), Index: 0}}},
		},
		Keychain:       kc,
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	for i := 0; i < 4; i++ {
		if _, err := v.EmbedFP32_1536d(context.Background(), "x"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if kc.callCount != 1 {
		t.Errorf("Keychain.GetGenericPassword called %d times; want 1 (cached)", kc.callCount)
	}
}

func TestVoyageCode3_EmbedBinary256d_Success(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	got, err := v.EmbedBinary256d(context.Background(), "func main() {}")
	if err != nil {
		t.Fatalf("EmbedBinary256d: %v", err)
	}
	if len(got) != 32 {
		t.Errorf("len=%d, want 32 (256 bits)", len(got))
	}

	wantBin := quantizeBinary256(wantFP32[:256])
	if string(got) != string(wantBin) {
		t.Errorf("cross-shape inconsistent: got=%x want=%x", got, wantBin)
	}
}

func TestVoyageCode3_EmbedFP32_1536d_Success(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	got, err := v.EmbedFP32_1536d(context.Background(), "x")
	if err != nil {
		t.Fatalf("EmbedFP32_1536d: %v", err)
	}
	if len(got) != 1536 {
		t.Errorf("len=%d, want 1536", len(got))
	}
	for i := range got {
		if got[i] != wantFP32[i] {
			t.Errorf("got[%d]=%f, want %f", i, got[i], wantFP32[i])
			break
		}
	}
}

func TestVoyageCode3_EmbedBoth_SingleRoundTrip(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	bin, fp32, err := v.EmbedBoth(context.Background(), "code")
	if err != nil {
		t.Fatalf("EmbedBoth: %v", err)
	}
	if len(bin) != 32 || len(fp32) != 1536 {
		t.Errorf("bin len=%d (want 32), fp32 len=%d (want 1536)", len(bin), len(fp32))
	}
	if fwd.callCount != 1 {
		t.Errorf("Forwarder called %d times, want 1 (single round-trip)", fwd.callCount)
	}

	if string(bin) != string(quantizeBinary256(fp32[:256])) {
		t.Errorf("cross-shape inconsistent in EmbedBoth")
	}
}

func TestVoyageCode3_EmbedBatch_RespectsBatchSize(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		respFn: func(req voyageRequest) voyageResponse {
			data := make([]voyageEmbedding, len(req.Input))
			for i := range req.Input {
				data[i] = voyageEmbedding{Embedding: wantFP32, Index: i}
			}
			return voyageResponse{Data: data}
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		BatchSize:      2,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	bins, fp32s, err := v.EmbedBatch(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(bins) != 5 || len(fp32s) != 5 {
		t.Errorf("bins=%d fp32s=%d, want 5 each", len(bins), len(fp32s))
	}
	if fwd.callCount != 3 {
		t.Errorf("Forwarder calls=%d, want 3 (ceil(5/2))", fwd.callCount)
	}

	if got := fwd.allReqs[2].Input; len(got) != 1 || got[0] != "e" {
		t.Errorf("3rd call input=%v, want [e]", got)
	}
	if got := fwd.allReqs[0].Input; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("1st call input=%v, want [a b]", got)
	}

	for i := range bins {
		if string(bins[i]) != string(quantizeBinary256(fp32s[i][:256])) {
			t.Errorf("cross-shape inconsistent at index %d", i)
		}
	}
}

func TestVoyageCode3_EmbedBatch_Empty(t *testing.T) {
	fwd := &fakeForwarder{}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	bins, fp32s, err := v.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Errorf("EmbedBatch(nil) err=%v, want nil", err)
	}
	if len(bins) != 0 || len(fp32s) != 0 {
		t.Errorf("bins=%d fp32s=%d, want 0 each", len(bins), len(fp32s))
	}
	if fwd.callCount != 0 {
		t.Errorf("Forwarder called %d times for empty input; want 0", fwd.callCount)
	}
}

func TestVoyageCode3_EmbedBatch_CtxCanceledMidLoop(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	ctx, cancel := context.WithCancel(context.Background())
	fwd := &fakeForwarder{
		respFn: func(req voyageRequest) voyageResponse {

			cancel()
			data := make([]voyageEmbedding, len(req.Input))
			for i := range req.Input {
				data[i] = voyageEmbedding{Embedding: wantFP32, Index: i}
			}
			return voyageResponse{Data: data}
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		BatchSize:      1,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, _, err = v.EmbedBatch(ctx, []string{"a", "b", "c"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("EmbedBatch err=%v, want context.Canceled", err)
	}
	if fwd.callCount >= 3 {
		t.Errorf("Forwarder called %d times; expected loop to abort early on ctx cancel", fwd.callCount)
	}
}

func TestVoyageCode3_RateLimitBackoff(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		errs: []error{
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rate limited"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rate limited"}`},
			nil,
		},
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     3,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedBinary256d(context.Background(), "x"); err != nil {
		t.Fatalf("EmbedBinary256d after retries: %v", err)
	}
	if fwd.callCount != 3 {
		t.Errorf("calls=%d, want 3 (2 retries + success)", fwd.callCount)
	}
}

func TestVoyageCode3_ServerErrorRetried(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		errs: []error{
			&VoyageHTTPError{StatusCode: 503, Body: `{"error":"unavailable"}`},
			nil,
		},
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     3,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedFP32_1536d(context.Background(), "x"); err != nil {
		t.Fatalf("EmbedFP32_1536d after 503 retry: %v", err)
	}
	if fwd.callCount != 2 {
		t.Errorf("calls=%d, want 2 (1 retry + success)", fwd.callCount)
	}
}

func TestVoyageCode3_TransportErrorRetried(t *testing.T) {
	wantFP32 := makeTestFP32(1536)
	fwd := &fakeForwarder{
		errs: []error{
			errors.New("connection refused"),
			nil,
		},
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: wantFP32, Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     3,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedFP32_1536d(context.Background(), "x"); err != nil {
		t.Fatalf("EmbedFP32_1536d after transport retry: %v", err)
	}
	if fwd.callCount != 2 {
		t.Errorf("calls=%d, want 2 (1 retry + success)", fwd.callCount)
	}
}

func TestVoyageCode3_AuthErrorWraps(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{&VoyageHTTPError{StatusCode: 401, Body: `{"error":"unauthorized"}`}},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "bad-token"},
		EnableFallback: true,
		MaxRetries:     0,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}
	var httpErr *VoyageHTTPError
	if !errors.As(err, &httpErr) {
		t.Errorf("err=%T, want VoyageHTTPError", err)
	} else if httpErr.StatusCode != 401 {
		t.Errorf("status=%d, want 401", httpErr.StatusCode)
	}
	if fwd.callCount != 1 {
		t.Errorf("calls=%d, want 1 (no retry on 401)", fwd.callCount)
	}
}

func TestVoyageCode3_BadRequestNotRetried(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{&VoyageHTTPError{StatusCode: 400, Body: `{"error":"invalid"}`}},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     5,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var httpErr *VoyageHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 400 {
		t.Errorf("err=%v, want 400 HTTP error", err)
	}
	if fwd.callCount != 1 {
		t.Errorf("calls=%d, want 1 (no retry on 400)", fwd.callCount)
	}
}

func TestVoyageCode3_RetriesExhausted(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rl1"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rl2"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rl3"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rl4"}`},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     2,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected exhausted error, got nil")
	}
	var httpErr *VoyageHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 429 {
		t.Errorf("err=%v, want last 429 HTTP error wrapped", err)
	}
	if fwd.callCount != 3 {
		t.Errorf("calls=%d, want 3 (initial + 2 retries)", fwd.callCount)
	}
}

func TestVoyageCode3_RetriesAbortOnCtxCancel(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{
			&VoyageHTTPError{StatusCode: 429, Body: ``},
			&VoyageHTTPError{StatusCode: 429, Body: ``},
			&VoyageHTTPError{StatusCode: 429, Body: ``},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     5,
		RetryBackoff:   500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = v.EmbedBinary256d(ctx, "x")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err=%v, want DeadlineExceeded (ctx kicked in during backoff)", err)
	}
}

func TestVoyageCode3_OutputDimensionParam(t *testing.T) {
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: makeTestFP32(1536), Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	if _, err := v.EmbedFP32_1536d(context.Background(), "x"); err != nil {
		t.Fatalf("EmbedFP32_1536d: %v", err)
	}
	if got := fwd.lastReq.OutputDimension; got != 1536 {
		t.Errorf("OutputDimension=%d, want 1536", got)
	}
	if got := fwd.lastReq.Model; got != "voyage-code-3" {
		t.Errorf("Model=%q, want voyage-code-3", got)
	}
	if got := fwd.lastReq.OutputDtype; got != "float" {
		t.Errorf("OutputDtype=%q, want float", got)
	}
	if got := fwd.lastReq.InputType; got != "document" {
		t.Errorf("InputType=%q, want document", got)
	}
	if got := fwd.lastReq.Input; len(got) != 1 || got[0] != "x" {
		t.Errorf("Input=%v, want [x]", got)
	}
}

func TestVoyageCode3_BadResponseShape(t *testing.T) {
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: makeTestFP32(512), Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedFP32_1536d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected dim-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "len=512, want 1536") {
		t.Errorf("err=%v, want dim-mismatch wording", err)
	}
}

func TestVoyageCode3_BadResponseIndexOutOfRange(t *testing.T) {
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: makeTestFP32(1536), Index: 99}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedFP32_1536d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected index-out-of-range error, got nil")
	}
	if !strings.Contains(err.Error(), "index 99") {
		t.Errorf("err=%v, want index-out-of-range wording", err)
	}
}

func TestVoyageCode3_BadResponseMalformedJSON(t *testing.T) {
	fwd := &fakeForwarder{rawResp: []byte(`{not valid json`)}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedFP32_1536d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal voyage resp") {
		t.Errorf("err=%v, want unmarshal wording", err)
	}
}

func TestVoyageCode3_BadResponseFewerThanRequestedDim(t *testing.T) {
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: makeTestFP32(128), Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected dim-mismatch error before quantize-panic, got nil")
	}
}

func TestVoyageCode3_EmbedBoth_FetchErrorPropagates(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{&VoyageHTTPError{StatusCode: 400, Body: `bad`}},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     0,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	bin, fp32, err := v.EmbedBoth(context.Background(), "x")
	if err == nil {
		t.Fatal("expected fetch error from EmbedBoth, got nil")
	}
	if bin != nil || fp32 != nil {
		t.Errorf("expected nil shapes on error; got bin=%v fp32=%v", bin, fp32)
	}
}

func TestVoyageCode3_EmbedBatch_FetchErrorPropagates(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{&VoyageHTTPError{StatusCode: 400, Body: `bad`}},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		BatchSize:      2,
		MaxRetries:     0,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, _, err = v.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err == nil {
		t.Fatal("expected fetch error from EmbedBatch, got nil")
	}
	if !strings.Contains(err.Error(), "EmbedBatch [0:2]:") {
		t.Errorf("err=%v, want [0:2] batch-index wrapping", err)
	}
}

func TestVoyageCode3_FetchOneRejectsZeroVecResponse(t *testing.T) {
	fwd := &fakeForwarder{
		resp: voyageResponse{
			Data: []voyageEmbedding{},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedFP32_1536d(context.Background(), "x")
	if err == nil {
		t.Fatal("expected vec-count error, got nil")
	}
	if !strings.Contains(err.Error(), "returned 0 vecs, want 1") {
		t.Errorf("err=%v, want vec-count wording", err)
	}
}

func TestVoyageCode3_CtxCancelledBeforeFetch(t *testing.T) {
	fwd := &fakeForwarder{}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = v.EmbedFP32_1536d(ctx, "x")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
	if fwd.callCount != 0 {
		t.Errorf("Forwarder called %d times after ctx cancel; want 0", fwd.callCount)
	}
}

func TestVoyageCode3_CloseIdempotent(t *testing.T) {
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder: &fakeForwarder{},
		Keychain:  &fakeKeychain{},
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := v.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestVoyageCode3_NegativeMaxRetriesIsCoerced(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{&VoyageHTTPError{StatusCode: 503, Body: `{"error":"service unavailable"}`}},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     -1,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, gotErr := v.EmbedBinary256d(context.Background(), "x")

	if fwd.callCount != 1 {
		t.Errorf("Forwarder.callCount=%d, want 1 (single attempt, no retry)", fwd.callCount)
	}
	if gotErr == nil {
		t.Fatal("expected 503 error to propagate; got nil")
	}
	// Error chain MUST be well-formed (no "%!w(<nil>)" — the bug under fix)
	if strings.Contains(gotErr.Error(), "%!w") {
		t.Errorf("error chain malformed (lastErr nil bug): %v", gotErr)
	}
	var httpErr *VoyageHTTPError
	if !errors.As(gotErr, &httpErr) {
		t.Errorf("errors.As(*VoyageHTTPError) failed: %v", gotErr)
	} else if httpErr.StatusCode != 503 {
		t.Errorf("StatusCode=%d, want 503", httpErr.StatusCode)
	}
}

func TestVoyageCode3_ZeroMaxRetriesDefaultsToThree(t *testing.T) {
	fwd := &fakeForwarder{
		errs: []error{
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rate limited"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rate limited"}`},
			&VoyageHTTPError{StatusCode: 429, Body: `{"error":"rate limited"}`},
			nil,
		},
		resp: voyageResponse{
			Data: []voyageEmbedding{{Embedding: makeTestFP32(1536), Index: 0}},
		},
	}
	v, err := NewVoyageCode3(VoyageCode3Options{
		Forwarder:      fwd,
		Keychain:       &fakeKeychain{token: "t"},
		EnableFallback: true,
		MaxRetries:     0,
		RetryBackoff:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewVoyageCode3: %v", err)
	}
	defer v.Close()
	_, err = v.EmbedBinary256d(context.Background(), "x")
	if err != nil {
		t.Fatalf("EmbedBinary256d after 3 retries + success: %v", err)
	}

	if fwd.callCount != 4 {
		t.Errorf("callCount=%d, want 4 (zero coerced to 3 retries + 1 initial)", fwd.callCount)
	}
}

type fakeForwarder struct {
	resp      voyageResponse
	respFn    func(req voyageRequest) voyageResponse
	rawResp   []byte
	errs      []error
	callCount int
	lastReq   voyageRequest
	allReqs   []voyageRequest
}

func (f *fakeForwarder) Forward(_ context.Context, body []byte) ([]byte, error) {
	f.callCount++
	var req voyageRequest
	_ = json.Unmarshal(body, &req)
	f.lastReq = req
	f.allReqs = append(f.allReqs, req)
	if len(f.errs) >= f.callCount {
		if err := f.errs[f.callCount-1]; err != nil {
			return nil, err
		}
	}
	if len(f.rawResp) > 0 {
		return f.rawResp, nil
	}
	resp := f.resp
	if f.respFn != nil {
		resp = f.respFn(req)
	}
	return json.Marshal(resp)
}

type fakeKeychain struct {
	token     string
	err       error
	callCount int
}

func (f *fakeKeychain) GetGenericPassword(_, _ string) (string, error) {
	f.callCount++
	if f.err != nil {
		return "", f.err
	}
	return f.token, nil
}

func makeTestFP32(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		if i%2 == 0 {
			out[i] = float32(i) / float32(n)
		} else {
			out[i] = -float32(i) / float32(n)
		}
	}
	return out
}

func TestJinaCodeEmbeddings_EmbedBatch_OrderPreservation_AcrossBatches(t *testing.T) {
	scriptPath := findJinaShimScript(t)
	var callCount int
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: 64,

		RequestHook: func(_ []string, _ string) { callCount++ },
	})
	if err != nil {
		t.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()
	const N = 130
	texts := make([]string, N)
	for i := range texts {
		texts[i] = "code_" + itoa(i)
	}
	bins, fp32s, err := emb.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(bins) != N || len(fp32s) != N {
		t.Fatalf("bins=%d fp32s=%d, want %d", len(bins), len(fp32s), N)
	}

	const wantCalls = 3
	if callCount != wantCalls {
		t.Errorf("subprocess call count = %d, want %d (BatchSize=64, N=130 → 3 chunks)",
			callCount, wantCalls)
	}

	callsBefore := callCount
	for _, i := range []int{0, 63, 64, 100, 129} {
		singleBin, singleFP, err := emb.EmbedBoth(context.Background(), texts[i])
		if err != nil {
			t.Fatalf("single EmbedBoth: %v", err)
		}
		if string(bins[i]) != string(singleBin) {
			t.Errorf("batch order broken at i=%d", i)
		}
		_ = singleFP
	}

	if callCount != callsBefore+5 {
		t.Errorf("EmbedBoth call count = %d, want %d (5 spot-checks)", callCount-callsBefore, 5)
	}
}

func BenchmarkJinaCodeEmbeddings_EmbedBatch_Throughput(b *testing.B) {
	scriptPath := findShimScriptBench(b)
	emb, err := NewJinaCodeEmbeddings(JinaCodeEmbeddingsOptions{
		PythonPath: "python3", ScriptPath: scriptPath, ShimMode: true,
		BatchSize: 64,
	})
	if err != nil {
		b.Fatalf("NewJinaCodeEmbeddings: %v", err)
	}
	defer emb.Close()
	texts := make([]string, 64)
	for i := range texts {
		texts[i] = "package main\nfunc f" + itoa(i) + "() {}"
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := emb.EmbedBatch(context.Background(), texts); err != nil {
			b.Fatalf("EmbedBatch: %v", err)
		}
	}

	chunksPerSec := float64(b.N*64) / b.Elapsed().Seconds()
	b.ReportMetric(chunksPerSec, "chunks/sec")
	b.ReportMetric(chunksPerSec*60.0, "chunks/min")
}

func findShimScriptBench(b *testing.B) string {
	b.Helper()
	wd, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		c := filepath.Join(wd, "internal", "research", "ecosystem", "scripts", "zen_jina_embed.py")
		if _, err := os.Stat(c); err == nil {
			return c
		}
		wd = filepath.Dir(wd)
	}
	b.Fatal("could not locate scripts/zen_jina_embed.py")
	return ""
}

func itoa(i int) string { return strconv.Itoa(i) }
