// SPDX-License-Identifier: MIT
package zenday

import (
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

const MaxBriefItems = 7

type LeverageRank int

const (
	RankOperatorGate LeverageRank = iota + 1

	RankFailedScheduledJob

	RankUrgentEvent

	RankCostCapWarning

	RankAutonomousMilestone

	RankExternalActivity

	RankInfoImmediate
)

func (r LeverageRank) Valid() bool {
	return r >= RankOperatorGate && r <= RankInfoImmediate
}

func (r LeverageRank) String() string {
	switch r {
	case RankOperatorGate:
		return "operator-gate"
	case RankFailedScheduledJob:
		return "failed-scheduled-job"
	case RankUrgentEvent:
		return "urgent-event"
	case RankCostCapWarning:
		return "cost-cap-warning"
	case RankAutonomousMilestone:
		return "autonomous-milestone"
	case RankExternalActivity:
		return "external-activity"
	case RankInfoImmediate:
		return "info-immediate"
	default:
		return "invalid"
	}
}

type BriefType int

const (
	BriefTypeMorning BriefType = iota + 1

	BriefTypeEOD

	BriefTypeCheckPending
)

func (bt BriefType) String() string {
	switch bt {
	case BriefTypeMorning:
		return "morning"
	case BriefTypeEOD:
		return "eod"
	case BriefTypeCheckPending:
		return "check-pending"
	default:
		return "invalid"
	}
}

// BriefItem is one row in the morning/EOD brief. Sourced by Collect from
// N upstream sources; sorted by Rank ascending; rendered by Render as
// one bullet line per item.
//
// Source carries free-form provenance for telemetry / recap walks
// (e.g. "operator-gate:internal-platform-x.autonomous-paused" |
// "scheduled-job:internal-platform-x.cost-sweep" | "external:gh:internal-platform-x#34").
//
// JSON tags are canonical: this struct is the wire shape consumed by
// Phase I HTTP handlers (`internal/daemon/handlers/zenday.go`) via
// `type ZenDayItem = zenday.BriefItem`. Stage 2 review CRITICAL #9
// reconciliation (2026-05-01): Phase I MUST NOT declare a parallel
// `ZenDayItem` struct that drops fields silently.
type BriefItem struct {
	Rank LeverageRank `json:"rank"`

	Severity inbox.Severity `json:"severity,omitempty"`

	Project string `json:"project_alias,omitempty"`

	EventType string `json:"event_type,omitempty"`

	Message string `json:"message"`

	Action string `json:"action,omitempty"`

	Source string `json:"source,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

func (bi BriefItem) Validate() error {
	if !bi.Rank.Valid() {
		return fmt.Errorf("zenday.BriefItem: rank invalid (got %d, want 1..7)", bi.Rank)
	}
	if bi.Message == "" {
		return errors.New("zenday.BriefItem: message required")
	}
	return nil
}

type ProjectStatusSection struct {
	Alias string `json:"alias"`

	AutonomousState string `json:"autonomous_state,omitempty"`

	HandoffSummary string `json:"handoff_summary,omitempty"`

	Tomorrow string `json:"next_session,omitempty"`

	Blockers []string `json:"blockers,omitempty"`
}

type AugmentationSection struct {
	TotalCostUSD       float64 `json:"total_cost_usd"`
	TokensConsumed     int     `json:"tokens_consumed"`
	TokensCeiling      int     `json:"tokens_ceiling"`
	KGQueriesFired     int     `json:"kg_queries_fired"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	LastIndexedRFC3339 string  `json:"last_indexed,omitempty"`
}

type KnowledgeSection struct {
	FTS5Docs                    int `json:"fts5_docs"`
	FTS5DocsDeltaSinceYesterday int `json:"fts5_docs_delta,omitempty"`
	AggregatorDBSizeMB          int `json:"aggregator_db_size_mb,omitempty"`
	PromoteToday                int `json:"promote_today,omitempty"`
	CrossProjectQueries         int `json:"cross_project_queries,omitempty"`
	LitestreamReplicaLagSec     int `json:"litestream_replica_lag_sec,omitempty"`
}

type NotificationsSection struct {
	RoutesActive         []string `json:"routes_active"`
	PendingAcks          int      `json:"pending_acks"`
	CostCap50Alerts      int      `json:"cost_cap_50_alerts"`
	CostCap80Alerts      int      `json:"cost_cap_80_alerts"`
	CostCap100Alerts     int      `json:"cost_cap_100_alerts"`
	CaronteHealthDigests int      `json:"caronte_health_digests"`
	HermesDispatchErrors int      `json:"hermes_dispatch_errors"`
}

type BriefDoc struct {
	Date time.Time `json:"date"`

	Type BriefType `json:"type"`

	Items []BriefItem `json:"items"`

	TruncatedCount int `json:"truncated_count,omitempty"`

	PerProjectStatus []ProjectStatusSection `json:"per_project_status,omitempty"`

	CostWatchUSD float64 `json:"cost_watch_usd,omitempty"`

	NextScheduledAt time.Time `json:"next_scheduled_at,omitempty"`

	PendingActionNeeded int `json:"pending_action_needed,omitempty"`

	PendingUrgent int `json:"pending_urgent,omitempty"`

	Augmentation  *AugmentationSection  `json:"augmentation,omitempty"`
	Knowledge     *KnowledgeSection     `json:"knowledge,omitempty"`
	Notifications *NotificationsSection `json:"notifications,omitempty"`
}

func (d BriefDoc) IsMorning() bool { return d.Type == BriefTypeMorning }

func (d BriefDoc) IsEOD() bool { return d.Type == BriefTypeEOD }

func (d BriefDoc) IsCheckPending() bool { return d.Type == BriefTypeCheckPending }

var (
	ErrAlreadyGenerated = errors.New("zenday: today's brief already generated (use --force to overwrite)")

	ErrSourceCollectFailed = errors.New("zenday: every source collection leg failed")

	ErrCollectCancelled = errors.New("zenday: collect cancelled")
)
