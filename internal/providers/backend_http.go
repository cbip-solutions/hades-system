// SPDX-License-Identifier: MIT
// Package providers — shared HTTP helpers used by every HTTP-based tier
// backend in the cascade (BypassBackend, AnthropicPaygoBackend,
// GeminiBackend, OpenAICompatBackend, OllamaBackend).
//
// These helpers were extracted from the routing-layer backend that
// tier backend in the cascade. They are package-private because they
// are not part of the public TierBackend contract — they are
// implementation glue that every HTTP backend implementation needs
// and would otherwise duplicate.
//
// Invariant: backends MUST NOT propagate caller-supplied
// Content-Type or Authorization headers. Each backend manages those itself
// (Content-Type is always "application/json"; Authorization is the bearer
// token configured at construction). The two constants below are the canonical
// keys that the forward-loop drop check compares against.

package providers

import "net/http"

const (
	headerContentType   = "Content-Type"
	headerAuthorization = "Authorization"
)

func responseHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}
