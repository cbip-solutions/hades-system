package research

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

type mockEcosystemDispatcher struct {
	mu      sync.Mutex
	result  *ecosystem.QueryResult
	err     error
	lastReq *ecosystem.QueryRequest
}

func (m *mockEcosystemDispatcher) Query(_ context.Context, req ecosystem.QueryRequest) (*ecosystem.QueryResult, error) {
	m.mu.Lock()
	r := req
	m.lastReq = &r
	res := m.result
	err := m.err
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (m *mockEcosystemDispatcher) LastReq() ecosystem.QueryRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastReq == nil {
		return ecosystem.QueryRequest{}
	}
	return *m.lastReq
}

func (m *mockEcosystemDispatcher) HasLastReq() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastReq != nil
}

func TestEcosystemDocsNilDispatcherSearchReturnsEmpty(t *testing.T) {
	e := NewEcosystemDocs(EcosystemDocsOptions{})
	hits, err := e.Search(context.Background(), "context", "go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("nil dispatcher: want 0 hits, got %d", len(hits))
	}
}

func TestEcosystemDocsNilDispatcherQueryReturnsNil(t *testing.T) {
	e := NewEcosystemDocs(EcosystemDocsOptions{})
	res, err := e.Query(context.Background(), ecosystem.QueryRequest{Query: "x", Ecosystem: ecosystem.EcoGo})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Errorf("nil dispatcher: want nil result, got %+v", res)
	}
}

func TestEcosystemDocsSearchDelegatesViaDispatcher(t *testing.T) {
	mock := &mockEcosystemDispatcher{
		result: &ecosystem.QueryResult{
			Chunks: []ecosystem.QueryChunk{
				{SymbolPath: "context.Context", SourceURL: "https://pkg.go.dev/context", RerankerScore: 0.9, ContentText: "ctx"},
				{SymbolPath: "context.TODO", SourceURL: "https://pkg.go.dev/context#TODO", SimilarityScore: 0.7, ContentText: "todo"},
			},
		},
	}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock, MaxHits: 20})
	hits, err := e.Search(context.Background(), "context", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Score != 0.9 {
		t.Errorf("hits[0].Score = %v, want 0.9 (RerankerScore wins)", hits[0].Score)
	}
	if hits[1].Score != 0.7 {
		t.Errorf("hits[1].Score = %v, want 0.7 (SimilarityScore fallback)", hits[1].Score)
	}
	if hits[0].Source != "ecosystem_docs" {
		t.Errorf("hits[0].Source = %q, want ecosystem_docs", hits[0].Source)
	}
	if hits[0].URL != "https://pkg.go.dev/context" {
		t.Errorf("hits[0].URL = %q, want pkg.go.dev/context", hits[0].URL)
	}
	if hits[0].Title != "context.Context" {
		t.Errorf("hits[0].Title = %q, want context.Context (SymbolPath)", hits[0].Title)
	}
	if hits[0].Excerpt != "ctx" {
		t.Errorf("hits[0].Excerpt = %q, want ctx (ContentText)", hits[0].Excerpt)
	}

	if !mock.HasLastReq() {
		t.Fatal("mock.LastReq nil — Search did not delegate to Query")
	}
	last := mock.LastReq()
	if last.Query != "context" {
		t.Errorf("delegated Query = %q, want context", last.Query)
	}
	if last.Ecosystem != ecosystem.EcoGo {
		t.Errorf("delegated Ecosystem = %q, want go", last.Ecosystem)
	}
	if last.MaxResults != 20 {
		t.Errorf("delegated MaxResults = %d, want 20", last.MaxResults)
	}
	if last.Scope != ecosystem.ScopeAll {
		t.Errorf("delegated Scope = %q, want all", last.Scope)
	}
}

func TestEcosystemDocsQueryDelegatesDirectly(t *testing.T) {
	want := &ecosystem.QueryResult{
		Chunks:    []ecosystem.QueryChunk{{SymbolPath: "fmt.Println"}},
		Abstained: false,
	}
	mock := &mockEcosystemDispatcher{result: want}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	req := ecosystem.QueryRequest{Query: "print", Ecosystem: ecosystem.EcoGo}
	got, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got != want {
		t.Errorf("Query result mismatch: got %p, want %p", got, want)
	}

	if !mock.HasLastReq() {
		t.Fatal("mock.LastReq nil — Query did not delegate")
	}
	last := mock.LastReq()
	if last.Query != "print" || last.Ecosystem != ecosystem.EcoGo {
		t.Errorf("delegated request mismatch: got %+v", last)
	}
}

func TestEcosystemDocsEmptyQuerySearchErrors(t *testing.T) {
	mock := &mockEcosystemDispatcher{}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	_, err := e.Search(context.Background(), "", "go")
	if !errors.Is(err, ErrEcosystemDocsEmptyQuery) {
		t.Fatalf("empty-query err = %v, want errors.Is(ErrEcosystemDocsEmptyQuery)", err)
	}
	if mock.HasLastReq() {
		t.Error("Search should fail-fast on empty query (no dispatcher delegation)")
	}
}

func TestEcosystemDocsEmptyEcosystemSearchErrors(t *testing.T) {
	mock := &mockEcosystemDispatcher{}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	_, err := e.Search(context.Background(), "context", "")
	if !errors.Is(err, ErrEcosystemDocsEmptyEcosystem) {
		t.Fatalf("empty-eco err = %v, want errors.Is(ErrEcosystemDocsEmptyEcosystem)", err)
	}
	if mock.HasLastReq() {
		t.Error("Search should fail-fast on empty ecosystem (no dispatcher delegation)")
	}
}

func TestEcosystemDocsSearchPropagatesDispatcherError(t *testing.T) {
	want := errors.New("rerank failure")
	mock := &mockEcosystemDispatcher{err: want}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	_, err := e.Search(context.Background(), "context", "go")
	if !errors.Is(err, want) {
		t.Errorf("Search err = %v, want %v", err, want)
	}
}

func TestEcosystemDocsMaxHitsDefault(t *testing.T) {
	mock := &mockEcosystemDispatcher{
		result: &ecosystem.QueryResult{},
	}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	if _, err := e.Search(context.Background(), "q", "go"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !mock.HasLastReq() {
		t.Fatal("mock.LastReq nil — Search did not delegate")
	}
	if got := mock.LastReq().MaxResults; got != 20 {
		t.Errorf("MaxResults = %d, want 20 (default)", got)
	}
}

func TestEcosystemDocsInterfaceAssertion(t *testing.T) {
	var _ EcosystemBackend = (*EcosystemDocs)(nil)
}

func TestEcosystemDocsChunksWithZeroScores(t *testing.T) {
	mock := &mockEcosystemDispatcher{
		result: &ecosystem.QueryResult{
			Chunks: []ecosystem.QueryChunk{{SymbolPath: "zero", RerankerScore: 0, SimilarityScore: 0}},
		},
	}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	hits, err := e.Search(context.Background(), "q", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Score != 0 {
		t.Errorf("Score = %v, want 0", hits[0].Score)
	}
}

func TestEcosystemDocsEmptyChunks(t *testing.T) {
	mock := &mockEcosystemDispatcher{
		result: &ecosystem.QueryResult{Chunks: nil, Abstained: true, AbstainReason: "low confidence"},
	}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	hits, err := e.Search(context.Background(), "q", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits = %d, want 0 (abstained)", len(hits))
	}
}

func TestEcosystemDocsSearchHandlesNilQueryResult(t *testing.T) {
	mock := &mockEcosystemDispatcher{result: nil}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	hits, err := e.Search(context.Background(), "q", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hits != nil {
		t.Errorf("hits = %v, want nil (defensive nil-result path)", hits)
	}
}

// TestEcosystemDocsTypedNilDispatcherSearchGracefulDegrades guards the
// typed-nil interface pitfall: a caller passing
// `(*ecosystem.Dispatcher)(nil)` for opts.Dispatcher creates an interface
// wrapper that is non-nil even though the concrete pointer is nil.
// Without defensive unwrapping the `e.disp == nil` check passes and the
// subsequent `e.disp.Query(...)` invocation panics with nil-pointer deref.
// NewEcosystemDocs MUST unwrap typed-nil so the documented
// graceful-degradation contract holds for both untyped-nil
// and typed-nil inputs.
func TestEcosystemDocsTypedNilDispatcherSearchGracefulDegrades(t *testing.T) {
	var typedNil *ecosystem.Dispatcher = nil
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: typedNil})
	hits, err := e.Search(context.Background(), "context", "go")
	if err != nil {
		t.Fatalf("typed-nil dispatcher: want nil error, got %v", err)
	}
	if hits != nil {
		t.Errorf("typed-nil dispatcher: want nil hits, got %v", hits)
	}
}

func TestEcosystemDocsTypedNilDispatcherQueryGracefulDegrades(t *testing.T) {
	var typedNil *ecosystem.Dispatcher = nil
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: typedNil})
	res, err := e.Query(context.Background(), ecosystem.QueryRequest{Query: "x", Ecosystem: ecosystem.EcoGo})
	if err != nil {
		t.Fatalf("typed-nil dispatcher: want nil error, got %v", err)
	}
	if res != nil {
		t.Errorf("typed-nil dispatcher: want nil result, got %+v", res)
	}
}

func TestEcosystemDocsQueryEmptyQueryErrors(t *testing.T) {
	mock := &mockEcosystemDispatcher{}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	_, err := e.Query(context.Background(), ecosystem.QueryRequest{Query: "", Ecosystem: ecosystem.EcoGo})
	if !errors.Is(err, ErrEcosystemDocsEmptyQuery) {
		t.Fatalf("Query empty-query err = %v, want errors.Is(ErrEcosystemDocsEmptyQuery)", err)
	}
	if mock.LastReq().Query != "" {
		t.Errorf("validation should fail-fast before dispatcher delegation; got Query=%q", mock.LastReq().Query)
	}
}

func TestEcosystemDocsQueryEmptyEcosystemErrors(t *testing.T) {
	mock := &mockEcosystemDispatcher{}
	e := NewEcosystemDocs(EcosystemDocsOptions{Dispatcher: mock})
	_, err := e.Query(context.Background(), ecosystem.QueryRequest{Query: "context", Ecosystem: ""})
	if !errors.Is(err, ErrEcosystemDocsEmptyEcosystem) {
		t.Fatalf("Query empty-ecosystem err = %v, want errors.Is(ErrEcosystemDocsEmptyEcosystem)", err)
	}
}

func TestEcosystemDocsSearchEmptyQueryUsesSentinel(t *testing.T) {

	e := NewEcosystemDocs(EcosystemDocsOptions{})
	_, err := e.Search(context.Background(), "", "go")
	if !errors.Is(err, ErrEcosystemDocsEmptyQuery) {
		t.Fatalf("Search empty-query (nil disp) err = %v, want errors.Is(ErrEcosystemDocsEmptyQuery)", err)
	}
}

func TestEcosystemDocsSearchEmptyEcosystemUsesSentinel(t *testing.T) {
	e := NewEcosystemDocs(EcosystemDocsOptions{})
	_, err := e.Search(context.Background(), "context", "")
	if !errors.Is(err, ErrEcosystemDocsEmptyEcosystem) {
		t.Fatalf("Search empty-eco (nil disp) err = %v, want errors.Is(ErrEcosystemDocsEmptyEcosystem)", err)
	}
}
