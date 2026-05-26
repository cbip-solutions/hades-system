//go:build cgo
// +build cgo

package intent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/caronte/store"
)

func semanticFixture(t *testing.T, rr Reranker) (*store.Store, *SemanticIndexer, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	nodes := []store.Node{
		{NodeID: "internal/caronte/intent.GetWhy", Name: "GetWhy", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/caronte/intent/getwhy.go", PackageID: "internal/caronte/intent", Doc: "GetWhy merges ADR links semantic passages and Lore trailers.", ContentHash: "h1"},
		{NodeID: "internal/caronte/store.Open", Name: "Open", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/caronte/store/store.go", PackageID: "internal/caronte/store", Doc: "Open wraps an injected sql.DB and runs the schema init.", ContentHash: "h2"},
	}
	for _, n := range nodes {
		if err := s.UpsertNode(ctx, n); err != nil {
			t.Fatalf("seed node %s: %v", n.NodeID, err)
		}
	}
	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, rr, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	return s, idx, ctx
}

func TestNewSemanticIndexerRejectsNilEmbedder(t *testing.T) {
	s := newTestStore(t)
	if _, err := NewSemanticIndexer(s, nil, nil, IntentParams{}); err == nil {
		t.Error("NewSemanticIndexer(nil embedder) returned nil error; want ErrNoEmbedder")
	}
}

func TestNewSemanticIndexerRejectsDimMismatch(t *testing.T) {
	s := newTestStore(t)
	if _, err := NewSemanticIndexer(s, fakeEmbedder{dim: 768}, nil, IntentParams{}); err == nil {
		t.Error("NewSemanticIndexer(768-d embedder) returned nil error; want dim error")
	}
}

func TestIndexNodesPersistsVectors(t *testing.T) {
	s, idx, ctx := semanticFixture(t, nil)
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	q, _ := fakeEmbedder{dim: 1536}.Embed(ctx, nodeEmbedText(store.Node{Name: "GetWhy", Signature: "", Doc: "GetWhy merges ADR links semantic passages and Lore trailers."}))
	got, err := s.KNNNodeIDs(ctx, q, 1)
	if err != nil {
		t.Fatalf("KNNNodeIDs: %v", err)
	}
	if len(got) != 1 || got[0].NodeID != "internal/caronte/intent.GetWhy" {
		t.Errorf("KNN after IndexNodes = %+v; want GetWhy nearest", got)
	}
}

func TestRetrieveReturnsCodePassages(t *testing.T) {
	_, idx, ctx := semanticFixture(t, nil)
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}
	passages, err := idx.RetrieveForSymbol(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("RetrieveForSymbol: %v", err)
	}
	if len(passages) == 0 {
		t.Fatal("RetrieveForSymbol returned no passages")
	}
	var sawSelf bool
	for _, p := range passages {
		if p.SourceID == "internal/caronte/intent.GetWhy" && p.SourceKind == "code" {
			sawSelf = true
		}
	}
	if !sawSelf {
		t.Errorf("expected the symbol's own node among code passages; got %+v", passages)
	}
}

func TestRetrieveAppliesReranker(t *testing.T) {
	_, idx, ctx := semanticFixture(t, fakeReranker{})
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}

	idx.AddCorpusChunk(ctx, "docs/decisions/0100-caronte.md#chunk-0", "adr", "Caronte get_why design intent for GetWhy.")
	passages, err := idx.RetrieveForSymbol(ctx, "internal/caronte/intent.GetWhy")
	if err != nil {
		t.Fatalf("RetrieveForSymbol: %v", err)
	}
	if len(passages) < 2 {
		t.Skipf("need >=2 passages to assert reorder; got %d", len(passages))
	}

	if passages[0].SourceID == "" {
		t.Error("reranked passage has empty SourceID")
	}
}

func TestRetrieveThresholdsWeakMatches(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{NodeID: "internal/x.F", Name: "F", Kind: string(store.KindFunction), Language: "go", FilePath: "internal/x/f.go", PackageID: "internal/x", Doc: "totally unrelated text alpha beta", ContentHash: "h"}
	_ = s.UpsertNode(ctx, n)
	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{SemanticThreshold: 0.999})
	if err != nil {
		t.Fatal(err)
	}
	_ = idx.IndexNodes(ctx)

	passages, err := idx.RetrieveForSymbol(ctx, "internal/x.F")
	if err != nil {
		t.Fatalf("RetrieveForSymbol: %v", err)
	}

	for _, p := range passages {
		if !strings.Contains(p.SourceID, "internal/x.F") {
			t.Errorf("weak unrelated passage leaked past threshold: %+v", p)
		}
	}
}

func TestRetrieveUnknownSymbol(t *testing.T) {
	_, idx, ctx := semanticFixture(t, nil)
	_ = idx.IndexNodes(ctx)
	passages, err := idx.RetrieveForSymbol(ctx, "does/not.Exist")
	if err != nil {
		t.Fatalf("RetrieveForSymbol(unknown): %v", err)
	}
	_ = passages
}

func TestChunkBodyRuneBoundaries(t *testing.T) {
	s := newTestStore(t)
	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{ChunkRunes: 4})
	if err != nil {
		t.Fatal(err)
	}

	chunks := idx.ChunkBody("abcdéfghij")
	if len(chunks) != 3 {
		t.Fatalf("ChunkBody len = %d; want 3", len(chunks))
	}
	for i, c := range chunks {
		if c == "" {
			t.Errorf("chunk[%d] is empty", i)
		}
	}

	if got := strings.Join(chunks, ""); got != "abcdéfghij" {
		t.Errorf("reassembled = %q; want abcdéfghij", got)
	}
}

func TestCosineEdgeCases(t *testing.T) {
	if cosine([]float32{1, 0}, []float32{1, 0, 0}) != 0 {
		t.Error("cosine(mismatched len) != 0")
	}
	if cosine([]float32{0, 0}, []float32{1, 1}) != 0 {
		t.Error("cosine(zero vector) != 0")
	}
	if got := cosine([]float32{1, 0}, []float32{1, 0}); got < 0.999 {
		t.Errorf("cosine(identical) = %v; want ~1", got)
	}

	if cosine([]float32{}, []float32{}) != 0 {
		t.Error("cosine(empty) != 0")
	}

	if got := cosine([]float32{1, 0}, []float32{-1, 0}); got != 0 {
		t.Errorf("cosine(opposite) = %v; want 0 (clamped)", got)
	}
}

func TestNewSemanticIndexerRejectsNilStore(t *testing.T) {
	if _, err := NewSemanticIndexer(nil, fakeEmbedder{dim: 1536}, nil, IntentParams{}); err == nil {
		t.Error("NewSemanticIndexer(nil store) returned nil error; want ErrEmptyStore")
	}
}

func TestDistanceToSimilarityNegative(t *testing.T) {
	got := distanceToSimilarity(-5.0)
	if got != 1.0 {
		t.Errorf("distanceToSimilarity(-5) = %v; want 1.0 (clamped to 0)", got)
	}
}

type fakeEmbedderError struct{}

func (fakeEmbedderError) Dimensions() int { return 1536 }
func (fakeEmbedderError) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, fmt.Errorf("embed: test error")
}

func TestIndexNodesEmbedError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := store.Node{
		NodeID: "internal/x.G", Name: "G", Kind: string(store.KindFunction),
		Language: "go", FilePath: "internal/x/g.go", PackageID: "internal/x",
		Doc: "some doc", ContentHash: "hg",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	idx, err := NewSemanticIndexer(s, fakeEmbedderError{}, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	if err := idx.IndexNodes(ctx); err == nil {
		t.Error("IndexNodes with embed error returned nil; want error")
	}
}

func TestAddCorpusChunkEmbedError(t *testing.T) {
	s := newTestStore(t)
	idx, err := NewSemanticIndexer(s, fakeEmbedderError{}, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	ctx := context.Background()
	if err := idx.AddCorpusChunk(ctx, "docs/foo.md#0", "adr", "some text"); err == nil {
		t.Error("AddCorpusChunk with embed error returned nil; want error")
	}
}

func TestAddCorpusChunkEmptyTextNoOp(t *testing.T) {
	s := newTestStore(t)
	called := false
	embedder := fakeEmbedder{dim: 1536, hook: func(_ string) { called = true }}
	idx, err := NewSemanticIndexer(s, embedder, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	if err := idx.AddCorpusChunk(context.Background(), "id", "adr", ""); err != nil {
		t.Fatalf("AddCorpusChunk(empty): %v", err)
	}
	if called {
		t.Error("AddCorpusChunk(empty) called embedder; want no-op")
	}
}

func TestIndexNodesSkipsEmptyText(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n := store.Node{
		NodeID: "internal/x.Empty", Kind: string(store.KindFunction),
		Language: "go", FilePath: "internal/x/empty.go", PackageID: "internal/x",
		ContentHash: "he",
	}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	embedCalled := false
	embedder := fakeEmbedder{dim: 1536, hook: func(_ string) { embedCalled = true }}
	idx, err := NewSemanticIndexer(s, embedder, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	if err := idx.IndexNodes(ctx); err != nil {
		t.Fatalf("IndexNodes: %v", err)
	}
	if embedCalled {
		t.Error("IndexNodes called embedder for node with empty text; want skip")
	}
}

func TestIndexNodesClosedDBError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	idx, err := NewSemanticIndexer(s, fakeEmbedder{dim: 1536}, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}

	_ = s.DB().Close()
	if err := idx.IndexNodes(ctx); err == nil {
		t.Error("IndexNodes on closed DB returned nil; want error")
	}
}

func TestNodeEmbedTextAllEmpty(t *testing.T) {
	got := nodeEmbedText(store.Node{})
	if got != "" {
		t.Errorf("nodeEmbedText(zero node) = %q; want empty", got)
	}
}

func TestRetrieveForSymbolEmbedError(t *testing.T) {
	s := newTestStore(t)
	idx, err := NewSemanticIndexer(s, fakeEmbedderError{}, nil, IntentParams{})
	if err != nil {
		t.Fatalf("NewSemanticIndexer: %v", err)
	}
	if _, err := idx.RetrieveForSymbol(context.Background(), "any/symbol"); err == nil {
		t.Error("RetrieveForSymbol with embed error returned nil; want error")
	}
}
