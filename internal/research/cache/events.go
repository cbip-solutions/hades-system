// go:build cgo
//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	EventResearchDispatchInitiated = "research.dispatch_initiated"

	EventResearchCacheHitExact = "research.cache_hit_exact"

	EventResearchCacheHitSemantic = "research.cache_hit_semantic"

	EventResearchCacheRevalidatedFresh = "research.cache_revalidated_fresh"

	EventResearchCacheRevalidatedStaleRefetched = "research.cache_revalidated_stale_refetched"

	EventResearchFindingsReturned = "research.findings_returned"
)

type Sink interface {
	Emit(ctx context.Context, eventType string, payload []byte) error
}

type DispatchInitiatedPayload struct {
	DispatchID int64 `json:"dispatch_id"`

	ProjectID string `json:"project_id"`

	SessionID string `json:"session_id"`

	QueryHash string `json:"query_hash"`

	At time.Time `json:"at"`
}

type CacheHitPayload struct {
	DispatchID int64 `json:"dispatch_id"`

	ProjectID string `json:"project_id"`

	SessionID string `json:"session_id"`

	QueryHash string `json:"query_hash"`

	HitReason CacheHitReason `json:"hit_reason"`

	Freshness FreshnessStatus `json:"freshness"`

	At time.Time `json:"at"`
}

type RevalidatedPayload struct {
	FindingID int64 `json:"finding_id"`

	SourceURL string `json:"source_url"`

	HTTPStatus int `json:"http_status"`

	ETag string `json:"etag,omitempty"`

	LastModified string `json:"last_modified,omitempty"`

	OldContentHash string `json:"old_content_hash,omitempty"`

	NewContentHash string `json:"new_content_hash,omitempty"`

	At time.Time `json:"at"`
}

type FindingsReturnedPayload struct {
	DispatchID int64 `json:"dispatch_id"`

	ProjectID string `json:"project_id"`

	SessionID string `json:"session_id"`

	QueryHash string `json:"query_hash"`

	FindingCount int `json:"finding_count"`

	HitReason CacheHitReason `json:"hit_reason"`

	Freshness FreshnessStatus `json:"freshness"`

	At time.Time `json:"at"`
}

func EmitDispatchInitiated(ctx context.Context, sink Sink, dispatchID int64, projectID, sessionID, queryHash string, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchDispatchInitiated, DispatchInitiatedPayload{
		DispatchID: dispatchID,
		ProjectID:  projectID,
		SessionID:  sessionID,
		QueryHash:  queryHash,
		At:         at,
	})
}

func EmitCacheHitExact(ctx context.Context, sink Sink, dispatchID int64, projectID, sessionID, queryHash string, freshness FreshnessStatus, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchCacheHitExact, CacheHitPayload{
		DispatchID: dispatchID,
		ProjectID:  projectID,
		SessionID:  sessionID,
		QueryHash:  queryHash,
		HitReason:  CacheHitExact,
		Freshness:  freshness,
		At:         at,
	})
}

func EmitCacheHitSemantic(ctx context.Context, sink Sink, dispatchID int64, projectID, sessionID, queryHash string, freshness FreshnessStatus, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchCacheHitSemantic, CacheHitPayload{
		DispatchID: dispatchID,
		ProjectID:  projectID,
		SessionID:  sessionID,
		QueryHash:  queryHash,
		HitReason:  CacheHitSemantic,
		Freshness:  freshness,
		At:         at,
	})
}

func EmitRevalidatedFresh(ctx context.Context, sink Sink, findingID int64, sourceURL string, httpStatus int, etag, lastModified, oldContentHash, newContentHash string, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchCacheRevalidatedFresh, RevalidatedPayload{
		FindingID:      findingID,
		SourceURL:      sourceURL,
		HTTPStatus:     httpStatus,
		ETag:           etag,
		LastModified:   lastModified,
		OldContentHash: oldContentHash,
		NewContentHash: newContentHash,
		At:             at,
	})
}

func EmitRevalidatedStaleRefetched(ctx context.Context, sink Sink, findingID int64, sourceURL string, httpStatus int, etag, lastModified, oldContentHash, newContentHash string, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchCacheRevalidatedStaleRefetched, RevalidatedPayload{
		FindingID:      findingID,
		SourceURL:      sourceURL,
		HTTPStatus:     httpStatus,
		ETag:           etag,
		LastModified:   lastModified,
		OldContentHash: oldContentHash,
		NewContentHash: newContentHash,
		At:             at,
	})
}

func EmitFindingsReturned(ctx context.Context, sink Sink, dispatchID int64, projectID, sessionID, queryHash string, findingCount int, hitReason CacheHitReason, freshness FreshnessStatus, at time.Time) error {
	return emitJSON(ctx, sink, EventResearchFindingsReturned, FindingsReturnedPayload{
		DispatchID:   dispatchID,
		ProjectID:    projectID,
		SessionID:    sessionID,
		QueryHash:    queryHash,
		FindingCount: findingCount,
		HitReason:    hitReason,
		Freshness:    freshness,
		At:           at,
	})
}

func emitJSON(ctx context.Context, sink Sink, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {

		return fmt.Errorf("emitJSON %s: marshal: %w", eventType, err)
	}
	return sink.Emit(ctx, eventType, raw)
}
