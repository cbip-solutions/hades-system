// SPDX-License-Identifier: MIT
// MessagesHandler is the HTTP handler bound to /v1/messages by the daemon
// wiring layer when an inbound request carries the X-HADES-Transport: hadessystem
// header (set by the Python HadesSystemTransport in plugin/hades-system/transports/).
//
// Behaviour
// 1. Decode JSON body into ForwardedRequest.
// 2. Translate to providers.TierRequest (preserving SessionID, Profile,
// Project, Model, IdempotencyKey, Body).
// 3. Call dispatcher.Forward — single-egress chokepoint per inv-hades-088.
// 4. On success: emit release Tessera audit anchor (best-effort; failure
// does not block forwarding); encode response as ForwardedResponse;
// write back to caller.
// 5. On dispatcher error: respond 502 with the wrapped error message and
// emit a MessageForwardFailed anchor (best-effort).
//
// What MessagesHandler does NOT do:
// - Inject credentials (defence in depth: tokens never cross the Python ↔
// Go boundary; release bypass module attaches them inside the dispatcher
// chain).
// - Log request bodies (operator privacy: bodies may carry user prompts).
// - Retry on dispatcher failure (dispatcher's own breaker handles failover;
// transport layer is a thin pass-through).
// - Implement augmentation (lives in /v1/augment — ships shell;
// ships pipeline).
//
// Concurrency safe for concurrent invocation. Holds an immutable Dispatcher
// + AuditAnchor; both are goroutine-safe per their respective contracts.

package transport

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cbip-solutions/hades-system/internal/providers"
)

const HeaderTransportSource = "X-HADES-Transport"

const HeaderIdempotencyKey = "X-HADES-Idempotency-Key"

const transportLabelHadesSystem = "hadessystem"

// maxRequestBodyBytes caps the inbound body at 32 MiB. Large prompts (vision
// payloads, multi-turn conversation history) MUST fit; truly oversized
// requests reject with 400 rather than DoS the daemon.
const maxRequestBodyBytes = 32 << 20

const (
	auditEventMessageForwarded = "MessageForwarded"
	auditEventMessageFailed    = "MessageForwardFailed"
)

// NewMessagesHandler constructs a MessagesHandler bound to the given
// dispatcher + anchor. dispatcher MUST be non-nil; anchor MAY be nil
// .
func NewMessagesHandler(dispatcher Dispatcher, anchor AuditAnchor) *MessagesHandler {
	if dispatcher == nil {
		panic("transport.NewMessagesHandler: dispatcher is required")
	}
	return &MessagesHandler{
		dispatcher: dispatcher,
		anchor:     anchor,
	}
}

func (h *MessagesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed; POST /v1/messages required", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}

	var fwd ForwardedRequest
	if err := json.Unmarshal(bodyBytes, &fwd); err != nil {
		http.Error(w, fmt.Sprintf("decode forwarded request: %v", err), http.StatusBadRequest)
		return
	}

	tierReq := translateRequest(r, fwd)
	transportHdr := r.Header.Get(HeaderTransportSource)

	resp, err := h.dispatcher.Forward(r.Context(), tierReq)
	if err != nil {

		h.emitAnchor(r, fwd, transportHdr, &dispatchFailure{err: err})
		http.Error(w, fmt.Sprintf("dispatch: %v", err), http.StatusBadGateway)
		return
	}
	if resp == nil {
		http.Error(w, "dispatcher returned nil response with nil error (contract violation)", http.StatusInternalServerError)
		return
	}

	auditID := h.emitAnchor(r, fwd, transportHdr, &dispatchSuccess{resp: resp})

	out := ForwardedResponse{
		Status:       resp.Status,
		Body:         string(resp.Body),
		Headers:      filteredResponseHeaders(resp.Headers),
		AuditEventID: auditID,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(w).Encode(out)
}

func translateRequest(r *http.Request, fwd ForwardedRequest) providers.TierRequest {
	headers := map[string]string{}
	for k, v := range fwd.Headers {
		if isSecretHeader(k) {
			continue
		}
		headers[k] = v
	}
	idempKey := fwd.IdempotencyKey
	if idempKey == "" {
		idempKey = r.Header.Get(HeaderIdempotencyKey)
	}
	return providers.TierRequest{
		Method:         http.MethodPost,
		Path:           "/v1/messages",
		Headers:        headers,
		Body:           bodyAsBytes(fwd.Body),
		ConversationID: fwd.ConversationID,
		SessionID:      fwd.SessionID,
		IdempotencyKey: idempKey,
		Profile:        fwd.Profile,
		Project:        fwd.Project,
		Model:          fwd.Model,
	}
}

func bodyAsBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}

	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return []byte(s)
		}
	}

	return []byte(raw)
}

type dispatchOutcome interface {
	auditPayload() map[string]any
	auditEventType() string
}

type dispatchSuccess struct {
	resp *providers.TierResponse
}

func (d *dispatchSuccess) auditEventType() string { return auditEventMessageForwarded }
func (d *dispatchSuccess) auditPayload() map[string]any {
	return map[string]any{
		"status":        d.resp.Status,
		"tier_used":     d.resp.TierUsed.String(),
		"model_used":    d.resp.ModelUsed,
		"input_tokens":  d.resp.InputTokens,
		"output_tokens": d.resp.OutputTokens,
		"latency_ms":    d.resp.LatencyMs,
	}
}

type dispatchFailure struct {
	err error
}

func (d *dispatchFailure) auditEventType() string { return auditEventMessageFailed }
func (d *dispatchFailure) auditPayload() map[string]any {
	return map[string]any{
		"error": d.err.Error(),
	}
}

func (h *MessagesHandler) emitAnchor(r *http.Request, fwd ForwardedRequest, transportHdr string, outcome dispatchOutcome) string {
	if transportHdr != transportLabelHadesSystem {
		return ""
	}
	if h.anchor == nil {
		return ""
	}
	payload := outcome.auditPayload()
	payload["session_id"] = fwd.SessionID
	payload["conversation_id"] = fwd.ConversationID
	payload["profile"] = fwd.Profile
	payload["project"] = fwd.Project
	payload["transport_source"] = transportSourceLabel
	id, err := h.anchor.Emit(r.Context(), outcome.auditEventType(), payload)
	if err != nil {

		return ""
	}
	return id
}

func isSecretHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Anthropic-Auth-Token", "X-Api-Key", "Cookie", "Set-Cookie":
		return true
	}
	return false
}

func filteredResponseHeaders(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if isSecretHeader(k) {
			continue
		}
		out[k] = v
	}
	return out
}
