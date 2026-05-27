// SPDX-License-Identifier: MIT
// ecosystem_docs.go — release SHIPPED; backed by internal/research/ecosystem/Dispatcher.
//
// index, simple name/description/tag scoring). release Task F-1 rewires
// to the full corpus Dispatcher (embeddings, FTS5, RRF fusion, BGE reranker,
// Bayesian abstention, citation grammar validation, symbol verification cascade,
// LLM-judge re-pass) while preserving the existing Search() surface non-breakingly
// .
//
// Migration strategy:
// - Search() maps QueryResult.Chunks → []SourceHit (backward-compat shape so
// existing release callers dispatch.go, server.go and main.go see no API drift).
// - Query() delegates directly to Dispatcher.Query() (new full-RAG path,
// exposes citations, verification, abstention, provenance, audit-chain seq).
// - When Dispatcher is nil,
// both methods return zero results without error (graceful degradation,
// inv-hades-202 contract).
//
// Plan-file F-1 deviation — narrow-interface seam (ecosystemQueryer):
//
// The plan-file F-1 verbatim code typed the dispatcher field as
// *ecosystem.Dispatcher and referenced a `mockEcosystemDispatcher.asDispatcher()`
// helper located in F-9 (not yet shipped at F-1 dispatch time, and which
// would require building a real *ecosystem.Dispatcher with stub embedder +
// stub reranker + stub aggregators + stub versionDetector). Constructing a
// real Dispatcher for unit tests requires more plumbing than the seam itself.
//
// F-1 introduces a one-method narrow seam `ecosystemQueryer` typed as the
// ecosystem_docs.go private dependency. *ecosystem.Dispatcher satisfies it
// trivially (it already has Query). Unit tests can implement it directly
// (see mockEcosystemDispatcher in ecosystem_docs_test.go). This matches the
// narrow-interface pattern proven at scale in D-4 (CohereForwarder),
// D-7 (AnswerGenerator), D-8 (JudgeBackend) — see HANDOFF lesson #5.
//
// Boundary (spec §3.5 + inv-hades-031): this file imports
// internal/research/ecosystem only. It never accesses ecosystem.db directly,
// never imports net/http, never imports internal/store. Confirmed by grep CI gate.
package research

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/research/ecosystem"
)

var (
	ErrEcosystemDocsEmptyQuery = errors.New("research/ecosystem_docs: query is empty")

	ErrEcosystemDocsEmptyEcosystem = errors.New("research/ecosystem_docs: ecosystem is empty")
)

type ecosystemQueryer interface {
	Query(ctx context.Context, req ecosystem.QueryRequest) (*ecosystem.QueryResult, error)
}

type EcosystemDocsOptions struct {
	Dispatcher ecosystemQueryer

	MaxHits int
}

type EcosystemDocs struct {
	disp ecosystemQueryer
	max  int
}

func NewEcosystemDocs(opts EcosystemDocsOptions) *EcosystemDocs {
	max := opts.MaxHits
	if max == 0 {
		max = 20
	}
	disp := opts.Dispatcher
	if v := reflect.ValueOf(disp); v.IsValid() && v.Kind() == reflect.Ptr && v.IsNil() {
		disp = nil
	}
	return &EcosystemDocs{disp: disp, max: max}
}

var _ EcosystemBackend = (*EcosystemDocs)(nil)

func (e *EcosystemDocs) Search(ctx context.Context, query, eco string) ([]SourceHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, ErrEcosystemDocsEmptyQuery
	}
	if eco == "" {
		return nil, ErrEcosystemDocsEmptyEcosystem
	}
	if e.disp == nil {
		return nil, nil
	}
	req := ecosystem.QueryRequest{
		Query:      query,
		Ecosystem:  ecosystem.Ecosystem(eco),
		MaxResults: e.max,
		Scope:      ecosystem.ScopeAll,
	}
	res, err := e.disp.Query(ctx, req)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return chunksToSourceHits(res.Chunks), nil
}

func (e *EcosystemDocs) Query(ctx context.Context, req ecosystem.QueryRequest) (*ecosystem.QueryResult, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, ErrEcosystemDocsEmptyQuery
	}
	if req.Ecosystem == "" {
		return nil, ErrEcosystemDocsEmptyEcosystem
	}
	if e.disp == nil {
		return nil, nil
	}
	return e.disp.Query(ctx, req)
}

func chunksToSourceHits(chunks []ecosystem.QueryChunk) []SourceHit {
	hits := make([]SourceHit, 0, len(chunks))
	for _, c := range chunks {
		score := c.SimilarityScore
		if c.RerankerScore > 0 {
			score = c.RerankerScore
		}
		hits = append(hits, SourceHit{
			Source:  "ecosystem_docs",
			URL:     c.SourceURL,
			Title:   c.SymbolPath,
			Excerpt: c.ContentText,
			Score:   score,
		})
	}
	return hits
}
