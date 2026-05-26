// SPDX-License-Identifier: MIT
// cmd/zen-swarm-ctld/mcpgateway_audit_adapter.go
//
// the daemon's AuditEmit (handlers.AuditEventIn) write path. Events emit
// with type prefix "mcpgateway.<eventType>" so the audit_events_raw
// payload namespace is collision-free with other subsystems.
package main

import (
	"encoding/json"

	"github.com/cbip-solutions/hades-system/internal/daemon"
	"github.com/cbip-solutions/hades-system/internal/daemon/handlers"
)

type daemonAuditServer interface {
	AuditEmit(event handlers.AuditEventIn) error
}

var _ daemonAuditServer = (*daemon.Server)(nil)

type mcpgwAuditAdapter struct {
	srv daemonAuditServer
}

func (a mcpgwAuditAdapter) Emit(eventType string, payload []byte) {
	if a.srv == nil {
		return
	}
	_ = a.srv.AuditEmit(handlers.AuditEventIn{
		Type:    "mcpgateway." + eventType,
		Payload: json.RawMessage(payload),
	})
}
