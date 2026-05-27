// SPDX-License-Identifier: MIT
// bearer_audit_adapter.go — Task I-9 production
// auth.AuditEmitter adapter. The auth package's middleware emits
// authentication-failure events through the abstract auth.AuditEmitter
// interface (Emit(ctx, map[string]any) error); production wires a real
// emitter at boot so middleware failures surface to the operator.
//
// Why a slog-based emitter (not the bypass.AuditWriter):
// bypass.AuditWriter's Write(AuditRow) signature is shaped for LLM
// audit rows (TS / RequestHash / ResponseHash / TierUsed / etc.), not
// for the generic map[string]any events the auth middleware emits
// (event_type, schedule_id, remote_addr, presented_prefix). Bridging
// would require synthesising bypass.AuditRow values per auth event,
// which loses the structured event_type discrimination the auth audit
// trail depends on. A dedicated slog channel surfaces the events
// at WARN level immediately + leaves the bypass audit table free of
// non-LLM rows.
//
// keyed by (ts, event_type, schedule_id, remote_addr) — readable by
// the operator-facing /v1/auth/audit endpoint. Until that table lands,
// slog is the canonical surface (ops grep daemon.log for
// "auth: bearer mismatch" / "auth: schedule_not_found" lines).
//
// invariant boundary: this file imports log/slog + internal/daemon/auth
// (interface only). It NEVER imports internal/store directly.

package daemon

import (
	"context"
	"log/slog"

	"github.com/cbip-solutions/hades-system/internal/daemon/auth"
)

type SlogBearerAuditEmitter struct {
	logger *slog.Logger
}

func NewSlogBearerAuditEmitter(logger *slog.Logger) *SlogBearerAuditEmitter {
	if logger == nil {
		panic("daemon.NewSlogBearerAuditEmitter: logger is nil")
	}
	return &SlogBearerAuditEmitter{logger: logger}
}

func (e *SlogBearerAuditEmitter) Emit(_ context.Context, event map[string]any) error {
	attrs := make([]any, 0, 2*len(event))
	for k, v := range event {
		attrs = append(attrs, k, v)
	}
	e.logger.Warn("auth audit event", attrs...)
	return nil
}

var _ auth.AuditEmitter = (*SlogBearerAuditEmitter)(nil)
