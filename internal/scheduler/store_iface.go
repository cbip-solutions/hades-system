// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"time"
)

type Store interface {
	Insert(ctx context.Context, s *Schedule) error

	Get(ctx context.Context, id string) (*Schedule, error)

	UpdateNextRun(ctx context.Context, id string, lastRunAt, nextRunAt time.Time) error

	UpdateStatus(ctx context.Context, id string, status Status) error

	Delete(ctx context.Context, id string) error

	ListDue(ctx context.Context, now time.Time) ([]*Schedule, error)

	ListByProject(ctx context.Context, alias string) ([]*Schedule, error)

	AppendHistory(ctx context.Context, h HistoryEntry) error

	QueryHistory(ctx context.Context, scheduleID string, from, to time.Time) ([]HistoryEntry, error)
}
