// SPDX-License-Identifier: MIT
package autonomy

import (
	"context"
	"fmt"
	"time"
)

// AutonomyEventKind enumerates the audit events the autonomy package emits.
// surfaces the kind constants; the eventlog wire format
// (eventlog.EventType) is wired by at the CLI entry point via an
// adapter that translates AutonomyEvent → eventlog.Event.
//
// Two kinds are exposed:
//
// - EventBypassedSoftCheck — emitted when --allow-soft-warnings caused a
// soft-tier check failure to be bypassed instead of warning-only. One
// event per bypassed check.
// - EventAutonomyOverrideRejected — emitted when Resolve's capa-firewall
// hard guard suppressed a non-manual override.
// wires the emission at the CLI entry point when
// Resolution.RejectedOverride != nil.
type AutonomyEventKind uint8

const (
	EventBypassedSoftCheck AutonomyEventKind = iota + 1

	EventAutonomyOverrideRejected
)

func (k AutonomyEventKind) String() string {
	switch k {
	case EventBypassedSoftCheck:
		return "bypassed-soft-check"
	case EventAutonomyOverrideRejected:
		return "autonomy-override-rejected"
	default:
		return fmt.Sprintf("autonomy-event(%d)", uint8(k))
	}
}

type AutonomyEvent struct {
	Kind       AutonomyEventKind
	CheckName  string
	Doctrine   string
	Reason     string
	OccurredAt time.Time
	Override   *RejectedOverride
}

// EventEmitter is the collaborator wires to the eventlog package.
// tests inject a fake; production injects an adapter that translates
// AutonomyEvent → eventlog.Event{Type: EventTypeAutonomy*}.
//
// Emission is best-effort: a returned error is logged by the caller but
// MUST NOT affect engine semantics (proceed/block/warning aggregation is
// independent of the audit channel).
type EventEmitter interface {
	Emit(ctx context.Context, ev AutonomyEvent) error
}

func (e *CheckEngine) emitBypassedSoft(ctx context.Context, doctrine string, bypassed []CheckResult) {
	if e.emitter == nil || len(bypassed) == 0 {
		return
	}
	auditCtx := context.WithoutCancel(ctx)
	for _, r := range bypassed {
		_ = e.emitter.Emit(auditCtx, AutonomyEvent{
			Kind:       EventBypassedSoftCheck,
			CheckName:  r.Name,
			Doctrine:   doctrine,
			Reason:     r.Reason,
			OccurredAt: e.now(),
		})
	}
}
