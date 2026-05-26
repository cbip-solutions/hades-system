// SPDX-License-Identifier: MIT
// Package scheduler implements the 3-tier (Routine/Task/Loop) scheduler
// for zen-swarm-ctld with deterministic jitter (inv-zen-120),
// doctrine-tunable miss policy + bounded catch-up + coalescing
// (inv-zen-121), and single-egress dispatch via Plan 3 dispatcher
// (inv-zen-080 / inv-zen-123).
//
// Boundary (inv-zen-031): this package NEVER imports internal/store.
// Persistence is bridged via internal/daemon/scheduleradapter/.
//
// Boundary (inv-zen-080 / inv-zen-123): scheduler.Fire dispatches
// LLM ONLY via the Dispatcher interface declared in dispatcher_iface.go;
// it never imports internal/providers or private-tier1-module.
//
// Zero-value defaults (load-bearing): a fresh Schedule{} carries the
// most common case (TierRoutine + TriggerCron + StatusEnabled +
// MissPolicySkip). INSERT-with-defaults lands a usable row.
package scheduler

import (
	"errors"
	"fmt"
	"time"
)

type Tier int

const (
	TierRoutine Tier = iota

	TierTask

	TierLoop
)

func (t Tier) String() string {
	switch t {
	case TierRoutine:
		return "routine"
	case TierTask:
		return "task"
	case TierLoop:
		return "loop"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

type TriggerType int

const (
	TriggerCron TriggerType = iota

	TriggerHTTP

	TriggerGitPoll
)

func (t TriggerType) String() string {
	switch t {
	case TriggerCron:
		return "cron"
	case TriggerHTTP:
		return "http"
	case TriggerGitPoll:
		return "git-poll"
	default:
		return fmt.Sprintf("trigger(%d)", int(t))
	}
}

type MissPolicy int

const (
	MissPolicySkip MissPolicy = iota

	MissPolicyCatchUpBounded

	MissPolicyCoalesce

	MissPolicyNotifyOnly
)

func (m MissPolicy) String() string {
	switch m {
	case MissPolicySkip:
		return "skip"
	case MissPolicyCatchUpBounded:
		return "catch-up-bounded"
	case MissPolicyCoalesce:
		return "coalesce"
	case MissPolicyNotifyOnly:
		return "notify-only"
	default:
		return fmt.Sprintf("miss-policy(%d)", int(m))
	}
}

type Status int

const (
	StatusEnabled Status = iota

	StatusDisabled

	StatusFailed
)

func (s Status) String() string {
	switch s {
	case StatusEnabled:
		return "enabled"
	case StatusDisabled:
		return "disabled"
	case StatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("status(%d)", int(s))
	}
}

type Outcome int

const (
	OutcomeSuccess Outcome = iota

	OutcomeFailed

	OutcomeSkipped

	OutcomeRateLimited
)

func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeFailed:
		return "failed"
	case OutcomeSkipped:
		return "skipped"
	case OutcomeRateLimited:
		return "rate-limited"
	default:
		return fmt.Sprintf("outcome(%d)", int(o))
	}
}

type TriggerConfig struct {
	CronExpr string `json:"cron_expr,omitempty"`

	BearerTokenHash string `json:"bearer_token_hash,omitempty"`

	RepoURL string `json:"repo_url,omitempty"`

	Branch string `json:"branch,omitempty"`

	PollIntervalSeconds int `json:"poll_interval_seconds,omitempty"`

	LastSeenSHA string `json:"last_seen_sha,omitempty"`
}

type Schedule struct {
	ID              string        `json:"id"`
	Tier            Tier          `json:"tier"`
	ProjectAlias    string        `json:"project_alias"`
	Action          string        `json:"action"`
	TriggerType     TriggerType   `json:"trigger_type"`
	TriggerConfig   TriggerConfig `json:"trigger_config"`
	MissPolicy      MissPolicy    `json:"miss_policy"`
	MissLookback    time.Duration `json:"miss_lookback"`
	CoalesceWindow  time.Duration `json:"coalesce_window"`
	LastRunAt       time.Time     `json:"last_run_at,omitempty"`
	NextRunAt       time.Time     `json:"next_run_at,omitempty"`
	Status          Status        `json:"status"`
	CreatedAt       time.Time     `json:"created_at"`
	BearerTokenHash string        `json:"-"`
}

func (s *Schedule) DueAt(t time.Time) bool {
	return s.Status == StatusEnabled && !s.NextRunAt.IsZero() && !s.NextRunAt.After(t)
}

func (s *Schedule) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("%w: empty ID", ErrInvalidSchedule)
	}
	if s.ProjectAlias == "" {
		return fmt.Errorf("%w: empty ProjectAlias", ErrInvalidSchedule)
	}
	if s.Action == "" {
		return fmt.Errorf("%w: empty Action", ErrInvalidSchedule)
	}
	if s.MissLookback < 0 {
		return fmt.Errorf("%w: negative MissLookback %v", ErrInvalidSchedule, s.MissLookback)
	}
	if s.CoalesceWindow < 0 {
		return fmt.Errorf("%w: negative CoalesceWindow %v", ErrInvalidSchedule, s.CoalesceWindow)
	}
	switch s.TriggerType {
	case TriggerCron:
		if s.TriggerConfig.CronExpr == "" {
			return fmt.Errorf("%w: TriggerCron requires CronExpr", ErrInvalidSchedule)
		}
	case TriggerHTTP:
		if s.TriggerConfig.BearerTokenHash == "" {
			return fmt.Errorf("%w: TriggerHTTP requires BearerTokenHash", ErrInvalidSchedule)
		}
	case TriggerGitPoll:
		if s.TriggerConfig.RepoURL == "" {
			return fmt.Errorf("%w: TriggerGitPoll requires RepoURL", ErrInvalidSchedule)
		}
	default:
		return fmt.Errorf("%w: unknown TriggerType %d", ErrInvalidSchedule, int(s.TriggerType))
	}
	switch s.MissPolicy {
	case MissPolicySkip, MissPolicyCatchUpBounded, MissPolicyCoalesce, MissPolicyNotifyOnly:

	default:
		return fmt.Errorf("%w: unknown MissPolicy %d", ErrInvalidSchedule, int(s.MissPolicy))
	}
	switch s.Status {
	case StatusEnabled, StatusDisabled, StatusFailed:

	default:
		return fmt.Errorf("%w: unknown Status %d", ErrInvalidSchedule, int(s.Status))
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("%w: zero CreatedAt", ErrInvalidSchedule)
	}
	return nil
}

type HistoryEntry struct {
	ScheduleID string    `json:"schedule_id"`
	FiredAt    time.Time `json:"fired_at"`
	Outcome    Outcome   `json:"outcome"`
	Reason     string    `json:"reason,omitempty"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

func (h *HistoryEntry) Validate() error {
	if h.ScheduleID == "" {
		return fmt.Errorf("%w: empty ScheduleID", ErrInvalidHistoryEntry)
	}
	if h.FiredAt.IsZero() {
		return fmt.Errorf("%w: zero FiredAt", ErrInvalidHistoryEntry)
	}
	if h.DurationMs < 0 {
		return fmt.Errorf("%w: negative DurationMs %d", ErrInvalidHistoryEntry, h.DurationMs)
	}
	switch h.Outcome {
	case OutcomeSuccess, OutcomeFailed, OutcomeSkipped, OutcomeRateLimited:

	default:
		return fmt.Errorf("%w: unknown Outcome %d", ErrInvalidHistoryEntry, int(h.Outcome))
	}
	return nil
}

type MissedFire struct {
	ScheduleID   string
	MissedCount  int
	LookbackUsed time.Duration
	From         time.Time
	To           time.Time
}

type BackfillWindow struct {
	From time.Time
	To   time.Time
}

// Sentinel errors used across the package; consumers MUST use errors.Is
// to compare (no string matching).
var (
	ErrInvalidSchedule = errors.New("scheduler: invalid Schedule")

	ErrInvalidHistoryEntry = errors.New("scheduler: invalid HistoryEntry")

	ErrNotFound = errors.New("scheduler: not found")

	ErrInvalidCron = errors.New("scheduler: invalid cron expression")

	ErrRateLimited = errors.New("scheduler: rate-limited")

	ErrQuotaCap = errors.New("scheduler: quota cap reached")

	ErrSessionGone = errors.New("scheduler: bound session is gone")
)
