// SPDX-License-Identifier: MIT
// Package zenday — git CLI poll + eventlog + inbox + scheduler contracts.
//
// All non-zenday source dependencies live behind thin local interfaces.
// Production wiring adapts the canonical concrete types from
// internal/{inbox,scheduler,eventlog} + release dispatcheradapter; tests
// substitute fakes (per invariant, zenday/ never imports
// internal/store).
package zenday

import (
	"context"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type GitActivity struct {
	ProjectAlias string

	Kind string

	Description string

	URL string

	CreatedAt time.Time
}

type GitCli interface {
	RecentActivity(ctx context.Context, since time.Time) ([]GitActivity, error)
}

type EventRecord struct {
	ProjectID string

	ProjectAlias string

	Kind string

	CreatedAt time.Time

	PayloadJSON []byte
}

// EventReader is the read-only contract zenday consumes for the
// HandoffPosted leg in --eod mode. Production wiring satisfies via
// the eventlog.Querier interface; tests substitute fakes.
//
// Implementations MUST scope to the operator's set of projects
// .
type EventReader interface {
	// QueryByType returns records of the given event-type Kind in
	// [from, to). Implementations MUST scope to the operator's set of
	// projects.
	QueryByType(ctx context.Context, kind string, from, to time.Time) ([]EventRecord, error)
}

type InboxQuerier interface {
	Query(ctx context.Context, filter InboxListFilter) ([]InboxCacheRow, error)
}

type InboxListFilter struct {
	ProjectID string

	Severity *inbox.Severity

	Since *time.Time

	Limit int

	IncludeAcked bool
}

type InboxCacheRow struct {
	CacheID int64

	ProjectID string

	ProjectAlias string

	NotificationID int64

	Severity inbox.Severity

	EventType string

	ContentHash string

	CreatedAt time.Time

	AckedAt *time.Time
}

type SchedulerHistorian interface {
	// QueryHistory returns history entries for the given scheduleID
	// (empty = all schedules) in [from, to). Implementations MUST
	// scope to the operator's set of projects.
	QueryHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]SchedulerHistoryEntry, error)
}

type SchedulerHistoryEntry struct {
	ScheduleID string

	ProjectAlias string

	Action string

	FiredAt time.Time

	Outcome string

	Reason string

	CostUSD float64

	DurationMs int64
}
