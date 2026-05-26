// SPDX-License-Identifier: MIT
package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrTaskNotFound = errors.New("workforce/queue: task not found")

var ErrTaskNotPending = errors.New("workforce/queue: task is not pending")

var ErrDuplicateTask = errors.New("workforce/queue: duplicate task (project_id, task_id)")

var ErrInvalidTransition = errors.New("workforce/queue: invalid status transition")

var ErrProjectIDMismatch = errors.New("workforce/queue: row project_id does not match scope")

var ErrAmbiguousTaskID = errors.New("workforce/queue: task_id matches rows in multiple projects (use ScopedTo)")

type Status int

const (
	StatusPending Status = iota + 1

	StatusInProgress

	StatusReview

	StatusDone

	StatusFailed
)

func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusInProgress:
		return "in_progress"
	case StatusReview:
		return "review"
	case StatusDone:
		return "done"
	case StatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown_status(%d)", int(s))
	}
}

func ParseStatus(s string) (Status, error) {
	switch s {
	case "pending":
		return StatusPending, nil
	case "in_progress":
		return StatusInProgress, nil
	case "review":
		return StatusReview, nil
	case "done":
		return StatusDone, nil
	case "failed":
		return StatusFailed, nil
	default:
		return 0, fmt.Errorf("workforce/queue: unknown status %q", s)
	}
}

func IsValidTransition(current, next Status) bool {
	switch current {
	case StatusPending:
		return next == StatusInProgress || next == StatusFailed
	case StatusInProgress:
		return next == StatusReview || next == StatusFailed
	case StatusReview:
		return next == StatusDone || next == StatusFailed || next == StatusInProgress
	case StatusDone, StatusFailed:

		return false
	default:

		return false
	}
}

type TaskID string

type TaskRow struct {
	TaskID TaskID

	ProjectID string

	Title string

	Description string

	Status Status

	ThreadID string

	Priority int

	ErrorDetail string

	CreatedAt time.Time

	UpdatedAt time.Time
}

type SharedTaskList interface {
	Enqueue(ctx context.Context, row TaskRow) error

	Claim(ctx context.Context, taskID TaskID, threadID string) error

	Advance(ctx context.Context, taskID TaskID, newStatus Status) error

	Get(ctx context.Context, taskID TaskID) (TaskRow, error)

	ListByStatus(ctx context.Context, projectID string, status Status) ([]TaskRow, error)

	ByThread(ctx context.Context, threadID string) ([]TaskRow, error)
}
