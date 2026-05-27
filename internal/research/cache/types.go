//go:build cgo
// +build cgo

// SPDX-License-Identifier: MIT

package cache

import "time"

// CacheHitReason describes why a LookupResult was (or was not) a cache hit.
//
// Invariant the set of valid values is exactly the four constants below.
// New values MUST go through an ADR and schema migration before being
// added; the CHECK constraint on research_findings.cache_hit_reason at
// the SQL layer enforces the same closed set.
type CacheHitReason string

const (
	CacheHitExact CacheHitReason = "exact"

	CacheHitSemantic CacheHitReason = "semantic"

	CacheHitExpired CacheHitReason = "expired"

	CacheHitMiss CacheHitReason = "miss"
)

func (r CacheHitReason) IsValid() bool {
	switch r {
	case CacheHitExact, CacheHitSemantic, CacheHitExpired, CacheHitMiss:
		return true
	}
	return false
}

type FreshnessStatus string

const (
	FreshnessFresh FreshnessStatus = "FRESH"

	FreshnessStale FreshnessStatus = "STALE"

	FreshnessExpired FreshnessStatus = "EXPIRED"

	FreshnessUnknown FreshnessStatus = "UNKNOWN"
)

func (s FreshnessStatus) IsValid() bool {
	switch s {
	case FreshnessFresh, FreshnessStale, FreshnessExpired:
		return true
	}
	return false
}

type DispatchStatus string

const (
	DispatchStatusPending DispatchStatus = "PENDING"

	DispatchStatusRunning DispatchStatus = "RUNNING"

	DispatchStatusDone DispatchStatus = "DONE"

	DispatchStatusFailed DispatchStatus = "FAILED"
)

func (s DispatchStatus) IsValid() bool {
	switch s {
	case DispatchStatusPending, DispatchStatusRunning, DispatchStatusDone, DispatchStatusFailed:
		return true
	}
	return false
}

type Dispatch struct {
	ID string `json:"id"`

	Query string `json:"query"`

	Status DispatchStatus `json:"status"`

	CreatedAt int64 `json:"created_at"`

	UpdatedAt int64 `json:"updated_at"`
}

type Finding struct {
	ID string `json:"id"`

	DispatchID string `json:"dispatch_id"`

	URL string `json:"url"`

	Title string `json:"title"`

	Snippet string `json:"snippet"`

	Freshness FreshnessStatus `json:"freshness"`

	RetrievedAt int64 `json:"retrieved_at"`

	ContentHash string `json:"content_hash,omitempty"`

	BodyInlineBlob []byte `json:"body_inline_blob,omitempty"`

	BodyPath string `json:"body_path,omitempty"`

	SourceURLCanonical string `json:"source_url_canonical,omitempty"`

	RetrievalTimestamp time.Time `json:"retrieval_timestamp,omitempty"`

	LastValidatedAt *time.Time `json:"last_validated_at,omitempty"`
}

type ValidationLog struct {
	ID string `json:"id"`

	FindingID string `json:"finding_id"`

	Passed bool `json:"passed"`

	Note string `json:"note"`

	ValidatedAt int64 `json:"validated_at"`
}

type LookupResult struct {
	Hit bool `json:"hit"`

	HitReason CacheHitReason `json:"hit_reason"`

	FreshnessStatus FreshnessStatus `json:"freshness_status,omitempty"`

	Dispatch *Dispatch `json:"dispatch,omitempty"`

	Findings []Finding `json:"findings,omitempty"`
}
