// SPDX-License-Identifier: MIT
// Package hadesday — inbox_severity_dispatch.go
//
// translation (spec §6.4).
//
// DispatchInboxSeverity maps a release event-type string to the canonical
// via the caller-supplied InboxNotifier.
//
// Severity routing per spec §6.4:
//
// URGENT (SeverityUrgent):
// audit.tamper_detected, daemon.witness_key_compromised,
// audit.partition_seal_failed
// HIGH (SeverityActionNeeded):
// audit.litestream_lag with lag_seconds > 3600 (>1h),
// audit.cold_archive_failed
// MEDIUM (SeverityInfoImmediate):
// research.cache_revalidation_stuck, knowledge.embed_worker_degraded,
// audit.litestream_lag with lag_seconds ≤ 3600 (<1h)
// LOW / info-digest (SeverityInfoDigest):
// audit.recovery_completed, adr.*, state.*, vault.*, research.cache_*,
// daemon.witness_co_signed, daemon.witness_rotated,
// audit.partition_sealed, audit.checkpoint,...
// Default for unmapped events: SeverityInfoDigest.
//
// in 60s → "Simple Alert") is handled at the inbox layer
// (inbox.AggregatorCacheStore); it is NOT re-implemented here. This
// function is the translation + emission entry point only.
//
// Invariant invariant (hadesday never imports internal/store) is
// preserved — this file depends only on inbox (domain types) and
// orchestrator/eventlog (event-type string constants).
package hadesday

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const (
	EvtResearchCacheRevalidationStuck = "research.cache_revalidation_stuck"

	EvtKnowledgeEmbedWorkerDegraded = "knowledge.embed_worker_degraded"
)

type InboxEvent struct {
	EventType string

	Severity inbox.Severity

	Title string

	Body string

	At time.Time
}

// InboxNotifier is the emission interface consumed by DispatchInboxSeverity.
// The production implementation is the daemon's inbox dispatch adapter;
// tests substitute a fake.
//
// Notify MUST be idempotent on retries and MUST NOT modify ev. Error is
// propagated by DispatchInboxSeverity to the caller.
type InboxNotifier interface {
	Notify(ctx context.Context, ev InboxEvent) error
}

type InboxSeverityDispatchDeps struct {
	Notifier InboxNotifier
}

func DispatchInboxSeverity(
	ctx context.Context,
	deps InboxSeverityDispatchDeps,
	eventTypeStr string,
	payload []byte,
	at time.Time,
) (inbox.Severity, error) {
	if deps.Notifier == nil {
		return inbox.SeverityInfoDigest, errors.New("DispatchInboxSeverity: Notifier is nil")
	}
	if err := ctx.Err(); err != nil {
		return inbox.SeverityInfoDigest, fmt.Errorf("DispatchInboxSeverity: ctx: %w", err)
	}

	sev := classifyEventSeverity(eventTypeStr, payload)
	ev := InboxEvent{
		EventType: eventTypeStr,
		Severity:  sev,
		Title:     buildInboxEventTitle(eventTypeStr, payload),
		Body:      buildInboxEventBody(eventTypeStr, payload),
		At:        at,
	}

	if err := deps.Notifier.Notify(ctx, ev); err != nil {
		return sev, fmt.Errorf("DispatchInboxSeverity: notify %s: %w", eventTypeStr, err)
	}
	return sev, nil
}

func classifyEventSeverity(eventType string, payload []byte) inbox.Severity {
	switch eventType {

	// ── URGENT ────────────────────────────────────────────────────────────────
	// Security and chain-integrity failures that interrupt DND unconditionally
	// (invariant) and require immediate operator response.
	case eventlog.EvtAuditTamperDetected,
		eventlog.EvtDaemonWitnessKeyCompromised,
		eventlog.EvtAuditPartitionSealFailed:
		return inbox.SeverityUrgent

	case eventlog.EvtAuditLitestreamLag:

		if extractPayloadLagSeconds(payload) > 3600 {
			return inbox.SeverityActionNeeded
		}
		return inbox.SeverityInfoImmediate

	case eventlog.EvtAuditColdArchiveFailed:

		return inbox.SeverityActionNeeded

	case EvtResearchCacheRevalidationStuck,
		EvtKnowledgeEmbedWorkerDegraded:
		return inbox.SeverityInfoImmediate

	case eventlog.EvtAuditRecoveryCompleted,
		eventlog.EvtAuditRecoveryInitiated,
		eventlog.EvtAuditPartitionSealed,
		eventlog.EvtAuditPartitionSealRecovered,
		eventlog.EvtAuditTesseraBatchRecovered,
		eventlog.EvtAuditCheckpoint,
		eventlog.EvtAuditRefuseTriggerFired,
		eventlog.EvtAuditHotPathLatencyBreach,
		eventlog.EvtAuditLitestreamFailed:
		return inbox.SeverityInfoDigest

	case eventlog.EvtDaemonWitnessCoSigned,
		eventlog.EvtDaemonWitnessRotated:
		return inbox.SeverityInfoDigest

	case eventlog.EvtVaultNotePromotedToGlobal,
		eventlog.EvtVaultNoteUnpromotedFromGlobal:
		return inbox.SeverityInfoDigest

	case eventlog.EvtADRTransitionProposed,
		eventlog.EvtADRTransitionAccepted,
		eventlog.EvtADRTransitionRejected,
		eventlog.EvtADRTransitionSuperseded,
		eventlog.EvtADRTransitionDeprecated:
		return inbox.SeverityInfoDigest

	case eventlog.EvtResearchDispatchInitiated,
		eventlog.EvtResearchCacheHitExact,
		eventlog.EvtResearchCacheHitSemantic,
		eventlog.EvtResearchCacheRevalidatedFresh,
		eventlog.EvtResearchCacheRevalidatedStaleRefetched,
		eventlog.EvtResearchFindingsReturned:
		return inbox.SeverityInfoDigest

	case eventlog.EvtStateManualFieldChanged,
		eventlog.EvtStateRegeneratePartial,
		eventlog.EvtStateRegenerated:
		return inbox.SeverityInfoDigest

	default:

		return inbox.SeverityInfoDigest
	}
}

func extractPayloadLagSeconds(payload []byte) int {
	var p struct {
		LagSeconds int `json:"lag_seconds"`
	}
	_ = json.Unmarshal(payload, &p)
	return p.LagSeconds
}

func extractPayloadProjectID(payload []byte) string {
	var p struct {
		ProjectID string `json:"project_id"`
	}
	_ = json.Unmarshal(payload, &p)
	return p.ProjectID
}

func buildInboxEventTitle(eventType string, payload []byte) string {
	projectID := extractPayloadProjectID(payload)
	if projectID != "" {
		return fmt.Sprintf("[%s] %s", projectID, eventType)
	}
	return eventType
}

func buildInboxEventBody(eventType string, payload []byte) string {
	return fmt.Sprintf("Plan 9 event: %s\nPayload: %s\nInvestigate: hades audit history --filter %s",
		eventType,
		string(payload),
		eventType,
	)
}
