// SPDX-License-Identifier: MIT
// Package handlers — audit_emit.go.
//
// POST /v1/audit/emit — write one audit event to audit_events_raw.
//
// Design notes:
// - Handler generates UUIDv4 id + sets emitted_at to time.Now().Unix() so
// callers never need to supply these fields (reduces surface for drift).
// - AuditEmitCtx.AuditEmit() is called synchronously in the handler. The
// daemon implementation may use a buffered-write goroutine internally
// (1s/100-events batch); the handler contract is fire-and-accept (202).
// - release wraps audit_events_raw with hash-chain WITHOUT migration;
// this file remains unchanged in release.
// - inv-hades-083: emit no-loss is enforced at the mcp/client layer;
// the daemon itself returns 202 on successful enqueue.
// - inv-hades-001: Unix socket only — enforced at server.go listener level.
// - inv-hades-031: never imports internal/store directly.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type AuditEventIn struct {
	ID string `json:"id,omitempty"`

	ProjectID string `json:"project_id"`

	Type string `json:"type"`

	Payload any `json:"payload"`

	EmittedAt int64 `json:"emitted_at,omitempty"`
}

type AuditEmitCtx interface {
	AuditEmit(event AuditEventIn) error
}

func AuditEmit(s AuditEmitCtx) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req AuditEventIn
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("invalid JSON: %s", err),
			})
			return
		}
		if req.Type == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type required"})
			return
		}
		req.ID = uuid.NewString()
		req.EmittedAt = time.Now().Unix()

		if err := s.AuditEmit(req); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id":         req.ID,
			"accepted":   true,
			"emitted_at": req.EmittedAt,
		})
	}
}
