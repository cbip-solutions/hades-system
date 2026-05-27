//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT
package plan9adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type KnowledgeAdapterDeps struct {
	Aggregator *aggregator.Aggregator
	Now        func() int64
}

type KnowledgeAdapter struct {
	agg *aggregator.Aggregator
	now func() int64
}

var _ handlers.KnowledgeAdapterP9 = (*KnowledgeAdapter)(nil)

func NewKnowledgeAdapter(deps KnowledgeAdapterDeps) (*KnowledgeAdapter, error) {
	if deps.Aggregator == nil {
		return nil, errors.New("plan9adapter: knowledge Aggregator is required")
	}
	now := deps.Now
	if now == nil {
		now = func() int64 { return time.Now().UTC().Unix() }
	}
	return &KnowledgeAdapter{agg: deps.Aggregator, now: now}, nil
}

func (a *KnowledgeAdapter) Query(ctx context.Context, req handlers.KnowledgeQueryReqP9) ([]handlers.KnowledgeResultP9, error) {
	scope := aggregator.Scope(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = aggregator.ScopeGlobal
	}
	if scope == aggregator.ScopeProject && strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("plan9adapter: knowledge query projectID required when scope=project")
	}
	rows, err := a.agg.Query(ctx, aggregator.QueryRequest{
		Text:             req.Query,
		Scope:            scope,
		ProjectID:        req.ProjectID,
		Limit:            req.Limit,
		AuditChainFilter: req.AuditChain,
	})
	if err != nil {
		return nil, err
	}
	out := make([]handlers.KnowledgeResultP9, 0, len(rows))
	for _, r := range rows {
		out = append(out, handlers.KnowledgeResultP9{
			NoteID:           r.NoteID,
			ProjectID:        r.ProjectID,
			Snippet:          r.Snippet,
			Score:            r.Score,
			AuditChainAnchor: r.AuditChainAnchor,
		})
	}
	return out, nil
}

func (a *KnowledgeAdapter) Promote(ctx context.Context, noteID, projectID, reason, operatorID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errors.New("plan9adapter: knowledge Promote: projectID required")
	}
	if strings.TrimSpace(operatorID) == "" {
		operatorID = "anonymous"
	}
	_, err := a.agg.Promote(ctx, noteID, projectID, operatorID, strings.TrimSpace(reason))
	return err
}

func (a *KnowledgeAdapter) Unpromote(ctx context.Context, noteID, _ string, reason, operatorID string) error {
	if strings.TrimSpace(operatorID) == "" {
		operatorID = "anonymous"
	}
	_, err := a.agg.Unpromote(ctx, noteID, operatorID, strings.TrimSpace(reason))
	return err
}

func (a *KnowledgeAdapter) List(ctx context.Context, projectID string, _ bool) ([]handlers.KnowledgeNoteP9, error) {
	pins, err := a.agg.ListPins(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]handlers.KnowledgeNoteP9, 0, len(pins))
	for _, p := range pins {
		out = append(out, handlers.KnowledgeNoteP9{
			NoteID:    p.NoteID,
			ProjectID: p.ProjectID,
			Pinned:    true,
			UpdatedAt: p.PromotedAt.UTC().Unix(),
		})
	}
	return out, nil
}

func (a *KnowledgeAdapter) Rebuild(ctx context.Context, projectID string) (handlers.KnowledgeRebuildRespP9, error) {
	rebuilt, err := a.agg.RebuildPinnedEmbeddings(ctx, projectID)
	if err != nil {
		return handlers.KnowledgeRebuildRespP9{}, err
	}
	started := a.now()
	return handlers.KnowledgeRebuildRespP9{
		JobID:        fmt.Sprintf("knowledge-rebuild-%s-%d", projectID, started),
		StartedAt:    started,
		RebuiltCount: rebuilt,
	}, nil
}
