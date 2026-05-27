// SPDX-License-Identifier: MIT
// Package cli — doctor_audit_emitter.go.
//
// # Wires a shared audit-emitter adapter that satisfies the
//
// aggregator.Emitter
// fix.Emitter
// cleanup.Emitter
// eval.Emitter
//
// shape (all four interfaces use the identical
// `Emit(ctx, eventType string, payload []byte) (auditHash string, err error)`
// signature) by routing emit calls through the daemon's
// POST /v1/audit/emit endpoint. Best-effort: daemon-down → silent warn
// (caller decides; this adapter never blocks the CLI surface).
//
// Why one file (not four per-surface adapters): the four surfaces
// (`zen doctor full`, `zen state cleanup`, `zen doctor restore`, and
// the daemon-side eval boundary via cmd/zen-swarm-ctld) all want the
// same daemon round-trip behaviour: marshal once, POST to /v1/audit/emit,
// return the resulting audit hash. The boundary is
// preserved: this is a CLI-layer adapter that consumes the daemon
// client — it does NOT touch internal/store.
package cli

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/cbip-solutions/hades-system/internal/client"
)

// DaemonAuditEmitter is the production audit emitter F-tail
// CLI surfaces. Wraps a daemon-bound *client.Client; each Emit call
// performs one POST /v1/audit/emit round-trip.
//
// Failure semantics (best-effort):
// - Daemon unreachable / 5xx → logs a warning via the slog default
// logger and returns ("", err). Callers (aggregator / fix / cleanup
// / eval) tolerate this per their respective Emitter contracts.
// - Daemon reachable + 2xx → returns the daemon-assigned event ID as
// the audit hash (the Tessera-anchored chain hash flows through
// /v1/audit/events later; the ID is sufficient for forensic trace).
//
// Construction prefer NewDaemonAuditEmitter(client) at the CLI entry
// point; the surface is intentionally interface-free so callers can
// substitute a stub Emitter in tests via the same `Emit` method.
type DaemonAuditEmitter struct {
	c      *client.Client
	logger *slog.Logger
}

func NewDaemonAuditEmitter(c *client.Client, logger *slog.Logger) *DaemonAuditEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &DaemonAuditEmitter{c: c, logger: logger}
}

func (e *DaemonAuditEmitter) Emit(ctx context.Context, eventType string, payload []byte) (string, error) {
	if e == nil || e.c == nil {
		return "", nil
	}

	var body any
	if len(payload) > 0 {
		var m map[string]any
		if err := json.Unmarshal(payload, &m); err == nil {
			body = m
		} else {
			body = map[string]any{"_raw": string(payload)}
		}
	}
	resp, err := e.c.AuditEmit(ctx, client.AuditEmitReq{
		Type:    eventType,
		Payload: body,
	})
	if err != nil {
		e.logger.Warn("audit emit failed (daemon unreachable?); continuing",
			"eventType", eventType, "err", err)
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.ID, nil
}
