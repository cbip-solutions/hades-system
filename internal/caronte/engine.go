//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package caronte

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/cbip-solutions/hades-system/internal/caronte/evolution"
	"github.com/cbip-solutions/hades-system/internal/caronte/intent"
	"github.com/cbip-solutions/hades-system/internal/caronte/semantic"
	"github.com/cbip-solutions/hades-system/internal/caronte/store"
	"github.com/cbip-solutions/hades-system/internal/caronte/structure"
	"github.com/cbip-solutions/hades-system/internal/mcp/research"
)

type Deps struct {
	OpenProjectDB func(ctx context.Context, projectID string) (*sql.DB, error)

	Dispatcher semantic.CaronteDispatcher

	Embedder intent.CodeEmbedder

	Selector semantic.Selector

	EmbedderConfig semantic.EmbedderConfig

	EmbedderLogger *slog.Logger

	Reranker intent.Reranker

	AuditEmit func(eventType string, payload []byte)

	Params evolution.ParamsAccessor

	IntentParams intent.IntentParams

	RepoRootFor func(ctx context.Context, projectID string) (string, error)

	FederationDB FederationStore
}

type Engine struct {
	deps Deps

	mu       sync.Mutex
	projects map[string]*projectEngine
	closed   bool
}

type projectEngine struct {
	projectID string
	repoRoot  string
	store     *store.Store

	resolver  *semantic.Resolver
	multiLang *semantic.MultiLangResolver
	builder   *evolution.Builder
	intent    *intent.Engine

	decompMu sync.Mutex
	decomp   structure.Decomposition
	decompOK bool
}

var _ research.GitnexusClient = (*Engine)(nil)

func NewEngine(d Deps) (*Engine, error) {
	if d.OpenProjectDB == nil {
		return nil, fmt.Errorf("caronte: NewEngine: nil OpenProjectDB (wiring bug)")
	}
	if d.Dispatcher == nil {
		return nil, fmt.Errorf("caronte: NewEngine: nil Dispatcher (wiring bug)")
	}

	logger := d.EmbedderLogger
	if logger == nil {
		logger = slog.Default()
	}

	if d.Embedder == nil {
		sel := d.Selector
		if sel == nil {
			sel = semantic.NewDefaultSelectorWithLogger(logger)
		}
		chosen, mode, err := sel.Select(context.Background(), d.EmbedderConfig)
		if err != nil {
			return nil, fmt.Errorf("caronte: NewEngine: embedder selection: %w", err)
		}
		if chosen == nil {
			return nil, fmt.Errorf("caronte: NewEngine: selector returned nil embedder (wiring bug)")
		}
		d.Embedder = chosen
		logger.InfoContext(context.Background(), "caronte.embedder.mode", "mode", string(mode))
	}

	if d.Embedder == nil {
		return nil, fmt.Errorf("caronte: NewEngine: nil Embedder (wiring bug)")
	}

	if d.AuditEmit == nil {
		d.AuditEmit = func(string, []byte) {}
	}
	if d.Params == nil {
		d.Params = staticDefaultParams{}
	}
	return &Engine{deps: d, projects: make(map[string]*projectEngine)}, nil
}

type staticDefaultParams struct{}

func (staticDefaultParams) CoChangeParams(string) evolution.Params { return evolution.DefaultParams() }

func (e *Engine) projectEngineFor(ctx context.Context, projectID string) (*projectEngine, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, ErrEngineClosed
	}
	if pe, ok := e.projects[projectID]; ok {
		return pe, nil
	}
	db, err := e.deps.OpenProjectDB(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrProjectUnavailable, projectID, err)
	}
	st, err := store.Open(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: store.Open: %v", ErrProjectUnavailable, projectID, err)
	}
	repoRoot := ""
	if e.deps.RepoRootFor != nil {

		if r, rerr := e.deps.RepoRootFor(ctx, projectID); rerr == nil {
			repoRoot = r
		}
	}
	semIdx, err := intent.NewSemanticIndexer(st, e.deps.Embedder, e.deps.Reranker, e.deps.IntentParams)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: NewSemanticIndexer: %v", ErrProjectUnavailable, projectID, err)
	}
	pe := &projectEngine{
		projectID: projectID,
		repoRoot:  repoRoot,
		store:     st,
		resolver:  semantic.NewResolver(st, e.deps.Dispatcher, semantic.ResolverOpts{}),
		multiLang: semantic.NewMultiLangResolver(st, nil, e.deps.Dispatcher, semantic.MultiLangOpts{}),
		builder:   evolution.NewBuilder(st, evolution.NewOSGitRunner(), e.deps.Params),

		intent: intent.NewEngine(st, semIdx, map[string]string{}),
	}
	e.projects[projectID] = pe
	return pe, nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var firstErr error
	for _, pe := range e.projects {
		if db := pe.store.DB(); db != nil {
			if err := db.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	e.projects = nil
	return firstErr
}

func (e *Engine) CodeGraph(ctx context.Context, query, projectID string) (research.CodeGraphResult, error) {
	pe, err := e.projectEngineFor(ctx, projectID)
	if err != nil {

		return research.CodeGraphResult{ProjectID: projectID}, err
	}
	hits, err := pe.searchSymbols(ctx, query, e.deps.Embedder)
	if err != nil {
		return research.CodeGraphResult{ProjectID: projectID}, err
	}
	return research.CodeGraphResult{Hits: hits, ProjectID: projectID}, nil
}

func (pe *projectEngine) searchSymbols(ctx context.Context, query string, emb intent.CodeEmbedder) ([]research.CodeGraphHit, error) {

	vec, err := emb.Embed(ctx, query)
	if err != nil {

		if errors.Is(err, semantic.ErrEmbedderUnavailable) {
			return pe.searchSymbolsBM25Only(ctx, query)
		}
		return nil, fmt.Errorf("caronte: searchSymbols embed: %w", err)
	}
	knn, err := pe.store.KNNNodeIDs(ctx, vec, 20)
	if err != nil {
		return nil, fmt.Errorf("caronte: searchSymbols knn: %w", err)
	}
	hits := make([]research.CodeGraphHit, 0, len(knn))
	for _, nd := range knn {

		score := 1.0 / (1.0 + nd.Distance)
		hits = append(hits, research.CodeGraphHit{
			Node:  nd.NodeID,
			Score: score,
			URL:   "caronte://" + pe.projectID + "/" + nd.NodeID,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Node < hits[j].Node
	})
	return hits, nil
}

func (pe *projectEngine) searchSymbolsBM25Only(ctx context.Context, query string) ([]research.CodeGraphHit, error) {
	const defaultK = 20
	rows, err := pe.store.LexicalSearchNodeIDs(ctx, query, defaultK)
	if err != nil {

		return nil, fmt.Errorf("caronte: searchSymbolsBM25Only: %w", err)
	}
	hits := make([]research.CodeGraphHit, 0, len(rows))
	for _, r := range rows {
		hits = append(hits, research.CodeGraphHit{
			Node:  r.NodeID,
			Score: r.Score,
			URL:   "caronte://" + pe.projectID + "/" + r.NodeID,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Node < hits[j].Node
	})
	return hits, nil
}
