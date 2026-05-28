// SPDX-License-Identifier: MIT
// Package notif — HADES design task
//
// Event-type taxonomy and structured payload schemas emit
// hooks. HADES design ships the emit-side only; HADES design ships the routing
// chain (Slack/email/webhook/etc). This package has no runtime
// dependencies beyond the standard library and must NEVER import
// internal packages that carry secrets or credentials. The PII-free
// invariant (invariant) is enforced by TestVerifyNoPII.
//
// Boundary (invariant): this package does NOT import internal/store.
// It is a pure types + constructors package; persistence is the
// responsibility of internal/daemon/handlers/audit_emit.go.
//
// Callers (each stage that emits a payload of this type):
//
// - internal/workforce/subprocess: SubprocessCrashChain, SubprocessTTLEvicted
// - internal/budget/anomaly: BudgetAnomalyZScore, BudgetCapExhausted
// - internal/daemon/handlers/audit_emit.go:
// AuditFamilyDisjointPoolEmpty,
// AuditEmitHTTPFailSustained
// - internal/mcp/sshexec: SSHExecInteractiveBlocked, SSHExecAllowlistDenied
// - internal/daemon/handlers/doctrine.go:
// DoctrineInvalidOnReload
// - internal/workforce/worker: WorkerCompleted, WorkerFailed
// - internal/mcp/research: ResearchCitationHallucinated
package notif

import (
	"fmt"
	"time"
)

// Severity mirrors the HADES design notifications-table severity column,
// extended with SeveritySecurityAlert for security-grade always-notify
// events that bypass any rate-limiting or batching layer.
type Severity string

const (
	SeverityInfo Severity = "INFO"

	SeverityWarn Severity = "WARN"

	SeverityCritical Severity = "CRITICAL"
	// SeveritySecurityAlert is the highest severity, reserved for
	// security-grade events that must always reach the operator regardless
	// of doctrine-tunable aggregation. HADES design routes these BEFORE any
	// rate-limiting or batching layer.
	SeveritySecurityAlert Severity = "SECURITY_ALERT"
)

type EventType int

const (
	// EventSSHExecInteractiveBlocked is part of the exported package contract.
	// Security-grade always-notify events (spec §6.3 "Always-notify").
	EventSSHExecInteractiveBlocked EventType = iota + 1

	EventAuditFamilyDisjointPoolEmpty

	EventDoctrineInvalidOnReload

	EventSubprocessCrashChain

	EventBudgetAnomalyZScore

	EventAuditEmitHTTPFailSustained

	EventSSHExecAllowlistDenied

	EventSubprocessTTLEvicted

	EventWorkerFailed

	EventBudgetCapExhausted

	EventWorkerCompleted

	EventResearchCitationHallucinated
)

var eventTypeStrings = map[EventType]string{
	EventSSHExecInteractiveBlocked:    "ssh_exec.interactive_blocked",
	EventAuditFamilyDisjointPoolEmpty: "audit.family_disjoint_pool_empty",
	EventDoctrineInvalidOnReload:      "doctrine.invalid_on_reload",
	EventSubprocessCrashChain:         "subprocess.crash_chain",
	EventBudgetAnomalyZScore:          "budget.anomaly.z_score",
	EventAuditEmitHTTPFailSustained:   "audit.emit.http_fail_sustained",
	EventSSHExecAllowlistDenied:       "ssh_exec.allowlist_denied",
	EventSubprocessTTLEvicted:         "subprocess.ttl_evicted",
	EventWorkerFailed:                 "worker.failed",
	EventBudgetCapExhausted:           "budget.cap_exhausted",
	EventWorkerCompleted:              "worker.completed",
	EventResearchCitationHallucinated: "research.citation_hallucinated",
}

var eventTypeSeverity = map[EventType]Severity{
	EventSSHExecInteractiveBlocked:    SeveritySecurityAlert,
	EventAuditFamilyDisjointPoolEmpty: SeveritySecurityAlert,
	EventDoctrineInvalidOnReload:      SeverityCritical,
	EventSubprocessCrashChain:         SeverityWarn,
	EventBudgetAnomalyZScore:          SeverityCritical,
	EventAuditEmitHTTPFailSustained:   SeverityCritical,
	EventSSHExecAllowlistDenied:       SeverityWarn,
	EventSubprocessTTLEvicted:         SeverityInfo,
	EventWorkerFailed:                 SeverityWarn,
	EventBudgetCapExhausted:           SeverityCritical,
	EventWorkerCompleted:              SeverityInfo,
	EventResearchCitationHallucinated: SeverityWarn,
}

var alwaysNotifySet = map[EventType]bool{
	EventSSHExecInteractiveBlocked:    true,
	EventAuditFamilyDisjointPoolEmpty: true,
	EventDoctrineInvalidOnReload:      true,
	EventBudgetAnomalyZScore:          true,
	EventAuditEmitHTTPFailSustained:   true,
}

func (et EventType) String() string {
	if s, ok := eventTypeStrings[et]; ok {
		return s
	}
	return fmt.Sprintf("unknown_event_type_%d", int(et))
}

func (et EventType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + et.String() + `"`), nil
}

func (et EventType) DefaultSeverity() Severity {
	if s, ok := eventTypeSeverity[et]; ok {
		return s
	}
	return SeverityInfo
}

// IsAlwaysNotify returns true if this event type is security-grade and
// must always be surfaced to the operator regardless of doctrine
// aggregation settings. HADES design consults this flag before applying
// any rate-limiting or batching.
func (et EventType) IsAlwaysNotify() bool {
	return alwaysNotifySet[et]
}

type Event struct {
	ID int64 `json:"id,omitempty"`

	Type EventType `json:"event_type"`

	Severity Severity `json:"severity"`

	EmittedAt time.Time `json:"emitted_at"`

	Payload any `json:"payload"`
}

type SSHExecInteractiveBlockedPayload struct {
	Host       string `json:"host"`
	CmdPrefix  string `json:"cmd_prefix"`
	WorkerID   string `json:"worker_id"`
	TaskID     string `json:"task_id"`
	DetectedAt string `json:"detected_at"`
}

type BudgetAnomalyZScorePayload struct {
	Scope      string  `json:"scope"`
	ScopeValue string  `json:"scope_value"`
	ZScore     float64 `json:"z_score"`
	ThresholdZ float64 `json:"threshold_z"`
	AutoPaused bool    `json:"auto_paused"`
}

type SubprocessCrashChainPayload struct {
	SpecID         string `json:"spec_id"`
	CrashCount     int    `json:"crash_count"`
	LastExitReason string `json:"last_exit_reason"`
}

type DoctrineInvalidPayload struct {
	DoctrineName string `json:"doctrine_name"`
	ParseError   string `json:"parse_error"`
}

type AuditEmitHTTPFailPayload struct {
	Endpoint         string  `json:"endpoint"`
	LastHTTPStatus   int     `json:"last_http_status"`
	SustainedSeconds float64 `json:"sustained_seconds"`
}

type AuditFamilyDisjointPoolEmptyPayload struct {
	DoctrineName    string `json:"doctrine_name"`
	GeneratorFamily string `json:"generator_family"`
}

type SSHExecAllowlistDeniedPayload struct {
	Host       string `json:"host"`
	CmdPrefix  string `json:"cmd_prefix"`
	DenyReason string `json:"deny_reason"`
	WorkerID   string `json:"worker_id"`
}

type SubprocessTTLEvictedPayload struct {
	SpecID  string `json:"spec_id"`
	TTL     string `json:"ttl"`
	IdleFor string `json:"idle_for"`
}

type WorkerCompletedPayload struct {
	WorkerID  string  `json:"worker_id"`
	TaskID    string  `json:"task_id"`
	DurationS float64 `json:"duration_s"`
}

type WorkerFailedPayload struct {
	WorkerID     string `json:"worker_id"`
	TaskID       string `json:"task_id"`
	ErrorSummary string `json:"error_summary"`
}

type BudgetCapExhaustedPayload struct {
	Scope      string  `json:"scope"`
	ScopeValue string  `json:"scope_value"`
	CapUSD     float64 `json:"cap_usd"`
	UsedUSD    float64 `json:"used_usd"`
}

type ResearchCitationHallucinatedPayload struct {
	QueryHash      string `json:"query_hash"`
	TotalCitations int    `json:"total_citations"`
	FailedCount    int    `json:"failed_count"`
}

const (
	cmdPrefixMax = 128

	errorSummaryMax = 256
)

func truncateString(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func NewSSHExecInteractiveBlockedEvent(host, cmdPrefix, workerID, taskID string) Event {
	now := time.Now().UTC()
	return Event{
		Type:      EventSSHExecInteractiveBlocked,
		Severity:  SeveritySecurityAlert,
		EmittedAt: now,
		Payload: &SSHExecInteractiveBlockedPayload{
			Host:       host,
			CmdPrefix:  truncateString(cmdPrefix, cmdPrefixMax),
			WorkerID:   workerID,
			TaskID:     taskID,
			DetectedAt: now.Format(time.RFC3339),
		},
	}
}

func NewBudgetAnomalyEvent(scopeValue, scope, _stage string, zScore, thresholdZ float64) Event {
	return Event{
		Type:      EventBudgetAnomalyZScore,
		Severity:  SeverityCritical,
		EmittedAt: time.Now().UTC(),
		Payload: &BudgetAnomalyZScorePayload{
			Scope:      scope,
			ScopeValue: scopeValue,
			ZScore:     zScore,
			ThresholdZ: thresholdZ,
			AutoPaused: true,
		},
	}
}

func NewSubprocessCrashChainEvent(specID string, crashCount int, lastExitReason string) Event {
	return Event{
		Type:      EventSubprocessCrashChain,
		Severity:  SeverityWarn,
		EmittedAt: time.Now().UTC(),
		Payload: &SubprocessCrashChainPayload{
			SpecID:         specID,
			CrashCount:     crashCount,
			LastExitReason: lastExitReason,
		},
	}
}

func NewDoctrineInvalidEvent(doctrineName, parseError string) Event {
	return Event{
		Type:      EventDoctrineInvalidOnReload,
		Severity:  SeverityCritical,
		EmittedAt: time.Now().UTC(),
		Payload: &DoctrineInvalidPayload{
			DoctrineName: doctrineName,
			ParseError:   parseError,
		},
	}
}

func NewAuditEmitHTTPFailEvent(endpoint string, lastHTTPStatus int, sustained time.Duration) Event {
	return Event{
		Type:      EventAuditEmitHTTPFailSustained,
		Severity:  SeverityCritical,
		EmittedAt: time.Now().UTC(),
		Payload: &AuditEmitHTTPFailPayload{
			Endpoint:         endpoint,
			LastHTTPStatus:   lastHTTPStatus,
			SustainedSeconds: sustained.Seconds(),
		},
	}
}

func NewAuditFamilyDisjointPoolEmptyEvent(doctrineName, generatorFamily string) Event {
	return Event{
		Type:      EventAuditFamilyDisjointPoolEmpty,
		Severity:  SeveritySecurityAlert,
		EmittedAt: time.Now().UTC(),
		Payload: &AuditFamilyDisjointPoolEmptyPayload{
			DoctrineName:    doctrineName,
			GeneratorFamily: generatorFamily,
		},
	}
}

func NewSSHExecAllowlistDeniedEvent(host, cmdPrefix, denyReason, workerID string) Event {
	return Event{
		Type:      EventSSHExecAllowlistDenied,
		Severity:  SeverityWarn,
		EmittedAt: time.Now().UTC(),
		Payload: &SSHExecAllowlistDeniedPayload{
			Host:       host,
			CmdPrefix:  truncateString(cmdPrefix, cmdPrefixMax),
			DenyReason: denyReason,
			WorkerID:   workerID,
		},
	}
}

func NewSubprocessTTLEvictedEvent(specID, ttl, idleFor string) Event {
	return Event{
		Type:      EventSubprocessTTLEvicted,
		Severity:  SeverityInfo,
		EmittedAt: time.Now().UTC(),
		Payload: &SubprocessTTLEvictedPayload{
			SpecID:  specID,
			TTL:     ttl,
			IdleFor: idleFor,
		},
	}
}

func NewWorkerCompletedEvent(workerID, taskID string, duration time.Duration) Event {
	return Event{
		Type:      EventWorkerCompleted,
		Severity:  SeverityInfo,
		EmittedAt: time.Now().UTC(),
		Payload: &WorkerCompletedPayload{
			WorkerID:  workerID,
			TaskID:    taskID,
			DurationS: duration.Seconds(),
		},
	}
}

func NewWorkerFailedEvent(workerID, taskID, errorSummary string) Event {
	return Event{
		Type:      EventWorkerFailed,
		Severity:  SeverityWarn,
		EmittedAt: time.Now().UTC(),
		Payload: &WorkerFailedPayload{
			WorkerID:     workerID,
			TaskID:       taskID,
			ErrorSummary: truncateString(errorSummary, errorSummaryMax),
		},
	}
}

func NewBudgetCapExhaustedEvent(scope, scopeValue string, capUSD, usedUSD float64) Event {
	return Event{
		Type:      EventBudgetCapExhausted,
		Severity:  SeverityCritical,
		EmittedAt: time.Now().UTC(),
		Payload: &BudgetCapExhaustedPayload{
			Scope:      scope,
			ScopeValue: scopeValue,
			CapUSD:     capUSD,
			UsedUSD:    usedUSD,
		},
	}
}

func NewResearchCitationHallucinatedEvent(queryHash string, total, failed int) Event {
	return Event{
		Type:      EventResearchCitationHallucinated,
		Severity:  SeverityWarn,
		EmittedAt: time.Now().UTC(),
		Payload: &ResearchCitationHallucinatedPayload{
			QueryHash:      queryHash,
			TotalCitations: total,
			FailedCount:    failed,
		},
	}
}
