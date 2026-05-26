// SPDX-License-Identifier: MIT
// handoff_emitter.go — Plan 7 Phase I Task I-9 production HandoffEmitter
// adapter (composition of *eventlog.Log on top of *orchestratoradapter.Adapter
// as the RawEmitter substrate).
//
// Why daemon-package (not a new internal/daemon/handoffemitter sub-package):
// the adapter is a thin (≤30 LOC) glue between three already-wired
// components — handlers.HandoffEmitter (handler-side interface),
// eventlog.Log (Plan 5 emission spine), and *orchestratoradapter.Adapter
// (already constructed in cmd/zen-swarm-ctld/main.go for Plan 5 wiring).
// Sub-package overhead would only add an import without any abstraction
// gain; the boundary that matters (handlers → handoff_emitter →
// eventlog.Log) is preserved at the *Server* method-call layer.
//
// inv-zen-031 boundary preserved: this file imports
// internal/orchestrator/eventlog (value types only — Event, EvtHandoffPosted
// constant, *Log via dependency injection) and internal/daemon/handlers
// (the satisfying interface). It NEVER imports internal/store.
//
// Synthesised SessionID rationale (handler invariant):
// eventlog.Log.Append rejects empty SessionID (it is the audit-trail
// grouping key per Plan 5 inv-zen-008). HandoffPostedEvent does not
// carry a session_id field by design — the plugin /handoff command runs
// outside any orchestrator session — so the daemon-side emitter must
// supply one. We use the constant "daemon-handoff" to identify the
// daemon-internal session that owns these emissions; downstream
// consumers (Plan 7 Phase F EOD digest) filter by EventType, not
// SessionID, so the synthetic value is operationally inert.
//
// Tests handoff_emitter_test.go (round-trip + nil-Log + invalid-event
// path).

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
	"github.com/cbip-solutions/hades-system/internal/orchestrator/eventlog"
)

const handoffSessionID = "daemon-handoff"

type HandoffEmitter struct {
	log *eventlog.Log
}

func NewHandoffEmitter(log *eventlog.Log) *HandoffEmitter {
	if log == nil {
		panic("daemon.NewHandoffEmitter: log is nil")
	}
	return &HandoffEmitter{log: log}
}

func (h *HandoffEmitter) Emit(ctx context.Context, ev handlers.HandoffPostedEvent) (string, error) {

	bs, err := json.Marshal(ev)
	if err != nil {
		// Sanitise do NOT include the raw payload (which may carry
		// summary/blocker bodies) in the error; only the event type tag.
		return "", fmt.Errorf("daemon.HandoffEmitter: marshal handoff event: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(bs, &payload); err != nil {

		return "", fmt.Errorf("daemon.HandoffEmitter: payload to map: %w", err)
	}
	id, err := h.log.Append(ctx, eventlog.Event{
		Type:      eventlog.EvtHandoffPosted,
		SessionID: handoffSessionID,
		ProjectID: ev.ProjectID,
		Timestamp: ev.Timestamp,
		Payload:   payload,
	})
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}
