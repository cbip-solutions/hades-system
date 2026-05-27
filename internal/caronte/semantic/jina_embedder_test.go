// go:build cgo && !nocgo
//go:build cgo && !nocgo
// +build cgo,!nocgo

package semantic

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
)

type stubProber struct {
	err error
}

func (s stubProber) probe(_ context.Context, _ string) error { return s.err }

func reachableProber(_ context.Context, _ string) error { return nil }

func unreachableProber(_ context.Context, _ string) error {
	return errors.New("test prober: unreachable")
}

type fakeONNXSession struct {
	dim       int
	closeErr  error
	closeOnce sync.Once
	closed    atomic.Bool

	embedCalls atomic.Int64
}

func (f *fakeONNXSession) Run(_ context.Context, inputIDs []int64) ([]float32, error) {
	f.embedCalls.Add(1)
	if f.closed.Load() {
		return nil, errors.New("fake session closed")
	}
	if f.dim <= 0 {
		f.dim = 1536
	}

	v := make([]float32, f.dim)
	var sum int64
	for _, id := range inputIDs {
		sum += id
	}
	hot := int(uint64(sum) % uint64(f.dim))
	if hot < 0 {
		hot = -hot
	}
	v[hot] = 2.0
	for i := range v {
		if i != hot {
			v[i] = 0.001
		}
	}
	return v, nil
}

func (f *fakeONNXSession) Close() error {
	f.closeOnce.Do(func() {
		f.closed.Store(true)
	})
	return f.closeErr
}

type fakeTokenizer struct{}

func (fakeTokenizer) Encode(text string) ([]int64, error) {
	if text == "" {
		return []int64{0}, nil
	}
	out := make([]int64, 0, len(text))
	for _, r := range text {
		out = append(out, int64(r))
	}
	return out, nil
}

func (fakeTokenizer) Close() error { return nil }

func withFakeSession(t *testing.T, session *fakeONNXSession) {
	t.Helper()
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = func(_ string) (onnxSession, error) {
		return session, nil
	}
	newTokenizer = func(_ string) (tokenizer, error) {
		return fakeTokenizer{}, nil
	}
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})
}

func withFakeSessionConstructor(t *testing.T, ctor func(string) (onnxSession, error)) {
	t.Helper()
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = ctor
	newTokenizer = func(_ string) (tokenizer, error) {
		return fakeTokenizer{}, nil
	}
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})
}

func writeFakeModel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "model.onnx")
	if err := os.WriteFile(path, []byte("fake-onnx-bytes"), 0o600); err != nil {
		t.Fatalf("writeFakeModel: %v", err)
	}
	return path
}

func TestNewJinaEmbedderMissingModel(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	cfg := EmbedderConfig{JinaModelPath: "/nonexistent/jina/model.onnx"}
	_, err := NewJinaEmbedder(cfg)
	if !errors.Is(err, ErrJinaModelMissing) {
		t.Fatalf("NewJinaEmbedder(missing) err = %v; want ErrJinaModelMissing", err)
	}
}

func TestNewJinaEmbedderLoadsValidModel(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	cfg := EmbedderConfig{JinaModelPath: writeFakeModel(t)}
	e, err := NewJinaEmbedder(cfg)
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	if e == nil {
		t.Fatal("NewJinaEmbedder returned nil embedder with nil error")
	}
	if got := e.Dimensions(); got != 1536 {
		t.Errorf("Dimensions() = %d; want 1536", got)
	}
	_ = e.Close()
}

func TestJinaEmbedderEmbedDims(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	v, err := e.Embed(context.Background(), "func main() {}")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 1536 {
		t.Fatalf("len(v) = %d; want 1536", len(v))
	}
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if math.Abs(sumSq-1.0) > 1e-3 {
		t.Errorf("unit-norm violated: sumSq = %f; want ~1.0", sumSq)
	}
}

func TestJinaEmbedderConcurrent(t *testing.T) {
	session := &fakeONNXSession{dim: 1536}
	withFakeSession(t, session)
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	const goroutines = 32
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, embErr := e.Embed(context.Background(), "text"+string(rune('a'+i%26)))
			if embErr != nil {
				errCh <- embErr
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent Embed err: %v", err)
	}
	if got := session.embedCalls.Load(); got != int64(goroutines) {
		t.Errorf("session.Run calls = %d; want %d", got, goroutines)
	}
}

func TestJinaEmbedderCloseIdempotent(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close #2 (idempotent): %v", err)
	}
}

func TestJinaEmbedderEmbedAfterCloseReturnsError(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	_ = e.Close()
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed after Close returned nil error; want non-nil")
	}
}

func TestJinaEmbedderRespectsContextCancellation(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Embed(ctx, "anything"); !errors.Is(err, context.Canceled) {
		t.Errorf("Embed(cancelled) = %v; want context.Canceled", err)
	}
}

func TestBM25OnlyNoopEmbedderReturnsSentinel(t *testing.T) {
	emb := NewBM25OnlyEmbedder()
	if emb == nil {
		t.Fatal("NewBM25OnlyEmbedder returned nil")
	}
	if got := emb.Dimensions(); got != 1536 {
		t.Errorf("BM25-only Dimensions = %d; want 1536 (parity with jina)", got)
	}
	_, err := emb.Embed(context.Background(), "anything")
	if !errors.Is(err, ErrEmbedderUnavailable) {
		t.Errorf("BM25-only Embed err = %v; want ErrEmbedderUnavailable", err)
	}
}

func TestDefaultSelectorChainJinaPresent(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	sel := NewDefaultSelector()
	cfg := EmbedderConfig{Mode: "auto", JinaModelPath: writeFakeModel(t)}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
	if mode != EmbedderJinaLocal {
		t.Errorf("mode = %q; want %q", mode, EmbedderJinaLocal)
	}

	if _, err := emb.Embed(context.Background(), "x"); err != nil {
		t.Errorf("jina-local Embed err = %v; want nil", err)
	}
}

func TestDefaultSelectorChainJinaMissingEcosystemSet(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	sel := NewDefaultSelectorWithProber(nil, reachableProber)
	cfg := EmbedderConfig{
		Mode:                 "auto",
		JinaModelPath:        "/nonexistent/jina/model.onnx",
		EcosystemMCPEndpoint: "http://stub-endpoint",
	}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
	if mode != EmbedderEcosystemMCP {
		t.Errorf("mode = %q; want %q", mode, EmbedderEcosystemMCP)
	}
}

func TestDefaultSelectorChainFullDegrade(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	logBuf := &threadSafeBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sel := NewDefaultSelectorWithLogger(logger)

	cfg := EmbedderConfig{
		Mode:                 "auto",
		JinaModelPath:        "/nonexistent/jina/model.onnx",
		EcosystemMCPEndpoint: "",
	}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
	if mode != EmbedderBM25Only {
		t.Errorf("mode = %q; want %q", mode, EmbedderBM25Only)
	}
	logs := logBuf.String()
	warnCount := strings.Count(logs, "level=WARN")
	if warnCount != 1 {
		t.Errorf("WARN count = %d; want 1 (one WARN at degrade-to-BM25)", warnCount)
	}
	if !strings.Contains(logs, "bm25-only") && !strings.Contains(logs, "BM25") {
		t.Errorf("WARN log missing bm25 mention: %s", logs)
	}
}

func TestDefaultSelectorChainPinnedMode(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	logBuf := &threadSafeBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sel := NewDefaultSelectorWithLogger(logger)

	cfg := EmbedderConfig{Mode: "bm25-only", JinaModelPath: writeFakeModel(t)}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select(bm25-only): %v", err)
	}
	if mode != EmbedderBM25Only {
		t.Errorf("mode = %q; want bm25-only", mode)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}

	logs := logBuf.String()
	if strings.Contains(logs, "level=WARN") {
		t.Errorf("pinned mode emitted WARN unexpectedly: %s", logs)
	}
}

func TestDefaultSelectorChainPinnedJinaLocal(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	sel := NewDefaultSelector()

	cfg := EmbedderConfig{Mode: "jina-local", JinaModelPath: "/nonexistent"}
	_, _, err := sel.Select(context.Background(), cfg)
	if err == nil {
		t.Fatal("Select(jina-local, missing model) returned nil error; want error (pinned modes refuse to degrade)")
	}
	if !errors.Is(err, ErrJinaModelMissing) {
		t.Errorf("err = %v; want ErrJinaModelMissing wrapped", err)
	}
}

func TestDefaultSelectorChainONNXRuntimeUnavailable(t *testing.T) {
	withFakeSessionConstructor(t, func(_ string) (onnxSession, error) {
		return nil, ErrONNXRuntimeUnavailable
	})
	logBuf := &threadSafeBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sel := NewDefaultSelectorWithLogger(logger)

	cfg := EmbedderConfig{Mode: "auto", JinaModelPath: writeFakeModel(t)}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if mode != EmbedderBM25Only {
		t.Errorf("mode = %q; want bm25-only", mode)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
}

func TestDefaultSelectorChainEcosystemMCPUnreachable(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	logBuf := &threadSafeBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sel := NewDefaultSelectorWithProber(logger, unreachableProber)

	cfg := EmbedderConfig{
		Mode:                 "auto",
		JinaModelPath:        "/nonexistent",
		EcosystemMCPEndpoint: "http://stub-endpoint",
	}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if mode != EmbedderBM25Only {
		t.Errorf("mode = %q; want bm25-only", mode)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
}

func TestJinaEmbedderSatisfiesCodeEmbedder(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	var _ intent.CodeEmbedder = e
}

func TestBM25OnlyEmbedderSatisfiesCodeEmbedder(t *testing.T) {
	emb := NewBM25OnlyEmbedder()
	var _ intent.CodeEmbedder = emb
}

type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var _ = runtime.GOOS

func TestNewJinaEmbedderEmptyPath(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	_, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: ""})
	if !errors.Is(err, ErrJinaModelMissing) {
		t.Errorf("NewJinaEmbedder(empty path) err = %v; want ErrJinaModelMissing", err)
	}
}

func TestNewJinaEmbedderStatNonExistError(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})

	_, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: "/dev/null/anything"})
	if err == nil {
		t.Fatal("NewJinaEmbedder(/dev/null/anything) returned nil error")
	}
	if errors.Is(err, ErrJinaModelMissing) {
		t.Errorf("err = %v; should be wrapped non-ENOENT, NOT ErrJinaModelMissing", err)
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("err does not mention stat: %v", err)
	}
}

func TestNewJinaEmbedderTokenizerFailure(t *testing.T) {
	closedSession := &fakeONNXSession{dim: 1536}
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = func(_ string) (onnxSession, error) { return closedSession, nil }
	newTokenizer = func(_ string) (tokenizer, error) {
		return nil, errors.New("tokenizer load failed")
	}
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})

	_, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err == nil {
		t.Fatal("NewJinaEmbedder(tokenizer fails) returned nil error")
	}
	if !closedSession.closed.Load() {
		t.Error("session not closed on tokenizer constructor failure (leak)")
	}
}

func TestJinaEmbedderEmbedEmptyText(t *testing.T) {
	session := &fakeONNXSession{dim: 1536}
	withFakeSession(t, session)
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	emptyTokTrigger := emptyTokenizer{}
	origTok := newTokenizer
	newTokenizer = func(_ string) (tokenizer, error) { return emptyTokTrigger, nil }
	t.Cleanup(func() { newTokenizer = origTok })

	e2, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder (empty-tok): %v", err)
	}
	t.Cleanup(func() { _ = e2.Close() })

	v, err := e2.Embed(context.Background(), "anything")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 1536 {
		t.Errorf("len(v) = %d; want 1536", len(v))
	}
	for i, x := range v {
		if x != 0 {
			t.Errorf("v[%d] = %f; want 0 (empty-token deterministic zero-pad)", i, x)
			break
		}
	}
}

type emptyTokenizer struct{}

func (emptyTokenizer) Encode(string) ([]int64, error) { return []int64{}, nil }
func (emptyTokenizer) Close() error                   { return nil }

func TestJinaEmbedderTokenizeError(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 1536})
	origTok := newTokenizer
	newTokenizer = func(_ string) (tokenizer, error) {
		return errTokenizer{err: errors.New("tokenize boom")}, nil
	}
	t.Cleanup(func() { newTokenizer = origTok })

	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed returned nil error on tokenize failure")
	} else if !strings.Contains(err.Error(), "tokenize") {
		t.Errorf("err does not mention tokenize: %v", err)
	}
}

type errTokenizer struct{ err error }

func (e errTokenizer) Encode(string) ([]int64, error) { return nil, e.err }
func (e errTokenizer) Close() error                   { return nil }

func TestJinaEmbedderSessionRunError(t *testing.T) {
	failingSession := &erringSession{err: errors.New("run failed")}
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = func(_ string) (onnxSession, error) { return failingSession, nil }
	newTokenizer = func(_ string) (tokenizer, error) { return fakeTokenizer{}, nil }
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})

	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed returned nil error on session.Run failure")
	} else if !strings.Contains(err.Error(), "session") {
		t.Errorf("err does not mention session: %v", err)
	}
}

type erringSession struct {
	err error
}

func (e *erringSession) Run(_ context.Context, _ []int64) ([]float32, error) { return nil, e.err }
func (e *erringSession) Close() error                                        { return nil }

func TestJinaEmbedderSessionWrongDimensions(t *testing.T) {
	withFakeSession(t, &fakeONNXSession{dim: 768})
	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("Embed returned nil error on wrong-dim session output")
	} else if !strings.Contains(err.Error(), "1536") {
		t.Errorf("err does not mention 1536: %v", err)
	}
}

func TestL2NormalizeZeroVector(t *testing.T) {
	v := make([]float32, 1536)
	got := l2Normalize(v)
	for _, x := range got {
		if x != 0 {
			t.Errorf("l2Normalize(0) produced non-zero: %f", x)
			break
		}
	}
}

func TestNewDefaultSelectorWithNilLogger(t *testing.T) {
	sel := NewDefaultSelectorWithLogger(nil)
	if sel == nil {
		t.Fatal("NewDefaultSelectorWithLogger(nil) returned nil selector")
	}

	_, _, err := sel.Select(context.Background(), EmbedderConfig{Mode: "bm25-only"})
	if err != nil {
		t.Fatalf("Select after nil-logger fallback: %v", err)
	}
}

func TestSelectorPinnedEcosystemMCPEmptyEndpoint(t *testing.T) {
	sel := NewDefaultSelector()
	_, _, err := sel.Select(context.Background(), EmbedderConfig{Mode: "ecosystem-mcp"})
	if err == nil {
		t.Fatal("Select(ecosystem-mcp, empty endpoint) returned nil error")
	}
	if !strings.Contains(err.Error(), "ecosystem-mcp") {
		t.Errorf("err missing ecosystem-mcp tag: %v", err)
	}
}

func TestSelectorPinnedEcosystemMCPReachable(t *testing.T) {
	sel := NewDefaultSelectorWithProber(nil, reachableProber)
	cfg := EmbedderConfig{Mode: "ecosystem-mcp", EcosystemMCPEndpoint: "http://stub"}
	emb, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select(ecosystem-mcp, reachable): %v", err)
	}
	if mode != EmbedderEcosystemMCP {
		t.Errorf("mode = %q; want ecosystem-mcp", mode)
	}
	if emb == nil {
		t.Fatal("Select returned nil embedder")
	}
}

func TestSelectorPinnedEcosystemMCPUnreachable(t *testing.T) {
	sel := NewDefaultSelectorWithProber(nil, unreachableProber)
	cfg := EmbedderConfig{Mode: "ecosystem-mcp", EcosystemMCPEndpoint: "http://stub"}
	_, _, err := sel.Select(context.Background(), cfg)
	if err == nil {
		t.Fatal("Select(ecosystem-mcp, unreachable) returned nil error; pinned modes refuse to degrade")
	}
}

func TestEcosystemMCPProbeUnixSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := defaultEcosystemMCPProbe(ctx, "unix:///nonexistent/socket.sock")
	if err == nil {
		t.Fatal("defaultEcosystemMCPProbe(unix://nonexistent) returned nil error")
	}
}

func TestEcosystemMCPProbeUnixSocketReachable(t *testing.T) {
	sockPath := "/tmp/zen-d-probe-" + t.Name() + ".sock"
	if len(sockPath) > 100 {
		sockPath = "/tmp/zen-d-probe.sock"
	}
	_ = os.Remove(sockPath)
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix %s: %v", sockPath, err)
	}
	t.Cleanup(func() {
		_ = l.Close()
		_ = os.Remove(sockPath)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := defaultEcosystemMCPProbe(ctx, "unix://"+sockPath); err != nil {
		t.Errorf("defaultEcosystemMCPProbe(unix:// listening) = %v; want nil", err)
	}
}

func TestEcosystemMCPProbeBadScheme(t *testing.T) {
	err := defaultEcosystemMCPProbe(context.Background(), "ftp://example.com")
	if err == nil {
		t.Fatal("defaultEcosystemMCPProbe(ftp://) returned nil error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("err does not mention unsupported: %v", err)
	}
}

func TestEcosystemMCPProbeBadURL(t *testing.T) {
	err := defaultEcosystemMCPProbe(context.Background(), ":://bad")
	if err == nil {
		t.Fatal("defaultEcosystemMCPProbe(:://bad) returned nil error")
	}
}

func TestEcosystemMCPProbeHTTPReturnsNotWired(t *testing.T) {
	for _, scheme := range []string{"http://example.com", "https://example.com"} {
		err := defaultEcosystemMCPProbe(context.Background(), scheme)
		if err == nil {
			t.Errorf("defaultEcosystemMCPProbe(%s) returned nil error; want ErrEcosystemMCPProbeNotWired", scheme)
		}
		if !errors.Is(err, ErrEcosystemMCPProbeNotWired) {
			t.Errorf("defaultEcosystemMCPProbe(%s) err = %v; want wraps ErrEcosystemMCPProbeNotWired", scheme, err)
		}
	}
}

func TestEcosystemMCPEmbedderEmbed(t *testing.T) {
	emb := newEcosystemMCPEmbedder("unix:///tmp/x.sock")
	if emb == nil {
		t.Fatal("newEcosystemMCPEmbedder returned nil")
	}
	if got := emb.Dimensions(); got != 1536 {
		t.Errorf("Dimensions = %d; want 1536", got)
	}
	_, err := emb.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedderUnavailable) {
		t.Errorf("Embed = %v; want wraps ErrEmbedderUnavailable", err)
	}
}

func TestSelectorAutoChainEcosystemTriedFirst(t *testing.T) {

	withFakeSessionConstructor(t, func(_ string) (onnxSession, error) {
		return nil, ErrONNXRuntimeUnavailable
	})

	sel := NewDefaultSelectorWithProber(nil, reachableProber)
	cfg := EmbedderConfig{
		Mode:                 "auto",
		JinaModelPath:        writeFakeModel(t),
		EcosystemMCPEndpoint: "http://stub",
	}
	_, mode, err := sel.Select(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if mode != EmbedderEcosystemMCP {
		t.Errorf("mode = %q; want ecosystem-mcp (jina runtime gone, ecosystem-mcp up)", mode)
	}
}

func TestSelectorUnknownModeRejected(t *testing.T) {
	sel := NewDefaultSelector()
	_, _, err := sel.Select(context.Background(), EmbedderConfig{Mode: "quantum"})
	if err == nil {
		t.Fatal("Select(quantum) returned nil error")
	}
}

func TestDefaultONNXSessionUnavailable(t *testing.T) {

	origSess := newONNXSession
	origTok := newTokenizer
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})

	newONNXSession = func(_ string) (onnxSession, error) {
		return nil, ErrONNXRuntimeUnavailable
	}
	newTokenizer = func(_ string) (tokenizer, error) {
		return nil, ErrONNXRuntimeUnavailable
	}
	_, err := newONNXSession("/anything")
	if !errors.Is(err, ErrONNXRuntimeUnavailable) {
		t.Errorf("default newONNXSession err = %v; want ErrONNXRuntimeUnavailable", err)
	}
	_, err = newTokenizer("/anything")
	if !errors.Is(err, ErrONNXRuntimeUnavailable) {
		t.Errorf("default newTokenizer err = %v; want ErrONNXRuntimeUnavailable", err)
	}
}

func TestJinaEmbedderCloseSurfacesErrors(t *testing.T) {
	failSess := &fakeONNXSession{dim: 1536, closeErr: errors.New("session close boom")}
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = func(_ string) (onnxSession, error) { return failSess, nil }
	newTokenizer = func(_ string) (tokenizer, error) {
		return errCloseTokenizer{closeErr: errors.New("tok close boom")}, nil
	}
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})

	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	err = e.Close()
	if err == nil {
		t.Fatal("Close swallowed session error")
	}
	if !strings.Contains(err.Error(), "session close boom") {
		t.Errorf("Close error did not propagate session error: %v", err)
	}
}

func TestJinaEmbedderCloseTokenizerOnlyError(t *testing.T) {
	origSess := newONNXSession
	origTok := newTokenizer
	newONNXSession = func(_ string) (onnxSession, error) {
		return &fakeONNXSession{dim: 1536}, nil
	}
	newTokenizer = func(_ string) (tokenizer, error) {
		return errCloseTokenizer{closeErr: errors.New("tok close boom")}, nil
	}
	t.Cleanup(func() {
		newONNXSession = origSess
		newTokenizer = origTok
	})

	e, err := NewJinaEmbedder(EmbedderConfig{JinaModelPath: writeFakeModel(t)})
	if err != nil {
		t.Fatalf("NewJinaEmbedder: %v", err)
	}
	err = e.Close()
	if err == nil {
		t.Fatal("Close swallowed tokenizer error")
	}
}

type errCloseTokenizer struct{ closeErr error }

func (e errCloseTokenizer) Encode(string) ([]int64, error) { return []int64{1, 2, 3}, nil }
func (e errCloseTokenizer) Close() error                   { return e.closeErr }
