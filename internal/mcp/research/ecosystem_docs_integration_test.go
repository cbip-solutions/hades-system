// ecosystem_docs_integration_test.go — inv-zen-202 compliance + Plan 4 regression gate.
//
// This file lives in the external test package (research_test) so it exercises the
// EcosystemDocs adapter through the SAME public surface that Plan 4 callers see
// (dispatch.go fan-out aggregator + server.go tool registration). The internal
// test file ecosystem_docs_test.go exercises the unexported ecosystemQueryer seam
// directly; this file exercises the contract a consumer sees.
//
// inv-zen-202 three-place enforcement (per plan-file §3.6 "Plan 4 partial replacement
// migration"):
//
//  1. Compile-time assertion (HERE, package-level): *research.EcosystemDocs satisfies
//     research.EcosystemBackend post-F-2 (the interface gained the Query() method
//     in F-2 — this gate catches breakage at build time if the production type drifts).
//  2. Runtime integration test (HERE, TestInvZen202SearchPathPreservesSourceHitShape):
//     Search() maps QueryResult.Chunks → []SourceHit preserving the Plan 4 SourceHit
//     schema (Source / URL / Title / Excerpt / Score) so the fan-out aggregator in
//     dispatch.go does not need to be touched at F-1.
//  3. Runtime integration test (HERE, TestInvZen202QueryPathReturnsFullQueryResult):
//     Query() delegates the full Plan 14 *QueryResult by pointer (citations,
//     verification, abstention, provenance, audit-chain seq preserved — no lossy
//     mapping in the adapter path).
//  4. Runtime integration test (HERE, TestInvZen202NilDispatcherGracefulDegradation):
//     nil Dispatcher returns zero results without panic — the contract that lets
//     the daemon boot before Phase F dispatcher wiring is completed (Plan 4 MCP
//     stays operational, returning empty results, not a hard error).
//
// Boundary (inv-zen-031): this test imports internal/research/ecosystem (the Plan 14
// dispatcher canonical types) and internal/mcp/research (the Plan 4 adapter). It MUST
// NOT import internal/store, net/http, or the production Dispatcher concrete type.
// Spec §3.5 mandates the adapter is the single integration point.
//
// Plan-file F-9 deviation rationale (verified at Stage 0):
//
//	The plan-file F-9 verbatim code typed `EcosystemDocsOptions.Dispatcher` as
//	*ecosystem.Dispatcher and assumed a Phase D test-helper `NewDispatcherForTest`
//	would exist. Reality at Stage 0:
//	  - F-1 typed the field as the unexported `ecosystemQueryer` narrow seam
//	    (see ecosystem_docs.go package-doc explaining the deviation).
//	  - Phase D ships NO `NewDispatcherForTest` helper; constructing a real
//	    *ecosystem.Dispatcher requires Embedder + Reranker + Router + Verifier
//	    + AbstentionPolicy + versionDetector + aggregators wired post-construction.
//	F-9 sidesteps both problems by providing a local test stub that satisfies the
//	narrow `Query(ctx, QueryRequest) (*QueryResult, error)` seam structurally. Go
//	interface satisfaction is structural — the external test package can pass a
//	value implementing the seam's method set even when the interface itself is
//	unexported. This is the same pattern Phase D D-4 / D-7 / D-8 use.
package research_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cbip-solutions/hades-system/internal/mcp/research"
	ecosystemtypes "github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

var _ research.EcosystemBackend = (*research.EcosystemDocs)(nil)

type stubEcosystemDispatcher struct {
	result *ecosystemtypes.QueryResult
	err    error
}

func (s *stubEcosystemDispatcher) Query(_ context.Context, _ ecosystemtypes.QueryRequest) (*ecosystemtypes.QueryResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestInvZen202SearchPathPreservesSourceHitShape(t *testing.T) {
	stub := &stubEcosystemDispatcher{
		result: &ecosystemtypes.QueryResult{
			Chunks: []ecosystemtypes.QueryChunk{
				{
					SymbolPath:      "crypto/sha256.Sum256",
					PackageName:     "crypto/sha256",
					Version:         "1.22.0",
					Kind:            ecosystemtypes.KindFunction,
					ContentText:     "Sum256 returns the SHA256 checksum of the data.",
					SourceURL:       "https://pkg.go.dev/crypto/sha256#Sum256",
					RerankerScore:   0.95,
					SimilarityScore: 0.88,
				},
			},
		},
	}
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{
		Dispatcher: stub,
		MaxHits:    10,
	})

	hits, err := e.Search(context.Background(), "sha256", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	h := hits[0]
	if h.Source != "ecosystem_docs" {
		t.Errorf("Source = %q, want ecosystem_docs", h.Source)
	}
	if h.URL != "https://pkg.go.dev/crypto/sha256#Sum256" {
		t.Errorf("URL = %q, want https://pkg.go.dev/crypto/sha256#Sum256", h.URL)
	}
	if h.Score != 0.95 {
		t.Errorf("Score = %v, want 0.95 (RerankerScore wins when > 0)", h.Score)
	}
	if h.Title == "" {
		t.Error("Title is empty — plan 4 callers may break (Title must be SymbolPath)")
	}
	if h.Title != "crypto/sha256.Sum256" {
		t.Errorf("Title = %q, want crypto/sha256.Sum256 (SymbolPath)", h.Title)
	}
	if h.Excerpt == "" {
		t.Error("Excerpt is empty — plan 4 callers may break (Excerpt must be ContentText)")
	}
	if h.Excerpt != "Sum256 returns the SHA256 checksum of the data." {
		t.Errorf("Excerpt = %q, want chunk ContentText", h.Excerpt)
	}
}

func TestInvZen202SearchPathScoreFallback(t *testing.T) {
	stub := &stubEcosystemDispatcher{
		result: &ecosystemtypes.QueryResult{
			Chunks: []ecosystemtypes.QueryChunk{
				{
					SymbolPath:      "fmt.Println",
					ContentText:     "Println formats using the default formats for its operands and writes to standard output.",
					SourceURL:       "https://pkg.go.dev/fmt#Println",
					RerankerScore:   0,
					SimilarityScore: 0.71,
				},
			},
		},
	}
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{Dispatcher: stub})
	hits, err := e.Search(context.Background(), "println", "go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Score != 0.71 {
		t.Errorf("Score = %v, want 0.71 (SimilarityScore fallback when RerankerScore <= 0)", hits[0].Score)
	}
}

func TestInvZen202QueryPathReturnsFullQueryResult(t *testing.T) {
	want := &ecosystemtypes.QueryResult{
		Chunks: []ecosystemtypes.QueryChunk{{SymbolPath: "fmt.Sprintf"}},
		Citations: []ecosystemtypes.CitationRef{
			{ID: "doc_1", ChunkID: 42, SymbolPath: "fmt.Sprintf", SourceURL: "https://pkg.go.dev/fmt#Sprintf"},
		},
		Abstained:     false,
		AbstainReason: "",
		Provenance: ecosystemtypes.QueryProvenance{
			DetectionLayer:    1,
			DetectedVersion:   "1.22.0",
			RoutingMethod:     "single",
			RoutingEcosystems: []ecosystemtypes.Ecosystem{ecosystemtypes.EcoGo},
		},
		AuditChainSeq: 1234,
	}
	stub := &stubEcosystemDispatcher{result: want}
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{Dispatcher: stub})

	req := ecosystemtypes.QueryRequest{Query: "sprintf", Ecosystem: ecosystemtypes.EcoGo}
	got, err := e.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got == nil {
		t.Fatal("got nil QueryResult")
	}

	if got != want {
		t.Errorf("Query result pointer mismatch: got %p, want %p (must delegate by pointer)", got, want)
	}
	if len(got.Chunks) != 1 || got.Chunks[0].SymbolPath != "fmt.Sprintf" {
		t.Errorf("unexpected chunks: %+v", got.Chunks)
	}
	if len(got.Citations) != 1 || got.Citations[0].ID != "doc_1" {
		t.Errorf("citations dropped or mutated: %+v", got.Citations)
	}
	if got.Provenance.DetectionLayer != 1 {
		t.Errorf("provenance.DetectionLayer = %d, want 1 (provenance must pass through)", got.Provenance.DetectionLayer)
	}
	if got.AuditChainSeq != 1234 {
		t.Errorf("AuditChainSeq = %d, want 1234 (audit seq must pass through)", got.AuditChainSeq)
	}
}

func TestInvZen202NilDispatcherGracefulDegradation(t *testing.T) {
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{})

	hits, err := e.Search(context.Background(), "x", "go")
	if err != nil {
		t.Fatalf("Search unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search nil dispatcher: want 0 hits, got %d", len(hits))
	}

	result, err := e.Query(context.Background(), ecosystemtypes.QueryRequest{
		Query:     "x",
		Ecosystem: ecosystemtypes.EcoGo,
	})
	if err != nil {
		t.Fatalf("Query unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("Query nil dispatcher: want nil result, got %+v", result)
	}
}

func TestInvZen202SearchEmptyChunksYieldsZeroHits(t *testing.T) {
	stub := &stubEcosystemDispatcher{
		result: &ecosystemtypes.QueryResult{Chunks: nil},
	}
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{Dispatcher: stub})
	hits, err := e.Search(context.Background(), "no-such-symbol", "go")
	if err != nil {
		t.Fatalf("Search unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("empty chunks: want 0 hits, got %d", len(hits))
	}
}

func TestInvZen202DispatcherErrorPropagates(t *testing.T) {
	want := errors.New("ecosystem dispatcher: synthetic rerank failure")
	stub := &stubEcosystemDispatcher{err: want}
	e := research.NewEcosystemDocs(research.EcosystemDocsOptions{Dispatcher: stub})

	_, err := e.Search(context.Background(), "x", "go")
	if !errors.Is(err, want) {
		t.Errorf("Search err = %v, want errors.Is(%v)", err, want)
	}

	_, err = e.Query(context.Background(), ecosystemtypes.QueryRequest{Query: "x", Ecosystem: ecosystemtypes.EcoGo})
	if !errors.Is(err, want) {
		t.Errorf("Query err = %v, want errors.Is(%v)", err, want)
	}
}
