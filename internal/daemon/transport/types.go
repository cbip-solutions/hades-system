// SPDX-License-Identifier: MIT
// Value types + interfaces shared across the package.
//
// Production wire-format contracts (Python ↔ Go):
//   - ForwardedRequest:  POST /v1/messages body shape
//   - ForwardedResponse: response envelope returned to Python
//
// Internal contracts:
//   - MessagesHandler:   concrete handler tied to the daemon dispatcher
//   - Dispatcher:        abstract dispatcher dependency (Plan 3 satisfies)
//   - AuditAnchor:       abstract Plan 9 Tessera audit hook
package transport

import (
	"context"
	"encoding/json"

	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/cbip-solutions/hades-system/internal/redact"
)

type ForwardedRequest struct {
	Body json.RawMessage `json:"body"`

	Headers map[string]string `json:"headers,omitempty"`

	SessionID string `json:"session_id,omitempty"`

	ConversationID string `json:"conversation_id,omitempty"`

	IdempotencyKey string `json:"idempotency_key,omitempty"`

	Profile string `json:"profile,omitempty"`

	Project string `json:"project,omitempty"`

	Model string `json:"model,omitempty"`

	TransportSource string `json:"transport_source,omitempty"`
}

type ForwardedResponse struct {
	Status int `json:"status"`

	Body string `json:"body"`

	Headers map[string]string `json:"headers,omitempty"`

	AuditEventID string `json:"audit_event_id,omitempty"`
}

type MessagesHandler struct {
	dispatcher Dispatcher

	anchor AuditAnchor
}

type Dispatcher interface {
	Forward(ctx context.Context, req providers.TierRequest) (*providers.TierResponse, error)
}

type AuditAnchor interface {
	Emit(ctx context.Context, eventType string, payload map[string]any) (eventID string, err error)
}

const transportSourceLabel = "zenswarm-transport"

var _ = redact.Marker
