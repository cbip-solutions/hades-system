// SPDX-License-Identifier: MIT
// Package aggregatorbridge provides AggregatorBridge, which wraps
// *aggregator.Aggregator and implements handlers.AggregatorService.
//
// This package lives outside internal/daemon so that the daemon package's
// test binaries are not contaminated by the mattn/go-sqlite3 CGO driver that
// internal/knowledge/aggregator imports via its db.go file. The daemon package
// itself imports ncruces/go-sqlite3 (via internal/store); if AggregatorBridge
// lived in internal/daemon, every daemon test binary would panic on startup
// with "sql: Register called twice for driver sqlite3".
//
// Import topology (inv-zen-031 compliant):
//
//	cmd/zen-swarm-ctld → aggregatorbridge (mattn)
//	cmd/zen-swarm-ctld → internal/daemon (ncruces via store)
//	aggregatorbridge   → internal/knowledge/aggregator (mattn via db.go CGO)
//	aggregatorbridge   → internal/daemon/handlers (no drivers)
//
// The production binary links both mattn and ncruces; mattn's init() runs
// first (it is imported deeper in the link graph via aggregator/db.go),
// registers "sqlite3", and ncruces silently skips its init() because
// driverName registration check detects the name already claimed.
//
// inv-zen-031: this package imports internal/knowledge/aggregator but NOT
//
//	internal/store. The daemon package + store coexist in the binary but
//	neither package directly imports the other via aggregatorbridge.
//
// inv-zen-129: does NOT import net/http.
package aggregatorbridge

import (
	"context"
	"errors"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/knowledge/aggregator"
)

type AggregatorBridge struct {
	agg *aggregator.Aggregator
}

var _ handlers.AggregatorService = (*AggregatorBridge)(nil)

func New(agg *aggregator.Aggregator) *AggregatorBridge {
	return &AggregatorBridge{agg: agg}
}

func (b *AggregatorBridge) AggQueryFTS(ctx context.Context, queryText string, limit int) ([]handlers.AggQueryResult, error) {
	results, err := b.agg.QueryFTS(ctx, queryText, limit)
	if err != nil {
		return nil, err
	}
	out := make([]handlers.AggQueryResult, 0, len(results))
	for _, r := range results {
		out = append(out, handlers.AggQueryResult{
			NoteID:           r.NoteID,
			Title:            r.Title,
			ProjectID:        r.ProjectID,
			Score:            r.Score,
			Snippet:          r.Snippet,
			AuditChainAnchor: r.AuditChainAnchor,
			Source:           r.Source,
		})
	}
	return out, nil
}

func (b *AggregatorBridge) AggPromote(ctx context.Context, noteID, projectID, operatorID, reason string) (*handlers.AggPromoteResult, error) {
	res, err := b.agg.Promote(ctx, noteID, projectID, operatorID, reason)
	if err != nil {
		if errors.Is(err, aggregator.ErrPromoteReasonRequired) {
			return nil, handlers.ErrAggPromoteReasonRequired
		}
		return nil, err
	}
	return &handlers.AggPromoteResult{
		NoteID:           res.NoteID,
		AuditChainAnchor: res.AuditChainAnchor,
		PromotedAt:       res.PromotedAt,
		Idempotent:       res.Idempotent,
	}, nil
}

func (b *AggregatorBridge) AggUnpromote(ctx context.Context, noteID, operatorID, reason string) (*handlers.AggUnpromoteResult, error) {
	res, err := b.agg.Unpromote(ctx, noteID, operatorID, reason)
	if err != nil {
		if errors.Is(err, aggregator.ErrPromoteReasonRequired) {
			return nil, handlers.ErrAggPromoteReasonRequired
		}
		return nil, err
	}
	return &handlers.AggUnpromoteResult{
		NoteID:       res.NoteID,
		UnpromotedAt: res.UnpromotedAt,
		Idempotent:   res.Idempotent,
	}, nil
}

func (b *AggregatorBridge) AggListPins(ctx context.Context, projectID string) ([]handlers.AggPinNote, error) {
	notes, err := b.agg.ListPins(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]handlers.AggPinNote, 0, len(notes))
	for _, n := range notes {
		out = append(out, handlers.AggPinNote{
			NoteID:           n.NoteID,
			ProjectID:        n.ProjectID,
			Title:            n.Title,
			Content:          n.Content,
			FrontmatterJSON:  n.FrontmatterJSON,
			PromotedAt:       n.PromotedAt,
			PromotedBy:       n.PromotedBy,
			PromoteReason:    n.PromoteReason,
			AuditChainAnchor: n.AuditChainAnchor,
		})
	}
	return out, nil
}

func (b *AggregatorBridge) AggEnqueueRebuild(ctx context.Context, projectID string) error {

	if err := b.agg.EnqueueRebuild(ctx, projectID); err != nil {
		if errors.Is(err, aggregator.ErrEmbedWorkerNotStarted) {
			return handlers.ErrAggWorkerNotStarted
		}
		return err
	}
	return nil
}
