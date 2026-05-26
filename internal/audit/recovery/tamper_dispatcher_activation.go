// SPDX-License-Identifier: MIT
// Package recovery — tamper_dispatcher_activation.go
//
// Phase C declared TamperDispatcher (tamper_dispatcher.go) with doctrine-name
// routing ("max-scope" / "default" / "capa-firewall"). Phase J ACTIVATES the
// dispatcher by binding it to the Plan 9 [audit.tamper_response] mode read
// per-project from the active doctrine bundle at Dispatch() call time (hot —
// respects Plan 8 doctrine reload mid-flight per Plan 8 Q10 C atomic-swap).
//
// DoctrineDispatcher is a NEW struct (separate from Phase C's TamperDispatcher)
// that carries the three Plan 9 dependencies and its own halt state. Its
// Dispatch() method routes per [audit.tamper_response] mode:
//
//	halt-per-project:  HALT P only; emit audit.tamper_detected + Plan 7 inbox URGENT;
//	                   emit audit.recovery_initiated (operator-gated recovery next step).
//	log-continue:      NO halt; emit audit.tamper_detected + Plan 7 inbox URGENT;
//	                   CONTINUE new appends.
//	cascade-halt-all:  HALT P + HALT ALL other known projects; emit per-project
//	                   audit.tamper_detected (one per affected project); emit
//	                   once (for the originating project).
//
// inv-zen-150 enforced: empty ProjectID REJECTED at Dispatch() entry.
// Per-project blast radius preserved unless mode=cascade-halt-all (capa-firewall
// opt-in only).
//
// Recursive chain anchor: every event emitted flows into Plan 5 eventlog →
// forensically anchored, not just the original tamper detection. Dispatcher
// MUST emit events synchronously to ensure ordering:
// detection → tamper_detected → recovery_initiated (if applicable).
//
// cmd/zen-swarm-ctld/main.go wiring is deferred to operator/ops boot path
// post-Phase-L (TODO comment in main.go).
package recovery

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cbip-solutions/hades-system/internal/inbox"
)

type DoctrineActive interface {
	TamperMode(projectID string) string

	AllProjectIDs() []string
}

type EventAppender interface {
	Append(ctx context.Context, eventType, projectID string, payload map[string]any) error
}

type InboxNotifier interface {
	// Notify emits one inbox notification. Implementations MUST be idempotent
	// on retries and MUST NOT mutate the arguments.
	Notify(ctx context.Context, severity inbox.Severity, projectID, message string) error
}

type DoctrineDispatcher struct {
	active DoctrineActive
	evlog  EventAppender
	inbox  InboxNotifier
	mu     sync.RWMutex
	halted map[string]struct{}
}

func NewDoctrineDispatcher(active DoctrineActive, evlog EventAppender, ibx InboxNotifier) (*DoctrineDispatcher, error) {
	if active == nil {
		return nil, errors.New("recovery.NewDoctrineDispatcher: nil DoctrineActive (inv-zen-150: doctrine mode required for routing)")
	}
	if evlog == nil {
		return nil, errors.New("recovery.NewDoctrineDispatcher: nil EventAppender (chain-anchor events must not be dropped)")
	}
	if ibx == nil {
		return nil, errors.New("recovery.NewDoctrineDispatcher: nil InboxNotifier (operator notification must not be silenced)")
	}
	return &DoctrineDispatcher{
		active: active,
		evlog:  evlog,
		inbox:  ibx,
		halted: make(map[string]struct{}),
	}, nil
}

func (d *DoctrineDispatcher) Dispatch(ctx context.Context, ev TamperEvent) error {
	if ev.ProjectID == "" {
		return errors.New("recovery.DoctrineDispatcher.Dispatch: empty project_id (inv-zen-150)")
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}

	mode := d.active.TamperMode(ev.ProjectID)

	switch mode {
	case "halt-per-project":
		return d.dispatchHaltPerProject(ctx, ev)
	case "log-continue":
		return d.dispatchLogContinue(ctx, ev)
	case "cascade-halt-all":
		return d.dispatchCascadeHaltAll(ctx, ev)
	default:
		return fmt.Errorf("recovery.DoctrineDispatcher.Dispatch: unknown tamper_response mode %q for project %q (valid: halt-per-project|log-continue|cascade-halt-all)", mode, ev.ProjectID)
	}
}

func (d *DoctrineDispatcher) dispatchHaltPerProject(ctx context.Context, ev TamperEvent) error {
	d.markHalted(ev.ProjectID)

	payload := buildTamperPayload(ev)

	if err := d.evlog.Append(ctx, "audit.tamper_detected", ev.ProjectID, payload); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: emit tamper_detected: %w", err)
	}
	if err := d.inbox.Notify(ctx, inbox.SeverityUrgent, ev.ProjectID,
		fmt.Sprintf("audit chain tamper detected (halt-per-project): project=%s path=%s", ev.ProjectID, ev.DetectionPath)); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: inbox notify: %w", err)
	}
	if err := d.evlog.Append(ctx, "audit.recovery_initiated", ev.ProjectID, payload); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: emit recovery_initiated: %w", err)
	}
	return nil
}

func (d *DoctrineDispatcher) dispatchLogContinue(ctx context.Context, ev TamperEvent) error {
	payload := buildTamperPayload(ev)

	if err := d.evlog.Append(ctx, "audit.tamper_detected", ev.ProjectID, payload); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: emit tamper_detected: %w", err)
	}
	if err := d.inbox.Notify(ctx, inbox.SeverityUrgent, ev.ProjectID,
		fmt.Sprintf("audit chain tamper detected (log-continue): project=%s path=%s", ev.ProjectID, ev.DetectionPath)); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: inbox notify: %w", err)
	}

	return nil
}

func (d *DoctrineDispatcher) dispatchCascadeHaltAll(ctx context.Context, ev TamperEvent) error {
	allIDs := d.active.AllProjectIDs()

	projectSet := make(map[string]struct{}, len(allIDs)+1)
	for _, pid := range allIDs {
		projectSet[pid] = struct{}{}
	}
	projectSet[ev.ProjectID] = struct{}{}

	for pid := range projectSet {
		d.markHalted(pid)

		cascadeEv := ev
		cascadeEv.ProjectID = pid
		payload := buildTamperPayload(cascadeEv)

		if err := d.evlog.Append(ctx, "audit.tamper_detected", pid, payload); err != nil {
			return fmt.Errorf("recovery.DoctrineDispatcher: cascade emit tamper_detected (project=%s): %w", pid, err)
		}
		if err := d.inbox.Notify(ctx, inbox.SeverityUrgent, pid,
			fmt.Sprintf("audit chain tamper cascade halt (capa-firewall): trigger=%s path=%s", ev.ProjectID, ev.DetectionPath)); err != nil {
			return fmt.Errorf("recovery.DoctrineDispatcher: cascade inbox notify (project=%s): %w", pid, err)
		}
	}

	triggerPayload := buildTamperPayload(ev)
	if err := d.evlog.Append(ctx, "audit.recovery_initiated", ev.ProjectID, triggerPayload); err != nil {
		return fmt.Errorf("recovery.DoctrineDispatcher: emit recovery_initiated: %w", err)
	}
	return nil
}

func (d *DoctrineDispatcher) IsHalted(projectID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.halted[projectID]
	return ok
}

func (d *DoctrineDispatcher) markHalted(projectID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.halted[projectID] = struct{}{}
}

// buildTamperPayload constructs the structured event payload for tamper events.
// Returns a map[string]any suitable for EventAppender.Append. Fields are
// minimal and redaction-safe (no raw payload bytes from the tampered record).
//
// Precondition ev.Timestamp MUST be non-zero (Dispatch sets it before calling
// any sub-function). The severity defaults to "URGENT" when empty — tamper
// events are always URGENT for chain-integrity detections.
func buildTamperPayload(ev TamperEvent) map[string]any {
	sev := ev.Severity
	if sev == "" {
		sev = "URGENT"
	}
	return map[string]any{
		"project_id":           ev.ProjectID,
		"last_clean_record_id": ev.LastCleanRecordID,
		"detection_path":       ev.DetectionPath,
		"severity":             sev,
		"timestamp":            ev.Timestamp.UTC().Format(time.RFC3339Nano),
	}
}
