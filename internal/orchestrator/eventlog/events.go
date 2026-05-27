// SPDX-License-Identifier: MIT
// internal/orchestrator/eventlog/events.go
package eventlog

import (
	"encoding/json"
	"fmt"
	"time"
)

type EventType int

const (
	EvtUnknown EventType = 0

	EvtOrchestratorStarted           EventType = 1
	EvtOrchestratorStateTransition   EventType = 2
	EvtOrchestratorRestarting        EventType = 3
	EvtOrchestratorRestoreFromReplay EventType = 4
	EvtOrchestratorStopped           EventType = 5

	EvtWorkerDispatched     EventType = 6
	EvtWorkerCheckpoint     EventType = 7
	EvtWorkerDeath          EventType = 8
	EvtWorkerRedispatched   EventType = 9
	EvtReviewerWaveStarted  EventType = 10
	EvtReviewerWaveComplete EventType = 11

	EvtTacticalAggregation  EventType = 12
	EvtStrategicAggregation EventType = 13
	EvtArchitecturalReview  EventType = 14
	EvtEscalationDecision   EventType = 15

	EvtVotingDecisionMade   EventType = 16
	EvtFMVAllFailed         EventType = 17
	EvtEMSConvergenceFailed EventType = 18

	EvtCostThresholdCrossed     EventType = 19
	EvtBudgetDegradationApplied EventType = 20
	EvtBudgetRecovered          EventType = 21
	EvtEmergencyTierActivated   EventType = 22

	EvtConfirmationRequested EventType = 23
	EvtOperatorConfirmation  EventType = 24
	EvtOperatorAmendmentDeny EventType = 25

	EvtDoctrineAmendmentProposed   EventType = 26
	EvtDoctrineAmendmentApplied    EventType = 27
	EvtDoctrineAmendmentReverted   EventType = 28
	EvtDoctrineAmendmentSuppressed EventType = 29

	EvtSubstrateDriftDetected   EventType = 30
	EvtConfigDivergenceDetected EventType = 31
	EvtRegressionBySelfAlarm    EventType = 32
	EvtSafetynetPrevMissing     EventType = 33

	EvtWorktreePoolDegraded  EventType = 34
	EvtWorktreePoolExhausted EventType = 35

	EvtOperatorOverrideApplied EventType = 36

	EvtResearchCompleted EventType = 37

	EvtDepthWidthDecided EventType = 38

	EvtReplayCorruptionDetected EventType = 39

	EvtApplyFixStarted   EventType = 40
	EvtApplyFixSucceeded EventType = 41
	EvtApplyFixReverted  EventType = 42

	EvtBudgetSnapshotError     EventType = 43
	EvtBudgetDegradationFailed EventType = 44

	EvtCostGatingAtomicityTimeout EventType = 45

	EvtBudgetFullyRecovered EventType = 46
	EvtBudgetRecoveryHeld   EventType = 47

	EvtPhaseBoundaryRecorded EventType = 48

	EvtFMVDegradedToPlurality EventType = 49

	EvtADRRangeExhausted EventType = 50

	EvtHandoffPosted EventType = 51

	EvtMorningBriefReady EventType = 52
	EvtEODDigestReady    EventType = 53

	EvtDoctrineLoaded           EventType = 54
	EvtDoctrineReloaded         EventType = 55
	EvtDoctrineReloadFailed     EventType = 56
	EvtDoctrineSchemaDeprecated EventType = 57
	EvtDoctrineSchemaMigrated   EventType = 58

	EvtDoctrineLoadFailed         EventType = 59
	EvtDoctrineOverrideLoadFailed EventType = 60
	EvtDoctrineLoadRefused        EventType = 61

	EvtDoctrineSchemaMigrationFailed EventType = 62

	EvtDoctrineAutonomousReverted       EventType = 63
	EvtDoctrineRevertSuppressedCooldown EventType = 64

	EvtDoctrineTightenViolationRejected EventType = 65

	EvtDoctrineAmendmentApplyFailed EventType = 66

	EvtDoctrineWatcherStalled   EventType = 67
	EvtDoctrineWatcherRestarted EventType = 68
	EvtDoctrineWatcherOverflow  EventType = 69

	EvtDoctrineAccessorAuditPassed EventType = 70

	// invariant: canonical ordering; numbers MUST NOT change post-declaration.
	// See internal design record §4.6.
	EvtRAGQuery         EventType = 92
	EvtRAGRetrieval     EventType = 93
	EvtRAGCitation      EventType = 94
	EvtRAGVerify        EventType = 95
	EvtRAGAbstain       EventType = 96
	EvtRAGAnswer        EventType = 97
	EvtRAGIngestPackage EventType = 98
	EvtRAGIngestJoinKey EventType = 99
)

const (
	EvtDaemonWitnessCoSigned       = "daemon.witness_co_signed"
	EvtDaemonWitnessRotated        = "daemon.witness_rotated"
	EvtDaemonWitnessKeyCompromised = "daemon.witness_key_compromised"
)

const (
	EvtAuditTamperDetected         = "audit.tamper_detected"
	EvtAuditRecoveryInitiated      = "audit.recovery_initiated"
	EvtAuditRecoveryCompleted      = "audit.recovery_completed"
	EvtAuditPartitionSealed        = "audit.partition_sealed"
	EvtAuditPartitionSealFailed    = "audit.partition_seal_failed"
	EvtAuditPartitionSealRecovered = "audit.partition_seal_recovered"
	EvtAuditTesseraBatchRecovered  = "audit.tessera_batch_recovered"
	EvtAuditLitestreamLag          = "audit.litestream_lag"
	EvtAuditLitestreamFailed       = "audit.litestream_failed"
	EvtAuditColdArchiveFailed      = "audit.cold_archive_failed"
	EvtAuditRefuseTriggerFired     = "audit.refuse_trigger_fired"
	EvtAuditHotPathLatencyBreach   = "audit.hot_path_latency_breach"
	EvtAuditCheckpoint             = "audit.checkpoint"
)

const EvtTesseraBatchRecovered = EvtAuditTesseraBatchRecovered

const (
	EvtVaultNotePromotedToGlobal     = "vault.note_promoted_to_global"
	EvtVaultNoteUnpromotedFromGlobal = "vault.note_unpromoted_from_global"
)

const (
	EvtADRTransitionProposed   = "adr.proposed"
	EvtADRTransitionAccepted   = "adr.accepted"
	EvtADRTransitionRejected   = "adr.rejected"
	EvtADRTransitionSuperseded = "adr.superseded"
	EvtADRTransitionDeprecated = "adr.deprecated"
)

const (
	EvtResearchDispatchInitiated              = "research.dispatch_initiated"
	EvtResearchCacheHitExact                  = "research.cache_hit_exact"
	EvtResearchCacheHitSemantic               = "research.cache_hit_semantic"
	EvtResearchCacheRevalidatedFresh          = "research.cache_revalidated_fresh"
	EvtResearchCacheRevalidatedStaleRefetched = "research.cache_revalidated_stale_refetched"
	EvtResearchFindingsReturned               = "research.findings_returned"
)

const (
	EvtStateManualFieldChanged = "state.manual_field_changed"
	EvtStateRegeneratePartial  = "state.regenerate_partial"
	EvtStateRegenerated        = "state.regenerated"
)

// IsValid reports whether et is a registered EventType (not EvtUnknown,
// not a reserved-for-future slot, not a typo). Future Log.Append (Task
// A-3) MUST reject events with !IsValid() Type() to prevent silent
// zero-value emit.
//
// wired the apply-engine slots
// (EvtApplyFixStarted/Succeeded/Reverted) into AllEventTypes() — they
// are now valid wire codes that the
// internal/daemon/orchestratoradapter bridge translates from
// apply-package-local apply.Event values.
func (et EventType) IsValid() bool {
	if et == EvtUnknown {
		return false
	}
	for _, valid := range AllEventTypes() {
		if et == valid {
			return true
		}
	}
	return false
}

func (et EventType) String() string {
	switch et {
	case EvtOrchestratorStarted:
		return "OrchestratorStarted"
	case EvtOrchestratorStateTransition:
		return "OrchestratorStateTransition"
	case EvtOrchestratorRestarting:
		return "OrchestratorRestarting"
	case EvtOrchestratorRestoreFromReplay:
		return "OrchestratorRestoreFromReplay"
	case EvtOrchestratorStopped:
		return "OrchestratorStopped"
	case EvtWorkerDispatched:
		return "WorkerDispatched"
	case EvtWorkerCheckpoint:
		return "WorkerCheckpoint"
	case EvtWorkerDeath:
		return "WorkerDeath"
	case EvtWorkerRedispatched:
		return "WorkerRedispatched"
	case EvtReviewerWaveStarted:
		return "ReviewerWaveStarted"
	case EvtReviewerWaveComplete:
		return "ReviewerWaveComplete"
	case EvtTacticalAggregation:
		return "TacticalAggregation"
	case EvtStrategicAggregation:
		return "StrategicAggregation"
	case EvtArchitecturalReview:
		return "ArchitecturalReview"
	case EvtEscalationDecision:
		return "EscalationDecision"
	case EvtVotingDecisionMade:
		return "VotingDecisionMade"
	case EvtFMVAllFailed:
		return "FMVAllFailed"
	case EvtEMSConvergenceFailed:
		return "EMSConvergenceFailed"
	case EvtCostThresholdCrossed:
		return "CostThresholdCrossed"
	case EvtBudgetDegradationApplied:
		return "BudgetDegradationApplied"
	case EvtBudgetRecovered:
		return "BudgetRecovered"
	case EvtEmergencyTierActivated:
		return "EmergencyTierActivated"
	case EvtConfirmationRequested:
		return "ConfirmationRequested"
	case EvtOperatorConfirmation:
		return "OperatorConfirmation"
	case EvtOperatorAmendmentDeny:
		return "OperatorAmendmentDeny"
	case EvtDoctrineAmendmentProposed:
		return "DoctrineAmendmentProposed"
	case EvtDoctrineAmendmentApplied:
		return "DoctrineAmendmentApplied"
	case EvtDoctrineAmendmentReverted:
		return "DoctrineAmendmentReverted"
	case EvtDoctrineAmendmentSuppressed:
		return "DoctrineAmendmentSuppressed"
	case EvtSubstrateDriftDetected:
		return "SubstrateDriftDetected"
	case EvtConfigDivergenceDetected:
		return "ConfigDivergenceDetected"
	case EvtRegressionBySelfAlarm:
		return "RegressionBySelfAlarm"
	case EvtSafetynetPrevMissing:
		return "SafetynetPrevMissing"
	case EvtWorktreePoolDegraded:
		return "WorktreePoolDegraded"
	case EvtWorktreePoolExhausted:
		return "WorktreePoolExhausted"
	case EvtOperatorOverrideApplied:
		return "OperatorOverrideApplied"
	case EvtResearchCompleted:
		return "ResearchCompleted"
	case EvtDepthWidthDecided:
		return "DepthWidthDecided"
	case EvtReplayCorruptionDetected:
		return "ReplayCorruptionDetected"
	case EvtBudgetSnapshotError:
		return "BudgetSnapshotError"
	case EvtBudgetDegradationFailed:
		return "BudgetDegradationFailed"
	case EvtCostGatingAtomicityTimeout:
		return "CostGatingAtomicityTimeout"
	case EvtBudgetFullyRecovered:
		return "BudgetFullyRecovered"
	case EvtBudgetRecoveryHeld:
		return "BudgetRecoveryHeld"
	case EvtPhaseBoundaryRecorded:
		return "PhaseBoundaryRecorded"
	case EvtFMVDegradedToPlurality:
		return "FMVDegradedToPlurality"
	case EvtApplyFixStarted:
		return "ApplyFixStarted"
	case EvtApplyFixSucceeded:
		return "ApplyFixSucceeded"
	case EvtApplyFixReverted:
		return "ApplyFixReverted"
	case EvtADRRangeExhausted:
		return "ADRRangeExhausted"
	case EvtHandoffPosted:
		return "HandoffPosted"
	case EvtMorningBriefReady:
		return "MorningBriefReady"
	case EvtEODDigestReady:
		return "EODDigestReady"

	case EvtDoctrineLoaded:
		return "DoctrineLoaded"
	case EvtDoctrineReloaded:
		return "DoctrineReloaded"
	case EvtDoctrineReloadFailed:
		return "DoctrineReloadFailed"
	case EvtDoctrineSchemaDeprecated:
		return "DoctrineSchemaDeprecated"
	case EvtDoctrineSchemaMigrated:
		return "DoctrineSchemaMigrated"
	case EvtDoctrineLoadFailed:
		return "DoctrineLoadFailed"
	case EvtDoctrineOverrideLoadFailed:
		return "DoctrineOverrideLoadFailed"
	case EvtDoctrineLoadRefused:
		return "DoctrineLoadRefused"
	case EvtDoctrineSchemaMigrationFailed:
		return "DoctrineSchemaMigrationFailed"
	case EvtDoctrineAutonomousReverted:
		return "DoctrineAutonomousReverted"
	case EvtDoctrineRevertSuppressedCooldown:
		return "DoctrineRevertSuppressedCooldown"
	case EvtDoctrineTightenViolationRejected:
		return "DoctrineTightenViolationRejected"
	case EvtDoctrineAmendmentApplyFailed:
		return "DoctrineAmendmentApplyFailed"
	case EvtDoctrineWatcherStalled:
		return "DoctrineWatcherStalled"
	case EvtDoctrineWatcherRestarted:
		return "DoctrineWatcherRestarted"
	case EvtDoctrineWatcherOverflow:
		return "DoctrineWatcherOverflow"
	case EvtDoctrineAccessorAuditPassed:
		return "DoctrineAccessorAuditPassed"
	case EvtRAGQuery:
		return "EvtRAGQuery"
	case EvtRAGRetrieval:
		return "EvtRAGRetrieval"
	case EvtRAGCitation:
		return "EvtRAGCitation"
	case EvtRAGVerify:
		return "EvtRAGVerify"
	case EvtRAGAbstain:
		return "EvtRAGAbstain"
	case EvtRAGAnswer:
		return "EvtRAGAnswer"
	case EvtRAGIngestPackage:
		return "EvtRAGIngestPackage"
	case EvtRAGIngestJoinKey:
		return "EvtRAGIngestJoinKey"
	default:
		return "Unknown"
	}
}

func AllEventTypes() []EventType {
	return []EventType{
		EvtOrchestratorStarted, EvtOrchestratorStateTransition,
		EvtOrchestratorRestarting, EvtOrchestratorRestoreFromReplay,
		EvtOrchestratorStopped, EvtWorkerDispatched, EvtWorkerCheckpoint,
		EvtWorkerDeath, EvtWorkerRedispatched, EvtReviewerWaveStarted,
		EvtReviewerWaveComplete, EvtTacticalAggregation,
		EvtStrategicAggregation, EvtArchitecturalReview,
		EvtEscalationDecision, EvtVotingDecisionMade, EvtFMVAllFailed,
		EvtEMSConvergenceFailed, EvtCostThresholdCrossed,
		EvtBudgetDegradationApplied, EvtBudgetRecovered,
		EvtEmergencyTierActivated, EvtConfirmationRequested,
		EvtOperatorConfirmation, EvtOperatorAmendmentDeny,
		EvtDoctrineAmendmentProposed, EvtDoctrineAmendmentApplied,
		EvtDoctrineAmendmentReverted, EvtDoctrineAmendmentSuppressed,
		EvtSubstrateDriftDetected, EvtConfigDivergenceDetected,
		EvtRegressionBySelfAlarm, EvtSafetynetPrevMissing,
		EvtWorktreePoolDegraded, EvtWorktreePoolExhausted,
		EvtOperatorOverrideApplied, EvtResearchCompleted,
		EvtDepthWidthDecided, EvtReplayCorruptionDetected,
		EvtBudgetSnapshotError, EvtBudgetDegradationFailed,
		EvtCostGatingAtomicityTimeout,
		EvtBudgetFullyRecovered, EvtBudgetRecoveryHeld,
		EvtPhaseBoundaryRecorded,
		EvtFMVDegradedToPlurality,
		EvtApplyFixStarted, EvtApplyFixSucceeded, EvtApplyFixReverted,
		EvtADRRangeExhausted,
		EvtHandoffPosted,
		EvtMorningBriefReady, EvtEODDigestReady,
		EvtDoctrineLoaded, EvtDoctrineReloaded, EvtDoctrineReloadFailed,
		EvtDoctrineSchemaDeprecated, EvtDoctrineSchemaMigrated,
		EvtDoctrineLoadFailed, EvtDoctrineOverrideLoadFailed, EvtDoctrineLoadRefused,
		EvtDoctrineSchemaMigrationFailed,
		EvtDoctrineAutonomousReverted, EvtDoctrineRevertSuppressedCooldown,
		EvtDoctrineTightenViolationRejected,
		EvtDoctrineAmendmentApplyFailed,
		EvtDoctrineWatcherStalled, EvtDoctrineWatcherRestarted, EvtDoctrineWatcherOverflow,
		EvtDoctrineAccessorAuditPassed,

		EvtRAGQuery, EvtRAGRetrieval, EvtRAGCitation, EvtRAGVerify,
		EvtRAGAbstain, EvtRAGAnswer, EvtRAGIngestPackage, EvtRAGIngestJoinKey,
	}
}

// PayloadEncoder is implemented by every event payload struct. Type returns
// the EventType discriminant; Payload returns canonical JSON.
//
// Decode returns the value form (not pointer). Type-switch consumers MUST
// match on the value type, not pointer:
//
// switch ev := decoded.(type) {
// case eventlog.WorkerDispatched: // correct
// case *eventlog.WorkerDispatched: // never matches
// }
//
// Rationale avoids a nil-pointer hazard if a future Decode arm forgets to
// dereference; trades a small-value copy for type-switch safety.
type PayloadEncoder interface {
	Payload() ([]byte, error)
	Type() EventType
}

type OrchestratorStarted struct {
	SessionID    string `json:"session_id"`
	ProjectID    string `json:"project_id"`
	AutonomyMode string `json:"autonomy_mode"`
}

func (e OrchestratorStarted) Type() EventType { return EvtOrchestratorStarted }

func (e OrchestratorStarted) Payload() ([]byte, error) { return json.Marshal(e) }

type OrchestratorStateTransition struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

func (e OrchestratorStateTransition) Type() EventType { return EvtOrchestratorStateTransition }

func (e OrchestratorStateTransition) Payload() ([]byte, error) { return json.Marshal(e) }

type OrchestratorRestarting struct {
	LastSeenState string `json:"last_seen_state"`
}

func (e OrchestratorRestarting) Type() EventType { return EvtOrchestratorRestarting }

func (e OrchestratorRestarting) Payload() ([]byte, error) { return json.Marshal(e) }

// OrchestratorRestoreFromReplay is the audit-trail event RecoveryEngine
// emits via ReconstructInFlight when a replay-resume scan
// completes — successful or HardPause-aborted. originally
// declared the struct with {SessionID, RecoveryMs, EventsReplayed,
// EventsCorrupted}; Task E-6 extends it with structured replay-result
// fields so downstream audit consumers + release hash-chain replay can
// observe the recovered task count and HardPause flag without re-
// scanning the eventlog.
//
// Field set evolution (E-6 fix-pass): pre-E-6 emitters (none in-tree
// today; the canonical event was reserved for this task) populated only
// the legacy four fields. Post-E-6 emitters MUST populate
// {RecoveredTaskCount, HardPause, Reason} as well. JSON tags use
// omitempty on the new fields so pre-E-6 audit_events_raw rows (if any
// existed) round-trip cleanly through json.Unmarshal — missing keys
// zero-initialise without error. RecoveryMs is left zero in
// .
//
// Pattern mirrors the E-2 (WorkerRedispatched) and E-5 (WorkerDeath)
// extensions: typed struct grows additively; JSON wire stays stable;
// exhaustive test table adds dual rows (legacy + post-E-6).
type OrchestratorRestoreFromReplay struct {
	SessionID          string `json:"session_id"`
	RecoveryMs         int64  `json:"recovery_ms,omitempty"`
	EventsReplayed     int    `json:"events_replayed"`
	EventsCorrupted    int    `json:"events_corrupted"`
	RecoveredTaskCount int    `json:"recovered_task_count,omitempty"`
	HardPause          bool   `json:"hard_pause,omitempty"`
	Reason             string `json:"reason,omitempty"`
}

func (e OrchestratorRestoreFromReplay) Type() EventType { return EvtOrchestratorRestoreFromReplay }

func (e OrchestratorRestoreFromReplay) Payload() ([]byte, error) { return json.Marshal(e) }

type OrchestratorStopped struct {
	Outcome string `json:"outcome"`
}

func (e OrchestratorStopped) Type() EventType { return EvtOrchestratorStopped }

func (e OrchestratorStopped) Payload() ([]byte, error) { return json.Marshal(e) }

type WorkerDispatched struct {
	WorkerID string `json:"worker_id"`
	TaskID   string `json:"task_id"`
	Tier     string `json:"tier"`
}

func (e WorkerDispatched) Type() EventType { return EvtWorkerDispatched }

func (e WorkerDispatched) Payload() ([]byte, error) { return json.Marshal(e) }

type WorkerCheckpoint struct {
	WorkerID      string `json:"worker_id"`
	TaskID        string `json:"task_id,omitempty"`
	CheckpointSHA string `json:"checkpoint_sha"`
	Summary       string `json:"summary"`
}

func (e WorkerCheckpoint) Type() EventType { return EvtWorkerCheckpoint }

func (e WorkerCheckpoint) Payload() ([]byte, error) { return json.Marshal(e) }

type WorkerDeath struct {
	WorkerID   string `json:"worker_id"`
	TaskID     string `json:"task_id,omitempty"`
	Class      string `json:"class,omitempty"`
	Reason     string `json:"reason,omitempty"`
	RetryCount int    `json:"retry_count"`
	Cause      string `json:"cause,omitempty"`
}

func (e WorkerDeath) Type() EventType { return EvtWorkerDeath }

func (e WorkerDeath) Payload() ([]byte, error) { return json.Marshal(e) }

// WorkerRedispatched is the audit-trail event RecoveryEngine emits when a
// worker death triggers a redispatch decision (RedispatchSameTier or
// RedispatchNextTier). defines the schema; (recovery.go)
// emits it. (replay) reads it.
//
// Field set is the FROZEN integration contract between recovery
// engine and downstream replay + audit consumers (release
// hash-chain). Adding fields is non-breaking; renaming or removing
// requires schema-version coordination.
//
// JSON tag names MUST match the canonical map keys recovery.go's
// HandleWorkerDeath emits via the Event-shape Append(map) path: a typed
// Decode round-trip must yield a struct with all fields populated. The
// match is verified by TestRecoveryEngine_HandleWorkerDeath_TypedPayloadRoundTrip
// (orchestrator package).
type WorkerRedispatched struct {
	TaskID       string `json:"task_id"`
	WorkerID     string `json:"worker_id"`
	Class        string `json:"class"`
	Action       string `json:"action"`
	NewTierIndex int    `json:"new_tier_index"`
	RetryCount   int    `json:"retry_count"`
	Reason       string `json:"reason"`
}

func (e WorkerRedispatched) Type() EventType { return EvtWorkerRedispatched }

func (e WorkerRedispatched) Payload() ([]byte, error) { return json.Marshal(e) }

type ReviewerWaveStarted struct {
	Layer     string   `json:"layer"`
	Reviewers []string `json:"reviewers"`
}

func (e ReviewerWaveStarted) Type() EventType { return EvtReviewerWaveStarted }

func (e ReviewerWaveStarted) Payload() ([]byte, error) { return json.Marshal(e) }

type ReviewerWaveComplete struct {
	Layer   string `json:"layer"`
	Verdict string `json:"verdict"`
}

func (e ReviewerWaveComplete) Type() EventType { return EvtReviewerWaveComplete }

func (e ReviewerWaveComplete) Payload() ([]byte, error) { return json.Marshal(e) }

type TacticalAggregation struct {
	WaveID  string `json:"wave_id"`
	Verdict string `json:"verdict"`
}

func (e TacticalAggregation) Type() EventType { return EvtTacticalAggregation }

func (e TacticalAggregation) Payload() ([]byte, error) { return json.Marshal(e) }

type StrategicAggregation struct {
	WaveID  string `json:"wave_id"`
	Verdict string `json:"verdict"`
}

func (e StrategicAggregation) Type() EventType { return EvtStrategicAggregation }

func (e StrategicAggregation) Payload() ([]byte, error) { return json.Marshal(e) }

type ArchitecturalReview struct {
	PhaseID string `json:"phase_id"`
	Verdict string `json:"verdict"`
}

func (e ArchitecturalReview) Type() EventType { return EvtArchitecturalReview }

func (e ArchitecturalReview) Payload() ([]byte, error) { return json.Marshal(e) }

type EscalationDecision struct {
	FromLayer string `json:"from_layer"`
	ToLayer   string `json:"to_layer"`
	Reason    string `json:"reason"`
}

func (e EscalationDecision) Type() EventType { return EvtEscalationDecision }

func (e EscalationDecision) Payload() ([]byte, error) { return json.Marshal(e) }

type VotingDecisionMade struct {
	Mechanism string `json:"mechanism"`
	Winner    string `json:"winner"`
}

func (e VotingDecisionMade) Type() EventType { return EvtVotingDecisionMade }

func (e VotingDecisionMade) Payload() ([]byte, error) { return json.Marshal(e) }

type FMVAllFailed struct {
	CandidateCount int `json:"candidate_count"`
	TestFailures   int `json:"test_failures"`
}

func (e FMVAllFailed) Type() EventType { return EvtFMVAllFailed }

func (e FMVAllFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type EMSConvergenceFailed struct {
	SampledCount int `json:"sampled_count"`
}

func (e EMSConvergenceFailed) Type() EventType { return EvtEMSConvergenceFailed }

func (e EMSConvergenceFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type CostThresholdCrossed struct {
	ThresholdPct int `json:"threshold_pct"`
	ObservedPct  int `json:"observed_pct"`
}

func (e CostThresholdCrossed) Type() EventType { return EvtCostThresholdCrossed }

func (e CostThresholdCrossed) Payload() ([]byte, error) { return json.Marshal(e) }

type BudgetDegradationApplied struct {
	Threshold int    `json:"threshold,omitempty"`
	Action    string `json:"action,omitempty"`

	ThresholdPct    int     `json:"threshold_pct,omitempty"`
	PriorAction     string  `json:"prior_action,omitempty"`
	Doctrine        string  `json:"doctrine,omitempty"`
	ProjectID       string  `json:"project_id,omitempty"`
	CumulativeUSD   float64 `json:"cumulative_usd,omitempty"`
	DailyCapUSD     float64 `json:"daily_cap_usd,omitempty"`
	ProjectedEODUSD float64 `json:"projected_eod_usd,omitempty"`
	PAYGActive      bool    `json:"payg_active,omitempty"`
}

func (e BudgetDegradationApplied) Type() EventType { return EvtBudgetDegradationApplied }

func (e BudgetDegradationApplied) Payload() ([]byte, error) { return json.Marshal(e) }

type BudgetRecovered struct {
	RestoredToPct int `json:"restored_to_pct,omitempty"`

	UndoneAction string `json:"undone_action,omitempty"`
	NextAction   string `json:"next_action,omitempty"`
	NextPct      int    `json:"next_pct,omitempty"`
	Doctrine     string `json:"doctrine,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
}

func (e BudgetRecovered) Type() EventType { return EvtBudgetRecovered }

func (e BudgetRecovered) Payload() ([]byte, error) { return json.Marshal(e) }

type BudgetFullyRecovered struct {
	Doctrine  string `json:"doctrine,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

func (e BudgetFullyRecovered) Type() EventType { return EvtBudgetFullyRecovered }

func (e BudgetFullyRecovered) Payload() ([]byte, error) { return json.Marshal(e) }

type BudgetRecoveryHeld struct {
	HeldAction string `json:"held_action,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Doctrine   string `json:"doctrine,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
}

func (e BudgetRecoveryHeld) Type() EventType { return EvtBudgetRecoveryHeld }

func (e BudgetRecoveryHeld) Payload() ([]byte, error) { return json.Marshal(e) }

type EmergencyTierActivated struct {
	Reason string `json:"reason"`
}

func (e EmergencyTierActivated) Type() EventType { return EvtEmergencyTierActivated }

func (e EmergencyTierActivated) Payload() ([]byte, error) { return json.Marshal(e) }

type ConfirmationRequested struct {
	EventID       string   `json:"event_id"`
	DecisionClass string   `json:"decision_class"`
	RequestSeq    uint64   `json:"request_seq,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	Alternatives  []string `json:"alternatives,omitempty"`
}

func (e ConfirmationRequested) Type() EventType { return EvtConfirmationRequested }

func (e ConfirmationRequested) Payload() ([]byte, error) { return json.Marshal(e) }

type OperatorConfirmation struct {
	EventID     string `json:"event_id"`
	Decision    string `json:"decision"`
	Rationale   string `json:"rationale"`
	RequestSeq  uint64 `json:"request_seq,omitempty"`
	OperatorUID int    `json:"operator_uid,omitempty"`
}

func (e OperatorConfirmation) Type() EventType { return EvtOperatorConfirmation }

func (e OperatorConfirmation) Payload() ([]byte, error) { return json.Marshal(e) }

type OperatorAmendmentDeny struct {
	ADRID     string `json:"adr_id"`
	Rationale string `json:"rationale"`
}

func (e OperatorAmendmentDeny) Type() EventType { return EvtOperatorAmendmentDeny }

func (e OperatorAmendmentDeny) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAmendmentProposed struct {
	ADRID       string `json:"adr_id"`
	DiffSummary string `json:"diff_summary"`
}

func (e DoctrineAmendmentProposed) Type() EventType { return EvtDoctrineAmendmentProposed }

func (e DoctrineAmendmentProposed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAmendmentApplied struct {
	ADRID     string `json:"adr_id"`
	CommitSHA string `json:"commit_sha"`
}

func (e DoctrineAmendmentApplied) Type() EventType { return EvtDoctrineAmendmentApplied }

func (e DoctrineAmendmentApplied) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAmendmentReverted struct {
	ADRID string `json:"adr_id"`
}

func (e DoctrineAmendmentReverted) Type() EventType { return EvtDoctrineAmendmentReverted }

func (e DoctrineAmendmentReverted) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAmendmentSuppressed struct {
	ADRID             string `json:"adr_id"`
	CooldownRemaining string `json:"cooldown_remaining"`
}

func (e DoctrineAmendmentSuppressed) Type() EventType { return EvtDoctrineAmendmentSuppressed }

func (e DoctrineAmendmentSuppressed) Payload() ([]byte, error) { return json.Marshal(e) }

type SubstrateDriftDetected struct {
	Severity string `json:"severity"`
	Findings string `json:"findings"`
}

func (e SubstrateDriftDetected) Type() EventType { return EvtSubstrateDriftDetected }

func (e SubstrateDriftDetected) Payload() ([]byte, error) { return json.Marshal(e) }

type ConfigDivergenceDetected struct {
	DiffSummary string `json:"diff_summary"`
}

func (e ConfigDivergenceDetected) Type() EventType { return EvtConfigDivergenceDetected }

func (e ConfigDivergenceDetected) Payload() ([]byte, error) { return json.Marshal(e) }

type RegressionBySelfAlarm struct {
	ObservedPassRate float64 `json:"observed_pass_rate"`
	BaselinePassRate float64 `json:"baseline_pass_rate"`
}

func (e RegressionBySelfAlarm) Type() EventType { return EvtRegressionBySelfAlarm }

func (e RegressionBySelfAlarm) Payload() ([]byte, error) { return json.Marshal(e) }

type SafetynetPrevMissing struct {
	Reason string `json:"reason"`
}

func (e SafetynetPrevMissing) Type() EventType { return EvtSafetynetPrevMissing }

func (e SafetynetPrevMissing) Payload() ([]byte, error) { return json.Marshal(e) }

type WorktreePoolDegraded struct {
	Floor  int `json:"floor"`
	Used   int `json:"used"`
	Target int `json:"target"`
}

func (e WorktreePoolDegraded) Type() EventType { return EvtWorktreePoolDegraded }

func (e WorktreePoolDegraded) Payload() ([]byte, error) { return json.Marshal(e) }

type WorktreePoolExhausted struct {
	Requested int `json:"requested"`
	Available int `json:"available"`
}

func (e WorktreePoolExhausted) Type() EventType { return EvtWorktreePoolExhausted }

func (e WorktreePoolExhausted) Payload() ([]byte, error) { return json.Marshal(e) }

type OperatorOverrideApplied struct {
	OverrideClass  string `json:"override_class,omitempty"`
	Rationale      string `json:"rationale,omitempty"`
	OperatorUID    int    `json:"operator_uid,omitempty"`
	OperatorReason string `json:"operator_reason,omitempty"`
	OverrideKind   string `json:"override_kind,omitempty"`
}

func (e OperatorOverrideApplied) Type() EventType { return EvtOperatorOverrideApplied }

func (e OperatorOverrideApplied) Payload() ([]byte, error) { return json.Marshal(e) }

type ResearchCompleted struct {
	FindingsSummary string  `json:"findings_summary"`
	CostUSD         float64 `json:"cost_usd"`
}

func (e ResearchCompleted) Type() EventType { return EvtResearchCompleted }

func (e ResearchCompleted) Payload() ([]byte, error) { return json.Marshal(e) }

type DepthWidthDecided struct {
	Depth     int    `json:"depth"`
	Width     int    `json:"width"`
	Rationale string `json:"rationale"`
}

func (e DepthWidthDecided) Type() EventType { return EvtDepthWidthDecided }

func (e DepthWidthDecided) Payload() ([]byte, error) { return json.Marshal(e) }

type ReplayCorruptionDetected struct {
	EventOffset int64  `json:"event_offset"`
	Reason      string `json:"reason"`
}

func (e ReplayCorruptionDetected) Type() EventType { return EvtReplayCorruptionDetected }

func (e ReplayCorruptionDetected) Payload() ([]byte, error) { return json.Marshal(e) }

// BudgetSnapshotError is the audit-trail event the cost-gating evaluator
// emits when BudgetSnapshotReader.Snapshot
// returns a non-nil error during a poll cycle. Run continues to the
// next tick; this event is the diagnostic seam for transient release
// budget-engine read failures.
//
// Privacy contract (IMP-3 carry-forward): Error is the wrapped
// error.Error() string; callers (release dispatcheradapter Snapshot
// implementation) MUST ensure the error message never echoes
// secret-shaped bytes (API tokens, raw URLs with embedded credentials,
// etc.). The cost-gating engine does NOT redact — it forwards verbatim.
type BudgetSnapshotError struct {
	Error string `json:"error"`
}

func (e BudgetSnapshotError) Type() EventType { return EvtBudgetSnapshotError }

func (e BudgetSnapshotError) Payload() ([]byte, error) { return json.Marshal(e) }

type BudgetDegradationFailed struct {
	Action string `json:"action"`
	Error  string `json:"error"`
}

func (e BudgetDegradationFailed) Type() EventType { return EvtBudgetDegradationFailed }

func (e BudgetDegradationFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type CostGatingAtomicityTimeout struct {
	TimeoutSec float64 `json:"timeout_sec"`
	Action     string  `json:"action,omitempty"`
}

func (e CostGatingAtomicityTimeout) Type() EventType { return EvtCostGatingAtomicityTimeout }

func (e CostGatingAtomicityTimeout) Payload() ([]byte, error) { return json.Marshal(e) }

type PhaseBoundaryRecorded struct {
	PhaseID string `json:"phase_id"`
	Trigger string `json:"trigger"`
}

func (e PhaseBoundaryRecorded) Type() EventType { return EvtPhaseBoundaryRecorded }

func (e PhaseBoundaryRecorded) Payload() ([]byte, error) { return json.Marshal(e) }

// FMVDegradedToPlurality is the audit-trail event the FMV
// emits when a mid-run Pool.Lease returns ErrPoolExhausted (or, in I-7,
// when the orchestrator preemptively skips FMV under cost-pressure)
// and the algorithm degrades to plurality voting on the candidates'
// SupportingReviewers axis (Q8 A pattern under Q8 B regime).
//
// Field semantics:
// - Reason — degradation cause (closed vocab today: "pool_exhausted";
// I-7 reuses with "cost_pressure"). Audit dashboards group fires by
// this field.
// - CandidateCount — total fix proposals supplied to FMV.Run
// (len(candidates)); independent of how many were actually evaluated
// before degradation fired.
// - CompletedCount — number of candidates evaluated (lease+apply+test)
// before the lease error caused degradation. Plurality picks the
// winner from the FULL set, not this subset; CompletedCount exists
// so audit consumers can reason about the partial trace shape
// (Trace length on the result equals CompletedCount).
// - WinnerID — the picked FixProposal.ID; empty string when the
// plurality fallback itself tied (Tie==true ⇒ caller escalates to
// L3 with ErrFMVTie).
// - Tie — true when the top two candidates share the highest
// SupportingReviewers count; the caller MUST escalate. The audit
// row is emitted regardless of Tie (the "we degraded" signal is
// independent of the plurality outcome).
//
// Privacy contract (IMP-3 carry-forward): WinnerID is the sanitised
// proposal identifier emitted by the L2 aggregator (same surface as
// VotingDecisionMade.Winner); Patch bytes never appear in this event.
type FMVDegradedToPlurality struct {
	Reason         string `json:"reason"`
	CandidateCount int    `json:"candidate_count"`
	CompletedCount int    `json:"completed_count"`
	WinnerID       string `json:"winner_id"`
	Tie            bool   `json:"tie,omitempty"`
}

func (e FMVDegradedToPlurality) Type() EventType { return EvtFMVDegradedToPlurality }

func (e FMVDegradedToPlurality) Payload() ([]byte, error) { return json.Marshal(e) }

type ApplyFixStarted struct {
	Branch string `json:"branch"`
	FixID  string `json:"fix_id"`
}

func (e ApplyFixStarted) Type() EventType { return EvtApplyFixStarted }

func (e ApplyFixStarted) Payload() ([]byte, error) { return json.Marshal(e) }

type ApplyFixSucceeded struct {
	Branch    string   `json:"branch"`
	FixID     string   `json:"fix_id"`
	CommitSHA string   `json:"commit_sha"`
	Files     []string `json:"files,omitempty"`
}

func (e ApplyFixSucceeded) Type() EventType { return EvtApplyFixSucceeded }

func (e ApplyFixSucceeded) Payload() ([]byte, error) { return json.Marshal(e) }

// ApplyFixReverted is the canonical wire shape emits when the
// apply-package engine fires apply.EventApplyReverted — TestCmd failed
// after a successful apply + commit, and the engine ran
// `git reset --hard <priorSHA>` to roll back. CommitSHA carries the
// transient commit that was rolled back (useful for forensic queries);
// Stderr captures the test's stderr output (or, in the rare double-
// failure case, the revert's own stderr) for diagnosis.
//
// Privacy contract: Stderr is forwarded verbatim from the child
// process; callers MUST ensure the test command
// itself does not echo secret-shaped bytes (the apply package does not
// redact at this seam).
type ApplyFixReverted struct {
	Branch    string   `json:"branch"`
	FixID     string   `json:"fix_id"`
	CommitSHA string   `json:"commit_sha"`
	Files     []string `json:"files,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
}

func (e ApplyFixReverted) Type() EventType { return EvtApplyFixReverted }

func (e ApplyFixReverted) Payload() ([]byte, error) { return json.Marshal(e) }

type ADRRangeExhausted struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

func (e ADRRangeExhausted) Type() EventType { return EvtADRRangeExhausted }

func (e ADRRangeExhausted) Payload() ([]byte, error) { return json.Marshal(e) }

// HandoffPostedEvent records a project /handoff slash command invocation.
// 8-field schema frozen master plan §"HandoffPosted event
// coordination" + spec §1 Q15 + §6.6 + §6.8. invariant enforces
// schema integrity (compliance test round-trips Marshal/Unmarshal +
// asserts field set; extends with fuzz N=100 random payloads).
//
// Producer/consumer wiring (cross-phase load-bearing):
// - Producer: plugin /handoff slash command emits via daemon
// HTTP POST /v1/events/handoff_posted. The plugin also
// continues to write .hades/session.md to disk (existing behaviour
// preserved); the event is the cross-project signal surface.
// - Consumer A: `hades day --eod` reads per-project event-log
// entries to compose ProjectStatusSection blocks in the EOD digest.
// - Consumer B: release doctrine notification routing + release
// multi-channel delivery may opt-in for operator-channel surfacing.
//
// Field semantics:
// - ProjectID — opaque project identifier (typically a 64-char
// hex hash); load-bearing for cross-source dedup +
// per-project event-log routing.
// - ProjectAlias — human-readable alias (e.g. "internal-platform-x") for
// digest rendering. NOT used for routing.
// - Timestamp — handoff posting time (UTC).
// - Summary — 1-2 sentence synopsis of session outcome.
// Free-form; emitter MUST pre-redact secrets.
// - RecentCommits — list of recent commit short SHAs + summaries
// (typically last 5-10). Free-form lines.
// - AutonomousState — project's autonomous-mode state at handoff
// time. Daemon HTTP handler validates the
// enum {active|paused|idle|complete} runtime per
// defense-in-depth Layer 3; the typed struct here
// accepts any string for forward-compat with future
// state additions.
// - Blockers — free-form per-blocker descriptions; empty
// slice when no blockers reported.
// - NextSession — single-line operator-friendly hint for the
// next session start.
//
// Privacy contract: Summary, RecentCommits, Blockers, NextSession are
// free-text fields that MAY contain leaked secrets if emitted verbatim
// from worker output, LLM responses, or git diffs. Emitters (plugin
// /handoff command + future autonomous-flow callers) MUST redact via
// internal/redact before constructing the event (see doc.go privacy
// contract).
type HandoffPostedEvent struct {
	ProjectID       string    `json:"project_id"`
	ProjectAlias    string    `json:"project_alias"`
	Timestamp       time.Time `json:"timestamp"`
	Summary         string    `json:"summary"`
	RecentCommits   []string  `json:"recent_commits"`
	AutonomousState string    `json:"autonomous_state"`
	Blockers        []string  `json:"blockers"`
	NextSession     string    `json:"next_session_action"`
}

func (e HandoffPostedEvent) Type() EventType { return EvtHandoffPosted }

func (e HandoffPostedEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type MorningBriefReadyEvent struct {
	Date         time.Time `json:"date"`
	ItemCount    int       `json:"item_count"`
	ProjectCount int       `json:"project_count"`
	FilePath     string    `json:"file_path"`
}

func (e MorningBriefReadyEvent) Type() EventType { return EvtMorningBriefReady }

func (e MorningBriefReadyEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type EODDigestReadyEvent struct {
	Date         time.Time `json:"date"`
	ProjectCount int       `json:"project_count"`
	TotalCostUSD float64   `json:"total_cost_usd"`
	FilePath     string    `json:"file_path"`
}

func (e EODDigestReadyEvent) Type() EventType { return EvtEODDigestReady }

func (e EODDigestReadyEvent) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineLoaded struct {
	Path            string `json:"path"`
	SchemaVersion   string `json:"schema_version"`
	DoctrineVersion string `json:"doctrine_version"`
	Source          string `json:"source"`
	ProjectID       string `json:"project_id,omitempty"`
}

func (e DoctrineLoaded) Type() EventType { return EvtDoctrineLoaded }

func (e DoctrineLoaded) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineReloaded struct {
	Path                string    `json:"path"`
	ProjectID           string    `json:"project_id,omitempty"`
	DoctrineName        string    `json:"doctrine_name"`
	FromDoctrineVersion string    `json:"from_doctrine_version,omitempty"`
	ToDoctrineVersion   string    `json:"to_doctrine_version,omitempty"`
	Source              string    `json:"source"`
	At                  time.Time `json:"at"`
}

func (e DoctrineReloaded) Type() EventType { return EvtDoctrineReloaded }

func (e DoctrineReloaded) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineReloadFailed struct {
	Path      string    `json:"path"`
	ProjectID string    `json:"project_id,omitempty"`
	Phase     string    `json:"phase"`
	Errors    []string  `json:"errors,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	KeptVer   string    `json:"kept_doctrine_ver,omitempty"`
	At        time.Time `json:"at"`
}

func (e DoctrineReloadFailed) Type() EventType { return EvtDoctrineReloadFailed }

func (e DoctrineReloadFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineSchemaDeprecated struct {
	Path           string    `json:"path"`
	ProjectID      string    `json:"project_id,omitempty"`
	DoctrineName   string    `json:"doctrine_name"`
	OnDiskVersion  string    `json:"on_disk_version"`
	CurrentVersion string    `json:"current_version"`
	Action         string    `json:"action"`
	At             time.Time `json:"at"`
}

func (e DoctrineSchemaDeprecated) Type() EventType { return EvtDoctrineSchemaDeprecated }

func (e DoctrineSchemaDeprecated) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineSchemaMigrated struct {
	Path        string `json:"path"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	BackupPath  string `json:"backup_path"`
}

func (e DoctrineSchemaMigrated) Type() EventType { return EvtDoctrineSchemaMigrated }

func (e DoctrineSchemaMigrated) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineLoadFailed struct {
	Path   string `json:"path"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

func (e DoctrineLoadFailed) Type() EventType { return EvtDoctrineLoadFailed }

func (e DoctrineLoadFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineOverrideLoadFailed struct {
	Path      string `json:"path"`
	ProjectID string `json:"project_id"`
	Reason    string `json:"reason"`
}

func (e DoctrineOverrideLoadFailed) Type() EventType { return EvtDoctrineOverrideLoadFailed }

func (e DoctrineOverrideLoadFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineLoadRefused struct {
	Path          string `json:"path"`
	Reason        string `json:"reason"`
	SchemaVersion string `json:"schema_version"`
	MinSupported  string `json:"min_supported"`
}

func (e DoctrineLoadRefused) Type() EventType { return EvtDoctrineLoadRefused }

func (e DoctrineLoadRefused) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineSchemaMigrationFailed struct {
	Path        string `json:"path"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Reason      string `json:"reason"`
}

func (e DoctrineSchemaMigrationFailed) Type() EventType { return EvtDoctrineSchemaMigrationFailed }

func (e DoctrineSchemaMigrationFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAutonomousReverted struct {
	ADRID             string  `json:"adr_id"`
	RulePath          string  `json:"rule_path"`
	TelemetryCategory string  `json:"telemetry_category"`
	ThresholdBreached float64 `json:"threshold_breached"`
	WindowSessions    int     `json:"window_sessions"`
	Reason            string  `json:"reason"`
}

func (e DoctrineAutonomousReverted) Type() EventType { return EvtDoctrineAutonomousReverted }

func (e DoctrineAutonomousReverted) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineRevertSuppressedCooldown struct {
	Path                   string    `json:"path,omitempty"`
	ADRID                  string    `json:"adr_id,omitempty"`
	RulePath               string    `json:"rule_path,omitempty"`
	TelemetryCategory      string    `json:"telemetry_category,omitempty"`
	FailureCount           int       `json:"failure_count,omitempty"`
	WindowSec              int       `json:"window_sec,omitempty"`
	AttemptedAtUnix        int64     `json:"attempted_at_unix,omitempty"`
	LastRevertedAtUnix     int64     `json:"last_reverted_at_unix,omitempty"`
	CooldownUntil          time.Time `json:"cooldown_until,omitempty"`
	CooldownRemainingHours float64   `json:"cooldown_remaining_hours,omitempty"`
	At                     time.Time `json:"at"`
}

func (e DoctrineRevertSuppressedCooldown) Type() EventType {
	return EvtDoctrineRevertSuppressedCooldown
}

func (e DoctrineRevertSuppressedCooldown) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineTightenViolation struct {
	RulePath       string `json:"rule_path"`
	AttemptedValue string `json:"attempted_value,omitempty"`
	BaselineValue  string `json:"baseline_value,omitempty"`
	Direction      string `json:"direction,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

type DoctrineTightenViolationRejected struct {
	Path           string                     `json:"path"`
	ProjectID      string                     `json:"project_id,omitempty"`
	DoctrineName   string                     `json:"doctrine_name,omitempty"`
	Source         string                     `json:"source"`
	ADRID          string                     `json:"adr_id,omitempty"`
	RulePath       string                     `json:"rule_path,omitempty"`
	AttemptedValue string                     `json:"attempted_value,omitempty"`
	BaselineValue  string                     `json:"baseline_value,omitempty"`
	Direction      string                     `json:"direction,omitempty"`
	RuleViolations []DoctrineTightenViolation `json:"rule_violations,omitempty"`
	At             time.Time                  `json:"at"`
}

func (e DoctrineTightenViolationRejected) Type() EventType {
	return EvtDoctrineTightenViolationRejected
}

func (e DoctrineTightenViolationRejected) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAmendmentApplyFailed struct {
	ADRID  string `json:"adr_id"`
	Path   string `json:"path"`
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

func (e DoctrineAmendmentApplyFailed) Type() EventType { return EvtDoctrineAmendmentApplyFailed }

func (e DoctrineAmendmentApplyFailed) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineWatcherStalled struct {
	Path            string    `json:"path,omitempty"`
	LastEventAt     time.Time `json:"last_event_at,omitempty"`
	StallTimeoutSec int       `json:"stall_timeout_sec,omitempty"`
	StaleSec        int       `json:"stale_sec"`
	At              time.Time `json:"at"`
}

func (e DoctrineWatcherStalled) Type() EventType { return EvtDoctrineWatcherStalled }

func (e DoctrineWatcherStalled) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineWatcherRestarted struct {
	Path   string    `json:"path,omitempty"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func (e DoctrineWatcherRestarted) Type() EventType { return EvtDoctrineWatcherRestarted }

func (e DoctrineWatcherRestarted) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineWatcherOverflow struct {
	Path           string    `json:"path,omitempty"`
	ReReadAllPaths int       `json:"re_read_all_paths"`
	QueueDepth     int       `json:"queue_depth"`
	Action         string    `json:"action"`
	AffectedFiles  []string  `json:"affected_files,omitempty"`
	At             time.Time `json:"at"`
}

func (e DoctrineWatcherOverflow) Type() EventType { return EvtDoctrineWatcherOverflow }

func (e DoctrineWatcherOverflow) Payload() ([]byte, error) { return json.Marshal(e) }

type DoctrineAccessorAuditPassed struct {
	AuditedAtUnix   int64 `json:"audited_at_unix"`
	PackagesScanned int   `json:"packages_scanned"`
	Violations      int   `json:"violations"`
}

func (e DoctrineAccessorAuditPassed) Type() EventType { return EvtDoctrineAccessorAuditPassed }

func (e DoctrineAccessorAuditPassed) Payload() ([]byte, error) { return json.Marshal(e) }

func Decode(et EventType, raw []byte) (PayloadEncoder, error) {
	switch et {
	case EvtOrchestratorStarted:
		var e OrchestratorStarted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOrchestratorStateTransition:
		var e OrchestratorStateTransition
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOrchestratorRestarting:
		var e OrchestratorRestarting
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOrchestratorRestoreFromReplay:
		var e OrchestratorRestoreFromReplay
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOrchestratorStopped:
		var e OrchestratorStopped
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorkerDispatched:
		var e WorkerDispatched
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorkerCheckpoint:
		var e WorkerCheckpoint
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorkerDeath:
		var e WorkerDeath
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorkerRedispatched:
		var e WorkerRedispatched
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtReviewerWaveStarted:
		var e ReviewerWaveStarted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtReviewerWaveComplete:
		var e ReviewerWaveComplete
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtTacticalAggregation:
		var e TacticalAggregation
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtStrategicAggregation:
		var e StrategicAggregation
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtArchitecturalReview:
		var e ArchitecturalReview
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtEscalationDecision:
		var e EscalationDecision
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtVotingDecisionMade:
		var e VotingDecisionMade
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtFMVAllFailed:
		var e FMVAllFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtEMSConvergenceFailed:
		var e EMSConvergenceFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtCostThresholdCrossed:
		var e CostThresholdCrossed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetDegradationApplied:
		var e BudgetDegradationApplied
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetRecovered:
		var e BudgetRecovered
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtEmergencyTierActivated:
		var e EmergencyTierActivated
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtConfirmationRequested:
		var e ConfirmationRequested
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOperatorConfirmation:
		var e OperatorConfirmation
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOperatorAmendmentDeny:
		var e OperatorAmendmentDeny
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAmendmentProposed:
		var e DoctrineAmendmentProposed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAmendmentApplied:
		var e DoctrineAmendmentApplied
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAmendmentReverted:
		var e DoctrineAmendmentReverted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAmendmentSuppressed:
		var e DoctrineAmendmentSuppressed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtSubstrateDriftDetected:
		var e SubstrateDriftDetected
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtConfigDivergenceDetected:
		var e ConfigDivergenceDetected
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtRegressionBySelfAlarm:
		var e RegressionBySelfAlarm
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtSafetynetPrevMissing:
		var e SafetynetPrevMissing
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorktreePoolDegraded:
		var e WorktreePoolDegraded
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtWorktreePoolExhausted:
		var e WorktreePoolExhausted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtOperatorOverrideApplied:
		var e OperatorOverrideApplied
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtResearchCompleted:
		var e ResearchCompleted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDepthWidthDecided:
		var e DepthWidthDecided
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtReplayCorruptionDetected:
		var e ReplayCorruptionDetected
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetSnapshotError:
		var e BudgetSnapshotError
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetDegradationFailed:
		var e BudgetDegradationFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtCostGatingAtomicityTimeout:
		var e CostGatingAtomicityTimeout
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetFullyRecovered:
		var e BudgetFullyRecovered
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtBudgetRecoveryHeld:
		var e BudgetRecoveryHeld
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtPhaseBoundaryRecorded:
		var e PhaseBoundaryRecorded
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtFMVDegradedToPlurality:
		var e FMVDegradedToPlurality
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtApplyFixStarted:
		var e ApplyFixStarted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtApplyFixSucceeded:
		var e ApplyFixSucceeded
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtApplyFixReverted:
		var e ApplyFixReverted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtADRRangeExhausted:
		var e ADRRangeExhausted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtHandoffPosted:
		var e HandoffPostedEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtMorningBriefReady:
		var e MorningBriefReadyEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtEODDigestReady:
		var e EODDigestReadyEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil

	case EvtDoctrineLoaded:
		var e DoctrineLoaded
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineReloaded:
		var e DoctrineReloaded
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineReloadFailed:
		var e DoctrineReloadFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineSchemaDeprecated:
		var e DoctrineSchemaDeprecated
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineSchemaMigrated:
		var e DoctrineSchemaMigrated
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineLoadFailed:
		var e DoctrineLoadFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineOverrideLoadFailed:
		var e DoctrineOverrideLoadFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineLoadRefused:
		var e DoctrineLoadRefused
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineSchemaMigrationFailed:
		var e DoctrineSchemaMigrationFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAutonomousReverted:
		var e DoctrineAutonomousReverted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineRevertSuppressedCooldown:
		var e DoctrineRevertSuppressedCooldown
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineTightenViolationRejected:
		var e DoctrineTightenViolationRejected
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAmendmentApplyFailed:
		var e DoctrineAmendmentApplyFailed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineWatcherStalled:
		var e DoctrineWatcherStalled
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineWatcherRestarted:
		var e DoctrineWatcherRestarted
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineWatcherOverflow:
		var e DoctrineWatcherOverflow
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	case EvtDoctrineAccessorAuditPassed:
		var e DoctrineAccessorAuditPassed
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("decode %s: %w", et, err)
		}
		return e, nil
	default:
		return nil, fmt.Errorf("decode: unknown event type %d", int(et))
	}
}
