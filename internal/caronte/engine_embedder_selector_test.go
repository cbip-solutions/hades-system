//go:build cgo
// +build cgo

package caronte

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

type stubSelector struct {
	emb        intent.CodeEmbedder
	mode       semantic.EmbedderMode
	err        error
	selectCall atomic.Int64
}

func (s *stubSelector) Select(_ context.Context, _ semantic.EmbedderConfig) (intent.CodeEmbedder, semantic.EmbedderMode, error) {
	s.selectCall.Add(1)
	if s.err != nil {
		return nil, "", s.err
	}
	return s.emb, s.mode, nil
}

type threadSafeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *threadSafeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *threadSafeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type embedderUnavailableEmbedder struct{}

func (embedderUnavailableEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, semantic.ErrEmbedderUnavailable
}
func (embedderUnavailableEmbedder) Dimensions() int { return 1536 }

func TestNewEngineUsesSelectorWhenEmbedderNil(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = nil
	stub := &stubSelector{
		emb:  fakeEmbedder{},
		mode: semantic.EmbedderJinaLocal,
	}
	deps.Selector = stub

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if stub.selectCall.Load() != 1 {
		t.Errorf("Selector.Select call count = %d; want 1", stub.selectCall.Load())
	}
	if e.deps.Embedder == nil {
		t.Fatal("e.deps.Embedder still nil after selector wiring")
	}

	dirs["proj-sel"] = t.TempDir()
	pe, err := e.projectEngineFor(context.Background(), "proj-sel")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}

	n := store.Node{
		NodeID: "pkg/x.Sel", Name: "Sel", Kind: string(store.KindFunction),
		Language: "go", FilePath: "pkg/x/sel.go", ContentHash: "h1",
	}
	if err := pe.store.UpsertNode(context.Background(), n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	emb, _ := fakeEmbedder{}.Embed(context.Background(), "Sel")
	if err := pe.store.UpsertNodeVector(context.Background(), n.NodeID, emb); err != nil {
		t.Fatalf("UpsertNodeVector: %v", err)
	}
	res, err := e.CodeGraph(context.Background(), "Sel", "proj-sel")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Error("CodeGraph returned no hits via selector-wired embedder")
	}
}

func TestNewEngineHonorsExplicitEmbedder(t *testing.T) {
	deps, _ := testDeps(t)

	stub := &stubSelector{
		emb:  embedderUnavailableEmbedder{},
		mode: semantic.EmbedderBM25Only,
	}
	deps.Selector = stub

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if got := stub.selectCall.Load(); got != 0 {
		t.Errorf("Selector.Select called %d times; explicit Embedder should bypass selector", got)
	}
}

func TestNewEngineSelectorErrorSurfaces(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Embedder = nil
	deps.Selector = &stubSelector{err: errors.New("selector boom")}

	_, err := NewEngine(deps)
	if err == nil {
		t.Fatal("NewEngine returned nil error when selector errored")
	}
	if !strings.Contains(err.Error(), "selector boom") {
		t.Errorf("err does not propagate selector error: %v", err)
	}
}

func TestNewEngineNilEmbedderNilSelectorUsesDefaultSelector(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Embedder = nil
	deps.Selector = nil

	logBuf := &threadSafeBuf{}
	deps.EmbedderLogger = slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	logs := logBuf.String()
	if !strings.Contains(logs, "caronte.embedder.mode") {
		t.Errorf("boot log missing caronte.embedder.mode: %s", logs)
	}
	modeLines := strings.Count(logs, "caronte.embedder.mode")
	if modeLines != 1 {
		t.Errorf("caronte.embedder.mode emitted %d times; want exactly 1", modeLines)
	}
}

func TestNewEngineLogsModeAtBoot(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Embedder = nil
	deps.Selector = &stubSelector{
		emb:  fakeEmbedder{},
		mode: semantic.EmbedderJinaLocal,
	}
	logBuf := &threadSafeBuf{}
	deps.EmbedderLogger = slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	logs := logBuf.String()
	if !strings.Contains(logs, "caronte.embedder.mode") || !strings.Contains(logs, "jina-local") {
		t.Errorf("boot log missing jina-local mode: %s", logs)
	}
}

func TestSearchSymbolsBM25FallbackOnSentinelError(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = embedderUnavailableEmbedder{}
	dirs["proj-bm25"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-bm25")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}

	n := store.Node{
		NodeID: "pkg/x.SearchSymbols", Name: "SearchSymbols",
		Kind: string(store.KindFunction), Language: "go",
		FilePath: "pkg/x/search.go", ContentHash: "h2",
		Signature: "func SearchSymbols(query string)",
		Doc:       "SearchSymbols ranks code symbols by lexical match.",
	}
	if err := pe.store.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	res, err := e.CodeGraph(ctx, "SearchSymbols", "proj-bm25")
	if err != nil {
		t.Fatalf("CodeGraph (BM25 fallback): %v", err)
	}
	var found bool
	for _, h := range res.Hits {
		if h.Node == "pkg/x.SearchSymbols" {
			found = true
		}
	}
	if !found {
		t.Errorf("BM25 fallback missed pkg/x.SearchSymbols; got %+v", res.Hits)
	}
}

func TestSearchSymbolsBM25FallbackURLShape(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = embedderUnavailableEmbedder{}
	dirs["proj-url"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-url")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}
	n := store.Node{
		NodeID: "pkg/a.LexFoo", Name: "LexFoo",
		Kind: string(store.KindFunction), Language: "go",
		FilePath: "pkg/a/lex.go", ContentHash: "u1",
	}
	if err := pe.store.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	res, err := e.CodeGraph(ctx, "LexFoo", "proj-url")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	for _, h := range res.Hits {
		if h.Node == "pkg/a.LexFoo" && h.URL != "caronte://proj-url/pkg/a.LexFoo" {
			t.Errorf("BM25 hit URL = %q; want canonical caronte://proj-url/pkg/a.LexFoo", h.URL)
		}
	}
}

func TestSearchSymbolsBM25FallbackEmptyResult(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = embedderUnavailableEmbedder{}
	dirs["proj-empty"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	res, err := e.CodeGraph(context.Background(), "noMatchTokenXyz", "proj-empty")
	if err != nil {
		t.Fatalf("CodeGraph(no-match): %v", err)
	}
	if res.ProjectID != "proj-empty" {
		t.Errorf("ProjectID = %q; want proj-empty", res.ProjectID)
	}
	if len(res.Hits) != 0 {
		t.Errorf("BM25 no-match returned %d hits; want 0", len(res.Hits))
	}
}

func TestSearchSymbolsRealEmbedErrorPreserved(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = realFailEmbedder{err: errors.New("ONNX session crashed")}
	dirs["proj-fail"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	_, err = e.CodeGraph(context.Background(), "anything", "proj-fail")
	if err == nil {
		t.Fatal("CodeGraph(real embed err) returned nil error; want wrapped")
	}
	if !strings.Contains(err.Error(), "ONNX session crashed") {
		t.Errorf("err does not include underlying message: %v", err)
	}
}

type realFailEmbedder struct{ err error }

func (e realFailEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, e.err
}
func (e realFailEmbedder) Dimensions() int { return 1536 }

func TestNewEngineSelectorReturnsNilEmbedder(t *testing.T) {
	deps, _ := testDeps(t)
	deps.Embedder = nil
	deps.Selector = nilEmbedderSelector{}
	_, err := NewEngine(deps)
	if err == nil {
		t.Fatal("NewEngine returned nil error when selector returned nil embedder")
	}
	if !strings.Contains(err.Error(), "nil embedder") {
		t.Errorf("err does not mention nil embedder: %v", err)
	}
}

type nilEmbedderSelector struct{}

func (nilEmbedderSelector) Select(_ context.Context, _ semantic.EmbedderConfig) (intent.CodeEmbedder, semantic.EmbedderMode, error) {
	return nil, semantic.EmbedderJinaLocal, nil
}

func TestSearchSymbolsBM25FallbackHitsSorted(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = embedderUnavailableEmbedder{}
	dirs["proj-sort"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx := context.Background()
	pe, err := e.projectEngineFor(ctx, "proj-sort")
	if err != nil {
		t.Fatalf("projectEngineFor: %v", err)
	}

	for _, id := range []string{"pkg/a.SortFoo", "pkg/a.SortBar", "pkg/a.SortBaz"} {
		n := store.Node{
			NodeID: id, Name: id[len("pkg/a."):], Kind: string(store.KindFunction),
			Language: "go", FilePath: "pkg/a/sort.go", ContentHash: id,
			Doc: "SortToken " + id[len("pkg/a."):],
		}
		if err := pe.store.UpsertNode(ctx, n); err != nil {
			t.Fatalf("UpsertNode %s: %v", id, err)
		}
	}

	res, err := e.CodeGraph(ctx, "SortToken", "proj-sort")
	if err != nil {
		t.Fatalf("CodeGraph: %v", err)
	}
	if len(res.Hits) < 2 {
		t.Skipf("BM25 returned %d hits (need ≥2 to assert ordering)", len(res.Hits))
	}

	for i := 1; i < len(res.Hits); i++ {
		if res.Hits[i].Score > res.Hits[i-1].Score {
			t.Errorf("hits not score-sorted at i=%d: %f > %f", i, res.Hits[i].Score, res.Hits[i-1].Score)
		}
	}
}

func TestSearchSymbolsBM25FallbackEnclosingProjectScope(t *testing.T) {
	deps, dirs := testDeps(t)
	deps.Embedder = embedderUnavailableEmbedder{}
	dirs["proj-a"] = t.TempDir()
	dirs["proj-b"] = t.TempDir()

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	ctx := context.Background()
	peA, _ := e.projectEngineFor(ctx, "proj-a")
	peB, _ := e.projectEngineFor(ctx, "proj-b")

	nA := store.Node{NodeID: "pkg/a.ScopeFoo", Name: "ScopeFoo", Kind: string(store.KindFunction), Language: "go", FilePath: "a.go", ContentHash: "ha"}
	nB := store.Node{NodeID: "pkg/b.ScopeBar", Name: "ScopeBar", Kind: string(store.KindFunction), Language: "go", FilePath: "b.go", ContentHash: "hb"}
	_ = peA.store.UpsertNode(ctx, nA)
	_ = peB.store.UpsertNode(ctx, nB)

	res, err := e.CodeGraph(ctx, "ScopeFoo", "proj-a")
	if err != nil {
		t.Fatalf("CodeGraph proj-a: %v", err)
	}
	for _, h := range res.Hits {
		if h.Node == "pkg/b.ScopeBar" {
			t.Errorf("BM25 result leaked across projects: %+v", h)
		}
	}
}

func TestEcosystemMCPProbeViaSelectorIntegration(t *testing.T) {
	reachable := func(_ context.Context, _ string) error { return nil }

	deps, _ := testDeps(t)
	deps.Embedder = nil
	deps.Selector = semantic.NewDefaultSelectorWithProber(nil, reachable)
	deps.EmbedderConfig = semantic.EmbedderConfig{
		Mode:                 "ecosystem-mcp",
		EcosystemMCPEndpoint: "http://stub",
	}

	e, err := NewEngine(deps)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
}
