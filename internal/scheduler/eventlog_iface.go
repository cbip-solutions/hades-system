// SPDX-License-Identifier: MIT
package scheduler

import (
	"context"
	"time"
)

type EventKind int

const (
	EventRoutineFired EventKind = iota

	EventRoutineFailed

	EventRoutineSkipped

	EventMissedFire

	EventQuotaCapReached

	EventRateLimited

	EventLoopBound

	EventLoopReleased
)

func (k EventKind) String() string {
	switch k {
	case EventRoutineFired:
		return "routine.fired"
	case EventRoutineFailed:
		return "routine.failed"
	case EventRoutineSkipped:
		return "routine.skipped"
	case EventMissedFire:
		return "scheduler.missed_fire"
	case EventQuotaCapReached:
		return "scheduler.quota_cap_reached"
	case EventRateLimited:
		return "scheduler.rate_limited"
	case EventLoopBound:
		return "scheduler.loop_bound"
	case EventLoopReleased:
		return "scheduler.loop_released"
	default:
		return "scheduler.unknown"
	}
}

type Event struct {
	Kind         EventKind
	ScheduleID   string
	ProjectAlias string
	At           time.Time
	Reason       string
	CostUSD      float64
}

type EventEmitter interface {
	Emit(ctx context.Context, e Event) error
}
