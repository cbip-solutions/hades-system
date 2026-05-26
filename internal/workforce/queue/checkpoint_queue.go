// SPDX-License-Identifier: MIT
package queue

import (
	"context"
	"time"
)

type Checkpoint struct {
	TaskID TaskID

	ProjectID string

	ThreadID string

	StateJSON string

	SeqNum int

	DeadlineAt time.Time

	Consumed bool

	CreatedAt time.Time
}

type CheckpointQueue interface {
	Put(ctx context.Context, cp Checkpoint) error

	Drain(ctx context.Context, taskID TaskID) ([]Checkpoint, error)

	Peek(ctx context.Context, taskID TaskID) ([]Checkpoint, error)

	ByThread(ctx context.Context, threadID string) ([]Checkpoint, error)
}
