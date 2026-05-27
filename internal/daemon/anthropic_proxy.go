// SPDX-License-Identifier: MIT
// Package daemon — anthropic_proxy.go is the orchestrator-facing ingress
// for POST /v1/messages.
//
// dispatches via the orchestrator → dispatcher → Tier 1 (bypass) /
// Tier 2+ (OpenClaude) chain. release functionality preserved end-to-end:
// Idempotency-Key auto-generation (invariant), X-HADES-Conversation-Id
// extraction, multi-value header preservation (Anthropic-Beta etc).
//
// invariant (single-egress-point): this file MUST NOT call
// bypass.Client.Forward directly. ALL LLM traffic flows through the
// orchestrator. The dispatcher then chooses tier 1 vs tier 2+ and emits
// CostEvents for the ledger.
//
// invariant (boundary): this file imports daemon/orchestrator (for
// the Call type) and providers (for TierResponse) but NOT internal/store
// — persistence concerns sit on the dispatcher side via dispatcheradapter.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cbip-solutions/hades-system/internal/daemon/dispatcher"
	"github.com/cbip-solutions/hades-system/internal/daemon/orchestrator"
	"github.com/cbip-solutions/hades-system/internal/providers"
	"github.com/google/uuid"
)

var multiValueHeaders = map[string]struct{}{
	"Anthropic-Beta":    {},
	"X-Forwarded-For":   {},
	"X-Forwarded-Host":  {},
	"X-Forwarded-Proto": {},
	"Via":               {},
	"Accept":            {},
	"Accept-Encoding":   {},
	"Accept-Language":   {},
	"Cache-Control":     {},
	"Warning":           {},
}

// OrchestratorForwarder is the minimal contract NewAnthropicProxy needs
// from the orchestrator. Defined as an interface so tests can substitute a
// fake without spinning up the real dispatcher / backends. Production
// *orchestrator.Orchestrator satisfies this trivially (the Forward
// signature is identical).
//
// Concurrency implementations MUST be safe for concurrent invocation
// (the proxy serves one HTTP request per goroutine). The production
// orchestrator + dispatcher document this guarantee.
type OrchestratorForwarder interface {
	Forward(ctx context.Context, call orchestrator.Call) (*providers.TierResponse, error)
}

var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Transfer-Encoding":   {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Upgrade":             {},
}

// NewAnthropicProxy builds an http.Handler that proxies POST /v1/messages
// through the orchestrator → dispatcher chain. Behaviour:
//
// - Method must be POST; everything else → 405 with Allow: POST.
// - Reads the request body fully (Anthropic /v1/messages is small JSON;
// the orchestrator forwards Body verbatim into the dispatcher chain).
// - Constructs an orchestrator.Call:
// Method=POST, Path=/v1/messages, Body=[]byte slurp,
// IdempotencyKey=Idempotency-Key header or uuid.NewString(),
// ConversationID=X-HADES-Conversation-Id (may be empty),
// Model=parsed JSON "model" field (may be empty if unparseable —
// non-fatal; backends fall back to req.Model when upstream omits it).
// - Headers are flattened to map[string]string. Single-value headers
// take v[0]; known multi-value headers (Anthropic-Beta, X-Forwarded-For,
// etc. — see multiValueHeaders) are comma-joined per RFC 7230 §3.2.2.
// Hop-by-hop and control-plane (Idempotency-Key, X-HADES-Conversation-Id)
// headers are dropped — they live in the typed Call fields above.
// - Forwards via the orchestrator which stamps X-HADES-* correlation
// headers, resolves the routing profile, and dispatches to Tier 1 /
// Tier 2+ via the dispatcher. Per ADR-0008: release dispatcher chooses
// tier-of-tier (in-house bypass vs OpenClaude substrate); provider-
// level routing within Tier 2+ is OpenClaude's responsibility.
// - Mirrors upstream status + headers verbatim, drops hop-by-hop on
// the response side, echoes the resolved Idempotency-Key, and stamps
// X-HADES-Tier-Used so clients (hades doctor, observability dashboards)
// can see which tier handled the call.
// - On orchestrator/dispatcher error: writes 502 Bad Gateway except
// for ErrAllTiersUnavailable which writes 503 (graceful-degradation
// contract: every tier is in breaker-open or unhealthy state). The
// error message is included verbatim because the dispatcher layer
// already wraps backend errors with truncated upstream bodies (cap
// 512 bytes per provider backend convention) and CostEvents redact
// credentials at emission. redact module (internal/redact)
// confirms zero-secret leakage on this path.
//
// invariant: this handler is the single egress point for /v1/messages.
// It MUST NOT call bypass.Client.Forward directly — all LLM traffic flows
// via the orchestrator → dispatcher chain.
func NewAnthropicProxy(forwarder OrchestratorForwarder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		idemKey := r.Header.Get("Idempotency-Key")
		if idemKey == "" {
			idemKey = uuid.NewString()
		}
		convID := r.Header.Get("X-HADES-Conversation-Id")

		profile := r.Header.Get("X-HADES-Profile")

		// Extract the requested model from the JSON body for cost-ledger
		// attribution. Best-effort: a body that fails to parse
		// (e.g. an Anthropic SDK that adds an unexpected wrapper) MUST
		// NOT abort the request. Backends fall back to req.Model when
		// the upstream response omits a model field; an empty Model here
		// just means the cost ledger has no requested-model field, which
		// treats as known-unknown rather than dropping the row.
		model := extractModel(bodyBytes)

		hdrs := make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			if _, hop := hopByHopHeaders[k]; hop {
				continue
			}
			if k == "Idempotency-Key" || k == "X-HADES-Conversation-Id" || k == "X-HADES-Profile" {
				continue
			}
			if len(v) == 0 {
				continue
			}

			if _, multi := multiValueHeaders[k]; multi && len(v) > 1 {
				hdrs[k] = strings.Join(v, ", ")
			} else {
				hdrs[k] = v[0]
			}
		}

		call := orchestrator.Call{

			Method:  http.MethodPost,
			Path:    "/v1/messages",
			Headers: hdrs,
			Body:    bodyBytes,

			Profile: profile,

			Model: model,

			IdempotencyKey: idemKey,
			ConversationID: convID,
		}

		resp, err := forwarder.Forward(r.Context(), call)
		if err != nil {

			if errors.Is(err, dispatcher.ErrAllTiersUnavailable) {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "orchestrator forward: "+err.Error(), http.StatusBadGateway)
			return
		}

		for k, v := range resp.Headers {
			if _, hop := hopByHopHeaders[k]; hop {
				continue
			}
			w.Header().Set(k, v)
		}
		w.Header().Set("Idempotency-Key", idemKey)

		if resp.TierUsed.String() != "" {
			w.Header().Set("X-HADES-Tier-Used", resp.TierUsed.String())
		}
		w.WriteHeader(resp.Status)

		if len(resp.Body) > 0 {
			_, _ = w.Write(resp.Body)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// extractModel parses the "model" field out of an Anthropic /v1/messages
// request body. Best-effort: returns "" on parse failure or absent field.
// The proxy MUST NOT abort the request just because the model field is
// missing — the upstream may still accept it (Anthropic supports a
// default-model deployment shape) and the cost ledger handles
// empty Model as known-unknown.
//
// Capped to a small JSON struct: we only care about the model field, so
// a 200KB request body still costs O(body-size) parse time but allocates
// minimally.
func extractModel(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var parsed struct {
		Model string `json:"model"`
	}

	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return parsed.Model
}
