// SPDX-License-Identifier: MIT
package inbox

import (
	"context"
	"time"
)

type CacheRow struct {
	CacheID        int64
	ProjectID      string
	ProjectAlias   string
	NotificationID int64
	Severity       Severity
	EventType      string
	ContentHash    string
	CreatedAt      time.Time
	AckedAt        *time.Time
}

// AggregatorCacheStore is the cache-side persistence contract. The
// production impl (`inboxadapter.Adapter`) writes to daemon.db
// inbox_aggregator_cache; the in-memory fake (`memCacheStore`) lives in
// outbox_test.go.
//
// Read paths (Query) MUST never reflect cross-project data — every row
// returned matches its ProjectID, and the ListFilter scopes ProjectID
// when set.
type AggregatorCacheStore interface {
	// Insert writes r to the cache. Populates r.CacheID on success.
	// MUST NOT modify r.ProjectID (inv-zen-113 anchor: the originating
	// project's ID is the only valid value).
	Insert(ctx context.Context, r CacheRow) error

	DeleteByProject(ctx context.Context, projectID string) error

	Query(ctx context.Context, filter ListFilter) ([]CacheRow, error)

	Rebuild(ctx context.Context, sources []Store) error
}
